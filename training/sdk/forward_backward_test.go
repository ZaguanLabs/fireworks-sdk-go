package sdk

import (
	"strings"
	"testing"
)

func TestCountResponseTokensPrefersNonZeroWeights(t *testing.T) {
	got := CountResponseTokens([]TrainingDatum{
		{
			LossFnInputs: map[string]TensorData{
				"weights":       {Data: []float64{0, 1, 1, 0}},
				"target_tokens": {Data: []int{10, 11, 12, 13}},
			},
		},
	})
	if got != 2 {
		t.Fatalf("response tokens = %v", got)
	}
}

func TestCountResponseTokensFallsBackToTargetTokenLength(t *testing.T) {
	got := CountResponseTokens([]TrainingDatum{
		{
			LossFnInputs: map[string]TensorData{
				"target_tokens": {Data: []int64{10, 11, 12}},
			},
		},
	})
	if got != 3 {
		t.Fatalf("response tokens = %v", got)
	}
}

func TestCountResponseTokensSumsMultipleRowsAndAnySlices(t *testing.T) {
	got := CountResponseTokens([]TrainingDatum{
		{
			LossFnInputs: map[string]TensorData{
				"weights": {Data: []any{0.0, 1.0, int64(2), "ignored"}},
			},
		},
		{
			LossFnInputs: map[string]TensorData{
				"target_tokens": {Data: []any{10, 11, 12, 13}},
			},
		},
		{},
	})
	if got != 6 {
		t.Fatalf("response tokens = %v", got)
	}
}

func TestAddCrossEntropyResponseTokensAddsMetric(t *testing.T) {
	output := AddCrossEntropyResponseTokens(
		ForwardBackwardOutput{
			LossFnOutputType: "cross_entropy",
			Metrics:          map[string]float64{"loss:sum": 3},
		},
		[]TrainingDatum{
			{
				LossFnInputs: map[string]TensorData{
					"weights": {Data: []float32{0, 1, 1, 0}},
				},
			},
		},
	)
	if output.Metrics[ResponseTokensMetric] != 2 {
		t.Fatalf("metrics = %#v", output.Metrics)
	}
}

func TestAddCrossEntropyResponseTokensPreservesExistingMetric(t *testing.T) {
	output := AddCrossEntropyResponseTokens(
		ForwardBackwardOutput{
			LossFnOutputType: "cross_entropy",
			Metrics:          map[string]float64{"loss:sum": 1, ResponseTokensMetric: 7},
		},
		[]TrainingDatum{
			{
				LossFnInputs: map[string]TensorData{
					"weights": {Data: []float64{1, 1}},
				},
			},
		},
	)
	if output.Metrics[ResponseTokensMetric] != 7 {
		t.Fatalf("metrics = %#v", output.Metrics)
	}
}

func TestAddCrossEntropyResponseTokensInitializesMetrics(t *testing.T) {
	output := AddCrossEntropyResponseTokens(
		ForwardBackwardOutput{},
		[]TrainingDatum{{LossFnInputs: map[string]TensorData{"target_tokens": {Data: []int{1, 2}}}}},
	)
	if output.Metrics[ResponseTokensMetric] != 2 {
		t.Fatalf("metrics = %#v", output.Metrics)
	}
}

func TestRejectProtoForwardBackwardTransport(t *testing.T) {
	if err := RejectProtoForwardBackwardTransport(false, false); err != nil {
		t.Fatalf("unexpected error = %v", err)
	}
	for _, flags := range []struct {
		write    bool
		compress bool
	}{
		{write: true},
		{compress: true},
		{write: true, compress: true},
	} {
		err := RejectProtoForwardBackwardTransport(flags.write, flags.compress)
		if err == nil || !strings.Contains(err.Error(), "proto forward_backward") {
			t.Fatalf("flags %#v error = %v", flags, err)
		}
	}
}

func TestDisableParallelForwardBackward(t *testing.T) {
	if DisableParallelForwardBackward(true) {
		t.Fatal("parallel forward/backward should be disabled")
	}
	if DisableParallelForwardBackward(false) {
		t.Fatal("false should remain false")
	}
}

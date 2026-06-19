package sdk

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeLossFnIncludesFireworksBuiltins(t *testing.T) {
	if !reflect.DeepEqual(FireworksBuiltinLossFns(), []LossFn{LossFnDAPO, LossFnGSPO}) {
		t.Fatalf("builtins = %#v", FireworksBuiltinLossFns())
	}
	tests := map[string]LossFn{
		"cross_entropy": LossFnCrossEntropy,
		" DAPO ":        LossFnDAPO,
		"gspo":          LossFnGSPO,
	}
	for input, want := range tests {
		got, err := NormalizeLossFn(input)
		if err != nil {
			t.Fatalf("NormalizeLossFn(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeLossFn(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsFireworksBuiltinLossFn(t *testing.T) {
	if !IsFireworksBuiltinLossFn("dapo") || !IsFireworksBuiltinLossFn("GSPO") {
		t.Fatal("expected dapo and gspo to be Fireworks built-in losses")
	}
	if IsFireworksBuiltinLossFn("cross_entropy") || IsFireworksBuiltinLossFn("unknown") {
		t.Fatal("unexpected Fireworks built-in loss")
	}
}

func TestNormalizeLossFnRejectsUnknown(t *testing.T) {
	_, err := NormalizeLossFn("orpo")
	if err == nil || !strings.Contains(err.Error(), "dapo") || !strings.Contains(err.Error(), "gspo") {
		t.Fatalf("err = %v", err)
	}
}

func TestModelInputFromInts(t *testing.T) {
	tokens := []int{1, 2, 3}
	modelInput := ModelInputFromInts(tokens)
	tokens[0] = 99
	chunks := modelInput["chunks"].([]map[string]any)
	if len(chunks) != 1 || chunks[0]["type"] != "encoded_text" {
		t.Fatalf("chunks = %#v", chunks)
	}
	if !reflect.DeepEqual(chunks[0]["tokens"], []int{1, 2, 3}) {
		t.Fatalf("tokens = %#v", chunks[0]["tokens"])
	}
	if _, ok := modelInput["routing_matrices"]; ok {
		t.Fatalf("model input unexpectedly includes routing matrices: %#v", modelInput)
	}
}

func TestModelInputFromIntsWithRoutingMatrices(t *testing.T) {
	routing := []string{"rm1"}
	modelInput := ModelInputFromInts([]int{1, 2, 3}, routing)
	routing[0] = "mutated"
	if !reflect.DeepEqual(modelInput["routing_matrices"], []string{"rm1"}) {
		t.Fatalf("routing matrices = %#v", modelInput["routing_matrices"])
	}
	if got := RoutingMatricesFromModelInput(modelInput); !reflect.DeepEqual(got, []string{"rm1"}) {
		t.Fatalf("routing matrices readback = %#v", got)
	}
}

func TestRoutingMatricesFromModelInputAnySlice(t *testing.T) {
	got := RoutingMatricesFromModelInput(map[string]any{
		"routing_matrices": []any{"rm1", "rm2", 3},
	})
	if !reflect.DeepEqual(got, []string{"rm1", "rm2"}) {
		t.Fatalf("routing matrices = %#v", got)
	}
	if got := RoutingMatricesFromModelInput(map[string]any{}); got != nil {
		t.Fatalf("missing routing matrices = %#v", got)
	}
}

func TestForwardBackwardWirePayloadPreservesRoutingMatrices(t *testing.T) {
	data := []TrainingDatum{{
		ModelInput: ModelInputFromInts([]int{1, 2, 3}, []string{"", "rm1", "rm2"}),
		LossFnInputs: map[string]TensorData{
			"target_tokens": {
				Data:  []int{2, 3, 4},
				DType: "int64",
				Shape: []int{3},
			},
		},
	}}
	forwardPayload := struct {
		ForwardInput struct {
			Data []TrainingDatum `json:"data"`
		} `json:"forward_input"`
		ModelID string `json:"model_id"`
		SeqID   int    `json:"seq_id"`
	}{ModelID: "model", SeqID: 1}
	forwardPayload.ForwardInput.Data = data
	assertNestedRoutingMatrices(t, forwardPayload, "forward_input")

	forwardBackwardPayload := struct {
		ForwardBackwardInput struct {
			Data []TrainingDatum `json:"data"`
		} `json:"forward_backward_input"`
		ModelID string `json:"model_id"`
		SeqID   int    `json:"seq_id"`
	}{ModelID: "model", SeqID: 2}
	forwardBackwardPayload.ForwardBackwardInput.Data = data
	assertNestedRoutingMatrices(t, forwardBackwardPayload, "forward_backward_input")
}

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

func assertNestedRoutingMatrices(t *testing.T, payload any, field string) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	input, ok := decoded[field].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v", field, decoded[field])
	}
	rows, ok := input["data"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("%s.data = %#v", field, input["data"])
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("%s.data[0] = %#v", field, rows[0])
	}
	modelInput, ok := row["model_input"].(map[string]any)
	if !ok {
		t.Fatalf("%s.data[0].model_input = %#v", field, row["model_input"])
	}
	if !reflect.DeepEqual(modelInput["routing_matrices"], []any{"", "rm1", "rm2"}) {
		t.Fatalf("routing_matrices = %#v", modelInput["routing_matrices"])
	}
}

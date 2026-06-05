package sdk

import (
	"reflect"
	"strings"
	"testing"
)

func TestTextTokenCountCountsEncodedTextChunks(t *testing.T) {
	datum := TrainingDatum{
		ModelInput: map[string]any{
			"chunks": []map[string]any{
				{"tokens": []int{1, 2}},
				{"type": "image", "tokens": []int{99, 100}},
				{"type": "encoded_text", "tokens": []any{3.0, 4.0, 5.0}},
			},
		},
	}
	if got := TextTokenCount(datum); got != 5 {
		t.Fatalf("token count = %d, want 5", got)
	}
}

func TestPoolEmbeddingTensorReturnsVectorUnchanged(t *testing.T) {
	embedding := TensorData{Data: []float64{1, 2}, DType: "float32", Shape: []int{2}}
	got, err := PoolEmbeddingTensor(embedding, TrainingDatum{}, EmbeddingPoolingLast)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, embedding) {
		t.Fatalf("embedding = %#v, want %#v", got, embedding)
	}
}

func TestPoolEmbeddingTensorLastUsesTextTokenCount(t *testing.T) {
	got, err := PoolEmbeddingTensor(
		TensorData{
			Data:  []float64{1, 2, 3, 4, 100, 200},
			DType: "float32",
			Shape: []int{3, 2},
		},
		TrainingDatum{
			ModelInput: map[string]any{
				"chunks": []any{
					map[string]any{"type": "encoded_text", "tokens": []int{1, 2}},
				},
			},
		},
		EmbeddingPoolingLast,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Data, []float64{3, 4}) {
		t.Fatalf("pooled data = %#v", got.Data)
	}
	if !reflect.DeepEqual(got.Shape, []int{2}) || got.DType != "float32" {
		t.Fatalf("pooled tensor = %#v", got)
	}
}

func TestPoolEmbeddingTensorMeanUsesTextTokenCount(t *testing.T) {
	got, err := PoolEmbeddingTensor(
		TensorData{
			Data:  []float32{1, 2, 3, 4, 100, 200},
			DType: "float32",
			Shape: []int{3, 2},
		},
		TrainingDatum{
			ModelInput: map[string]any{
				"chunks": []map[string]any{
					{"tokens": []int{1, 2}},
				},
			},
		},
		EmbeddingPoolingMean,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Data, []float64{2, 3}) {
		t.Fatalf("pooled data = %#v", got.Data)
	}
}

func TestPoolEmbeddingTensorPreservesFeatureShape(t *testing.T) {
	got, err := PoolEmbeddingTensor(
		TensorData{
			Data:  []any{1.0, 2.0, 3.0, 4.0, 100.0, 200.0},
			DType: "float32",
			Shape: []int{3, 1, 2},
		},
		TrainingDatum{
			ModelInput: map[string]any{
				"chunks": []map[string]any{
					{"tokens": []int{1, 2}},
				},
			},
		},
		EmbeddingPoolingLast,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Data, []float64{3, 4}) {
		t.Fatalf("pooled data = %#v", got.Data)
	}
	if !reflect.DeepEqual(got.Shape, []int{1, 2}) {
		t.Fatalf("shape = %#v", got.Shape)
	}
}

func TestPoolEmbeddingTensorRejectsEmptyTextSequence(t *testing.T) {
	_, err := PoolEmbeddingTensor(
		TensorData{Data: []float64{1, 2, 3, 4}, Shape: []int{2, 2}},
		TrainingDatum{},
		EmbeddingPoolingLast,
	)
	if err == nil || !strings.Contains(err.Error(), "empty text sequence") {
		t.Fatalf("error = %v", err)
	}
}

func TestPoolEmbeddingTensorRejectsUnsupportedPooling(t *testing.T) {
	_, err := PoolEmbeddingTensor(
		TensorData{Data: []float64{1, 2, 3, 4}, Shape: []int{2, 2}},
		TrainingDatum{ModelInput: map[string]any{"chunks": []map[string]any{{"tokens": []int{1}}}}},
		EmbeddingPooling("max"),
	)
	if err == nil || !strings.Contains(err.Error(), "Unsupported") && !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error = %v", err)
	}
}

func TestPoolEmbeddingTensorRejectsShapeMismatch(t *testing.T) {
	_, err := PoolEmbeddingTensor(
		TensorData{Data: []float64{1, 2, 3}, Shape: []int{2, 2}},
		TrainingDatum{ModelInput: map[string]any{"chunks": []map[string]any{{"tokens": []int{1}}}}},
		EmbeddingPoolingLast,
	)
	if err == nil || !strings.Contains(err.Error(), "does not match shape") {
		t.Fatalf("error = %v", err)
	}
}

func TestEmbeddingGradDatum(t *testing.T) {
	source := TrainingDatum{
		ModelInput: map[string]any{"chunks": []map[string]any{{"tokens": []int{1, 2}}}},
		LossFnInputs: map[string]TensorData{
			"target_tokens": {Data: []int{3, 4}},
		},
	}
	got := EmbeddingGradDatum(source, []float64{5, -2}, []int{2})
	if !reflect.DeepEqual(got.ModelInput, source.ModelInput) {
		t.Fatalf("model input = %#v", got.ModelInput)
	}
	if len(got.LossFnInputs) != 1 {
		t.Fatalf("loss inputs = %#v", got.LossFnInputs)
	}
	grads := got.LossFnInputs["embedding_grads"]
	if grads.DType != "float32" || !reflect.DeepEqual(grads.Shape, []int{2}) || !reflect.DeepEqual(grads.Data, []float64{5, -2}) {
		t.Fatalf("grads = %#v", grads)
	}
}

package sdk

import "fmt"

type EmbeddingPooling string

const (
	EmbeddingPoolingMean EmbeddingPooling = "mean"
	EmbeddingPoolingLast EmbeddingPooling = "last"
)

func TextTokenCount(datum TrainingDatum) int {
	modelInput, _ := datum.ModelInput["chunks"]
	total := 0
	for _, chunk := range modelInputChunks(modelInput) {
		chunkType, _ := chunk["type"].(string)
		if chunkType == "" {
			chunkType = "encoded_text"
		}
		if chunkType != "encoded_text" {
			continue
		}
		total += tensorLen(chunk["tokens"])
	}
	return total
}

func PoolEmbeddingTensor(embedding TensorData, datum TrainingDatum, pooling EmbeddingPooling) (TensorData, error) {
	if len(embedding.Shape) <= 1 {
		return embedding, nil
	}
	switch pooling {
	case EmbeddingPoolingMean, EmbeddingPoolingLast:
	default:
		return TensorData{}, fmt.Errorf("unsupported pooling=%q; expected 'mean' or 'last'", pooling)
	}

	tokenCount := TextTokenCount(datum)
	if tokenCount <= 0 {
		return TensorData{}, fmt.Errorf("cannot pool embedding from an empty text sequence")
	}

	sequenceLength := embedding.Shape[0]
	if sequenceLength <= 0 {
		return TensorData{}, fmt.Errorf("embedding sequence dimension must be positive")
	}
	rowWidth := 1
	for _, dim := range embedding.Shape[1:] {
		if dim <= 0 {
			return TensorData{}, fmt.Errorf("embedding feature dimensions must be positive")
		}
		rowWidth *= dim
	}
	values, err := tensorFloat64Slice(embedding.Data)
	if err != nil {
		return TensorData{}, err
	}
	if len(values) != sequenceLength*rowWidth {
		return TensorData{}, fmt.Errorf("embedding data length %d does not match shape %v", len(values), embedding.Shape)
	}
	if tokenCount > sequenceLength {
		tokenCount = sequenceLength
	}

	pooled := make([]float64, rowWidth)
	if pooling == EmbeddingPoolingLast {
		start := (tokenCount - 1) * rowWidth
		copy(pooled, values[start:start+rowWidth])
	} else {
		for row := 0; row < tokenCount; row++ {
			start := row * rowWidth
			for col := 0; col < rowWidth; col++ {
				pooled[col] += values[start+col]
			}
		}
		for col := range pooled {
			pooled[col] /= float64(tokenCount)
		}
	}

	return TensorData{
		Data:  pooled,
		DType: embedding.DType,
		Shape: append([]int(nil), embedding.Shape[1:]...),
	}, nil
}

func EmbeddingGradDatum(datum TrainingDatum, grad []float64, shape []int) TrainingDatum {
	return TrainingDatum{
		ModelInput: datum.ModelInput,
		LossFnInputs: map[string]TensorData{
			"embedding_grads": {
				Data:  append([]float64(nil), grad...),
				DType: "float32",
				Shape: append([]int(nil), shape...),
			},
		},
	}
}

func modelInputChunks(value any) []map[string]any {
	switch chunks := value.(type) {
	case []map[string]any:
		return chunks
	case []any:
		out := make([]map[string]any, 0, len(chunks))
		for _, item := range chunks {
			if chunk, ok := item.(map[string]any); ok {
				out = append(out, chunk)
			}
		}
		return out
	default:
		return nil
	}
}

func tensorFloat64Slice(data any) ([]float64, error) {
	switch v := data.(type) {
	case []float64:
		return append([]float64(nil), v...), nil
	case []float32:
		out := make([]float64, len(v))
		for i, item := range v {
			out[i] = float64(item)
		}
		return out, nil
	case []int:
		out := make([]float64, len(v))
		for i, item := range v {
			out[i] = float64(item)
		}
		return out, nil
	case []any:
		out := make([]float64, len(v))
		for i, item := range v {
			value, err := asFloat(item, fmt.Sprintf("embedding.data[%d]", i))
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	default:
		return nil, fmt.Errorf("embedding data must be a numeric slice")
	}
}

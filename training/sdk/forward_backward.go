package sdk

import "fmt"

const ResponseTokensMetric = "response_tokens"

type TensorData struct {
	Data  any    `json:"data"`
	DType string `json:"dtype,omitempty"`
	Shape []int  `json:"shape,omitempty"`
}

type TrainingDatum struct {
	LossFnInputs map[string]TensorData `json:"loss_fn_inputs,omitempty"`
	ModelInput   map[string]any        `json:"model_input,omitempty"`
}

type ForwardBackwardOutput struct {
	LossFnOutputType string             `json:"loss_fn_output_type,omitempty"`
	LossFnOutputs    []map[string]any   `json:"loss_fn_outputs,omitempty"`
	Metrics          map[string]float64 `json:"metrics,omitempty"`
}

func CountResponseTokens(data []TrainingDatum) float64 {
	var total float64
	for _, datum := range data {
		if len(datum.LossFnInputs) == 0 {
			continue
		}
		if weights, ok := datum.LossFnInputs["weights"]; ok {
			total += float64(tensorNonZeroCount(weights.Data))
			continue
		}
		if targetTokens, ok := datum.LossFnInputs["target_tokens"]; ok {
			total += float64(tensorLen(targetTokens.Data))
		}
	}
	return total
}

func AddCrossEntropyResponseTokens(output ForwardBackwardOutput, data []TrainingDatum) ForwardBackwardOutput {
	if output.Metrics == nil {
		output.Metrics = map[string]float64{}
	}
	if _, ok := output.Metrics[ResponseTokensMetric]; ok {
		return output
	}
	output.Metrics[ResponseTokensMetric] = CountResponseTokens(data)
	return output
}

func RejectProtoForwardBackwardTransport(protoWrite, protoCompress bool) error {
	if protoWrite || protoCompress {
		return fmt.Errorf("FiretitanTrainingClient does not support Tinker's proto forward_backward transport. Use the JSON forward_backward path")
	}
	return nil
}

func DisableParallelForwardBackward(parallel bool) bool {
	if parallel {
		return false
	}
	return parallel
}

func tensorLen(data any) int {
	switch v := data.(type) {
	case []any:
		return len(v)
	case []int:
		return len(v)
	case []int8:
		return len(v)
	case []int16:
		return len(v)
	case []int32:
		return len(v)
	case []int64:
		return len(v)
	case []uint:
		return len(v)
	case []uint8:
		return len(v)
	case []uint16:
		return len(v)
	case []uint32:
		return len(v)
	case []uint64:
		return len(v)
	case []float32:
		return len(v)
	case []float64:
		return len(v)
	case []string:
		return len(v)
	default:
		return 0
	}
}

func tensorNonZeroCount(data any) int {
	switch v := data.(type) {
	case []any:
		total := 0
		for _, item := range v {
			if numericNonZero(item) {
				total++
			}
		}
		return total
	case []int:
		return nonZeroInts(v)
	case []int8:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	case []int16:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	case []int32:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	case []int64:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	case []uint:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	case []uint8:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	case []uint16:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	case []uint32:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	case []uint64:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	case []float32:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	case []float64:
		total := 0
		for _, item := range v {
			if item != 0 {
				total++
			}
		}
		return total
	default:
		return 0
	}
}

func numericNonZero(value any) bool {
	switch v := value.(type) {
	case int:
		return v != 0
	case int8:
		return v != 0
	case int16:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case uint:
		return v != 0
	case uint8:
		return v != 0
	case uint16:
		return v != 0
	case uint32:
		return v != 0
	case uint64:
		return v != 0
	case float32:
		return v != 0
	case float64:
		return v != 0
	default:
		return false
	}
}

func nonZeroInts(values []int) int {
	total := 0
	for _, value := range values {
		if value != 0 {
			total++
		}
	}
	return total
}

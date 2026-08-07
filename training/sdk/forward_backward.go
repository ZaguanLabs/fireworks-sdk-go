package sdk

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const ResponseTokensMetric = "response_tokens"

const (
	ParallelChunkSendConcurrencyEnv     = "FIREWORKS_TRAINING_PARALLEL_CHUNK_SEND_CONCURRENCY"
	DefaultParallelChunkSendConcurrency = 128
)

type LossFn string

const (
	LossFnCrossEntropy LossFn = "cross_entropy"
	LossFnDAPO         LossFn = "dapo"
	LossFnGSPO         LossFn = "gspo"
)

func FireworksBuiltinLossFns() []LossFn {
	return []LossFn{LossFnDAPO, LossFnGSPO}
}

func NormalizeLossFn(lossFn string) (LossFn, error) {
	normalized := LossFn(strings.ToLower(strings.TrimSpace(lossFn)))
	switch normalized {
	case LossFnCrossEntropy, LossFnDAPO, LossFnGSPO:
		return normalized, nil
	default:
		return "", fmt.Errorf("unknown loss_fn %q; expected one of: cross_entropy, dapo, gspo", lossFn)
	}
}

func IsFireworksBuiltinLossFn(lossFn string) bool {
	normalized, err := NormalizeLossFn(lossFn)
	if err != nil {
		return false
	}
	return normalized == LossFnDAPO || normalized == LossFnGSPO
}

type TensorData struct {
	Data  any    `json:"data"`
	DType string `json:"dtype,omitempty"`
	Shape []int  `json:"shape,omitempty"`
}

type TrainingDatum struct {
	LossFnInputs map[string]TensorData `json:"loss_fn_inputs,omitempty"`
	ModelInput   map[string]any        `json:"model_input,omitempty"`
}

func ModelInputFromInts(tokens []int, routingMatrices ...[]string) map[string]any {
	chunkTokens := append([]int(nil), tokens...)
	modelInput := map[string]any{
		"chunks": []map[string]any{
			{
				"type":   "encoded_text",
				"tokens": chunkTokens,
			},
		},
	}
	if len(routingMatrices) > 0 && routingMatrices[0] != nil {
		modelInput["routing_matrices"] = append([]string(nil), routingMatrices[0]...)
	}
	return modelInput
}

func RoutingMatricesFromModelInput(modelInput map[string]any) []string {
	value, ok := modelInput["routing_matrices"]
	if !ok || value == nil {
		return nil
	}
	switch matrices := value.(type) {
	case []string:
		return append([]string(nil), matrices...)
	case []any:
		out := make([]string, 0, len(matrices))
		for _, item := range matrices {
			if matrix, ok := item.(string); ok {
				out = append(out, matrix)
			}
		}
		return out
	default:
		return nil
	}
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
	return parallel
}

func R3RequestIssues(data []TrainingDatum) []string {
	var issues []string
	for datumIndex, datum := range data {
		matrices := RoutingMatricesFromModelInput(datum.ModelInput)
		if _, present := datum.ModelInput["routing_matrices"]; !present {
			continue
		}
		positionCount, ok := modelInputPositionCount(datum.ModelInput)
		if !ok {
			continue
		}
		if len(matrices) == 0 {
			issues = append(issues, fmt.Sprintf("datum[%d] routing_matrices is empty; expected %d", datumIndex, positionCount))
		} else if len(matrices) != positionCount {
			issues = append(issues, fmt.Sprintf("datum[%d] routing_matrix_count=%d; expected %d", datumIndex, len(matrices), positionCount))
		}
	}
	return issues
}

func modelInputPositionCount(modelInput map[string]any) (int, bool) {
	chunks, ok := modelInput["chunks"].([]map[string]any)
	if !ok {
		if raw, rawOK := modelInput["chunks"].([]any); rawOK {
			chunks = make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				chunk, chunkOK := item.(map[string]any)
				if !chunkOK {
					return 0, false
				}
				chunks = append(chunks, chunk)
			}
		} else {
			return 0, false
		}
	}
	total := 0
	for _, chunk := range chunks {
		if tokens, ok := chunk["tokens"]; ok {
			total += tensorLen(tokens)
			continue
		}
		expected, ok := chunk["expected_tokens"]
		if !ok {
			return 0, false
		}
		count, ok := expected.(int)
		if !ok || count < 0 {
			return 0, false
		}
		total += count
	}
	return total, true
}

func RoutingMatricesWireBytes(modelInput map[string]any) int {
	matrices, present := modelInput["routing_matrices"]
	if !present || matrices == nil {
		return 0
	}
	encoded, err := json.Marshal(matrices)
	if err != nil {
		return 0
	}
	return len(`,"routing_matrices":`) + len(encoded)
}

func ParallelChunkSendConcurrency() (int, error) {
	raw := strings.TrimSpace(os.Getenv(ParallelChunkSendConcurrencyEnv))
	if raw == "" {
		return DefaultParallelChunkSendConcurrency, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%s must be a positive integer; got %q", ParallelChunkSendConcurrencyEnv, raw)
	}
	return value, nil
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

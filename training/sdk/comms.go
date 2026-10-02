package sdk

import (
	"context"
	"encoding/json"
	"fmt"
)

type Comms string

const (
	CommsV1 Comms = "v1"
	CommsV2 Comms = "v2"
)

// CreateModelResponse retains per-model optional trainer capabilities.
type CreateModelResponse struct {
	SupportsRouterReplay   *bool               `json:"supports_router_replay,omitempty"`
	SupportsRDMAWeightSync bool                `json:"supports_rdma_weight_sync,omitempty"`
	ModelID                string              `json:"model_id"`
	Comms                  Comms               `json:"comms,omitempty"`
	RoutingMatrixFormat    RoutingMatrixFormat `json:"routing_matrix_format,omitempty"`
	R3StoreID              string              `json:"r3_store_id,omitempty"`
}

// TrainingModelCreator lets a transport return the complete model-creation result.
// Capabilities belong to the returned model, never to a shared global transport.
type TrainingModelCreator interface {
	CreateModel(context.Context, FiretitanProvisioningConfig, map[string]string) (CreateModelResponse, error)
}

func (c *FiretitanTrainingClient) ApplyModelCreationResponse(response CreateModelResponse) {
	c.ModelID = response.ModelID
	c.SupportsRouterReplay = cloneBoolPointer(response.SupportsRouterReplay)
	c.trainerSupportsRDMA = response.SupportsRDMAWeightSync
	c.Communication = CommsV1
	if response.Comms == CommsV2 {
		c.Communication = CommsV2
	}
	c.RoutingMatrixFormat = RoutingBase64Inline
	c.R3StoreID = ""
	if response.RoutingMatrixFormat == RoutingParquetV1 {
		c.RoutingMatrixFormat = RoutingParquetV1
		c.R3StoreID = response.R3StoreID
	}
}
func (c *FiretitanTrainingClient) Comms() Comms {
	if c != nil && c.Communication == CommsV2 {
		return CommsV2
	}
	return CommsV1
}

// RequestBody supplies the wire form for transport implementations. v1 omits comms.
func (o ForwardBackwardOptions) RequestBody() map[string]any {
	input := map[string]any{"data": o.Data, "loss_fn": o.LossFn}
	if o.LossFnConfig != nil {
		input["loss_fn_config"] = o.LossFnConfig
	}
	body := map[string]any{"model_id": o.ModelID, "seq_id": o.SeqID, "forward_backward_input": input}
	if o.Comms == CommsV2 {
		body["comms"] = "v2"
	}
	return body
}
func (c *FiretitanTrainingClient) ValidateRoutingData(data []TrainingDatum) error {
	return validateRoutingData(data, c.RoutingMatrixFormat, c.R3StoreID)
}

// LinearLossWeights preserves the original tensor dimensions while flattening
// the negative custom-loss gradient into the float32 wire tensor.
func LinearLossWeights(gradient []float32, shape []int) (TensorData, error) {
	size := 1
	for _, n := range shape {
		if n < 0 || (n != 0 && size > int(^uint(0)>>1)/n) {
			return TensorData{}, fmt.Errorf("invalid gradient shape")
		}
		size *= n
	}
	if size != len(gradient) {
		return TensorData{}, fmt.Errorf("gradient shape does not match data")
	}
	data := make([]float32, len(gradient))
	for i, v := range gradient {
		data[i] = -v
	}
	return TensorData{Data: data, DType: "float32", Shape: append([]int(nil), shape...)}, nil
}

// UnmarshalJSON treats malformed capability flags as unknown/unsupported,
// matching trainers that predate these optional fields.
func (r *CreateModelResponse) UnmarshalJSON(data []byte) error {
	type plain CreateModelResponse
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	replay := raw["supports_router_replay"]
	rdma := raw["supports_rdma_weight_sync"]
	delete(raw, "supports_router_replay")
	delete(raw, "supports_rdma_weight_sync")
	rest, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var out plain
	if err := json.Unmarshal(rest, &out); err != nil {
		return err
	}
	var value bool
	if string(replay) == "true" || string(replay) == "false" {
		_ = json.Unmarshal(replay, &value)
		out.SupportsRouterReplay = &value
	}
	out.SupportsRDMAWeightSync = string(rdma) == "true"
	*r = CreateModelResponse(out)
	return nil
}

// CreateModelRequestBody exposes the FireTitan create-model extensions for adapters.
func (c FiretitanProvisioningConfig) CreateModelRequestBody(sessionID string, modelSeqID int, metadata map[string]string) map[string]any {
	body := map[string]any{"session_id": sessionID, "model_seq_id": modelSeqID, "base_model": c.BaseModel, "user_metadata": metadata}
	lora := map[string]any{"rank": c.LoraRank, "train_mlp": boolOrDefault(c.TrainMLP, true), "train_attn": boolOrDefault(c.TrainAttn, true), "train_unembed": boolOrDefault(c.TrainUnembed, true)}
	if c.LoraRank > 0 {
		if c.LoraAlpha != nil {
			lora["alpha"] = *c.LoraAlpha
		} else {
			lora["alpha"] = DefaultLoraAlpha
		}
		if c.LoraInitMethod != "" {
			lora["init_method"] = c.LoraInitMethod
		}
	}
	body["lora_config"] = lora
	if c.Seed != nil {
		lora["seed"] = *c.Seed
	}
	if c.ProjectionHeadDim != nil && *c.ProjectionHeadDim > 0 {
		body["projection_head_dim"] = *c.ProjectionHeadDim
	}
	return body
}

// ForwardRequestBody is the forward-only endpoint's wire envelope.
func (o ForwardBackwardOptions) ForwardRequestBody() map[string]any {
	body := o.RequestBody()
	body["forward_input"] = body["forward_backward_input"]
	delete(body, "forward_backward_input")
	return body
}

package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const DefaultLoraInitMethod = "kaiming"

type ModelDetailsUnavailableError struct {
	StatusCode int
	Message    string
}

func (e *ModelDetailsUnavailableError) Error() string { return e.Message }

func TrainerExtraArgs(config FiretitanProvisioningConfig) []string {
	args := append([]string(nil), config.ExtraArgs...)
	if config.ProjectionHeadDim != nil && *config.ProjectionHeadDim > 0 {
		args = append(args, fmt.Sprintf("--projection-head-dim=%d", *config.ProjectionHeadDim))
	}
	return args
}
func RDMACandidate(config FiretitanProvisioningConfig) bool {
	if config.CreateDeployment != nil && !*config.CreateDeployment {
		return false
	}
	if config.LoraRank != 0 || config.MaxLoraRank != nil || config.ForwardOnly || PolicyOutputCMEKResource(config.ExtraArgs) != "" {
		return false
	}
	for _, values := range []map[string]string{config.ExtraValues, config.DeploymentExtraValues} {
		if v, ok := values["rdmaWeightSyncEnabled"]; ok && v != "true" {
			return false
		}
	}
	return true
}
func deploymentBaseModel(info DeploymentInfo, fallback string) string {
	if info.BaseModel != "" {
		return info.BaseModel
	}
	return fallback
}

type WeightSyncResponse struct {
	Version          string             `json:"version"`
	OptimizerVersion *int               `json:"optimizer_version,omitempty"`
	Metrics          map[string]float64 `json:"metrics,omitempty"`
}

// TrainingWeightSyncBackend performs submission and waits for trainer publication
// completion. It must distinguish a rejected route from an accepted operation.
type TrainingWeightSyncBackend interface {
	WeightSync(context.Context, map[string]any) (WeightSyncResponse, error)
}

// WeightSyncRouteUnavailableError is only for submission-time HTTP 404/405.
// Completion failures must not use this type: retrying via FILE could duplicate publication.
type WeightSyncRouteUnavailableError struct{ StatusCode int }

func (e *WeightSyncRouteUnavailableError) Error() string {
	return fmt.Sprintf("weight_sync route unavailable (HTTP %d)", e.StatusCode)
}

func (c *FiretitanTrainingClient) SupportsRDMAWeightSync() bool {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	return c.rdmaAvailable
}
func (c *FiretitanTrainingClient) refreshRDMA(ctx context.Context) (bool, error) {
	c.rdmaAvailable = false
	b := c.SamplerBackend
	if b == nil || b.DeployMgr == nil || !c.trainerSupportsRDMA || c.Config.LoraRank != 0 || c.RunName != "" || b.CMEKResource != "" || !RDMACandidate(c.Config) || (b.AllowRDMA != nil && !*b.AllowRDMA) {
		return false, nil
	}
	state, err := b.DeployMgr.HotloadCheckStatus(ctx, b.DeploymentID, b.BaseModel)
	if err != nil {
		return false, err
	}
	if _, fanout := state["replica_distribution"]; fanout {
		return false, nil
	}
	replicas, ok := state["replicas"].([]any)
	if !ok || len(replicas) == 0 {
		return false, nil
	}
	for _, replica := range replicas {
		m, ok := replica.(map[string]any)
		if !ok || m["supports_rdma_weight_sync"] != true {
			return false, nil
		}
	}
	if c.rdmaSessionID == "" {
		c.rdmaSessionID = newSamplingRequestID()
		c.rdmaSourceEpoch = newSamplingRequestID()
	}
	c.rdmaAvailable = true
	return true, nil
}

// WeightSync negotiates before each publication. Only a rejected 404/405 route
// falls back to FILE; an accepted RDMA publication's errors are returned directly.
func (c *FiretitanTrainingClient) WeightSync(ctx context.Context) (WeightSyncResponse, error) {
	if c == nil || c.SamplerBackend == nil {
		return WeightSyncResponse{}, fmt.Errorf("weight_sync requires a sampler backend")
	}
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	if c.ModelID == "" {
		return WeightSyncResponse{}, fmt.Errorf("model_id is required")
	}
	rdma, err := c.refreshRDMA(ctx)
	if err != nil {
		return WeightSyncResponse{}, err
	}
	if rdma {
		transport, ok := c.ComputeBackend.(TrainingWeightSyncBackend)
		if !ok {
			return WeightSyncResponse{}, fmt.Errorf("compute backend does not implement RDMA weight_sync")
		}
		b := c.SamplerBackend
		deployment, err := b.DeploymentModel(ctx)
		if err != nil {
			return WeightSyncResponse{}, err
		}
		headers, err := b.DeployMgr.HotloadHeaders(ctx, b.DeploymentID, b.BaseModel, "")
		if err != nil {
			return WeightSyncResponse{}, err
		}
		flat := map[string]string{}
		for key := range headers {
			flat[key] = headers.Get(key)
		}
		c.RequestSeqID++
		response, err := transport.WeightSync(ctx, map[string]any{"protocol_version": 1, "transport": "RDMA", "session_id": c.rdmaSessionID, "source_epoch": c.rdmaSourceEpoch, "deployment": deployment, "hotload_url": strings.TrimRight(b.DeployMgr.HotloadAPIURL, "/") + "/hot_load/v1/models/hot_load", "hotload_headers": flat, "operation_id": newSamplingRequestID(), "model_id": c.ModelID, "seq_id": c.RequestSeqID})
		if err == nil {
			return response, nil
		}
		var unavailable *WeightSyncRouteUnavailableError
		if !errors.As(err, &unavailable) || (unavailable.StatusCode != 404 && unavailable.StatusCode != 405) {
			return WeightSyncResponse{}, err
		}
		c.trainerSupportsRDMA = false
		c.rdmaAvailable = false
	}
	saved, err := c.saveWeightsForSamplerExt(ctx, "weight-sync-"+newSamplingRequestID(), SaveWeightsForSamplerOptions{})
	if err != nil {
		return WeightSyncResponse{}, err
	}
	ok, err := c.SamplerBackend.HotloadSavedSnapshot(ctx, saved.Path)
	if err != nil {
		return WeightSyncResponse{}, err
	}
	if !ok {
		return WeightSyncResponse{}, fmt.Errorf("hotload failed for sampler snapshot %q", saved.Path)
	}
	return WeightSyncResponse{Version: saved.Path}, nil
}
func (c *FiretitanTrainingClient) WeightSyncFuture(ctx context.Context) *Future[WeightSyncResponse] {
	return SubmitFuture(func() (WeightSyncResponse, error) { return c.WeightSync(ctx) })
}

// TrainingForwardBackend adds forward-only requests to a compute adapter.
type TrainingForwardBackend interface {
	Forward(context.Context, ForwardBackwardOptions) (ForwardBackwardOutput, error)
}

// CustomTensorLoss returns gradients with respect to the selected output tensors.
// Go callers supply differentiation; no Python/PyTorch runtime is required.
type CustomTensorLoss func([]TrainingDatum, []TensorData) ([]TensorData, map[string]float64, error)

func (c *FiretitanTrainingClient) ForwardProjection(ctx context.Context, data []TrainingDatum) (ForwardBackwardOutput, error) {
	return c.forwardTensor(ctx, data, "projection", EmbeddingPoolingMean)
}
func (c *FiretitanTrainingClient) forwardTensor(ctx context.Context, data []TrainingDatum, output string, pooling EmbeddingPooling) (ForwardBackwardOutput, error) {
	if c == nil {
		return ForwardBackwardOutput{}, fmt.Errorf("training client is nil")
	}
	backend, ok := c.ComputeBackend.(TrainingForwardBackend)
	if !ok {
		return ForwardBackwardOutput{}, fmt.Errorf("compute backend does not implement forward")
	}
	if c.ModelID == "" {
		return ForwardBackwardOutput{}, fmt.Errorf("model_id is required")
	}
	if err := c.ValidateRoutingData(data); err != nil {
		return ForwardBackwardOutput{}, err
	}
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	c.RequestSeqID++
	return backend.Forward(ctx, ForwardBackwardOptions{Comms: c.Comms(), ModelID: c.ModelID, SeqID: c.RequestSeqID, Data: data, LossFn: LossFnCrossEntropy, LossFnConfig: map[string]any{"output": output, "pooling": string(pooling)}})
}
func (c *FiretitanTrainingClient) ForwardProjectionFuture(ctx context.Context, data []TrainingDatum) *Future[ForwardBackwardOutput] {
	return SubmitFuture(func() (ForwardBackwardOutput, error) { return c.ForwardProjection(ctx, data) })
}
func (c *FiretitanTrainingClient) ForwardBackwardCustom(ctx context.Context, data []TrainingDatum, loss CustomTensorLoss, output string, pooling EmbeddingPooling) (ForwardBackwardOutput, error) {
	if loss == nil {
		return ForwardBackwardOutput{}, fmt.Errorf("custom loss is required")
	}
	if output != "embedding" && output != "projection" && output != "cos_similarity_matrix" {
		return ForwardBackwardOutput{}, fmt.Errorf("unsupported tensor output %q", output)
	}
	if pooling == "" {
		pooling = EmbeddingPoolingMean
	}
	if pooling != EmbeddingPoolingMean && pooling != EmbeddingPoolingLast {
		return ForwardBackwardOutput{}, fmt.Errorf("invalid pooling")
	}
	forward, err := c.forwardTensor(ctx, data, output, pooling)
	if err != nil {
		return ForwardBackwardOutput{}, err
	}
	if len(forward.LossFnOutputs) != len(data) {
		return ForwardBackwardOutput{}, fmt.Errorf("forward outputs do not match data")
	}
	field := "embedding"
	if output == "projection" {
		field = "projection"
	}
	tensors := make([]TensorData, len(data))
	for i, row := range forward.LossFnOutputs {
		value, ok := row[field]
		if !ok {
			return ForwardBackwardOutput{}, fmt.Errorf("missing %s tensor", field)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return ForwardBackwardOutput{}, err
		}
		if err = json.Unmarshal(raw, &tensors[i]); err != nil {
			return ForwardBackwardOutput{}, err
		}
		if output == "embedding" {
			tensors[i], err = PoolEmbeddingTensor(tensors[i], data[i], pooling)
			if err != nil {
				return ForwardBackwardOutput{}, err
			}
		}
	}
	grads, metrics, err := loss(data, tensors)
	if err != nil {
		return ForwardBackwardOutput{}, err
	}
	if len(grads) != len(data) {
		return ForwardBackwardOutput{}, fmt.Errorf("gradient count does not match data")
	}
	backward := make([]TrainingDatum, len(data))
	for i, grad := range grads {
		values, err := tensorFloat64Slice(grad.Data)
		if err != nil {
			return ForwardBackwardOutput{}, err
		}
		flat := make([]float32, len(values))
		for j, v := range values {
			flat[j] = float32(v)
		}
		if _, err := LinearLossWeights(flat, grad.Shape); err != nil {
			return ForwardBackwardOutput{}, err
		}
		backward[i] = TrainingDatum{ModelInput: data[i].ModelInput, LossFnInputs: map[string]TensorData{field + "_grads": {Data: flat, DType: "float32", Shape: append([]int(nil), grad.Shape...)}}}
	}
	result, err := c.ForwardBackward(ctx, backward, string(LossFnCrossEntropy), map[string]any{"output": output, "pooling": string(pooling)})
	if err == nil {
		if result.Metrics == nil {
			result.Metrics = map[string]float64{}
		}
		for k, v := range metrics {
			result.Metrics[k] = v
		}
	}
	return result, err
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value != nil {
		return *value
	}
	return fallback
}

func (c *FiretitanTrainingClient) ForwardBackwardCustomFuture(ctx context.Context, data []TrainingDatum, loss CustomTensorLoss, output string, pooling EmbeddingPooling) *Future[ForwardBackwardOutput] {
	return SubmitFuture(func() (ForwardBackwardOutput, error) {
		return c.ForwardBackwardCustom(ctx, data, loss, output, pooling)
	})
}

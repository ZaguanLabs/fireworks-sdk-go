// Code generated from Fireworks Python SDK 1.2.30 training types. DO NOT EDIT.
package types

import (
	"encoding/json"
	"time"
)

type CheckpointType = string
type EventType = string
type FutureRetrieveResponse = any
type LossFnInputs = map[string]TensorData
type LossFnOutput = map[string]TensorData
type LossFnType = string
type ModelID = string
type ModelInputChunk = any
type RequestID = string
type Severity = string
type StopReason = string
type TelemetryEvent = any
type TensorDtype = string
type AdamParams struct {
	EmitGradNormMetrics any      `json:"emit_grad_norm_metrics,omitempty"`
	LearningRate        *float64 `json:"learning_rate,omitempty"`
	Beta1               *float64 `json:"beta1,omitempty"`
	Beta2               *float64 `json:"beta2,omitempty"`
	Eps                 *float64 `json:"eps,omitempty"`
	WeightDecay         *float64 `json:"weight_decay,omitempty"`
	GradClipNorm        *float64 `json:"grad_clip_norm,omitempty"`
}

func (v AdamParams) MarshalJSON() ([]byte, error) {
	type wire AdamParams
	if v.LearningRate == nil {
		value := float64(0.0001)
		v.LearningRate = &value
	}
	if v.Beta1 == nil {
		value := float64(0.9)
		v.Beta1 = &value
	}
	if v.Beta2 == nil {
		value := float64(0.95)
		v.Beta2 = &value
	}
	if v.Eps == nil {
		value := float64(1e-12)
		v.Eps = &value
	}
	if v.WeightDecay == nil {
		value := float64(0.0)
		v.WeightDecay = &value
	}
	if v.GradClipNorm == nil {
		value := float64(0.0)
		v.GradClipNorm = &value
	}
	return json.Marshal(wire(v))
}

type AuthTokenResponse struct {
	Jwt string `json:"jwt"`
}
type Checkpoint struct {
	CheckpointID   string         `json:"checkpoint_id"`
	CheckpointType CheckpointType `json:"checkpoint_type"`
	Time           time.Time      `json:"time"`
	TinkerPath     string         `json:"tinker_path"`
	SizeBytes      *int           `json:"size_bytes,omitempty"`
	Public         *bool          `json:"public,omitempty"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
}

func (v Checkpoint) MarshalJSON() ([]byte, error) {
	type wire Checkpoint
	if v.Public == nil {
		value := false
		v.Public = &value
	}
	return json.Marshal(wire(v))
}

type ClientConfigRequest struct {
	SdkVersion string `json:"sdk_version"`
}
type ClientConfigResponse struct {
	PjwtAuthEnabled                     *bool   `json:"pjwt_auth_enabled,omitempty"`
	CredentialDefaultSource             *string `json:"credential_default_source,omitempty"`
	SampleDispatchBytesSemaphoreSize    int     `json:"sample_dispatch_bytes_semaphore_size,omitempty"`
	InflightResponseBytesSemaphoreSize  int     `json:"inflight_response_bytes_semaphore_size,omitempty"`
	ParallelFwdbwdChunks                *bool   `json:"parallel_fwdbwd_chunks,omitempty"`
	ProtoWriteFwdbwd                    *bool   `json:"proto_write_fwdbwd,omitempty"`
	ProtoCompressFwdbwd                 *bool   `json:"proto_compress_fwdbwd,omitempty"`
	FwdViaFwdbwd                        *bool   `json:"fwd_via_fwdbwd,omitempty"`
	BillingExceptionMaxPauseDurationSec int     `json:"billing_exception_max_pause_duration_sec,omitempty"`
	SampleNoRetries                     *bool   `json:"sample_no_retries,omitempty"`
	SampleEnableStuckDetection          *bool   `json:"sample_enable_stuck_detection,omitempty"`
	SampleMaxConcurrentRequests         *int    `json:"sample_max_concurrent_requests,omitempty"`
	UsePyqwestTransport                 *bool   `json:"use_pyqwest_transport,omitempty"`
}

func (v ClientConfigResponse) MarshalJSON() ([]byte, error) {
	type wire ClientConfigResponse
	if v.PjwtAuthEnabled == nil {
		value := false
		v.PjwtAuthEnabled = &value
	}
	if v.CredentialDefaultSource == nil {
		value := "api_key"
		v.CredentialDefaultSource = &value
	}
	if v.ParallelFwdbwdChunks == nil {
		value := true
		v.ParallelFwdbwdChunks = &value
	}
	if v.ProtoWriteFwdbwd == nil {
		value := false
		v.ProtoWriteFwdbwd = &value
	}
	if v.ProtoCompressFwdbwd == nil {
		value := false
		v.ProtoCompressFwdbwd = &value
	}
	if v.FwdViaFwdbwd == nil {
		value := false
		v.FwdViaFwdbwd = &value
	}
	if v.SampleNoRetries == nil {
		value := false
		v.SampleNoRetries = &value
	}
	if v.SampleEnableStuckDetection == nil {
		value := true
		v.SampleEnableStuckDetection = &value
	}
	if v.SampleMaxConcurrentRequests == nil {
		value := 2000
		v.SampleMaxConcurrentRequests = &value
	}
	if v.UsePyqwestTransport == nil {
		value := true
		v.UsePyqwestTransport = &value
	}
	return json.Marshal(wire(v))
}

type CreateModelRequest struct {
	ProjectionHeadDim *int           `json:"projection_head_dim,omitempty"`
	SessionID         string         `json:"session_id"`
	ModelSeqID        int            `json:"model_seq_id"`
	BaseModel         string         `json:"base_model"`
	UserMetadata      map[string]any `json:"user_metadata,omitempty"`
	LoraConfig        *LoraConfig    `json:"lora_config,omitempty"`
	Type              *string        `json:"type,omitempty"`
}

func (v CreateModelRequest) MarshalJSON() ([]byte, error) {
	type wire CreateModelRequest
	if v.Type == nil {
		value := "create_model"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type CreateModelResponse struct {
	ModelID ModelID `json:"model_id"`
	Type    *string `json:"type,omitempty"`
}

func (v CreateModelResponse) MarshalJSON() ([]byte, error) {
	type wire CreateModelResponse
	if v.Type == nil {
		value := "create_model"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type CreateSamplingSessionRequest struct {
	SessionID            string  `json:"session_id"`
	SamplingSessionSeqID int     `json:"sampling_session_seq_id"`
	BaseModel            *string `json:"base_model,omitempty"`
	ModelPath            *string `json:"model_path,omitempty"`
	Type                 *string `json:"type,omitempty"`
}

func (v CreateSamplingSessionRequest) MarshalJSON() ([]byte, error) {
	type wire CreateSamplingSessionRequest
	if v.Type == nil {
		value := "create_sampling_session"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type CreateSamplingSessionResponse struct {
	Type              *string `json:"type,omitempty"`
	SamplingSessionID string  `json:"sampling_session_id"`
}

func (v CreateSamplingSessionResponse) MarshalJSON() ([]byte, error) {
	type wire CreateSamplingSessionResponse
	if v.Type == nil {
		value := "create_sampling_session"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type CreateSessionRequest struct {
	Tags         []string       `json:"tags"`
	UserMetadata map[string]any `json:"user_metadata"`
	SdkVersion   string         `json:"sdk_version"`
	ProjectID    *string        `json:"project_id,omitempty"`
	Type         *string        `json:"type,omitempty"`
}

func (v CreateSessionRequest) MarshalJSON() ([]byte, error) {
	type wire CreateSessionRequest
	if v.Type == nil {
		value := "create_session"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type CreateSessionResponse struct {
	Type           *string `json:"type,omitempty"`
	InfoMessage    *string `json:"info_message,omitempty"`
	WarningMessage *string `json:"warning_message,omitempty"`
	ErrorMessage   *string `json:"error_message,omitempty"`
	SessionID      string  `json:"session_id"`
}

func (v CreateSessionResponse) MarshalJSON() ([]byte, error) {
	type wire CreateSessionResponse
	if v.Type == nil {
		value := "create_session"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type Cursor struct {
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
	TotalCount int `json:"total_count"`
}
type Datum struct {
	LossFnInputs LossFnInputs `json:"loss_fn_inputs"`
	ModelInput   ModelInput   `json:"model_input"`
}
type DmelChunk struct {
	Dmel any     `json:"dmel"`
	Type *string `json:"type,omitempty"`
}

func (v DmelChunk) MarshalJSON() ([]byte, error) {
	type wire DmelChunk
	if v.Type == nil {
		value := "dmel"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type EncodedTextChunk struct {
	Tokens []int   `json:"tokens"`
	Type   *string `json:"type,omitempty"`
}

func (v EncodedTextChunk) MarshalJSON() ([]byte, error) {
	type wire EncodedTextChunk
	if v.Type == nil {
		value := "encoded_text"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type ForwardBackwardInput struct {
	Data         []Datum        `json:"data"`
	LossFn       LossFnType     `json:"loss_fn"`
	LossFnConfig map[string]any `json:"loss_fn_config,omitempty"`
}
type ForwardBackwardOutput struct {
	LossFnOutputType string             `json:"loss_fn_output_type"`
	LossFnOutputs    []LossFnOutput     `json:"loss_fn_outputs"`
	Metrics          map[string]float64 `json:"metrics"`
}
type ForwardBackwardRequest struct {
	ForwardBackwardInput ForwardBackwardInput `json:"forward_backward_input"`
	ModelID              ModelID              `json:"model_id"`
	SeqID                *int                 `json:"seq_id,omitempty"`
}
type ForwardRequest struct {
	ForwardInput ForwardBackwardInput `json:"forward_input"`
	ModelID      ModelID              `json:"model_id"`
	SeqID        *int                 `json:"seq_id,omitempty"`
}
type FutureRetrieveRequest struct {
	RequestID         RequestID `json:"request_id"`
	AllowMetadataOnly *bool     `json:"allow_metadata_only,omitempty"`
}

func (v FutureRetrieveRequest) MarshalJSON() ([]byte, error) {
	type wire FutureRetrieveRequest
	if v.AllowMetadataOnly == nil {
		value := false
		v.AllowMetadataOnly = &value
	}
	return json.Marshal(wire(v))
}

type GenericEvent struct {
	Event             EventType      `json:"event"`
	EventID           string         `json:"event_id"`
	EventName         string         `json:"event_name"`
	EventSessionIndex int            `json:"event_session_index"`
	Severity          Severity       `json:"severity"`
	Timestamp         time.Time      `json:"timestamp"`
	EventData         map[string]any `json:"event_data,omitempty"`
}
type GetInfoRequest struct {
	ModelID ModelID `json:"model_id"`
	Type    *string `json:"type,omitempty"`
}

func (v GetInfoRequest) MarshalJSON() ([]byte, error) {
	type wire GetInfoRequest
	if v.Type == nil {
		value := "get_info"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type GetInfoResponse struct {
	Type      *string   `json:"type,omitempty"`
	ModelData ModelData `json:"model_data"`
	ModelID   ModelID   `json:"model_id"`
	IsLora    *bool     `json:"is_lora,omitempty"`
	LoraRank  *int      `json:"lora_rank,omitempty"`
	ModelName *string   `json:"model_name,omitempty"`
}
type GetServerCapabilitiesResponse struct {
	SupportedModels []SupportedModel `json:"supported_models"`
}
type HealthResponse struct {
	Status string `json:"status"`
}
type ImageAssetPointerChunk struct {
	Format         string  `json:"format"`
	Location       string  `json:"location"`
	ExpectedTokens *int    `json:"expected_tokens,omitempty"`
	Type           *string `json:"type,omitempty"`
}

func (v ImageAssetPointerChunk) MarshalJSON() ([]byte, error) {
	type wire ImageAssetPointerChunk
	if v.Type == nil {
		value := "image_asset_pointer"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type ImageChunk struct {
	Data           any     `json:"data"`
	Format         string  `json:"format"`
	ExpectedTokens *int    `json:"expected_tokens,omitempty"`
	Type           *string `json:"type,omitempty"`
}

func (v ImageChunk) MarshalJSON() ([]byte, error) {
	type wire ImageChunk
	if v.Type == nil {
		value := "image"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type LoadWeightsRequest struct {
	ModelID            ModelID `json:"model_id"`
	Path               string  `json:"path"`
	Optimizer          bool    `json:"optimizer"`
	SeqID              *int    `json:"seq_id,omitempty"`
	WeightsAccessToken *string `json:"weights_access_token,omitempty"`
	Type               *string `json:"type,omitempty"`
}

func (v LoadWeightsRequest) MarshalJSON() ([]byte, error) {
	type wire LoadWeightsRequest
	if v.Type == nil {
		value := "load_weights"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type LoadWeightsResponse struct {
	Path *string `json:"path,omitempty"`
	Type *string `json:"type,omitempty"`
}
type LoraConfig struct {
	Alpha        *int    `json:"alpha,omitempty"`
	InitMethod   *string `json:"init_method,omitempty"`
	Rank         int     `json:"rank"`
	Seed         *int    `json:"seed,omitempty"`
	TrainUnembed *bool   `json:"train_unembed,omitempty"`
	TrainMlp     *bool   `json:"train_mlp,omitempty"`
	TrainAttn    *bool   `json:"train_attn,omitempty"`
}

func (v LoraConfig) MarshalJSON() ([]byte, error) {
	type wire LoraConfig
	if v.TrainUnembed == nil {
		value := true
		v.TrainUnembed = &value
	}
	if v.TrainMlp == nil {
		value := true
		v.TrainMlp = &value
	}
	if v.TrainAttn == nil {
		value := true
		v.TrainAttn = &value
	}
	return json.Marshal(wire(v))
}

type ModelData struct {
	Arch        *string `json:"arch,omitempty"`
	ModelName   *string `json:"model_name,omitempty"`
	TokenizerID *string `json:"tokenizer_id,omitempty"`
}
type ModelInput struct {
	Chunks                 []ModelInputChunk `json:"chunks"`
	RoutingMatrices        []string          `json:"routing_matrices,omitempty"`
	RoutingReferences      map[string]any    `json:"routing_references,omitempty"`
	RoutingMatrixFormat    any               `json:"routing_matrix_format,omitempty"`
	TopSamplingReferences  map[string]any    `json:"top_sampling_references,omitempty"`
	SamplingMaskReferences map[string]any    `json:"sampling_mask_references,omitempty"`
}
type OptimStepRequest struct {
	AdamParams AdamParams `json:"adam_params"`
	ModelID    ModelID    `json:"model_id"`
	SeqID      *int       `json:"seq_id,omitempty"`
	Type       *string    `json:"type,omitempty"`
}

func (v OptimStepRequest) MarshalJSON() ([]byte, error) {
	type wire OptimStepRequest
	if v.Type == nil {
		value := "optim_step"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type OptimStepResponse struct {
	Metrics map[string]float64 `json:"metrics,omitempty"`
}
type ParsedCheckpointTinkerPath struct {
	TinkerPath     string         `json:"tinker_path"`
	TrainingRunID  string         `json:"training_run_id"`
	CheckpointType CheckpointType `json:"checkpoint_type"`
	CheckpointID   string         `json:"checkpoint_id"`
}
type RequestErrorCategory struct {
}
type RequestFailedResponse struct {
	Error    string               `json:"error"`
	Category RequestErrorCategory `json:"category"`
}
type SampleRequest struct {
	NumSamples         *int           `json:"num_samples,omitempty"`
	Prompt             ModelInput     `json:"prompt"`
	SamplingParams     SamplingParams `json:"sampling_params"`
	BaseModel          *string        `json:"base_model,omitempty"`
	ModelPath          *string        `json:"model_path,omitempty"`
	SamplingSessionID  *string        `json:"sampling_session_id,omitempty"`
	SeqID              *int           `json:"seq_id,omitempty"`
	PromptLogprobs     *bool          `json:"prompt_logprobs,omitempty"`
	TopkPromptLogprobs *int           `json:"topk_prompt_logprobs,omitempty"`
	Type               *string        `json:"type,omitempty"`
}

func (v SampleRequest) MarshalJSON() ([]byte, error) {
	type wire SampleRequest
	if v.NumSamples == nil {
		value := 1
		v.NumSamples = &value
	}
	if v.TopkPromptLogprobs == nil {
		value := 0
		v.TopkPromptLogprobs = &value
	}
	if v.Type == nil {
		value := "sample"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type SampleResponse struct {
	Sequences            []SampledSequence `json:"sequences"`
	Type                 *string           `json:"type,omitempty"`
	PromptLogprobs       []*float64        `json:"prompt_logprobs,omitempty"`
	TopkPromptLogprobs   [][]any           `json:"topk_prompt_logprobs,omitempty"`
	PromptCacheHitTokens *int              `json:"prompt_cache_hit_tokens,omitempty"`
}

func (v SampleResponse) MarshalJSON() ([]byte, error) {
	type wire SampleResponse
	if v.Type == nil {
		value := "sample"
		v.Type = &value
	}
	if v.PromptCacheHitTokens == nil {
		value := 0
		v.PromptCacheHitTokens = &value
	}
	return json.Marshal(wire(v))
}

type SampledSequence struct {
	StopReason StopReason `json:"stop_reason"`
	Tokens     []int      `json:"tokens"`
	Logprobs   []float64  `json:"logprobs,omitempty"`
}
type SamplingParams struct {
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Seed        *int     `json:"seed,omitempty"`
	Stop        any      `json:"stop,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopK        int      `json:"top_k,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
}

func (v SamplingParams) MarshalJSON() ([]byte, error) {
	type wire SamplingParams
	if v.Temperature == nil {
		value := float64(1)
		v.Temperature = &value
	}
	if v.TopP == nil {
		value := float64(1)
		v.TopP = &value
	}
	return json.Marshal(wire(v))
}

type SaveWeightsForSamplerRequest struct {
	ModelID              ModelID `json:"model_id"`
	Path                 *string `json:"path,omitempty"`
	SamplingSessionSeqID *int    `json:"sampling_session_seq_id,omitempty"`
	SeqID                *int    `json:"seq_id,omitempty"`
	TTLSeconds           *int    `json:"ttl_seconds,omitempty"`
	Type                 *string `json:"type,omitempty"`
}

func (v SaveWeightsForSamplerRequest) MarshalJSON() ([]byte, error) {
	type wire SaveWeightsForSamplerRequest
	if v.Type == nil {
		value := "save_weights_for_sampler"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type SaveWeightsForSamplerResponse struct {
	Path string  `json:"path"`
	Type *string `json:"type,omitempty"`
}
type SaveWeightsForSamplerResponseInternal struct {
	Path              *string `json:"path,omitempty"`
	SamplingSessionID *string `json:"sampling_session_id,omitempty"`
	Type              *string `json:"type,omitempty"`
}
type SaveWeightsRequest struct {
	ModelID    ModelID `json:"model_id"`
	Path       *string `json:"path,omitempty"`
	SeqID      *int    `json:"seq_id,omitempty"`
	TTLSeconds *int    `json:"ttl_seconds,omitempty"`
	Overwrite  *bool   `json:"overwrite,omitempty"`
	Type       *string `json:"type,omitempty"`
}

func (v SaveWeightsRequest) MarshalJSON() ([]byte, error) {
	type wire SaveWeightsRequest
	if v.Overwrite == nil {
		value := false
		v.Overwrite = &value
	}
	if v.Type == nil {
		value := "save_weights"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type SaveWeightsResponse struct {
	Path string  `json:"path"`
	Type *string `json:"type,omitempty"`
}
type SessionEndEvent struct {
	Duration          string    `json:"duration"`
	Event             EventType `json:"event"`
	EventID           string    `json:"event_id"`
	EventSessionIndex int       `json:"event_session_index"`
	Severity          Severity  `json:"severity"`
	Timestamp         time.Time `json:"timestamp"`
}
type SessionHeartbeatRequest struct {
	SessionID string  `json:"session_id"`
	Type      *string `json:"type,omitempty"`
}

func (v SessionHeartbeatRequest) MarshalJSON() ([]byte, error) {
	type wire SessionHeartbeatRequest
	if v.Type == nil {
		value := "session_heartbeat"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type SessionHeartbeatResponse struct {
	Type *string `json:"type,omitempty"`
}

func (v SessionHeartbeatResponse) MarshalJSON() ([]byte, error) {
	type wire SessionHeartbeatResponse
	if v.Type == nil {
		value := "session_heartbeat"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type SessionStartEvent struct {
	Event             EventType `json:"event"`
	EventID           string    `json:"event_id"`
	EventSessionIndex int       `json:"event_session_index"`
	Severity          Severity  `json:"severity"`
	Timestamp         time.Time `json:"timestamp"`
}
type SupportedModel struct {
	ModelName        *string `json:"model_name,omitempty"`
	MaxContextLength *int    `json:"max_context_length,omitempty"`
}
type TelemetryBatch struct {
	Events     []TelemetryEvent `json:"events"`
	Platform   string           `json:"platform"`
	SdkVersion string           `json:"sdk_version"`
	SessionID  string           `json:"session_id"`
}
type TelemetryResponse struct {
	Status string `json:"status"`
}
type TelemetrySendRequest struct {
	Events     []TelemetryEvent `json:"events"`
	Platform   string           `json:"platform"`
	SdkVersion string           `json:"sdk_version"`
	SessionID  string           `json:"session_id"`
}
type TensorData struct {
	Data              any         `json:"data"`
	Dtype             TensorDtype `json:"dtype"`
	Shape             []int       `json:"shape,omitempty"`
	SparseCrowIndices []int       `json:"sparse_crow_indices,omitempty"`
	SparseColIndices  []int       `json:"sparse_col_indices,omitempty"`
}
type TopkPromptLogprobs struct {
	TokenIds any `json:"token_ids"`
	Logprobs any `json:"logprobs"`
}
type TryAgainResponse struct {
	Type       *string `json:"type,omitempty"`
	RequestID  string  `json:"request_id"`
	QueueState string  `json:"queue_state"`
}

func (v TryAgainResponse) MarshalJSON() ([]byte, error) {
	type wire TryAgainResponse
	if v.Type == nil {
		value := "try_again"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type UnhandledExceptionEvent struct {
	ErrorMessage      string    `json:"error_message"`
	ErrorType         string    `json:"error_type"`
	Event             EventType `json:"event"`
	EventID           string    `json:"event_id"`
	EventSessionIndex int       `json:"event_session_index"`
	Severity          Severity  `json:"severity"`
	Timestamp         time.Time `json:"timestamp"`
	Traceback         *string   `json:"traceback,omitempty"`
}
type UnloadModelRequest struct {
	ModelID ModelID `json:"model_id"`
	Type    *string `json:"type,omitempty"`
}

func (v UnloadModelRequest) MarshalJSON() ([]byte, error) {
	type wire UnloadModelRequest
	if v.Type == nil {
		value := "unload_model"
		v.Type = &value
	}
	return json.Marshal(wire(v))
}

type UnloadModelResponse struct {
	ModelID ModelID `json:"model_id"`
	Type    *string `json:"type,omitempty"`
}
type WeightsInfoResponse struct {
	BaseModel    string `json:"base_model"`
	IsLora       bool   `json:"is_lora"`
	LoraRank     *int   `json:"lora_rank,omitempty"`
	TrainUnembed *bool  `json:"train_unembed,omitempty"`
	TrainMlp     *bool  `json:"train_mlp,omitempty"`
	TrainAttn    *bool  `json:"train_attn,omitempty"`
}

package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// NativeTrainingRuntime implements the owned JSON training protocol directly.
// It can be used as CreateFiretitanTrainingClientOptions.ComputeBackend.
var regexpTrainingSessionName = regexp.MustCompile(`^accounts/[^/]+/trainingSessions/ts-[A-Za-z0-9-]+$`)

type NativeTrainingRuntime struct {
	REST             *TrainingRestClient
	SessionID        string
	PollInterval     time.Duration
	mu               sync.Mutex
	modelSequence    int
	requestSequences map[string]int
}

func NewNativeTrainingRuntime(rest *TrainingRestClient, sessionID string) *NativeTrainingRuntime {
	return &NativeTrainingRuntime{REST: rest, SessionID: sessionID, PollInterval: time.Second, requestSequences: map[string]int{}}
}

type NativeTrainingError struct {
	Status    int
	RequestID string
	Category  string
	Message   string
}

func (e *NativeTrainingError) Error() string {
	return fmt.Sprintf("training request failed (HTTP %d, category %s): %s", e.Status, e.Category, e.Message)
}

// Request is the low-level resource path. Additional request fields and all
// response fields are retained for the training protocol's extensible objects.
func (r *NativeTrainingRuntime) Request(ctx context.Context, method, operation string, body any) (map[string]any, error) {
	if r == nil || r.REST == nil {
		return nil, fmt.Errorf("native training runtime requires a REST client")
	}
	if strings.ContainsAny(operation, ".?#") || strings.HasPrefix(operation, "/") || strings.HasSuffix(operation, "/") || operation == "" {
		return nil, fmt.Errorf("invalid training operation")
	}
	var response *http.Response
	var err error
	if method == http.MethodGet {
		response, err = r.REST.Get(ctx, "/api/v1/"+operation, nil)
	} else {
		response, err = r.REST.Post(ctx, "/api/v1/"+operation, body, nil)
	}
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if len(data) > 0 {
		if err = json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("invalid training response: %w", err)
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, &NativeTrainingError{Status: response.StatusCode, RequestID: response.Header.Get("X-Request-Id"), Message: ParseAPIErrorBody(data)}
	}
	if result == nil {
		return nil, fmt.Errorf("training response must be an object")
	}
	return result, nil
}

func (r *NativeTrainingRuntime) submit(ctx context.Context, operation string, body map[string]any) (map[string]any, error) {
	result, err := r.Request(ctx, http.MethodPost, operation, body)
	if err != nil {
		return nil, err
	}
	id, _ := result["request_id"].(string)
	if id == "" {
		return result, nil
	}
	model, _ := body["model_id"].(string)
	for {
		poll := map[string]any{"request_id": id}
		if model != "" {
			poll["model_id"] = model
		}
		result, err = r.Request(ctx, http.MethodPost, "retrieve_future", poll)
		if err != nil {
			if failure, ok := err.(*NativeTrainingError); !ok || failure.Status != 408 {
				return nil, err
			}
		}
		if err == nil && result["type"] != "try_again" && result["status"] != "complete_metadata" {
			if message, exists := result["error"]; exists {
				return nil, &NativeTrainingError{Status: 200, RequestID: id, Category: stringFromAny(result["category"]), Message: fmt.Sprint(message)}
			}
			return result, nil
		}
		delay := r.PollInterval
		if delay <= 0 {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
func (r *NativeTrainingRuntime) nextSequence(model string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requestSequences[model]++
	return r.requestSequences[model]
}
func decodeNativeResult[T any](result map[string]any, err error) (T, error) {
	var value T
	if err != nil {
		return value, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(data, &value)
	return value, err
}

func (r *NativeTrainingRuntime) CreateModel(ctx context.Context, config FiretitanProvisioningConfig, metadata map[string]string) (CreateModelResponse, error) {
	r.mu.Lock()
	if r.SessionID == "" {
		result, err := r.Request(ctx, http.MethodPost, "create_session", map[string]any{"type": "create_session", "tags": []string{}, "user_metadata": nil, "sdk_version": "1.2.30"})
		if err != nil {
			r.mu.Unlock()
			return CreateModelResponse{}, err
		}
		r.SessionID = stringFromAny(result["session_id"])
		if r.SessionID == "" {
			r.mu.Unlock()
			return CreateModelResponse{}, fmt.Errorf("create_session omitted session_id")
		}
	}
	r.modelSequence++
	sequence := r.modelSequence
	session := r.SessionID
	r.mu.Unlock()
	body := config.CreateModelRequestBody(session, sequence, metadata)
	body["type"] = "create_model"
	if config.LoraRank == 0 {
		body["lora_config"] = nil
	}
	return decodeNativeResult[CreateModelResponse](r.submit(ctx, "create_model", body))
}
func (r *NativeTrainingRuntime) Forward(ctx context.Context, options ForwardBackwardOptions) (ForwardBackwardOutput, error) {
	body := options.ForwardRequestBody()
	body["seq_id"] = r.nextSequence(options.ModelID)
	return decodeNativeResult[ForwardBackwardOutput](r.submit(ctx, "forward", body))
}
func (r *NativeTrainingRuntime) ForwardBackward(ctx context.Context, options ForwardBackwardOptions) (ForwardBackwardOutput, error) {
	body := options.RequestBody()
	body["seq_id"] = r.nextSequence(options.ModelID)
	return decodeNativeResult[ForwardBackwardOutput](r.submit(ctx, "forward_backward", body))
}
func (r *NativeTrainingRuntime) OptimStep(ctx context.Context, options OptimStepOptions) (map[string]any, error) {
	body := map[string]any{"type": "optim_step", "model_id": options.ModelID, "seq_id": r.nextSequence(options.ModelID), "adam_params": options.AdamParams}
	if options.GradAccumulationNormalization != "" {
		body["grad_accumulation_normalization"] = options.GradAccumulationNormalization
	}
	if options.EmitGradNormMetrics != nil {
		body["emit_grad_norm_metrics"] = options.EmitGradNormMetrics
	}
	return r.submit(ctx, "optim_step", body)
}
func (r *NativeTrainingRuntime) UnloadModel(ctx context.Context, model string, _ int) (map[string]any, error) {
	return r.submit(ctx, "unload_model", map[string]any{"type": "unload_model", "model_id": model, "seq_id": r.nextSequence(model)})
}
func (r *NativeTrainingRuntime) WeightSync(ctx context.Context, request map[string]any) (WeightSyncResponse, error) {
	body := cloneAnyMap(request)
	model, _ := body["model_id"].(string)
	body["seq_id"] = r.nextSequence(model)
	return decodeNativeResult[WeightSyncResponse](r.submit(ctx, "weight_sync", body))
}

type NativeTrainingModel struct {
	runtime           *NativeTrainingRuntime
	ModelID           string
	SnapshotSessionID string
}

func (r *NativeTrainingRuntime) ForModel(model string) *NativeTrainingModel {
	id, err := GenerateSessionID()
	if err != nil {
		id = fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return &NativeTrainingModel{runtime: r, ModelID: model, SnapshotSessionID: id}
}
func (m *NativeTrainingModel) SaveState(ctx context.Context, name string, options SaveStateOptions) (SaveStateResult, error) {
	if options.TTLSeconds != nil {
		return SaveStateResult{}, fmt.Errorf("Fireworks does not support checkpoint ttl_seconds")
	}
	result, err := m.runtime.submit(ctx, "save_weights", map[string]any{"type": "save_weights", "model_id": m.ModelID, "path": name, "seq_id": m.runtime.nextSequence(m.ModelID), "overwrite": options.Overwrite})
	if err != nil {
		return SaveStateResult{}, err
	}
	return SaveStateResult{Name: name, Path: stringFromAny(result["path"])}, nil
}
func (m *NativeTrainingModel) load(ctx context.Context, path string, optimizer bool) error {
	_, err := m.runtime.submit(ctx, "load_weights", map[string]any{"type": "load_weights", "model_id": m.ModelID, "path": path, "optimizer": optimizer, "seq_id": m.runtime.nextSequence(m.ModelID)})
	return err
}
func (m *NativeTrainingModel) LoadState(ctx context.Context, path string) error {
	return m.load(ctx, path, false)
}
func (m *NativeTrainingModel) LoadStateWithOptimizer(ctx context.Context, path string) error {
	return m.load(ctx, path, true)
}
func (m *NativeTrainingModel) LoadAdapter(ctx context.Context, options LoadAdapterOptions) (LoadAdapterResponse, error) {
	return decodeNativeResult[LoadAdapterResponse](m.runtime.submit(ctx, "load_adapter", map[string]any{"model_id": m.ModelID, "adapter_path": options.AdapterPath, "seq_id": m.runtime.nextSequence(m.ModelID)}))
}
func (m *NativeTrainingModel) SaveWeightsForSamplerExt(ctx context.Context, name string, options SaveWeightsForSamplerOptions) (SaveSamplerResult, error) {
	if options.TTL != 0 {
		return SaveSamplerResult{}, fmt.Errorf("Fireworks does not support checkpoint ttl_seconds")
	}
	snapshot := QualifySnapshotName(m.SnapshotSessionID, name)
	body := map[string]any{"type": "save_weights_for_sampler", "model_id": m.ModelID, "path": snapshot, "seq_id": m.runtime.nextSequence(m.ModelID)}
	if options.CheckpointType != "" {
		body["checkpoint_type"] = options.CheckpointType
	}
	if options.ExportPrecision != "" {
		body["export_precision"] = options.ExportPrecision
	}
	if options.Metadata != nil {
		body["metadata"] = options.Metadata
	}
	result, err := m.runtime.submit(ctx, "save_weights_for_sampler", body)
	if err != nil {
		return SaveSamplerResult{}, err
	}
	return SaveSamplerResult{Path: stringFromAny(result["path"]), SnapshotName: snapshot}, nil
}

func (c *FiretitanServiceClient) DeleteTrainingSession(ctx context.Context) error {
	if c == nil || c.TrainingSessionID() == "" {
		return nil
	}
	name := c.TrainingSessionName(ctx)
	if name == "" {
		return fmt.Errorf("cannot resolve training-session account")
	}
	return c.Fireworks.DeleteTrainingSession(ctx, name)
}
func (c *FireworksClient) DeleteTrainingSession(ctx context.Context, name string) error {
	if !regexpTrainingSessionName.MatchString(name) {
		return fmt.Errorf("training session name must be accounts/<account>/trainingSessions/<session>")
	}
	response, err := c.Delete(ctx, "/v1/"+name, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == 404 || response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(response.Body)
	return &NativeTrainingError{Status: response.StatusCode, Message: ParseAPIErrorBody(body)}
}

func (r *NativeTrainingRuntime) HealthCheck(ctx context.Context) (map[string]any, error) {
	return r.Request(ctx, http.MethodGet, "healthz", nil)
}
func (r *NativeTrainingRuntime) GetServerCapabilities(ctx context.Context) (ServerCapabilitiesResponse, error) {
	if isServerlessGatewayURL(r.REST.BaseURL()) {
		return ServerCapabilitiesResponse{}, fmt.Errorf("serverless endpoints do not expose server-wide capabilities; use model-creation capabilities")
	}
	return decodeNativeResult[ServerCapabilitiesResponse](r.Request(ctx, http.MethodGet, "get_server_capabilities", nil))
}
func (r *NativeTrainingRuntime) AuthToken(ctx context.Context) (map[string]any, error) {
	return r.Request(ctx, http.MethodPost, "auth/token", map[string]any{})
}
func (r *NativeTrainingRuntime) ClientConfig(ctx context.Context, body any) (map[string]any, error) {
	return r.Request(ctx, http.MethodPost, "client/config", body)
}
func (r *NativeTrainingRuntime) SessionHeartbeat(ctx context.Context) (map[string]any, error) {
	return r.Request(ctx, http.MethodPost, "session_heartbeat", map[string]any{"session_id": r.SessionID, "type": "session_heartbeat"})
}
func (r *NativeTrainingRuntime) CreateSamplingSession(ctx context.Context, body map[string]any) (map[string]any, error) {
	return r.Request(ctx, http.MethodPost, "create_sampling_session", body)
}
func (r *NativeTrainingRuntime) Sample(ctx context.Context, body map[string]any) (map[string]any, error) {
	return r.submit(ctx, "asample", body)
}
func (r *NativeTrainingRuntime) GetInfo(ctx context.Context, model string) (map[string]any, error) {
	return r.Request(ctx, http.MethodPost, "get_info", map[string]any{"model_id": model, "type": "get_info"})
}
func (r *NativeTrainingRuntime) SendTelemetry(ctx context.Context, body any) (map[string]any, error) {
	return r.Request(ctx, http.MethodPost, "telemetry", body)
}

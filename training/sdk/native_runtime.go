package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	if method == http.MethodPost && operation == "retrieve_future" {
		return r.retrieveFuture(ctx, body, 45*time.Second)
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
	return decodeTrainingResponse(response, data)
}

func decodeTrainingResponse(response *http.Response, data []byte) (map[string]any, error) {
	var result map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &result); err != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
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

// A large future may download for longer than the header or per-read timeout.
func (r *NativeTrainingRuntime) retrieveFuture(ctx context.Context, body any, idle time.Duration) (map[string]any, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	attempt, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	req, err := http.NewRequestWithContext(attempt, http.MethodPost, r.REST.BaseURL()+"/api/v1/retrieve_future", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header = r.REST.Headers(nil)
	client := *r.REST.httpClient
	client.Timeout = 0
	timer := time.AfterFunc(idle, func() { cancel(context.DeadlineExceeded) })
	defer timer.Stop()
	resp, err := client.Do(req)
	if err != nil {
		if cause := context.Cause(attempt); cause != nil {
			return nil, cause
		}
		return nil, err
	}
	defer resp.Body.Close()
	var data bytes.Buffer
	buffer := make([]byte, 32*1024)
	for {
		timer.Reset(idle)
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			data.Write(buffer[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if cause := context.Cause(attempt); cause != nil {
				return nil, cause
			}
			return nil, err
		}
	}
	return decodeTrainingResponse(resp, data.Bytes())
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
	allowMetadata := true
	badRequests, connectionErrors := 0, 0
	for {
		poll := map[string]any{"request_id": id, "allow_metadata_only": allowMetadata}
		if model != "" {
			poll["model_id"] = model
		}
		result, err = r.Request(ctx, http.MethodPost, "retrieve_future", poll)
		delay := r.PollInterval
		if delay <= 0 {
			delay = time.Second
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var failure *NativeTrainingError
		var networkFailure net.Error
		backoff := false
		switch {
		case errors.As(err, &failure):
			switch {
			case failure.Status == 408:
				badRequests = 0
			case failure.Status >= 500 && failure.Status < 600:
			case failure.Status == 429 || (failure.Status == 409 && ShouldRetryServerlessCapacity409(r.REST.BaseURL()+"/api/v1/retrieve_future", []byte(failure.Message), r.REST.serverlessSupervised409Retry.Load())):
				backoff = true
			case failure.Status == 400 && badRequests < 3:
				badRequests++
			default:
				return nil, err
			}
		case errors.As(err, &networkFailure), errors.Is(err, io.ErrUnexpectedEOF):
			backoff = true
		case err != nil:
			return nil, err
		case result["status"] == "complete_metadata":
			if !allowMetadata {
				return nil, fmt.Errorf("repeated metadata-only response for %s", id)
			}
			allowMetadata = false
		case result["type"] != "try_again":
			if message, exists := result["error"]; exists {
				return nil, &NativeTrainingError{Status: 200, RequestID: id, Category: stringFromAny(result["category"]), Message: fmt.Sprint(message)}
			}
			return result, nil
		}
		if backoff {
			delay = min(30*time.Second, time.Second<<min(connectionErrors, 5))
			connectionErrors++
		} else {
			connectionErrors = 0
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
		result, err := r.Request(ctx, http.MethodPost, "create_session", map[string]any{"type": "create_session", "tags": []string{}, "user_metadata": nil, "sdk_version": "1.2.39"})
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
	sequence := r.modelSequence
	r.modelSequence++
	session := r.SessionID
	r.mu.Unlock()
	body := config.CreateModelRequestBody(session, sequence, metadata)
	body["type"] = "create_model"
	if config.ForwardOnly && config.LoraRank == 0 {
		delete(body, "lora_config")
		body["base_only"] = true
	} else if config.LoraRank == 0 {
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
	adapter, err := adapterResource(options.AdapterPath)
	if err != nil {
		return LoadAdapterResponse{}, err
	}
	return decodeNativeResult[LoadAdapterResponse](m.runtime.submit(ctx, "load_adapter", map[string]any{"model_id": m.ModelID, "adapter_path": adapter, "seq_id": m.runtime.nextSequence(m.ModelID)}))
}
func (m *NativeTrainingModel) SaveWeightsForSamplerExt(ctx context.Context, name string, options SaveWeightsForSamplerOptions) (SaveSamplerResult, error) {
	if options.TTL != 0 {
		return SaveSamplerResult{}, fmt.Errorf("Fireworks does not support checkpoint ttl_seconds")
	}
	rounding, err := ResolveExportRounding(SamplerCheckpointType(options.CheckpointType), options.ExportRounding)
	if err != nil {
		return SaveSamplerResult{}, err
	}
	snapshot := QualifySnapshotName(m.SnapshotSessionID, name)
	body := map[string]any{"type": "save_weights_for_sampler", "model_id": m.ModelID, "path": snapshot, "seq_id": m.runtime.nextSequence(m.ModelID)}
	if options.CheckpointType != "" {
		body["checkpoint_type"] = options.CheckpointType
	}
	if rounding != "" {
		body["export_rounding"] = rounding
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

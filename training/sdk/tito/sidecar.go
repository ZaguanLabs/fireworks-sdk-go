package tito

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/ZaguanLabs/fireworks-sdk-go/training/sdk"
)

const maxHTTPRequestBytes = 64 * 1024 * 1024

type ExactTokenSampler interface {
	SampleWithPromptTokensResult(context.Context, []int, ...sdk.SampleOptions) (sdk.SampledRequestResult, error)
}

type CallClassifier func(ChatRequest) (CallKind, string)

type SidecarOptions struct {
	FailureTraceConfig *FailureTraceConfig

	BindAddress              string
	IncrementalPromptRouting bool
	MaxContextTokens         int
	MaxOutputTokens          int
	CallClassifier           CallClassifier
	SamplingDefaults         map[string]any
	BackendHeaders           map[string]string
	Observer                 EventObserver
	DefaultDriftPolicy       TrajectoryDriftPolicy
	PromptMode               PromptMode
	Keepalive                time.Duration
	Model                    string
}

type Sidecar struct {
	traces *failureTraceRecorder

	defaultTemperature *float64
	sampler            ExactTokenSampler
	renderer           Renderer
	options            SidecarOptions

	mu           sync.RWMutex
	server       *http.Server
	listener     net.Listener
	baseURL      string
	trajectories map[string]*trajectory
}

type TITOSidecar = Sidecar
type TITOSidecarOptions = SidecarOptions

type trajectory struct {
	admission    *trajectoryAdmission
	discardError *Error

	policyMu           sync.Mutex
	mu                 sync.Mutex
	id                 string
	credential         string
	affinityHash       string
	servingAffinityKey string
	metadata           map[string]any
	status             TrajectoryStatus
	terminalReason     string
	segments           []SegmentResult
	calls              []CallRecord
	responseAttempts   []ResponseAttempt
	metrics            MetricSummary
	startedAt          float64
	finishedAt         float64
	latestSegment      int
	checkpoints        map[int][]turnLocation
	driftPolicy        TrajectoryDriftPolicy
	idempotency        map[string]idempotencyEntry
}

type idempotencyEntry struct {
	Incremental bool
	Parent      turnLocation
	Fingerprint string
	PromptHash  string
	Result      CallResult
}

func NewSidecar(sampler ExactTokenSampler, renderer Renderer, options SidecarOptions) (*Sidecar, error) {
	if sampler == nil || renderer == nil {
		return nil, errors.New("sampler and renderer are required")
	}
	if options.BindAddress == "" {
		options.BindAddress = "127.0.0.1"
	}
	address := net.ParseIP(options.BindAddress)
	if address == nil || address.IsUnspecified() || address.IsMulticast() {
		return nil, fmt.Errorf("bind_address must be a specific unicast IPv4 or IPv6 address")
	}
	options.BindAddress = address.String()
	if options.MaxContextTokens < 2 || options.MaxOutputTokens < 1 {
		return nil, errors.New("invalid context/output token limits")
	}
	if options.PromptMode == "" {
		options.PromptMode = PromptModeFullHistory
	}
	if options.PromptMode != PromptModeFullHistory && options.PromptMode != PromptModeIncremental {
		return nil, fmt.Errorf("unknown TITO prompt mode: %q", options.PromptMode)
	}
	if options.PromptMode == PromptModeIncremental {
		if _, ok := renderer.(IncrementalRenderer); !ok {
			return nil, errors.New("experimental incremental prompt mode requires an incremental renderer")
		}
	}
	if options.DefaultDriftPolicy.OnOtherMismatch == "" {
		options.DefaultDriftPolicy = DefaultTrajectoryDriftPolicy()
	}
	if err := options.DefaultDriftPolicy.Validate(); err != nil {
		return nil, err
	}
	if options.Model == "" {
		options.Model = "policy"
	}
	if options.Keepalive < 0 {
		return nil, errors.New("keepalive duration must be positive")
	}
	if options.Keepalive == 0 {
		options.Keepalive = 5 * time.Second
	}
	config := DefaultFailureTraceConfig()
	if options.FailureTraceConfig != nil {
		config = *options.FailureTraceConfig
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	options.BackendHeaders = cloneStringMap(options.BackendHeaders)
	options.SamplingDefaults = cloneAnyMap(options.SamplingDefaults)
	for _, key := range []string{"prompt_cache_key", "prompt_cache_isolation_key", "user", "additional_headers_snapshot", "logical_request_id"} {
		if _, found := options.SamplingDefaults[key]; found {
			return nil, fmt.Errorf("sampling defaults cannot set %s", key)
		}
	}
	var temperature *float64
	if value := options.SamplingDefaults["temperature"]; value != nil {
		number, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid default temperature: %w", err)
		}
		temperature = &number
	}
	if flag, ok := options.SamplingDefaults["incremental_prompt_routing"].(bool); ok {
		options.IncrementalPromptRouting = flag
	}
	for _, key := range []string{"temperature", "incremental_prompt_routing", "max_seq_len", "max_tokens", "n", "echo", "stop", "logprobs"} {
		delete(options.SamplingDefaults, key)
	}
	return &Sidecar{defaultTemperature: temperature, traces: newFailureTraceRecorder(config), sampler: sampler, renderer: renderer, options: options, trajectories: make(map[string]*trajectory)}, nil
}

func NewSidecarFromDeploymentSampler(sampler *sdk.DeploymentSampler, renderer Renderer, options SidecarOptions) (*Sidecar, error) {
	if sampler == nil {
		return nil, errors.New("sampler is required")
	}
	for name := range sampler.AdditionalHeaders {
		switch strings.ToLower(name) {
		case "x-multi-turn-session-id", "x-session-affinity":
			return nil, fmt.Errorf("sampler additional_headers contains a fixed affinity header: %s", name)
		case "authorization", "x-api-key", "x-fireworks-session-id":
			return nil, fmt.Errorf("sampler additional_headers overrides SDK request-local headers: %s", name)
		}
	}
	options.BackendHeaders = cloneStringMap(sampler.AdditionalHeaders)
	if options.Model == "" {
		options.Model = sampler.Model
	}
	return NewSidecar(sampler, renderer, options)
}

func (s *Sidecar) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return 0
	}
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *Sidecar) Start(port ...int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil
	}
	requested := 0
	if len(port) > 0 {
		requested = port[0]
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(s.options.BindAddress, strconv.Itoa(requested)))
	if err != nil {
		return err
	}
	s.listener = listener
	s.baseURL = "http://" + listener.Addr().String()
	s.server = &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: s.options.Keepalive, MaxHeaderBytes: 1 << 20}
	server := s.server
	go func() { _ = server.Serve(listener) }()
	return nil
}

func (s *Sidecar) Close(ctx context.Context) error {
	s.mu.Lock()
	server := s.server
	s.server = nil
	s.listener = nil
	s.baseURL = ""
	ids := make([]string, 0, len(s.trajectories))
	for id := range s.trajectories {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	var shutdownErr error
	for _, id := range ids {
		_, err := s.AbandonTrajectory(id, "sidecar_closed")
		var terminal *Error
		if err != nil && !(errors.As(err, &terminal) && terminal.Code == "tito_trajectory_closed") {
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}
	if server != nil {
		shutdownErr = errors.Join(shutdownErr, server.Shutdown(ctx))
	}
	return shutdownErr
}

func (s *Sidecar) CreateTrajectory(trajectoryID, servingAffinityKey string, metadata map[string]any, policy ...TrajectoryDriftPolicy) (TrajectoryEndpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return TrajectoryEndpoint{}, errors.New("start the sidecar before creating a trajectory")
	}
	if trajectoryID == "" {
		trajectoryID = randomHex(16)
	}
	if _, exists := s.trajectories[trajectoryID]; exists {
		return TrajectoryEndpoint{}, fmt.Errorf("duplicate trajectory_id: %s", trajectoryID)
	}
	if servingAffinityKey == "" {
		servingAffinityKey = randomHex(32)
	}
	drift := s.options.DefaultDriftPolicy
	if len(policy) > 0 {
		drift = policy[0]
	}
	if err := drift.Validate(); err != nil {
		return TrajectoryEndpoint{}, err
	}
	credential := "tito_" + randomHex(32)
	t := &trajectory{admission: newTrajectoryAdmission(), id: trajectoryID, credential: credential, affinityHash: fmt.Sprintf("%x", sha256.Sum256([]byte(servingAffinityKey))), servingAffinityKey: servingAffinityKey, metadata: cloneAnyMap(metadata), status: TrajectoryStatusActive, metrics: MetricSummary{Counters: make(map[string]int), Distributions: make(map[string]Distribution)}, startedAt: nowSeconds(), driftPolicy: drift, idempotency: make(map[string]idempotencyEntry)}
	delete(t.metadata, "tito_failure")
	delete(t.metadata, "tito_malformed_tool_call_id")
	t.checkpoints = make(map[int][]turnLocation)
	s.trajectories[trajectoryID] = t
	if s.options.Observer != nil {
		if _, err := s.options.Observer.Record("trajectory_created", trajectoryID, map[string]any{"metadata": metadata}, nil); err != nil {
			delete(s.trajectories, trajectoryID)
			return TrajectoryEndpoint{}, err
		}
	}
	s.traces.record(trajectoryID, "trajectory_open", map[string]any{"metadata": metadata})
	return TrajectoryEndpoint{TrajectoryID: trajectoryID, OpenAIBaseURL: s.baseURL + "/trajectories/" + trajectoryID + "/v1", APIKey: credential}, nil
}

func (s *Sidecar) FinishTrajectory(id string) (TrajectoryArtifact, error) {
	return s.terminate(id, TrajectoryStatusCompleted, "")
}
func (s *Sidecar) AbandonTrajectory(id, reason string) (TrajectoryArtifact, error) {
	if reason == "" {
		reason = "caller_abandoned"
	}
	return s.terminate(id, TrajectoryStatusAbandoned, reason)
}
func (s *Sidecar) FailTrajectory(id, reason string) (TrajectoryArtifact, error) {
	return s.terminate(id, TrajectoryStatusFailed, reason)
}

// Deprecated: agent wall timing is carried by the trajectory summary; no metric is emitted.
func (s *Sidecar) ObserveAgentWall(id string, seconds float64) error {
	_, err := s.trajectoryFor(id)
	return err
}

func (s *Sidecar) terminate(id string, status TrajectoryStatus, reason string) (TrajectoryArtifact, error) {
	t, err := s.trajectoryFor(id)
	if err != nil {
		return TrajectoryArtifact{}, err
	}
	if err := t.admission.beginClose(status, reason); err != nil {
		return TrajectoryArtifact{}, err
	}
	t.admission.pending.Wait()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.discardError != nil {
		status = TrajectoryStatusFailed
		reason = t.discardError.Code
	}
	if t.status == TrajectoryStatusActive {
		t.status = status
		t.terminalReason = reason
		t.finishedAt = nowSeconds()
		for i := range t.segments {
			if t.segments[i].ClosedReason == "" {
				t.segments[i].ClosedReason = "trajectory_" + string(status)
			}
		}
	}
	defer func() { t.admission.finishClose(t.status, t.finishedAt, t.discardError) }()
	if s.options.Observer != nil {
		if _, err := s.options.Observer.CloseTrajectory(id, t.status, map[string]any{"terminal_reason": t.terminalReason}); err != nil {
			t.status = TrajectoryStatusFailed
			if t.discardError != nil {
				if t.discardError.Context == nil {
					t.discardError.Context = make(map[string]any)
				}
				t.discardError.Context["debug_storage_error"] = "local TITO debug close failed"
				t.metadata["tito_failure"] = t.discardError.AsErrorRecord()
			}
			s.traces.record(id, "terminalization_error", map[string]any{"message": err.Error()})
			s.traces.closeTrajectory(id, true)
			return t.artifact(), err
		}
	}
	s.traces.closeTrajectory(id, t.status != TrajectoryStatusCompleted)
	return t.artifact(), nil
}

func (s *Sidecar) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 || len(parts) > 5 || parts[0] != "trajectories" || parts[2] != "v1" {
		http.NotFound(w, r)
		return
	}
	t, err := s.authenticate(parts[1], r.Header.Get("Authorization"))
	if err != nil {
		writeTITOError(w, err)
		return
	}
	if len(parts) == 5 && parts[3] == "chat" && parts[4] == "completions" && r.Method == http.MethodPost {
		s.handleChat(w, r, t)
		return
	}
	if len(parts) == 4 && parts[3] == "models" && r.Method == http.MethodGet {
		s.handleModels(w, t)
		return
	}
	http.NotFound(w, r)
}

func (s *Sidecar) handleModels(w http.ResponseWriter, t *trajectory) {
	_, leave, err := t.admission.enter(context.Background())
	if err != nil {
		writeTITOError(w, err)
		return
	}
	defer leave()
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := requireActive(t); err != nil {
		writeTITOError(w, err)
		return
	}
	models := []string{"policy"}
	if s.options.Model != "policy" {
		models = append(models, s.options.Model)
	}
	data := make([]any, len(models))
	for i, id := range models {
		data[i] = map[string]any{"id": id, "object": "model", "created": 0, "owned_by": "fireworks"}
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Sidecar) handleChat(w http.ResponseWriter, r *http.Request, t *trajectory) {
	ctx, leave, blocked := t.admission.enter(r.Context())
	if blocked != nil {
		writeTITOError(w, blocked)
		return
	}
	defer leave()
	r = r.WithContext(ctx)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHTTPRequestBytes))
	if err != nil {
		writeTITOError(w, invalidRequest("invalid JSON request"))
		return
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		writeTITOError(w, invalidRequest("invalid JSON request"))
		return
	}
	request, err := NewChatRequestFromOpenAI(payload, string(body))
	if err != nil {
		writeTITOError(w, asTITOError(err))
		return
	}
	key := r.Header.Get("Idempotency-Key")
	stream, _ := payload["stream"].(bool)
	if !stream {
		result, err := s.complete(r.Context(), t, request, key)
		if err != nil {
			writeTITOError(w, asTITOError(err))
			return
		}
		emission := EmissionCompleted
		if err := writeJSON(w, http.StatusOK, result.Response); err != nil {
			emission = EmissionAmbiguous
		}
		s.recordEmission(t, result.TurnID, emission)
		return
	}
	result, ok := s.streamInference(w, r.Context(), t, request, key)
	if !ok {
		return
	}
	chunks := openAIStreamChunks(result.Response, payload)
	var writeErr error
	for _, chunk := range chunks {
		data, _ := canonicalJSON(chunk)
		if _, writeErr = fmt.Fprintf(w, "data: %s\n\n", data); writeErr != nil {
			break
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	if writeErr == nil {
		_, writeErr = io.WriteString(w, "data: [DONE]\n\n")
	}
	emission := EmissionCompleted
	if writeErr != nil {
		emission = EmissionAmbiguous
	}
	s.recordEmission(t, result.TurnID, emission)
}

// Stream headers become visible only after deterministic admission completes.
func (s *Sidecar) streamInference(w http.ResponseWriter, ctx context.Context, t *trajectory, request ChatRequest, key string) (CallResult, bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type completion struct {
		result CallResult
		err    error
	}
	done := make(chan completion, 1)
	prepared := make(chan struct{})
	go func() {
		r, err := s.complete(ctx, t, request, key, func() { close(prepared) })
		done <- completion{r, err}
	}()
	started := false
	start := func() {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		started = true
	}
	admission := prepared
	ticker := time.NewTicker(s.options.Keepalive)
	defer ticker.Stop()
	for {
		select {
		case <-admission:
			start()
			admission = nil
		case <-ticker.C:
			if !started {
				continue
			}
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				cancel()
				outcome := <-done
				s.recordEmission(t, outcome.result.TurnID, EmissionAmbiguous)
				return CallResult{}, false
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case outcome := <-done:
			if !started {
				select {
				case <-prepared:
					start()
				default:
				}
			}
			if outcome.err != nil {
				e := asTITOError(outcome.err)
				if !started {
					writeTITOError(w, e)
				} else {
					data, _ := json.Marshal(e.OpenAIBody())
					fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
				}
				return CallResult{}, false
			}
			return outcome.result, true
		case <-ctx.Done():
			outcome := <-done
			s.recordEmission(t, outcome.result.TurnID, EmissionAmbiguous)
			return CallResult{}, false
		}
	}
}

func (s *Sidecar) classify(request ChatRequest) (kind CallKind, source string) {
	if s.options.CallClassifier == nil {
		return CallKindPolicy, "default"
	}
	defer func() {
		if recover() != nil {
			kind, source = CallKindPolicy, "fail_closed"
		}
	}()
	kind, source = s.options.CallClassifier(request)
	if (kind != CallKindPolicy && kind != CallKindAuxiliary) || source == "" {
		return CallKindPolicy, "fail_closed"
	}
	return kind, source
}

func (s *Sidecar) complete(ctx context.Context, t *trajectory, request ChatRequest, idempotencyKey string, prepared ...func()) (callResult CallResult, returnErr error) {
	ctx, leave, blocked := t.admission.enter(ctx)
	if blocked != nil {
		return CallResult{}, blocked
	}
	defer leave()
	kind, source := s.classify(request)
	if kind == CallKindPolicy {
		t.policyMu.Lock()
		defer t.policyMu.Unlock()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			returnErr = &Error{Code: "tito_sdk_internal_error", Status: 500, Message: "local TITO SDK operation failed", Origin: "sdk", DiscardTrajectory: true}
		}
		if returnErr != nil {
			typed := asTITOError(returnErr)
			if t.discardError == typed {
				return
			}
			if typed.Code == "tito_completion_alignment_error" || typed.Code == "tito_sampler_contract_error" {
				typed.DiscardTrajectory = true
			}
			if typed.DiscardTrajectory {
				if t.discardError != nil && t.discardError != typed {
					delete(t.metadata, "tito_malformed_tool_call_id")
				}
				t.discardError = typed
			}

			s.traces.record(t.id, "call_failure", typed.AsErrorRecord())
			for key, value := range s.traces.evidence(t.id) {
				if typed.Context == nil {
					typed.Context = map[string]any{}
				}
				typed.Context[key] = value
			}
			if typed.DiscardTrajectory {
				t.metadata["tito_failure"] = typed.AsErrorRecord()
			}
			returnErr = typed
		}
	}()
	if err := ctx.Err(); err != nil {
		return CallResult{}, operationError(err, "admission")
	}
	s.traces.record(t.id, "call_request", map[string]any{"request": request})
	if err := requireActive(t); err != nil {
		return CallResult{}, err
	}
	started := nowSeconds()
	callID := randomHex(16)
	if source == "fail_closed" {
		t.metrics.Counters["admission/classifier_failed"]++
	}
	contract := s.renderer.RenderContractID(request)
	if contract == "" {
		contract = s.renderer.RendererID()
	}
	fingerprint := hashJSON(map[string]any{"render_contract_id": contract, "request": request.CanonicalValue(), "sampling": request.SamplingValue()})
	call := CallRecord{CallID: callID, Kind: kind, ClassificationSource: source, StartedAt: started, RequestFingerprint: fingerprint}
	if kind == CallKindPolicy && idempotencyKey != "" {
		if prior, ok := t.idempotency[idempotencyKey]; ok {
			var retryPrompt []int
			var renderErr error
			if fingerprint == prior.Fingerprint {
				if prior.Incremental {
					turn := t.at(prior.Parent)
					stored := acceptedMessages(turn)
					if historyExtends(stored, request.Messages) {
						var inc *IncrementalPrompt
						inc, renderErr = s.renderer.(IncrementalRenderer).PrepareIncrementalPrompt(request, stored, request.Messages[len(stored):], turn.ExactCheckpointIDs())
						if inc != nil {
							retryPrompt = inc.PromptIDs
						}
					}
				} else {
					retryPrompt, renderErr = s.renderer.RenderConversationTokens(request)
				}
			}
			promptMatches := renderErr == nil && hashTokens(retryPrompt) == prior.PromptHash
			if fingerprint != prior.Fingerprint || !promptMatches {
				call.Outcome = CallOutcomeRejected
				call.EndedAt = nowSeconds()
				call.ErrorCode = "idempotency_key_reused"
				t.recordCall(call)
				t.metrics.Counters["admission/idempotency_conflict"]++
				return CallResult{}, &Error{Code: call.ErrorCode, Status: 409, Message: "Idempotency-Key was reused with a different request"}
			}
			call.Outcome = CallOutcomeReplayed
			call.EndedAt = nowSeconds()
			call.PreparedPromptHash = prior.PromptHash
			call.TurnID = prior.Result.TurnID
			t.recordCall(call)
			result := prior.Result
			result.Response = deepCloneMap(prior.Result.Response)
			result.Call = call
			result.Replayed = true
			if len(prepared) > 0 {
				prepared[0]()
			}
			return result, nil
		}
	}
	var parent *turnLocation
	reason := "initial"
	if kind == CallKindPolicy {
		parent, reason = t.parent(request.Messages, contract)
	}
	promptMode := s.options.PromptMode
	disposition := PromptDispositionAppend
	var prefix, realigned int
	var incremental *IncrementalPrompt
	var previous *Turn
	var checkpoint []int
	var realignFrom *int
	fallbackReason := ""
	if parent != nil {
		previous = t.at(*parent)
		checkpoint = previous.ExactCheckpointIDs()
	}
	rejectDivergence := func(reason string) error {
		call.Outcome, call.EndedAt, call.ErrorCode = CallOutcomeRejected, nowSeconds(), "tito_lineage_divergence"
		t.recordCall(call)
		t.metrics.Counters["lineage/reject"]++
		return &Error{Code: call.ErrorCode, Status: 409, Message: "no unique trajectory continuation: " + reason}
	}
	if reason != "" && reason != "initial" {
		t.metrics.Counters["lineage/boundary_reason_"+reason]++
		if reason != "length_truncation" && t.driftPolicy.OnOtherMismatch == "reject" {
			return CallResult{}, rejectDivergence(reason)
		}
		if promptMode == PromptModeIncremental && reason != "length_truncation" {
			fallbackReason = reason
		}
	}
	if promptMode == PromptModeIncremental && reason == "" {
		stored := acceptedMessages(previous)
		var err error
		incremental, err = s.renderer.(IncrementalRenderer).PrepareIncrementalPrompt(request, stored, request.Messages[len(stored):], slices.Clone(checkpoint))
		if err != nil {
			return CallResult{}, err
		}
		if incremental == nil {
			reason, fallbackReason = "incremental_unsupported", "unsupported_incremental_join"
			t.metrics.Counters["lineage/boundary_reason_"+reason]++
			if t.driftPolicy.OnOtherMismatch == "reject" {
				return CallResult{}, rejectDivergence(reason)
			}
		} else if err := incremental.Validate(); err != nil {
			return CallResult{}, err
		}
	}
	var prompt []int
	if incremental != nil {
		prompt = slices.Clone(incremental.PromptIDs)
	} else {
		promptMode = PromptModeFullHistory
		var err error
		prompt, err = s.renderer.RenderConversationTokens(request)
		if err != nil {
			return CallResult{}, err
		}
	}
	call.PreparedPromptHash = hashTokens(prompt)
	havePrefix := reason == ""
	if havePrefix {
		prefix = commonPrefix(checkpoint, prompt)
		t.metrics.Counters["lineage/prefix_check"]++
		if incremental != nil {
			trim := incremental.CheckpointTrimTokens
			retained := len(checkpoint) - trim
			if trim > len(previous.ExactCompletionIDs) || len(prompt) <= retained || prefix < retained {
				return CallResult{}, &Error{Code: "tito_renderer_contract_error", Status: 500, Message: "invalid incremental checkpoint junction"}
			}
		} else if prefix < len(checkpoint) {
			t.metrics.Counters["lineage/prefix_mismatch"]++
			start := len(previous.ExactPromptIDs)
			masked := len(prompt) - start
			if prefix >= start && masked < t.driftPolicy.MaxMaskedTokens {
				disposition, realigned, realignFrom = PromptDispositionRealign, masked, &start
			} else {
				reason = "token_drift"
				t.metrics.Counters["lineage/boundary_reason_unbounded_or_ambiguous_drift"]++
				if t.driftPolicy.OnOtherMismatch == "reject" {
					return CallResult{}, rejectDivergence(reason)
				}
			}
		}
		if reason == "" {
			if parent.segment != t.latestSegment && prefix != len(checkpoint) {
				reason = "branch_token_drift"
			} else if parent.turn != len(t.segments[parent.segment].Turns)-1 {
				reason = "fork"
			}
		}
	}
	requested := s.options.MaxOutputTokens
	if request.MaxTokens != nil && *request.MaxTokens < requested {
		requested = *request.MaxTokens
	}
	remaining := s.options.MaxContextTokens - len(prompt)
	effective := requested
	if remaining < effective {
		effective = remaining
	}
	if effective < 1 {
		return CallResult{}, &Error{Code: "tito_context_exhausted", Status: 400, Message: "prompt exhausts the trajectory context window"}
	}
	extra := cloneAnyMap(request.SamplingFields)
	for _, k := range []string{"prompt_cache_key", "prompt_cache_isolation_key", "user"} {
		if _, found := extra[k]; found {
			return CallResult{}, &Error{Code: "tito_affinity_override", Status: 400, Message: "request overrides trajectory affinity: " + k}
		}
	}
	for _, k := range []string{"additional_headers_snapshot", "logical_request_id", "max_seq_len", "n", "echo", "stop", "prompt", "stream", "raw_output", "perf_metrics_in_response"} {
		if _, found := extra[k]; found {
			return CallResult{}, &Error{Code: "tito_invalid_sampling_field", Status: 400, Message: "request overrides internal sampling field: " + k}
		}
	}
	for k, v := range s.options.SamplingDefaults {
		extra[k] = v
	}
	if kind == CallKindAuxiliary {
		for _, key := range []string{"top_sampling_logprobs", "top_sampling_format", "sampling_mask", "sample_extra_tokens", "sample_extra_format"} {
			delete(extra, key)
		}
	}
	includeRouting, _ := extra["include_routing_matrix"].(bool)
	delete(extra, "include_routing_matrix")
	includeMask, _ := extra["sampling_mask"].(bool)
	includeTop := positiveCount(extra["top_sampling_logprobs"])
	includeExtra := positiveCount(extra["sample_extra_tokens"])
	if includeTop && includeExtra {
		return CallResult{}, &Error{Code: "tito_invalid_sampling_field", Status: 400, Message: "top sampling and extra draws are mutually exclusive"}
	}
	if reason == "" && ((previous.RoutingReferences != nil) != includeRouting || (previous.SamplingMaskReferences != nil) != includeMask || (previous.TopSamplingReferences != nil) != includeTop || (previous.SampleExtraReferences != nil) != includeExtra) {
		reason = "sampling_evidence_change"
		t.metrics.Counters["lineage/boundary_reason_"+reason]++
	}
	if reason != "" {
		disposition, realigned, realignFrom = PromptDispositionNewSegment, 0, nil
	}
	incrementalRouting := kind == CallKindPolicy && includeRouting && s.options.IncrementalPromptRouting
	if incrementalRouting {
		retained := 0
		if reason == "" {
			retained = prefix
			if realignFrom != nil {
				retained = *realignFrom
			}
		}
		extra["echo_last"] = len(prompt) - retained
	}
	temperature := request.Temperature
	if s.defaultTemperature != nil {
		temperature = *s.defaultTemperature
	}
	delete(extra, "temperature")
	delete(extra, "logprobs")
	stop := s.renderer.StopSequences(request)
	if len(prepared) > 0 {
		prepared[0]()
	}
	// A policy call owns the lineage cursor while sampling. Auxiliary calls
	// share affinity and diagnostics but may sample concurrently.
	options := sdk.SampleOptions{MaxTokens: effective, MaxSeqLen: s.options.MaxContextTokens, Temperature: temperature, Logprobs: true, IncludeRoutingMatrix: includeRouting, Stop: stop, Extra: extra, PromptCacheKey: t.servingAffinityKey, AdditionalHeaders: cloneStringMap(s.options.BackendHeaders), SamplingContext: map[string]any{"trajectory_id": t.id, "call_id": callID}}
	result, sampleErr := func() (sdk.SampledRequestResult, error) {
		t.mu.Unlock()
		defer t.mu.Lock()
		return s.sampler.SampleWithPromptTokensResult(ctx, prompt, options)
	}()
	if err := ctx.Err(); err != nil {
		call.Outcome = CallOutcomeCancelled
		call.EndedAt = nowSeconds()
		t.recordCall(call)
		return CallResult{}, operationError(err, "sampling")
	}
	if sampleErr != nil {
		call.Outcome = CallOutcomeFailed
		call.EndedAt = nowSeconds()
		call.ErrorCode = "tito_upstream_error"
		if sampled := new(sdk.SamplingRequestError); errors.As(sampleErr, &sampled) {
			call.ServerAttempts = sampled.ServerAttempts
			call.Attempts = sampled.Attempts
			call.LogicalRequestID = sampled.LogicalRequestID
		}
		t.recordCall(call)
		return CallResult{}, operationError(sampleErr, "sampling")
	}
	if len(result.Completions) != 1 {
		return CallResult{}, &Error{Code: "tito_sampler_contract_error", Status: 502, Message: "policy inference must return exactly one completion", DiscardTrajectory: true}
	}
	completion := result.Completions[0]
	s.traces.record(t.id, "sampled_completion", map[string]any{"completion": completion, "result": result})
	if completion.FinishReason != "stop" && completion.FinishReason != "length" && completion.FinishReason != "tool_calls" {
		return CallResult{}, &Error{Code: "tito_sampler_finish_contract_error", Status: 502, Message: "sampler returned no supported successful finish reason", DiscardTrajectory: true, Origin: "sdk"}
	}
	if completion.PromptLen < 0 || completion.PromptLen > len(completion.FullTokens) {
		return CallResult{}, &Error{Code: "tito_sampler_contract_error", Status: 502, Message: "policy inference returned invalid token boundaries", DiscardTrajectory: true}
	}

	if completion.RoutingMatrices != nil || completion.InferenceTopKLogprobs != nil || completion.InferenceTopKTokenIDs != nil {
		return CallResult{}, &Error{Code: "tito_completion_alignment_error", Status: 502, Message: "inline replay channels are unsupported"}
	}

	var promptRefs *sdk.RoutingReferences
	var promptRoutingStart *int
	if incrementalRouting {
		count := completion.EchoedPromptLogprobCount
		expected := extra["echo_last"].(int)
		if max := completion.PromptLen - 1; expected > max {
			expected = max
		}
		if expected < 0 {
			expected = 0
		}
		if count != expected {
			return CallResult{}, &Error{Code: "tito_completion_alignment_error", Status: 502, Message: "incremental prompt routing length mismatch"}
		}
		start := completion.PromptLen - 1 - count
		promptRoutingStart = &start
		if completion.RoutingReferences == nil || completion.RoutingReferences.Length < count || completion.RoutingReferences.Validate() != nil {
			return CallResult{}, &Error{Code: "tito_completion_alignment_error", Status: 502, Message: "missing or invalid prompt routing references"}
		}
		promptRefs, _ = completion.RoutingReferences.Slice(0, count)
		completion.RoutingReferences, _ = completion.RoutingReferences.Slice(count, completion.RoutingReferences.Length)
		if completion.InferenceLogprobs != nil {
			if len(completion.InferenceLogprobs) < count {
				return CallResult{}, invalidRequest("short inference logprobs")
			}
			completion.InferenceLogprobs = completion.InferenceLogprobs[count:]
		}
		if completion.SamplingLogprobs != nil {
			if len(completion.SamplingLogprobs) < count {
				return CallResult{}, invalidRequest("short sampling logprobs")
			}
			completion.SamplingLogprobs = completion.SamplingLogprobs[count:]
		}

	}
	completionIDs := append([]int(nil), completion.FullTokens[completion.PromptLen:]...)
	if includeMask && (completion.SamplingMaskReferences == nil || completion.SamplingMaskReferences.Length != len(completionIDs)) {
		return CallResult{}, &Error{Code: "tito_completion_alignment_error", Status: 502, Message: "sampling mask references do not align with completion IDs", DiscardTrajectory: true}
	}
	if includeTop && (completion.TopSamplingReferences == nil || completion.TopSamplingReferences.Length != len(completionIDs)) {
		return CallResult{}, &Error{Code: "tito_completion_alignment_error", Status: 502, Message: "top-K references do not align with completion IDs"}
	}
	if !slices.Equal(completion.FullTokens[:completion.PromptLen], prompt) {
		return CallResult{}, &Error{Code: "tito_prompt_mismatch", Status: 502, Message: "sampler changed prepared prompt IDs", DiscardTrajectory: true}
	}
	if len(completionIDs) == 0 || len(completionIDs) != completion.CompletionLen || len(completionIDs) > effective {
		return CallResult{}, &Error{Code: "tito_completion_alignment_error", Status: 502, Message: "invalid completion length"}
	}
	if includeExtra && (completion.SampleExtraReferences == nil || completion.SampleExtraReferences.Length != len(completionIDs)) {
		return CallResult{}, &Error{Code: "tito_completion_alignment_error", Status: 502, Message: "extra-draw references do not align with completion IDs"}
	}
	if err := validateCompletionArrays(completion, len(completionIDs), includeRouting); err != nil {
		return CallResult{}, err
	}
	assistant, parseErr := s.renderer.ParseAssistant(request, completionIDs, completion.Text, completion.FinishReason)
	var malformed *Error
	parserFallback := false
	if parseErr != nil {
		code := "tito_sdk_parser_internal_error"
		context := map[string]any{"call_id": callID, "renderer_id": safeParserTag(s.renderer.RendererID()), "parser_error_type": fmt.Sprintf("%T", parseErr), "finish_reason": completion.FinishReason, "prompt_tokens": completion.PromptLen, "completion_tokens": len(completionIDs), "requested_max_tokens": requested, "max_output_tokens": s.options.MaxOutputTokens}
		var recognized *ParseError
		if errors.As(parseErr, &recognized) {
			code = "tito_sdk_parse_error"
			context["parser_reason_code"] = safeParserTag(recognized.ReasonCode)
			if recognized.ClosedToolAction != nil {
				context["closed_tool_action"] = *recognized.ClosedToolAction
			}
		}
		call.Outcome = CallOutcomeModelMalformed
		call.EndedAt = nowSeconds()
		call.ErrorCode = code
		malformed = &Error{Code: code, Status: 422, Message: "local TITO parser rejected the sampled output; discard this trajectory", Origin: "sdk_parser", DiscardTrajectory: true, Context: context}
		if kind != CallKindPolicy || t.discardError != nil || !retainedReason(context) || source == "fail_closed" || !exactToolSample(completion.FinishReason, completion.SamplingLogprobs, len(completionIDs)) {
			t.recordCall(call)
			return CallResult{}, malformed
		}
		assistant = ParsedAssistant{Message: map[string]any{"role": "assistant", "content": ""}, OutputKind: "malformed_tool_call"}
	}

	if assistant.OutputKind == "" {
		assistant.OutputKind = "text"
	}
	if assistant.ResponseMessage == nil {
		assistant.ResponseMessage = cloneAnyMap(assistant.Message)
	}
	canonicalPayload := map[string]any{"messages": []any{assistant.Message}}
	if canonical, err := NewChatRequestFromOpenAI(canonicalPayload); err == nil {
		assistant.Message = canonical.Messages[0]
	}
	turnID := randomHex(16)
	responseID := result.UpstreamResponseID
	if responseID == "" {
		responseID = "chatcmpl-tito-" + turnID
	}
	turn := Turn{SampleExtraReferences: completion.SampleExtraReferences, SamplingMaskReferences: completion.SamplingMaskReferences, TopSamplingReferences: completion.TopSamplingReferences, RoutingReferences: completion.RoutingReferences, PromptRoutingStart: promptRoutingStart, PromptRoutingReferences: promptRefs, TurnID: turnID, Request: request, Assistant: assistant, ExactPromptIDs: append([]int(nil), prompt...), ExactCompletionIDs: completionIDs, InferenceLogprobs: append([]float64(nil), completion.InferenceLogprobs...), SamplingLogprobs: append([]*float64(nil), completion.SamplingLogprobs...), ResponseID: responseID, FinishReason: completion.FinishReason, PromptDisposition: disposition, RealignedMaskedTokens: realigned, RequestedOutputTokens: requested, EffectiveOutputTokens: effective, ContextRemainingTokens: remaining, ServerMetrics: result.ServerMetrics, SamplerWallSeconds: result.WallSeconds, LogicalRequestID: result.LogicalRequestID, UpstreamResponseID: result.UpstreamResponseID, UpstreamAttempts: result.Attempts, PromptMode: promptMode, IncrementalFallbackReason: fallbackReason, ServerAttempts: result.ServerAttempts, ParserFallback: parserFallback}
	if havePrefix {
		turn.PrefixMatchTokens = &prefix
	}
	turn.RealignFromToken = realignFrom
	if previous != nil {
		turn.ParentTurnID = previous.TurnID
	}
	if incremental != nil {
		turn.IncrementalContractID = incremental.ContractID
		turn.IncrementalJunctionKind = incremental.JunctionKind
		turn.IncrementalCheckpointTrimTokens = incremental.CheckpointTrimTokens
	}
	call.LogicalRequestID = result.LogicalRequestID
	call.UpstreamResponseID = result.UpstreamResponseID
	call.Attempts = result.Attempts
	call.ServerAttempts = result.ServerAttempts
	if s.options.Observer != nil {
		event := "turn_committed"
		payload := map[string]any{"turn_id": turnID, "call_id": callID}
		if kind == CallKindAuxiliary {
			event = "auxiliary_complete"
			delete(payload, "turn_id")
		}
		if _, err := s.options.Observer.Record(event, t.id, payload, map[string][]any{"prompt_ids": intsToAny(prompt), "completion_ids": intsToAny(completionIDs)}); err != nil {
			call.Outcome, call.ErrorCode, call.EndedAt = CallOutcomeFailed, "tito_debug_storage_error", nowSeconds()
			t.recordCall(call)
			if malformed != nil {
				malformed.Context["debug_storage_error"] = "local TITO debug commit failed"
				return CallResult{}, malformed
			}
			return CallResult{}, &Error{Code: call.ErrorCode, Status: 500, Origin: "sdk", Message: "local TITO debug commit failed", DiscardTrajectory: true}
		}
	}
	if kind == CallKindAuxiliary {
		call.Outcome, call.EndedAt = CallOutcomeSucceeded, nowSeconds()
		t.recordCall(call)
		return CallResult{Response: openAIResponse(turn, completion.Text, s.options.Model), Call: call}, nil
	}
	var segmentIndex int
	if reason != "" {
		segmentIndex = len(t.segments)
		t.segments = append(t.segments, SegmentResult{SegmentID: randomHex(16), StartReason: reason, RenderContractID: contract})
	} else {
		segmentIndex = parent.segment
	}
	segment := &t.segments[segmentIndex]
	segment.Turns = append(segment.Turns, turn)
	if malformed != nil {
		segment.ClosedReason = "malformed_tool_call"
		call.TurnID = turnID
		t.metadata["tito_malformed_tool_call_id"] = callID
		t.recordCall(call)
		return CallResult{}, malformed
	}
	t.latestSegment = segmentIndex
	depth := len(request.Messages) + 1
	t.checkpoints[depth] = append(t.checkpoints[depth], turnLocation{segmentIndex, len(segment.Turns) - 1})
	t.metrics.Counters["lineage/"+string(disposition)]++
	if completion.FinishReason == "length" {
		segment.ClosedReason = "length_truncation"
	}
	call.Outcome = CallOutcomeSucceeded
	call.EndedAt = nowSeconds()
	call.TurnID = turnID
	t.recordCall(call)
	t.metrics.Distributions["sampler_wall_seconds"] = t.metrics.Distributions["sampler_wall_seconds"].Add(result.WallSeconds)
	response := openAIResponse(turn, completion.Text, s.options.Model)
	output := CallResult{Response: response, Call: call, TurnID: turnID}
	clear(t.idempotency)
	if idempotencyKey != "" {
		var parentLoc turnLocation
		if parent != nil {
			parentLoc = *parent
		}
		t.idempotency[idempotencyKey] = idempotencyEntry{Incremental: incremental != nil, Parent: parentLoc, Fingerprint: fingerprint, PromptHash: call.PreparedPromptHash, Result: CallResult{Response: deepCloneMap(response), Call: call, TurnID: turnID}}
	}
	return output, nil
}

func openAIResponse(turn Turn, text, model string) map[string]any {
	message := cloneAnyMap(turn.Assistant.ResponseMessage)
	if message == nil {
		message = cloneAnyMap(turn.Assistant.Message)
	}
	if _, ok := message["role"]; !ok {
		message["role"] = "assistant"
	}
	if _, ok := message["content"]; !ok {
		message["content"] = text
	}
	usage := map[string]any{"prompt_tokens": len(turn.ExactPromptIDs), "completion_tokens": len(turn.ExactCompletionIDs), "total_tokens": len(turn.ExactPromptIDs) + len(turn.ExactCompletionIDs)}
	if metrics := turn.ServerMetrics; metrics != nil && metrics.CachedPromptTokens != nil {
		cached := *metrics.CachedPromptTokens
		if cached >= 0 && cached <= len(turn.ExactPromptIDs) {
			usage["prompt_tokens_details"] = map[string]any{"cached_tokens": cached}
		}
	}
	return map[string]any{"id": turn.ResponseID, "object": "chat.completion", "created": int64(time.Now().Unix()), "model": model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": turn.FinishReason}}, "usage": usage}
}

func openAIStreamChunks(response, request map[string]any) []map[string]any {
	id, _ := response["id"].(string)
	model, _ := response["model"].(string)
	created := response["created"]
	base := func() map[string]any {
		return map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model}
	}
	message := map[string]any{"role": "assistant"}
	if choices, ok := response["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if parsed, ok := choice["message"].(map[string]any); ok {
				message, _ = cloneJSONMap(parsed)
			}
		}
	}
	if calls, err := mapSlice(message["tool_calls"]); err == nil && len(calls) > 0 {
		for i := range calls {
			calls[i]["index"] = i
		}
		message["tool_calls"] = anyMapSlice(calls)
	}
	first := base()
	first["choices"] = []any{map[string]any{"index": 0, "delta": message, "finish_reason": nil}}
	finishReason := "stop"
	if choices, ok := response["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if value, ok := choice["finish_reason"].(string); ok {
				finishReason = value
			}
		}
	}
	last := base()
	last["choices"] = []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}}
	chunks := []map[string]any{first, last}
	if options, ok := request["stream_options"].(map[string]any); ok {
		if include, ok := options["include_usage"].(bool); ok && include {
			usage := base()
			usage["choices"] = []any{}
			usage["usage"] = response["usage"]
			chunks = append(chunks, usage)
		}
	}
	return chunks
}

func (t *trajectory) artifact() TrajectoryArtifact {
	return TrajectoryArtifact{LineageMode: "tree", SchemaVersion: 2, TrajectoryID: t.id, ServingAffinityKeyHash: t.affinityHash, Metadata: cloneAnyMap(t.metadata), Status: t.status, TerminalReason: t.terminalReason, Segments: append([]SegmentResult(nil), t.segments...), Calls: append([]CallRecord(nil), t.calls...), ResponseAttempts: append([]ResponseAttempt(nil), t.responseAttempts...), Metrics: t.metrics, StartedAt: t.startedAt, FinishedAt: t.finishedAt}
}
func (t *trajectory) recordCall(call CallRecord) {
	t.calls = append(t.calls, call)
	t.metrics.Counters["calls/total"]++
	t.metrics.Counters["calls/"+string(call.Kind)]++
	t.metrics.Counters["calls/"+string(call.Outcome)]++
}
func (s *Sidecar) trajectoryFor(id string) (*trajectory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.trajectories[id]
	if t == nil {
		return nil, &Error{Code: "tito_trajectory_not_found", Status: 404, Message: "unknown trajectory"}
	}
	return t, nil
}
func (s *Sidecar) authenticate(id, authorization string) (*trajectory, *Error) {
	t, err := s.trajectoryFor(id)
	if err != nil {
		return nil, asTITOError(err)
	}
	if !strings.HasPrefix(authorization, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(authorization, "Bearer ")), []byte(t.credential)) != 1 {
		return nil, &Error{Code: "invalid_api_key", Status: 401, Message: "invalid trajectory credential"}
	}
	return t, nil
}
func requireActive(t *trajectory) *Error {
	if t.discardError != nil {
		return t.discardError
	}
	if t.status != TrajectoryStatusActive {
		return &Error{Code: "tito_trajectory_terminal", Status: 409, Message: "trajectory is already terminal"}
	}
	return nil
}
func (s *Sidecar) recordEmission(t *trajectory, turnID string, emission Emission) {
	if turnID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.responseAttempts = append(t.responseAttempts, ResponseAttempt{AttemptID: randomHex(16), TurnID: turnID, Emission: emission, CreatedAt: nowSeconds()})
}
func historyExtends(stored, incoming []map[string]any) bool {
	if len(incoming) < len(stored) {
		return false
	}
	for i := range stored {
		a, _ := canonicalJSON(stored[i])
		b, _ := canonicalJSON(incoming[i])
		if string(a) != string(b) {
			return false
		}
	}
	return true
}
func commonPrefix(a, b []int) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}
func randomHex(bytesCount int) string {
	data := make([]byte, bytesCount)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)
}
func nowSeconds() float64 { return float64(time.Now().UnixNano()) / 1e9 }
func cloneStringMap(value map[string]string) map[string]string {
	out := make(map[string]string, len(value))
	for k, v := range value {
		out[k] = v
	}
	return out
}
func cloneMessages(value []map[string]any) []map[string]any {
	out := make([]map[string]any, len(value))
	for i := range value {
		out[i] = cloneAnyMap(value[i])
	}
	return out
}
func deepCloneMap(value map[string]any) map[string]any {
	encoded, _ := json.Marshal(value)
	var out map[string]any
	_ = json.Unmarshal(encoded, &out)
	return out
}
func intsToAny(value []int) []any {
	out := make([]any, len(value))
	for i, v := range value {
		out[i] = v
	}
	return out
}
func asTITOError(err error) *Error {
	var typed *Error
	if errors.As(err, &typed) {
		return typed
	}
	return &Error{Code: "tito_sdk_internal_error", Status: 500, Message: "local TITO SDK operation failed", Origin: "sdk", DiscardTrajectory: true}
}
func writeTITOError(w http.ResponseWriter, err *Error) {
	w.Header().Set("X-Should-Retry", strconv.FormatBool(err.ShouldRetry))
	writeJSON(w, err.Status, err.OpenAIBody())
}
func writeJSON(w http.ResponseWriter, status int, value any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(value)
}

func validateCompletionArrays(c sdk.SampledCompletion, n int, routing bool) error {
	fail := func() error {
		return &Error{Code: "tito_completion_alignment_error", Status: 502, Message: "completion arrays do not align with output token IDs"}
	}
	if c.InferenceLogprobs != nil && len(c.InferenceLogprobs) != n {
		return fail()
	}
	if c.SamplingLogprobs != nil && len(c.SamplingLogprobs) != n {
		return fail()
	}
	for _, refs := range []*sdk.RoutingReferences{c.TopSamplingReferences, c.SamplingMaskReferences, c.SampleExtraReferences} {
		if refs != nil && (refs.Length != n || refs.Validate() != nil) {
			return fail()
		}
	}
	if c.RoutingReferences != nil {
		if err := c.RoutingReferences.Validate(); err != nil {
			return fail()
		}
		if c.RoutingReferences.Length != n {
			return fail()
		}
	} else if routing {
		return fail()
	}
	return nil
}

func positiveCount(v any) bool { n := optionalInt(v); return n != nil && *n > 0 }

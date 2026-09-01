package tito

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	MaxContextTokens   int
	MaxOutputTokens    int
	CallClassifier     CallClassifier
	SamplingDefaults   map[string]any
	BackendHeaders     map[string]string
	Observer           EventObserver
	DefaultDriftPolicy TrajectoryDriftPolicy
	PromptMode         PromptMode
	Keepalive          time.Duration
	Model              string
}

type Sidecar struct {
	sampler  ExactTokenSampler
	renderer Renderer
	options  SidecarOptions

	mu           sync.RWMutex
	server       *http.Server
	listener     net.Listener
	baseURL      string
	trajectories map[string]*trajectory
}

type TITOSidecar = Sidecar
type TITOSidecarOptions = SidecarOptions

type trajectory struct {
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
	storedMessages     []map[string]any
	checkpoint         []int
	driftPolicy        TrajectoryDriftPolicy
	idempotency        map[string]idempotencyEntry
}

type idempotencyEntry struct {
	Fingerprint string
	PromptHash  string
	Result      CallResult
}

func NewSidecar(sampler ExactTokenSampler, renderer Renderer, options SidecarOptions) (*Sidecar, error) {
	if sampler == nil || renderer == nil {
		return nil, errors.New("sampler and renderer are required")
	}
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
	if options.Keepalive == 0 {
		options.Keepalive = 5 * time.Second
	}
	return &Sidecar{sampler: sampler, renderer: renderer, options: options, trajectories: make(map[string]*trajectory)}, nil
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
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(requested))
	if err != nil {
		return err
	}
	s.listener = listener
	s.baseURL = "http://" + listener.Addr().String()
	s.server = &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: s.options.Keepalive, MaxHeaderBytes: 1 << 20}
	go func() { _ = s.server.Serve(listener) }()
	return nil
}

func (s *Sidecar) Close(ctx context.Context) error {
	s.mu.Lock()
	server := s.server
	s.server = nil
	s.listener = nil
	s.baseURL = ""
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
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
	t := &trajectory{id: trajectoryID, credential: credential, affinityHash: hashJSON(servingAffinityKey), servingAffinityKey: servingAffinityKey, metadata: cloneAnyMap(metadata), status: TrajectoryStatusActive, metrics: MetricSummary{Counters: make(map[string]int), Distributions: make(map[string]Distribution)}, startedAt: nowSeconds(), driftPolicy: drift, idempotency: make(map[string]idempotencyEntry)}
	s.trajectories[trajectoryID] = t
	if s.options.Observer != nil {
		_, _ = s.options.Observer.Record("trajectory_created", trajectoryID, map[string]any{"metadata": metadata}, nil)
	}
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

func (s *Sidecar) ObserveAgentWall(id string, seconds float64) error {
	t, err := s.trajectoryFor(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	d := t.metrics.Distributions["agent_wall_seconds"].Add(seconds)
	t.metrics.Distributions["agent_wall_seconds"] = d
	return nil
}

func (s *Sidecar) terminate(id string, status TrajectoryStatus, reason string) (TrajectoryArtifact, error) {
	t, err := s.trajectoryFor(id)
	if err != nil {
		return TrajectoryArtifact{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.status == TrajectoryStatusActive {
		t.status = status
		t.terminalReason = reason
		t.finishedAt = nowSeconds()
		if len(t.segments) > 0 && t.segments[len(t.segments)-1].ClosedReason == "" {
			t.segments[len(t.segments)-1].ClosedReason = string(status)
		}
	}
	artifact := t.artifact()
	if s.options.Observer != nil {
		_, _ = s.options.Observer.CloseTrajectory(id, t.status, map[string]any{"terminal_reason": t.terminalReason})
	}
	return artifact, nil
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
	result, err := s.complete(r.Context(), t, request, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeTITOError(w, asTITOError(err))
		return
	}
	stream, _ := payload["stream"].(bool)
	if !stream {
		writeJSON(w, http.StatusOK, result.Response)
		s.recordEmission(t, result.TurnID, EmissionCompleted)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
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

func (s *Sidecar) complete(ctx context.Context, t *trajectory, request ChatRequest, idempotencyKey string) (CallResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := requireActive(t); err != nil {
		return CallResult{}, err
	}
	started := nowSeconds()
	callID := randomHex(16)
	kind, source := CallKindPolicy, "default"
	if s.options.CallClassifier != nil {
		kind, source = s.options.CallClassifier(request)
	}
	fingerprint := hashJSON(request.CanonicalValue())
	call := CallRecord{CallID: callID, Kind: kind, ClassificationSource: source, StartedAt: started, RequestFingerprint: fingerprint}
	if idempotencyKey != "" {
		if prior, ok := t.idempotency[idempotencyKey]; ok {
			retryPrompt, renderErr := s.renderer.RenderConversationTokens(request)
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
			return result, nil
		}
	}
	if kind != CallKindPolicy {
		call.Outcome = CallOutcomeRejected
		call.EndedAt = nowSeconds()
		call.ErrorCode = "tito_auxiliary_unsupported"
		t.recordCall(call)
		return CallResult{}, &Error{Code: call.ErrorCode, Status: 400, Message: "auxiliary calls require an application-provided handler"}
	}
	if !historyExtends(t.storedMessages, request.Messages) {
		call.Outcome = CallOutcomeRejected
		call.EndedAt = nowSeconds()
		call.ErrorCode = "tito_history_rewrite"
		t.recordCall(call)
		return CallResult{}, &Error{Code: call.ErrorCode, Status: 409, Message: "request rewrites committed conversation history"}
	}

	promptMode := s.options.PromptMode
	disposition := PromptDispositionAppend
	prefix := 0
	realigned := 0
	var incremental *IncrementalPrompt
	fallbackReason := ""
	if promptMode == PromptModeIncremental && len(t.checkpoint) > 0 {
		incRenderer := s.renderer.(IncrementalRenderer)
		inc, incErr := incRenderer.PrepareIncrementalPrompt(request, t.storedMessages, request.Messages[len(t.storedMessages):], append([]int(nil), t.checkpoint...))
		if incErr != nil {
			return CallResult{}, incErr
		}
		if inc != nil {
			if err := inc.Validate(); err != nil {
				return CallResult{}, err
			}
			incremental = inc
		}
		if inc == nil {
			promptMode = PromptModeFullHistory
			fallbackReason = "renderer_declined"
		}
	}
	var prompt []int
	if incremental != nil {
		prompt = append([]int(nil), incremental.PromptIDs...)
	} else {
		var renderErr error
		prompt, renderErr = s.renderer.RenderConversationTokens(request)
		if renderErr != nil {
			return CallResult{}, renderErr
		}
	}
	call.PreparedPromptHash = hashTokens(prompt)
	if len(t.checkpoint) > 0 && incremental == nil {
		prefix = commonPrefix(t.checkpoint, prompt)
		if prefix < len(t.checkpoint) {
			realigned = len(t.checkpoint) - prefix
			if realigned <= t.driftPolicy.MaxMaskedTokens {
				disposition = PromptDispositionRealign
			} else if t.driftPolicy.OnOtherMismatch == "new_segment" {
				disposition = PromptDispositionNewSegment
			} else {
				call.Outcome = CallOutcomeRejected
				call.EndedAt = nowSeconds()
				call.ErrorCode = "tito_prompt_drift"
				t.recordCall(call)
				return CallResult{}, &Error{Code: call.ErrorCode, Status: 409, Message: "rendered prompt drift exceeds trajectory policy"}
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
	extra := cloneAnyMap(s.options.SamplingDefaults)
	for key, value := range request.SamplingFields {
		extra[key] = value
	}
	delete(extra, "temperature")
	stop := s.renderer.StopSequences(request)
	result, sampleErr := s.sampler.SampleWithPromptTokensResult(ctx, prompt, sdk.SampleOptions{MaxTokens: effective, MaxSeqLen: s.options.MaxContextTokens, Temperature: request.Temperature, Logprobs: true, IncludeRoutingMatrix: true, Stop: stop, Extra: extra, PromptCacheKey: t.servingAffinityKey, AdditionalHeaders: cloneStringMap(s.options.BackendHeaders), SamplingContext: map[string]any{"trajectory_id": t.id, "call_id": callID}})
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
		return CallResult{}, &Error{Code: call.ErrorCode, Status: 502, Message: "policy inference failed before commit", ShouldRetry: true}
	}
	if len(result.Completions) == 0 {
		return CallResult{}, &Error{Code: "tito_upstream_error", Status: 502, Message: "policy inference returned no completion", ShouldRetry: true}
	}
	completion := result.Completions[0]
	if completion.PromptLen < 0 || completion.PromptLen > len(completion.FullTokens) {
		return CallResult{}, &Error{Code: "tito_upstream_error", Status: 502, Message: "policy inference returned invalid token boundaries", ShouldRetry: true}
	}
	completionIDs := append([]int(nil), completion.FullTokens[completion.PromptLen:]...)
	assistant, parseErr := s.renderer.ParseAssistant(request, completionIDs, completion.Text, completion.FinishReason)
	parserFallback := false
	if parseErr != nil {
		fallback := s.renderer.FallbackAssistantText(request, completionIDs, completion.FinishReason, parseErr)
		if fallback == nil {
			call.Outcome = CallOutcomeModelMalformed
			call.EndedAt = nowSeconds()
			call.ErrorCode = "tito_model_malformed"
			t.recordCall(call)
			return CallResult{}, &Error{Code: call.ErrorCode, Status: 422, Message: "model completion could not be parsed"}
		}
		assistant = ParsedAssistant{Message: map[string]any{"role": "assistant", "content": *fallback}, OutputKind: "text", ParserFallback: true}
		parserFallback = true
	}
	if assistant.OutputKind == "" {
		assistant.OutputKind = "text"
	}
	turnID := randomHex(16)
	responseID := result.UpstreamResponseID
	if responseID == "" {
		responseID = "chatcmpl-tito-" + turnID
	}
	turn := Turn{TurnID: turnID, Request: request, Assistant: assistant, ExactPromptIDs: append([]int(nil), prompt...), ExactCompletionIDs: completionIDs, InferenceLogprobs: append([]float64(nil), completion.InferenceLogprobs...), SamplingLogprobs: append([]*float64(nil), completion.SamplingLogprobs...), RoutingMatrices: append([]string(nil), completion.RoutingMatrices...), ResponseID: responseID, FinishReason: completion.FinishReason, PromptDisposition: disposition, RealignedMaskedTokens: realigned, RequestedOutputTokens: requested, EffectiveOutputTokens: effective, ContextRemainingTokens: remaining, ServerMetrics: result.ServerMetrics, SamplerWallSeconds: result.WallSeconds, LogicalRequestID: result.LogicalRequestID, UpstreamResponseID: result.UpstreamResponseID, UpstreamAttempts: result.Attempts, PromptMode: promptMode, IncrementalFallbackReason: fallbackReason, ServerAttempts: result.ServerAttempts, ParserFallback: parserFallback}
	if len(t.checkpoint) > 0 {
		p := prefix
		turn.PrefixMatchTokens = &p
	}
	if realigned > 0 {
		p := prefix
		turn.RealignFromToken = &p
	}
	if incremental != nil {
		turn.IncrementalContractID = incremental.ContractID
		turn.IncrementalJunctionKind = incremental.JunctionKind
		turn.IncrementalCheckpointTrimTokens = incremental.CheckpointTrimTokens
	}
	contract := s.renderer.RenderContractID(request)
	if contract == "" {
		contract = s.renderer.RendererID()
	}
	if len(t.segments) == 0 || disposition == PromptDispositionNewSegment {
		if len(t.segments) > 0 {
			t.segments[len(t.segments)-1].ClosedReason = "prompt_drift"
		}
		reason := "trajectory_started"
		if len(t.segments) > 0 {
			reason = "prompt_drift"
		}
		t.segments = append(t.segments, SegmentResult{SegmentID: randomHex(16), StartReason: reason, RenderContractID: contract})
	}
	t.segments[len(t.segments)-1].Turns = append(t.segments[len(t.segments)-1].Turns, turn)
	t.storedMessages = cloneMessages(request.Messages)
	t.storedMessages = append(t.storedMessages, cloneAnyMap(assistant.Message))
	t.checkpoint = turn.ExactCheckpointIDs()
	call.Outcome = CallOutcomeSucceeded
	call.EndedAt = nowSeconds()
	call.TurnID = turnID
	call.LogicalRequestID = result.LogicalRequestID
	call.UpstreamResponseID = result.UpstreamResponseID
	call.Attempts = result.Attempts
	call.ServerAttempts = result.ServerAttempts
	t.recordCall(call)
	t.metrics.Distributions["sampler_wall_seconds"] = t.metrics.Distributions["sampler_wall_seconds"].Add(result.WallSeconds)
	response := openAIResponse(turn, completion.Text, s.options.Model)
	output := CallResult{Response: response, Call: call, TurnID: turnID}
	if idempotencyKey != "" {
		t.idempotency[idempotencyKey] = idempotencyEntry{Fingerprint: fingerprint, PromptHash: call.PreparedPromptHash, Result: CallResult{Response: deepCloneMap(response), Call: call, TurnID: turnID}}
	}
	if s.options.Observer != nil {
		_, _ = s.options.Observer.Record("turn_committed", t.id, map[string]any{"turn_id": turnID, "call_id": callID}, map[string][]any{"prompt_ids": intsToAny(prompt), "completion_ids": intsToAny(completionIDs)})
	}
	return output, nil
}

func openAIResponse(turn Turn, text, model string) map[string]any {
	message := cloneAnyMap(turn.Assistant.Message)
	if _, ok := message["role"]; !ok {
		message["role"] = "assistant"
	}
	if _, ok := message["content"]; !ok {
		message["content"] = text
	}
	return map[string]any{"id": turn.ResponseID, "object": "chat.completion", "created": int64(time.Now().Unix()), "model": model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": turn.FinishReason}}, "usage": map[string]any{"prompt_tokens": len(turn.ExactPromptIDs), "completion_tokens": len(turn.ExactCompletionIDs), "total_tokens": len(turn.ExactPromptIDs) + len(turn.ExactCompletionIDs)}}
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
				message = cloneAnyMap(parsed)
			}
		}
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
	return TrajectoryArtifact{SchemaVersion: 1, TrajectoryID: t.id, ServingAffinityKeyHash: t.affinityHash, Metadata: cloneAnyMap(t.metadata), Status: t.status, TerminalReason: t.terminalReason, Segments: append([]SegmentResult(nil), t.segments...), Calls: append([]CallRecord(nil), t.calls...), ResponseAttempts: append([]ResponseAttempt(nil), t.responseAttempts...), Metrics: t.metrics, StartedAt: t.startedAt, FinishedAt: t.finishedAt}
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
	return &Error{Code: "tito_internal_error", Status: 500, Message: err.Error()}
}
func writeTITOError(w http.ResponseWriter, err *Error) {
	w.Header().Set("X-Should-Retry", strconv.FormatBool(err.ShouldRetry))
	writeJSON(w, err.Status, err.OpenAIBody())
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

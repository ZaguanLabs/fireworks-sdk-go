// Package tito provides exact-token, multi-turn rollout contracts and an
// environment-local OpenAI-compatible sidecar.
package tito

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	sdk "github.com/ZaguanLabs/fireworks-sdk-go/training/sdk"
)

type CallKind string
type CallOutcome string
type Emission string
type TrajectoryStatus string
type PromptDisposition string
type PromptMode string

const (
	CallKindPolicy    CallKind = "policy"
	CallKindAuxiliary CallKind = "auxiliary"

	CallOutcomeSucceeded      CallOutcome = "succeeded"
	CallOutcomeReplayed       CallOutcome = "replayed"
	CallOutcomeModelMalformed CallOutcome = "model_malformed"
	CallOutcomeRejected       CallOutcome = "rejected"
	CallOutcomeFailed         CallOutcome = "failed"
	CallOutcomeCancelled      CallOutcome = "cancelled"

	EmissionCompleted Emission = "completed"
	EmissionAmbiguous Emission = "ambiguous"

	TrajectoryStatusActive    TrajectoryStatus = "active"
	TrajectoryStatusCompleted TrajectoryStatus = "completed"
	TrajectoryStatusAbandoned TrajectoryStatus = "abandoned"
	TrajectoryStatusFailed    TrajectoryStatus = "failed"

	PromptDispositionAppend     PromptDisposition = "append"
	PromptDispositionRealign    PromptDisposition = "realign"
	PromptDispositionNewSegment PromptDisposition = "new_segment"

	PromptModeFullHistory PromptMode = "full_history"
	PromptModeIncremental PromptMode = "incremental"
)

// Error is an OpenAI-compatible TITO error.
type Error struct {
	Code        string
	Status      int
	Message     string
	ShouldRetry bool
}

func (e *Error) Error() string { return e.Message }

func (e *Error) OpenAIBody() map[string]any {
	return map[string]any{"error": map[string]any{
		"message": e.Message,
		"type":    "invalid_request_error",
		"code":    e.Code,
	}}
}

func invalidRequest(message string) *Error {
	return &Error{Code: "tito_invalid_request", Status: 400, Message: message}
}

// NormalizeOpenAIToolArguments canonicalizes JSON-equivalent tool arguments.
// Invalid JSON strings are retained verbatim so the renderer remains
// authoritative about malformed model output.
func NormalizeOpenAIToolArguments(value any) string {
	if raw, ok := value.(string); ok {
		var decoded any
		if json.Unmarshal([]byte(raw), &decoded) != nil {
			return raw
		}
		value = decoded
	}
	encoded, err := canonicalJSON(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

type ParsedAssistant struct {
	Message        map[string]any `json:"message"`
	OutputKind     string         `json:"output_kind"`
	ParserFallback bool           `json:"parser_fallback"`
}

type ChatRequest struct {
	Messages           []map[string]any `json:"messages"`
	Tools              []map[string]any `json:"tools,omitempty"`
	Model              string           `json:"model"`
	MaxTokens          *int             `json:"max_tokens,omitempty"`
	Temperature        float64          `json:"temperature"`
	SamplingFields     map[string]any   `json:"sampling_fields,omitempty"`
	AdapterMetadata    map[string]any   `json:"adapter_metadata,omitempty"`
	WireRequest        map[string]any   `json:"wire_request,omitempty"`
	WireRequestBody    string           `json:"wire_request_body,omitempty"`
	NormalizationSteps []string         `json:"normalization_steps,omitempty"`
}

func NewChatRequestFromOpenAI(payload map[string]any, wireBody ...string) (ChatRequest, error) {
	if choice, ok := payload["tool_choice"]; ok && choice != nil && choice != "auto" {
		return ChatRequest{}, invalidRequest("TITO currently supports only the default or tool_choice='auto'")
	}
	if parallel, ok := payload["parallel_tool_calls"].(bool); ok && !parallel {
		return ChatRequest{}, invalidRequest("TITO currently supports only the default parallel tool-call policy")
	}
	if store, ok := payload["store"].(bool); ok && store {
		return ChatRequest{}, invalidRequest("TITO does not support server-side response storage")
	}

	wire, err := cloneJSONMap(payload)
	if err != nil {
		return ChatRequest{}, invalidRequest("request contains a non-JSON value: " + err.Error())
	}
	messages, err := mapSlice(payload["messages"])
	if err != nil || len(messages) == 0 {
		return ChatRequest{}, invalidRequest("messages must not be empty")
	}
	steps := make([]string, 0)
	for i := range messages {
		role, ok := messages[i]["role"].(string)
		if !ok {
			return ChatRequest{}, invalidRequest("every message requires a string role")
		}
		if role != "assistant" {
			continue
		}
		calls, _ := mapSlice(messages[i]["tool_calls"])
		if len(calls) == 0 {
			continue
		}
		if content, exists := messages[i]["content"]; !exists || content == nil {
			messages[i]["content"] = ""
			steps = append(steps, fmt.Sprintf("messages[%d].content:null_to_empty", i))
		} else if text, ok := content.(string); ok && text != "" && strings.TrimSpace(text) == "" {
			messages[i]["content"] = ""
			steps = append(steps, fmt.Sprintf("messages[%d].content:whitespace_to_empty", i))
		}
		for j := range calls {
			fn, _ := calls[j]["function"].(map[string]any)
			if fn == nil {
				continue
			}
			before := fn["arguments"]
			after := NormalizeOpenAIToolArguments(before)
			fn["arguments"] = after
			if fmt.Sprint(before) != after {
				steps = append(steps, fmt.Sprintf("messages[%d].tool_calls[%d].function.arguments:canonical_json", i, j))
			}
		}
		messages[i]["tool_calls"] = anyMapSlice(calls)
	}
	tools, err := mapSlice(payload["tools"])
	if err != nil {
		return ChatRequest{}, invalidRequest("tools must be an array of objects")
	}

	maxTokens := optionalInt(payload["max_completion_tokens"])
	if maxTokens == nil {
		maxTokens = optionalInt(payload["max_tokens"])
	}
	if maxTokens != nil && *maxTokens < 1 {
		return ChatRequest{}, invalidRequest("max_tokens must be positive")
	}
	temperature := 1.0
	if raw, ok := payload["temperature"].(float64); ok {
		temperature = raw
	}
	model, _ := payload["model"].(string)
	if model == "" {
		model = "policy"
	}
	known := map[string]bool{"messages": true, "tools": true, "model": true, "max_tokens": true, "max_completion_tokens": true, "temperature": true, "stream": true, "stream_options": true, "tool_choice": true, "parallel_tool_calls": true, "store": true, "_tito": true}
	sampling := make(map[string]any)
	for key, value := range payload {
		if !known[key] {
			sampling[key] = value
		}
	}
	metadata, _ := payload["_tito"].(map[string]any)
	request := ChatRequest{Messages: messages, Tools: tools, Model: model, MaxTokens: maxTokens, Temperature: temperature, SamplingFields: sampling, AdapterMetadata: metadata, WireRequest: wire, NormalizationSteps: steps}
	if len(wireBody) > 0 {
		request.WireRequestBody = wireBody[0]
	}
	return request, nil
}

func (r ChatRequest) CanonicalValue() map[string]any {
	return map[string]any{"messages": anyMapSlice(r.Messages), "tools": anyMapSlice(r.Tools), "model": r.Model}
}

func (r ChatRequest) SamplingValue() map[string]any {
	result := cloneAnyMap(r.SamplingFields)
	result["max_tokens"] = r.MaxTokens
	result["temperature"] = r.Temperature
	return result
}

type Renderer interface {
	RendererID() string
	RenderConversationTokens(ChatRequest) ([]int, error)
	ParseAssistant(ChatRequest, []int, string, string) (ParsedAssistant, error)
	FallbackAssistantText(ChatRequest, []int, string, error) *string
	RenderContractID(ChatRequest) string
	StopSequences(ChatRequest) []string
}

type IncrementalPrompt struct {
	PromptIDs            []int  `json:"prompt_ids"`
	ContractID           string `json:"contract_id"`
	JunctionKind         string `json:"junction_kind"`
	CheckpointTrimTokens int    `json:"checkpoint_trim_tokens"`
}

func (p IncrementalPrompt) Validate() error {
	if len(p.PromptIDs) == 0 || p.ContractID == "" || p.JunctionKind == "" || p.CheckpointTrimTokens < 0 {
		return errors.New("invalid incremental prompt")
	}
	return nil
}

type IncrementalRenderer interface {
	PrepareIncrementalPrompt(ChatRequest, []map[string]any, []map[string]any, []int) (*IncrementalPrompt, error)
}

type EventObserver interface {
	Record(event, trajectoryID string, payload map[string]any, arrays map[string][]any) (int, error)
	CloseTrajectory(trajectoryID string, status TrajectoryStatus, payload map[string]any) (int, error)
	RecordTombstoneEvent(event, trajectoryID string, payload map[string]any) (int, error)
}

type TrajectoryDriftPolicy struct {
	MaxMaskedTokens int    `json:"max_masked_tokens"`
	OnOtherMismatch string `json:"on_other_mismatch"`
}

func DefaultTrajectoryDriftPolicy() TrajectoryDriftPolicy {
	return TrajectoryDriftPolicy{MaxMaskedTokens: 1024, OnOtherMismatch: "new_segment"}
}

func (p TrajectoryDriftPolicy) Validate() error {
	if p.MaxMaskedTokens < 0 || (p.OnOtherMismatch != "new_segment" && p.OnOtherMismatch != "reject") {
		return errors.New("invalid trajectory drift policy")
	}
	return nil
}

type Distribution struct {
	Count int      `json:"count"`
	Sum   float64  `json:"sum"`
	Min   *float64 `json:"min"`
	Max   *float64 `json:"max"`
}

func (d Distribution) Add(value float64) Distribution {
	d.Count++
	d.Sum += value
	if d.Min == nil || value < *d.Min {
		v := value
		d.Min = &v
	}
	if d.Max == nil || value > *d.Max {
		v := value
		d.Max = &v
	}
	return d
}

func (d Distribution) Merge(other Distribution) Distribution {
	if other.Count == 0 {
		return d
	}
	if d.Count == 0 {
		return other
	}
	d.Count += other.Count
	d.Sum += other.Sum
	if other.Min != nil && (d.Min == nil || *other.Min < *d.Min) {
		v := *other.Min
		d.Min = &v
	}
	if other.Max != nil && (d.Max == nil || *other.Max > *d.Max) {
		v := *other.Max
		d.Max = &v
	}
	return d
}

type MetricSummary struct {
	Counters      map[string]int          `json:"counters"`
	Distributions map[string]Distribution `json:"distributions"`
}

func (m MetricSummary) Flattened(root ...string) map[string]float64 {
	prefix := "tito"
	if len(root) > 0 {
		prefix = root[0]
	}
	out := make(map[string]float64)
	for name, value := range m.Counters {
		out[prefix+"/"+name] = float64(value)
	}
	for name, dist := range m.Distributions {
		key := prefix + "/" + name
		out[key+"_count"] = float64(dist.Count)
		out[key+"_sum"] = dist.Sum
		if dist.Count > 0 {
			out[key+"_mean"] = dist.Sum / float64(dist.Count)
			out[key+"_min"] = *dist.Min
			out[key+"_max"] = *dist.Max
		}
	}
	return out
}

type ResponseAttempt struct {
	AttemptID string   `json:"attempt_id"`
	TurnID    string   `json:"turn_id"`
	Emission  Emission `json:"emission"`
	CreatedAt float64  `json:"created_at"`
}
type CallRecord struct {
	CallID               string                     `json:"call_id"`
	Kind                 CallKind                   `json:"kind"`
	ClassificationSource string                     `json:"classification_source"`
	Outcome              CallOutcome                `json:"outcome"`
	StartedAt            float64                    `json:"started_at"`
	EndedAt              float64                    `json:"ended_at"`
	RequestFingerprint   string                     `json:"request_fingerprint,omitempty"`
	PreparedPromptHash   string                     `json:"prepared_prompt_hash,omitempty"`
	TurnID               string                     `json:"turn_id,omitempty"`
	LogicalRequestID     string                     `json:"logical_request_id,omitempty"`
	UpstreamResponseID   string                     `json:"upstream_response_id,omitempty"`
	Attempts             int                        `json:"attempts"`
	ServerAttempts       []sdk.SampledServerAttempt `json:"server_attempts,omitempty"`
	ErrorCode            string                     `json:"error_code,omitempty"`
}
type Turn struct {
	TurnID                          string                     `json:"turn_id"`
	Request                         ChatRequest                `json:"request"`
	Assistant                       ParsedAssistant            `json:"assistant"`
	ExactPromptIDs                  []int                      `json:"exact_prompt_ids"`
	ExactCompletionIDs              []int                      `json:"exact_completion_ids"`
	InferenceLogprobs               []float64                  `json:"inference_logprobs,omitempty"`
	SamplingLogprobs                []*float64                 `json:"sampling_logprobs,omitempty"`
	RoutingMatrices                 []string                   `json:"routing_matrices,omitempty"`
	ResponseID                      string                     `json:"response_id"`
	FinishReason                    string                     `json:"finish_reason"`
	PromptDisposition               PromptDisposition          `json:"prompt_disposition"`
	PrefixMatchTokens               *int                       `json:"prefix_match_tokens,omitempty"`
	RealignFromToken                *int                       `json:"realign_from_token,omitempty"`
	RealignedMaskedTokens           int                        `json:"realigned_masked_tokens"`
	RequestedOutputTokens           int                        `json:"requested_output_tokens"`
	EffectiveOutputTokens           int                        `json:"effective_output_tokens"`
	ContextRemainingTokens          int                        `json:"context_remaining_tokens"`
	ServerMetrics                   *sdk.ServerMetrics         `json:"server_metrics,omitempty"`
	SamplerWallSeconds              float64                    `json:"sampler_wall_seconds"`
	LogicalRequestID                string                     `json:"logical_request_id"`
	UpstreamResponseID              string                     `json:"upstream_response_id,omitempty"`
	UpstreamAttempts                int                        `json:"upstream_attempts"`
	PromptMode                      PromptMode                 `json:"prompt_mode"`
	IncrementalContractID           string                     `json:"incremental_contract_id,omitempty"`
	IncrementalJunctionKind         string                     `json:"incremental_junction_kind,omitempty"`
	IncrementalCheckpointTrimTokens int                        `json:"incremental_checkpoint_trim_tokens"`
	IncrementalFallbackReason       string                     `json:"incremental_fallback_reason,omitempty"`
	ServerAttempts                  []sdk.SampledServerAttempt `json:"server_attempts,omitempty"`
	ParserFallback                  bool                       `json:"parser_fallback"`
}

func (t Turn) ExactCheckpointIDs() []int {
	out := append([]int(nil), t.ExactPromptIDs...)
	return append(out, t.ExactCompletionIDs...)
}

type SegmentResult struct {
	SegmentID        string `json:"segment_id"`
	StartReason      string `json:"start_reason"`
	RenderContractID string `json:"render_contract_id"`
	Turns            []Turn `json:"turns"`
	ClosedReason     string `json:"closed_reason,omitempty"`
}
type TrajectoryEndpoint struct {
	TrajectoryID  string `json:"trajectory_id"`
	OpenAIBaseURL string `json:"openai_base_url"`
	APIKey        string `json:"api_key"`
}
type TrajectoryArtifact struct {
	SchemaVersion          int               `json:"schema_version"`
	TrajectoryID           string            `json:"trajectory_id"`
	ServingAffinityKeyHash string            `json:"serving_affinity_key_hash"`
	Metadata               map[string]any    `json:"metadata"`
	Status                 TrajectoryStatus  `json:"status"`
	TerminalReason         string            `json:"terminal_reason,omitempty"`
	Segments               []SegmentResult   `json:"segments"`
	Calls                  []CallRecord      `json:"calls"`
	ResponseAttempts       []ResponseAttempt `json:"response_attempts"`
	Metrics                MetricSummary     `json:"metrics"`
	StartedAt              float64           `json:"started_at"`
	FinishedAt             float64           `json:"finished_at"`
}
type CallResult struct {
	Response map[string]any
	Call     CallRecord
	TurnID   string
	Replayed bool
}

// Python-parity aliases retain the upstream public names while the shorter
// forms remain idiomatic inside package tito.
type TITOCallKind = CallKind
type TITOCallOutcome = CallOutcome
type TITOEmission = Emission
type TITOTrajectoryStatus = TrajectoryStatus
type TITOPromptDisposition = PromptDisposition
type TITOPromptMode = PromptMode
type TITOError = Error
type TITOParsedAssistant = ParsedAssistant
type TITOChatRequest = ChatRequest
type TITORenderer = Renderer
type TITOIncrementalPrompt = IncrementalPrompt
type TITOIncrementalRenderer = IncrementalRenderer
type TITOEventObserver = EventObserver
type TITODistribution = Distribution
type TITOMetricSummary = MetricSummary
type TITOResponseAttempt = ResponseAttempt
type TITOCallRecord = CallRecord
type TITOTurn = Turn
type TITOSegmentResult = SegmentResult
type TITOTrajectoryEndpoint = TrajectoryEndpoint
type TITOTrajectoryArtifact = TrajectoryArtifact
type TITOCallResult = CallResult

func hashJSON(value any) string {
	data, _ := canonicalJSON(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func hashTokens(value []int) string { return hashJSON(value) }

func canonicalJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanonicalJSON(&buf, value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonicalJSON(buf *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			buf.Write(encoded)
			buf.WriteByte(':')
			if err := writeCanonicalJSON(buf, typed[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case []any:
		buf.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonicalJSON(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	default:
		var encodedBuffer bytes.Buffer
		encoder := json.NewEncoder(&encodedBuffer)
		encoder.SetEscapeHTML(false)
		err := encoder.Encode(value)
		if err != nil {
			return err
		}
		encoded := bytes.TrimSuffix(encodedBuffer.Bytes(), []byte{'\n'})
		buf.Write(encoded)
		return nil
	}
}

func cloneJSONMap(value map[string]any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(data, &out)
	return out, err
}
func cloneAnyMap(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for k, v := range value {
		out[k] = v
	}
	return out
}
func optionalInt(value any) *int {
	if n, ok := value.(float64); ok {
		v := int(n)
		return &v
	}
	if n, ok := value.(int); ok {
		v := n
		return &v
	}
	return nil
}
func mapSlice(value any) ([]map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]map[string]any); ok {
			return typed, nil
		}
		return nil, errors.New("not an array")
	}
	out := make([]map[string]any, len(raw))
	for i, item := range raw {
		mapped, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("array item is not an object")
		}
		out[i] = mapped
	}
	return out, nil
}
func anyMapSlice(value []map[string]any) []any {
	out := make([]any, len(value))
	for i := range value {
		out[i] = value[i]
	}
	return out
}

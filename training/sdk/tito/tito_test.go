package tito

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/ZaguanLabs/fireworks-sdk-go/training/sdk"
)

type fakeSampler struct{ calls int }

func (s *fakeSampler) SampleWithPromptTokensResult(_ context.Context, prompt []int, options ...sdk.SampleOptions) (sdk.SampledRequestResult, error) {
	s.calls++
	full := append(append([]int(nil), prompt...), 90+s.calls)
	status := 200
	return sdk.SampledRequestResult{
		Completions:   []sdk.SampledCompletion{{Text: "answer", FullTokens: full, PromptLen: len(prompt), CompletionLen: 1, FinishReason: "stop", InferenceLogprobs: []float64{-0.1}}},
		ServerMetrics: &sdk.ServerMetrics{HTTPStatusCode: &status}, LogicalRequestID: "logical", Attempts: 1, WallSeconds: 0.25, UpstreamResponseID: "upstream",
	}, nil
}

type fakeRenderer struct{}

func (fakeRenderer) RendererID() string { return "fake-v1" }
func (fakeRenderer) RenderConversationTokens(request ChatRequest) ([]int, error) {
	result := []int{1}
	for range request.Messages {
		result = append(result, 2)
	}
	return result, nil
}
func (fakeRenderer) ParseAssistant(_ ChatRequest, ids []int, text, _ string) (ParsedAssistant, error) {
	if len(ids) != 1 {
		return ParsedAssistant{}, invalidRequest("wrong completion boundary")
	}
	return ParsedAssistant{Message: map[string]any{"role": "assistant", "content": text}, OutputKind: "text"}, nil
}
func (fakeRenderer) FallbackAssistantText(ChatRequest, []int, string, error) *string { return nil }
func (fakeRenderer) RenderContractID(ChatRequest) string                             { return "fake-v1" }
func (fakeRenderer) StopSequences(ChatRequest) []string                              { return []string{"stop"} }

func TestNormalizeOpenAIToolArguments(t *testing.T) {
	if got := NormalizeOpenAIToolArguments(`{"b":2, "a":1}`); got != `{"a":1,"b":2}` {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeOpenAIToolArguments(`{broken`); got != `{broken` {
		t.Fatalf("invalid JSON changed: %q", got)
	}
}

func TestArtifactRoundTripIsDeterministic(t *testing.T) {
	artifact := TrajectoryArtifact{TrajectoryID: "t", ServingAffinityKeyHash: "hash", Metadata: map[string]any{"b": 2, "a": 1}, Status: TrajectoryStatusCompleted, Segments: []SegmentResult{}, Calls: []CallRecord{}, ResponseAttempts: []ResponseAttempt{}, Metrics: MetricSummary{Counters: map[string]int{}, Distributions: map[string]Distribution{}}, StartedAt: 1, FinishedAt: 2}
	one, err := artifact.Pack()
	if err != nil {
		t.Fatal(err)
	}
	two, err := artifact.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one, two) {
		t.Fatal("artifact encoding is not deterministic")
	}
	decoded, err := UnpackTrajectoryArtifact(one)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 1 || decoded.TrajectoryID != "t" || decoded.Metadata["a"].(float64) != 1 {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestSidecarOpenAIFlow(t *testing.T) {
	sampler := &fakeSampler{}
	sidecar, err := NewSidecar(sampler, fakeRenderer{}, SidecarOptions{MaxContextTokens: 32, MaxOutputTokens: 4, Model: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sidecar.Start(); err != nil {
		t.Fatal(err)
	}
	defer sidecar.Close(context.Background())
	endpoint, err := sidecar.CreateTrajectory("trajectory-1", "affinity", map[string]any{"task": "test"})
	if err != nil {
		t.Fatal(err)
	}

	request := map[string]any{"model": "policy", "messages": []any{map[string]any{"role": "user", "content": "hello"}}, "max_completion_tokens": 2}
	response := postChat(t, endpoint, request, "idem-1")
	if response["id"] != "upstream" {
		t.Fatalf("response = %#v", response)
	}
	_ = postChat(t, endpoint, request, "idem-1")
	if sampler.calls != 1 {
		t.Fatalf("idempotency replay sampled %d times", sampler.calls)
	}
	artifact, err := sidecar.FinishTrajectory(endpoint.TrajectoryID)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Status != TrajectoryStatusCompleted || len(artifact.Segments) != 1 || len(artifact.Segments[0].Turns) != 1 {
		t.Fatalf("artifact = %#v", artifact)
	}
	turn := artifact.Segments[0].Turns[0]
	if len(turn.ExactCompletionIDs) != 1 || turn.ExactCompletionIDs[0] != 91 {
		t.Fatalf("completion IDs = %#v", turn.ExactCompletionIDs)
	}
}

func TestLocalDebugSinkRedactsAndBoundsEvidence(t *testing.T) {
	sink, err := NewLocalDebugSink(LocalDebugConfig{RootDir: t.TempDir(), MaxLocalBytes: 1 << 20, RedactText: true, RunID: "run-1", WriterID: "writer-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Record("request", "trajectory/raw", map[string]any{"authorization": "Bearer secret", "content": "private text", "note": "token=abc"}, nil); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(sink.TrajectoriesDir, "*", "events.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%#v err=%v", files, err)
	}
	payload, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, "secret") || strings.Contains(text, "private text") || strings.Contains(text, "token=abc") {
		t.Fatalf("secret leaked: %s", text)
	}
	if !strings.Contains(text, "<redacted>") || !strings.Contains(text, "sha256") {
		t.Fatalf("missing redaction evidence: %s", text)
	}
	sink.Close()
	if _, err := sink.Record("late", "trajectory/raw", nil, nil); err == nil {
		t.Fatal("closed sink accepted an event")
	}
}

func postChat(t *testing.T, endpoint TrajectoryEndpoint, body map[string]any, idempotency string) map[string]any {
	t.Helper()
	encoded, _ := json.Marshal(body)
	request, err := http.NewRequest(http.MethodPost, endpoint.OpenAIBaseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	request.Header.Set("Content-Type", "application/json")
	if idempotency != "" {
		request.Header.Set("Idempotency-Key", idempotency)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var decoded map[string]any
	if json.Unmarshal(payload, &decoded) != nil {
		t.Fatalf("body=%s", payload)
	}
	return decoded
}

package tito

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	sdk "github.com/ZaguanLabs/fireworks-sdk-go/training/sdk"
)

type exactRenderer struct{ fakeRenderer }

func (exactRenderer) RenderConversationTokens(r ChatRequest) ([]int, error) {
	ids := []int{}
	for _, m := range r.Messages {
		for _, c := range m["content"].(string) {
			ids = append(ids, int(c))
		}
	}
	return ids, nil
}

type exactSampler struct {
	reason string
	prob   float64
}

func (s exactSampler) SampleWithPromptTokensResult(_ context.Context, p []int, o ...sdk.SampleOptions) (sdk.SampledRequestResult, error) {
	x := s.prob
	return sdk.SampledRequestResult{Completions: []sdk.SampledCompletion{{FullTokens: append(append([]int{}, p...), int('A')), Text: "A", PromptLen: len(p), CompletionLen: 1, FinishReason: s.reason, SamplingLogprobs: []*float64{&x}}}}, nil
}
func makeTrajectory(t *testing.T, sampler ExactTokenSampler, r Renderer) (*Sidecar, *trajectory) {
	t.Helper()
	c := privateTraceConfig(t)
	c.Enabled = false
	s, err := NewSidecar(sampler, r, SidecarOptions{MaxContextTokens: 100, MaxOutputTokens: 5, FailureTraceConfig: &c})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	ep, err := s.CreateTrajectory("t", "affinity", nil)
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := s.trajectoryFor(ep.TrajectoryID)
	return s, tr
}
func req(s ...string) ChatRequest {
	r := ChatRequest{Model: "policy"}
	for i, v := range s {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		r.Messages = append(r.Messages, map[string]any{"role": role, "content": v})
	}
	return r
}
func TestTreeResumesBranchesAndValidatesParents(t *testing.T) {
	s, tr := makeTrajectory(t, exactSampler{"stop", -1}, exactRenderer{})
	requests := []ChatRequest{req("X"), req("X", "A", "Y"), req("X", "A", "Z"), req("X", "A", "Y", "A", "W")}
	for _, r := range requests {
		if _, err := s.complete(context.Background(), tr, r, ""); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.FinishTrajectory("t")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Segments) != 2 || len(a.Segments[0].Turns) != 3 || a.Segments[1].StartReason != "fork" {
		t.Fatal(a.Segments)
	}
	continued, err := a.ContinuedTurnIDs()
	if err != nil || len(continued) != 2 {
		t.Fatal(continued, err)
	}
	data, err := a.Pack()
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	read := func(b []byte, n int64, hash string, limit int64) error {
		_, err := UnpackTrajectoryStream(bytes.NewReader(b), n, hash, limit)
		return err
	}
	n := int64(len(data))
	if err := read(data, n, digest, 100000); err != nil {
		t.Fatal(err)
	}
	if read(data, n+1, "", 100000) == nil || read(append(data, 0), n+1, "", 100000) == nil || read(data, n, strings.Repeat("0", 64), 100000) == nil || read(data, n, "", 4) == nil || read(data[:n-1], n-1, "", 100000) == nil {
		t.Fatal("accepted corrupt or oversized artifact")
	}
	a.Segments[0].Turns[2].ParentTurnID = "unknown"
	if _, err := a.ContinuedTurnIDs(); err == nil {
		t.Fatal("accepted missing parent")
	}
}

type toolRejectRenderer struct {
	exactRenderer
	closed *bool
}

func (r toolRejectRenderer) ParseAssistant(ChatRequest, []int, string, string) (ParsedAssistant, error) {
	return ParsedAssistant{}, &ParseError{Message: "invalid", ReasonCode: "tool_invalid_argument", ClosedToolAction: r.closed}
}
func TestTerminalMalformedToolSample(t *testing.T) {
	y, n := true, false
	for _, tc := range []struct {
		closed *bool
		finish string
		prob   float64
		want   bool
	}{{&y, "stop", -1, true}, {&y, "length", -1, false}, {nil, "stop", -1, false}, {&n, "tool_calls", -1, false}, {&y, "stop", math.Inf(-1), false}} {
		s, tr := makeTrajectory(t, exactSampler{tc.finish, tc.prob}, toolRejectRenderer{closed: tc.closed})
		if _, err := s.complete(context.Background(), tr, req("X"), ""); err == nil {
			t.Fatal("parser error swallowed")
		}
		a, err := s.FinishTrajectory("t")
		if err != nil || a.Status != TrajectoryStatusFailed || len(a.ResponseAttempts) != 0 {
			t.Fatal(a, err)
		}
		data, err := a.Pack()
		if err != nil {
			t.Fatal(err)
		}
		a, err = UnpackTrajectoryArtifact(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := a.MalformedToolCall(); (got != nil) != tc.want {
			t.Fatalf("want retained %v, got %+v: %+v", tc.want, got, a.Metadata)
		}
	}
}
func TestRolloutPhaseAccounting(t *testing.T) {
	now := time.Unix(0, 0)
	metrics := newPhaseMetrics(func() time.Time { return now })
	step := func() { now = now.Add(time.Second) }
	step()
	model, _ := metrics.Begin("model")
	step()
	tool, _ := metrics.Begin("tool")
	step()
	file, _ := metrics.Begin("tool")
	step()
	_ = model("succeeded")
	step()
	_ = file("failed")
	step()
	_ = tool("succeeded")
	step()
	other, _ := metrics.Begin("other")
	step()
	_ = other("succeeded")
	step()
	m, err := metrics.Finish()
	if err != nil || m["wall_seconds"] != 9 || m["model_tool_overlap_wall_seconds"] != 2 || m["tool_work_seconds"] != 6 || m["unattributed_wall_seconds"] != 3 || m["tool/failed"] != 1 {
		t.Fatal(m, err)
	}
}

type blockedSampler struct {
	entered chan sdk.SampleOptions
	resume  chan struct{}
}

func (s blockedSampler) SampleWithPromptTokensResult(ctx context.Context, ids []int, opts ...sdk.SampleOptions) (sdk.SampledRequestResult, error) {
	s.entered <- opts[0]
	select {
	case <-ctx.Done():
		return sdk.SampledRequestResult{}, ctx.Err()
	case <-s.resume:
		return (exactSampler{"stop", -1}).SampleWithPromptTokensResult(ctx, ids, opts...)
	}
}

func TestAuxiliarySamplingOverlapsPolicyAndHasNoTurn(t *testing.T) {
	sampler := blockedSampler{make(chan sdk.SampleOptions, 2), make(chan struct{})}
	s, tr := makeTrajectory(t, sampler, exactRenderer{})
	s.options.CallClassifier = func(r ChatRequest) (CallKind, string) {
		if r.Model == "tool" {
			return CallKindAuxiliary, "adapter"
		}
		return CallKindPolicy, "adapter"
	}
	type outcome struct {
		call CallResult
		err  error
	}
	run := func(r ChatRequest) <-chan outcome {
		ch := make(chan outcome, 1)
		go func() { c, e := s.complete(context.Background(), tr, r, "one"); ch <- outcome{c, e} }()
		return ch
	}
	policy := run(req("P"))
	<-sampler.entered
	auxReq := req("Z")
	auxReq.Model = "tool"
	auxReq.SamplingFields = map[string]any{"sample_extra_tokens": 3, "sampling_mask": true}
	aux := run(auxReq)
	select {
	case opts := <-sampler.entered:
		if _, ok := opts.Extra["sample_extra_tokens"]; ok {
			t.Fatal("auxiliary published replay draws")
		}
		if _, ok := opts.Extra["sampling_mask"]; ok {
			t.Fatal("auxiliary published a replay mask")
		}
	case <-time.After(2 * time.Second):
		close(sampler.resume)
		t.Fatal("auxiliary waited for the policy sampler")
	}
	if _, e := s.FinishTrajectory("t"); asTITOError(e).Code != "tito_call_in_flight" {
		t.Fatal(e)
	}
	close(sampler.resume)
	p, x := <-policy, <-aux
	if p.err != nil || x.err != nil || p.call.TurnID == "" || x.call.TurnID != "" || x.call.Call.Kind != CallKindAuxiliary {
		t.Fatal(p, x)
	}
	a, e := s.FinishTrajectory("t")
	if e != nil || len(a.Calls) != 2 || len(a.Segments) != 1 || len(a.Segments[0].Turns) != 1 {
		t.Fatal(a, e)
	}
	if _, e := s.complete(context.Background(), tr, req("later"), ""); asTITOError(e).Context["termination_phase"] != "terminal" {
		t.Fatal(e)
	}
}

func TestSidecarCloseCancelsPendingSample(t *testing.T) {
	sampler := blockedSampler{make(chan sdk.SampleOptions, 1), make(chan struct{})}
	s, tr := makeTrajectory(t, sampler, exactRenderer{})
	done := make(chan error, 1)
	go func() { _, e := s.complete(context.Background(), tr, req("P"), ""); done <- e }()
	<-sampler.entered
	if e := s.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e == nil {
		t.Fatal("cancelled request succeeded")
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.status != TrajectoryStatusAbandoned || len(tr.segments) != 0 {
		t.Fatal(tr.status, tr.segments)
	}
}

func TestOpenAIUsageCarriesServerCacheCounts(t *testing.T) {
	for _, n := range []int{-1, 0, 2, 3} {
		turn := Turn{ExactPromptIDs: []int{1, 2}, ExactCompletionIDs: []int{3}, ServerMetrics: &sdk.ServerMetrics{CachedPromptTokens: &n}}
		response := openAIResponse(turn, "hello", "model")
		chunks := openAIStreamChunks(response, map[string]any{"stream_options": map[string]any{"include_usage": true}})
		usage := chunks[len(chunks)-1]["usage"].(map[string]any)
		d, ok := usage["prompt_tokens_details"].(map[string]any)
		if ok != (n >= 0 && n <= 2) || ok && d["cached_tokens"] != n || usage["total_tokens"] != 3 {
			t.Fatal(usage)
		}
	}
}

func TestAuxiliaryFailureInvalidatesConcurrentMalformedReward(t *testing.T) {
	for _, auxFirst := range []bool{true, false} {
		entered := make(chan string, 2)
		gates := map[string]chan struct{}{"A": make(chan struct{}), "P": make(chan struct{})}
		sampler := testSampleFunc(func(ctx context.Context, p []int, o ...sdk.SampleOptions) (sdk.SampledRequestResult, error) {
			label := string(rune(p[0]))
			entered <- label
			select {
			case <-ctx.Done():
				return sdk.SampledRequestResult{}, ctx.Err()
			case <-gates[label]:
			}
			if label == "A" {
				return sdk.SampledRequestResult{}, nil
			}
			return (exactSampler{"stop", -1}).SampleWithPromptTokensResult(ctx, p, o...)
		})
		y := true
		s, tr := makeTrajectory(t, sampler, toolRejectRenderer{closed: &y})
		s.options.CallClassifier = func(r ChatRequest) (CallKind, string) {
			if r.Messages[0]["content"] == "A" {
				return CallKindAuxiliary, "fixture"
			}
			return CallKindPolicy, "fixture"
		}
		calls := map[string]chan error{"A": make(chan error, 1), "P": make(chan error, 1)}
		for label, done := range calls {
			go func() { _, err := s.complete(context.Background(), tr, req(label), ""); done <- err }()
		}
		<-entered
		<-entered
		order := []string{"P", "A"}
		if auxFirst {
			order = []string{"A", "P"}
		}
		for _, label := range order {
			close(gates[label])
			if <-calls[label] == nil {
				t.Fatal("expected rejection")
			}
		}
		artifact, err := s.FinishTrajectory("t")
		if err != nil || artifact.MalformedToolCall() != nil || artifact.Status != TrajectoryStatusFailed {
			t.Fatal(artifact, err)
		}
	}
}

type testSampleFunc func(context.Context, []int, ...sdk.SampleOptions) (sdk.SampledRequestResult, error)

func (f testSampleFunc) SampleWithPromptTokensResult(c context.Context, p []int, o ...sdk.SampleOptions) (sdk.SampledRequestResult, error) {
	return f(c, p, o...)
}

func TestUTF8ArtifactValidationAcrossChunks(t *testing.T) {
	for _, v := range []string{"", "abc", "á小🌍👩🏿‍💻"} {
		for split := 0; split <= len(v); split++ {
			check := utf8Validator{}
			if _, err := check.Write([]byte(v[:split])); err != nil {
				t.Fatal(v, split, err)
			}
			if _, err := check.Write([]byte(v[split:])); err != nil || check.remaining != 0 {
				t.Fatal(v, split, err)
			}
		}
	}
	for _, bad := range [][]byte{{0x90}, {0xff}, {0xc0, 0x80}, {0xed, 0xa0, 0x80}} {
		check := utf8Validator{}
		if _, err := check.Write(bad); err == nil {
			t.Fatal(bad)
		}
	}
}

package sdk

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestServerMetricsFromHeadersFull(t *testing.T) {
	headers := map[string]string{
		"num-concurrent-requests":    "12",
		"prefill-queue-duration":     "0.250",
		"generation-queue-duration":  "1.100",
		"server-time-to-first-token": "0.350",
		"cached-prompt-tokens":       "128",
		"prompt-tokens":              "256",
		"server-processing-time":     "2.500",
	}
	metrics := ServerMetricsFromHeaders(headers, 0.4)

	assertIntPtr(t, metrics.NumConcurrentRequests, 12)
	assertFloatPtr(t, metrics.PrefillQueueDuration, 0.25)
	assertFloatPtr(t, metrics.GenerationQueueDuration, 1.1)
	assertFloatPtr(t, metrics.ServerTTFT, 0.35)
	assertIntPtr(t, metrics.CachedPromptTokens, 128)
	assertIntPtr(t, metrics.PromptTokens, 256)
	assertFloatPtr(t, metrics.ServerProcessingTime, 2.5)
	assertFloatPtr(t, metrics.ClientTTFT, 0.4)
}

func TestServerMetricsFromHeadersEmpty(t *testing.T) {
	metrics := ServerMetricsFromHeaders(map[string]string{})
	if metrics.NumConcurrentRequests != nil || metrics.PrefillQueueDuration != nil || metrics.ClientTTFT != nil {
		t.Fatalf("metrics = %#v, want nil optional fields", metrics)
	}
}

func TestServerMetricsFromHeadersInvalidValues(t *testing.T) {
	metrics := ServerMetricsFromHeaders(map[string]string{
		"num-concurrent-requests": "bad",
		"prefill-queue-duration":  "bad",
	})
	if metrics.NumConcurrentRequests != nil || metrics.PrefillQueueDuration != nil {
		t.Fatalf("metrics = %#v, want invalid fields ignored", metrics)
	}
}

func TestServerMetricsFromHTTPHeaders(t *testing.T) {
	headers := http.Header{
		"Num-Concurrent-Requests": {"12"},
		"Prefill-Queue-Duration":  {"0.25"},
	}
	metrics := ServerMetricsFromHTTPHeaders(headers)
	assertIntPtr(t, metrics.NumConcurrentRequests, 12)
	assertFloatPtr(t, metrics.PrefillQueueDuration, 0.25)
}

func TestFixedConcurrencyController(t *testing.T) {
	ctrl := NewFixedConcurrencyController(2)
	if ctrl.WindowSize() != 2 {
		t.Fatalf("window = %d, want 2", ctrl.WindowSize())
	}
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if err := ctrl.Acquire(ctx); err == nil {
		t.Fatal("expected acquire timeout")
	}
	ctrl.Release(nil)
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ctrl.StepCompleted()["window"]; got != 2 {
		t.Fatalf("window summary = %f, want 2", got)
	}
}

func TestAdaptiveConcurrencyControllerInitialWindow(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 16})
	if ctrl.WindowSize() != 16 {
		t.Fatalf("window = %d, want 16", ctrl.WindowSize())
	}
}

func TestAdaptiveConcurrencyControllerAcquireReleaseBasic(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 2})
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if err := ctrl.Acquire(ctx); err == nil {
		t.Fatal("expected acquire timeout")
	}
	ctrl.Release(nil)
	ctrl.Release(nil)
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAdaptiveConcurrencyReleaseCollectsButDoesNotAdjust(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 10, EMAAlpha: 1.0})
	pq := 5.0
	ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
	if ctrl.WindowSize() != 10 {
		t.Fatalf("window = %d, want 10", ctrl.WindowSize())
	}
}

func TestAdaptiveConcurrencyStepCompletedDecreaseOnHighPQ(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{
		InitialWindow:          10,
		PrefillQueueTarget:     0.5,
		MultiplicativeDecrease: 0.5,
		EMAAlpha:               1.0,
	})
	pq := 2.0
	for i := 0; i < 8; i++ {
		ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
	}
	summary := ctrl.StepCompleted()
	if ctrl.WindowSize() >= 10 {
		t.Fatalf("window = %d, want less than 10", ctrl.WindowSize())
	}
	assertApprox(t, summary["avg_pq"], 2.0)
}

func TestAdaptiveConcurrencyStepCompletedIncreaseOnLowPQ(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{
		InitialWindow:      4,
		PrefillQueueTarget: 1.0,
		AdditiveIncrease:   2.0,
		EMAAlpha:           1.0,
	})
	pq := 0.1
	for i := 0; i < 8; i++ {
		ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
	}
	ctrl.StepCompleted()
	if ctrl.window <= 4.0 {
		t.Fatalf("raw window = %f, want greater than 4", ctrl.window)
	}
}

func TestAdaptiveConcurrencyNoChangeWithoutMetrics(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 8})
	ctrl.Release(nil)
	ctrl.StepCompleted()
	if ctrl.WindowSize() != 8 {
		t.Fatalf("window = %d, want 8", ctrl.WindowSize())
	}
	if ctrl.EMAPrefillQueue() != nil {
		t.Fatalf("ema = %v, want nil", *ctrl.EMAPrefillQueue())
	}
}

func TestAdaptiveConcurrencyEMASmoothingAcrossSteps(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{
		InitialWindow:      10,
		EMAAlpha:           0.5,
		PrefillQueueTarget: 1.0,
	})
	pq := 0.4
	ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
	ctrl.StepCompleted()
	assertFloatPtr(t, ctrl.EMAPrefillQueue(), 0.4)

	pq = 0.8
	ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
	ctrl.StepCompleted()
	assertFloatPtr(t, ctrl.EMAPrefillQueue(), 0.5*0.8+0.5*0.4)
}

func TestAdaptiveConcurrencyRepeatedCongestionFloorsAtMin(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{
		InitialWindow:          16,
		MinWindow:              2,
		PrefillQueueTarget:     0.1,
		MultiplicativeDecrease: 0.5,
		EMAAlpha:               1.0,
	})
	pq := 5.0
	for i := 0; i < 20; i++ {
		ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
		ctrl.StepCompleted()
	}
	if ctrl.WindowSize() != 2 {
		t.Fatalf("window = %d, want 2", ctrl.WindowSize())
	}
}

func TestAdaptiveConcurrencyRepeatedGoodMetricsCapsAtMax(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{
		InitialWindow:      4,
		MaxWindow:          16,
		PrefillQueueTarget: 1.0,
		AdditiveIncrease:   5.0,
		EMAAlpha:           1.0,
	})
	pq := 0.01
	for i := 0; i < 100; i++ {
		ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
		ctrl.StepCompleted()
	}
	if ctrl.WindowSize() != 16 {
		t.Fatalf("window = %d, want 16", ctrl.WindowSize())
	}
}

func TestAdaptiveConcurrencyStepCompletedResetsAccumulators(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 8})
	pq1 := 0.3
	cached1 := 10
	prompt1 := 100
	pq2 := 0.5
	cached2 := 20
	prompt2 := 100
	ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq1, CachedPromptTokens: &cached1, PromptTokens: &prompt1})
	ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq2, CachedPromptTokens: &cached2, PromptTokens: &prompt2})

	summary := ctrl.StepCompleted()
	assertApprox(t, summary["requests"], 2)
	assertApprox(t, summary["avg_pq"], 0.4)
	assertApprox(t, summary["cache_hit_rate"], 30.0/200.0)

	summary = ctrl.StepCompleted()
	assertApprox(t, summary["requests"], 0)
	if _, ok := summary["avg_pq"]; ok {
		t.Fatalf("summary includes avg_pq after reset: %#v", summary)
	}
}

type fakeDeploymentTokenizer struct {
	tokens   []int
	messages []map[string]string
}

func (t *fakeDeploymentTokenizer) ApplyChatTemplate(messages []map[string]string) ([]int, error) {
	t.messages = messages
	return append([]int(nil), t.tokens...), nil
}

func (t *fakeDeploymentTokenizer) Decode(tokenIDs []int) (string, error) {
	if len(tokenIDs) == 0 {
		return "", nil
	}
	return "<" + strconv.Itoa(tokenIDs[0]) + ">", nil
}

func TestExtractLogprobs(t *testing.T) {
	got, ok := ExtractLogprobs(map[string]any{
		"logprobs": map[string]any{"content": []any{
			map[string]any{"logprob": -0.5},
			map[string]any{"logprob": -1.2},
		}},
	})
	if !ok || len(got) != 2 || got[0] != -0.5 || got[1] != -1.2 {
		t.Fatalf("logprobs = %#v ok=%v", got, ok)
	}
	if _, ok := ExtractLogprobs(map[string]any{}); ok {
		t.Fatal("expected missing logprobs")
	}
	if _, ok := ExtractLogprobs(map[string]any{"logprobs": map[string]any{"content": []any{}}}); ok {
		t.Fatal("expected empty logprobs")
	}
}

func TestExtractRoutingMatrices(t *testing.T) {
	got, ok := ExtractRoutingMatrices(map[string]any{
		"logprobs": map[string]any{"content": []any{
			map[string]any{"logprob": -0.5, "routing_matrix": "AQID"},
			map[string]any{"logprob": -1.2},
		}},
	})
	if !ok || len(got) != 2 || got[0] != "AQID" || got[1] != "" {
		t.Fatalf("matrices = %#v ok=%v", got, ok)
	}
	if _, ok := ExtractRoutingMatrices(map[string]any{
		"logprobs": map[string]any{"content": []any{map[string]any{"logprob": -0.5}}},
	}); ok {
		t.Fatal("expected no matrices")
	}
}

func TestDeploymentSamplerSampleWithTokensUsesTokenizer(t *testing.T) {
	tokenizer := &fakeDeploymentTokenizer{tokens: []int{1, 100, 200}}
	sampler := NewDeploymentSampler("https://api.example.com", "m", "key",
		WithDeploymentSamplerTokenizer(tokenizer),
		WithDeploymentSamplerRequester(func(_ context.Context, prompt []int, opts CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			if opts.Temperature != 1.0 || opts.MaxTokens != 1024 || !opts.RawOutput {
				t.Fatalf("opts = %#v", opts)
			}
			if !intSlicePrefixEqual(prompt, []int{1, 100, 200}) {
				t.Fatalf("prompt = %#v", prompt)
			}
			return map[string]any{"choices": []any{map[string]any{
				"text":          "hello world",
				"finish_reason": "stop",
				"raw_output":    map[string]any{"completion_token_ids": []any{400.0, 500.0}},
			}}}, ServerMetrics{}, nil
		}),
	)

	results, err := sampler.SampleWithTokens(context.Background(), []map[string]string{{"role": "user", "content": "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokenizer.messages) != 1 {
		t.Fatalf("messages = %#v", tokenizer.messages)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	got := results[0]
	if got.Text != "hello world" || got.PromptLen != 3 || got.CompletionLen != 2 || got.FinishReason != "stop" {
		t.Fatalf("completion = %#v", got)
	}
	want := []int{1, 100, 200, 400, 500}
	for i := range want {
		if got.FullTokens[i] != want[i] {
			t.Fatalf("tokens = %#v", got.FullTokens)
		}
	}
}

func TestDeploymentSamplerMissingCompletionIDsRaises(t *testing.T) {
	sampler := NewDeploymentSampler("https://api.example.com", "m", "key",
		WithDeploymentSamplerRequester(func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			return map[string]any{"choices": []any{map[string]any{
				"text": "ok", "finish_reason": "stop", "raw_output": map[string]any{},
			}}}, ServerMetrics{}, nil
		}),
	)
	_, err := sampler.SampleWithPromptTokens(context.Background(), []int{1, 2, 3})
	if err == nil || !strings.Contains(err.Error(), "missing completion_token_ids") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeploymentSamplerEchoStripsPromptAndAlignsLogprobs(t *testing.T) {
	sampler := NewDeploymentSampler("https://api.example.com", "m", "key",
		WithDeploymentSamplerRequester(func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			return map[string]any{"choices": []any{map[string]any{
				"text":          "gen",
				"finish_reason": "stop",
				"raw_output":    map[string]any{"completion_token_ids": []any{1.0, 100.0, 200.0, 400.0, 500.0}},
				"logprobs": map[string]any{"content": []any{
					map[string]any{"logprob": 0.0},
					map[string]any{"logprob": -0.1},
					map[string]any{"logprob": -0.2},
					map[string]any{"logprob": -0.3},
					map[string]any{"logprob": -0.4},
				}},
			}}}, ServerMetrics{}, nil
		}),
	)
	results, err := sampler.SampleWithPromptTokens(context.Background(), []int{1, 100, 200}, SampleOptions{Echo: true, Logprobs: true})
	if err != nil {
		t.Fatal(err)
	}
	got := results[0]
	if !got.LogprobsEchoed || got.CompletionLen != 2 {
		t.Fatalf("completion = %#v", got)
	}
	wantTokens := []int{1, 100, 200, 400, 500}
	for i := range wantTokens {
		if got.FullTokens[i] != wantTokens[i] {
			t.Fatalf("tokens = %#v", got.FullTokens)
		}
	}
	wantLP := []float64{-0.1, -0.2, -0.3, -0.4}
	for i := range wantLP {
		if got.InferenceLogprobs[i] != wantLP[i] {
			t.Fatalf("logprobs = %#v", got.InferenceLogprobs)
		}
	}
}

func TestDeploymentSamplerExpandedPromptIDsFromRawOutput(t *testing.T) {
	sampler := NewDeploymentSampler("https://api.example.com", "m", "key",
		WithDeploymentSamplerRequester(func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			return map[string]any{"choices": []any{map[string]any{
				"text": "vision",
				"raw_output": map[string]any{
					"prompt_token_ids":     []any{1.0, 2.0, 3.0, 4.0},
					"completion_token_ids": []any{99.0},
				},
			}}}, ServerMetrics{}, nil
		}),
	)
	results, err := sampler.SampleWithPromptTokens(context.Background(), []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].PromptLen != 4 || len(results[0].FullTokens) != 5 {
		t.Fatalf("completion = %#v", results[0])
	}
}

func TestDeploymentSamplerLogprobsAndRoutingMatrices(t *testing.T) {
	sampler := NewDeploymentSampler("https://api.example.com", "m", "key",
		WithDeploymentSamplerRequester(func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			return map[string]any{"choices": []any{map[string]any{
				"text":       "hi",
				"raw_output": map[string]any{"completion_token_ids": []any{99.0}},
				"logprobs": map[string]any{"content": []any{
					map[string]any{"logprob": -1.5, "routing_matrix": "AQID"},
				}},
			}}}, ServerMetrics{}, nil
		}),
	)
	results, err := sampler.SampleWithPromptTokens(context.Background(), []int{1, 2}, SampleOptions{Logprobs: true, IncludeRoutingMatrix: true})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].InferenceLogprobs[0] != -1.5 || results[0].RoutingMatrices[0] != "AQID" {
		t.Fatalf("completion = %#v", results[0])
	}
}

func TestDeploymentSamplerMaxSeqLenFilters(t *testing.T) {
	sampler := NewDeploymentSampler("https://api.example.com", "m", "key",
		WithDeploymentSamplerRequester(func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			return map[string]any{"choices": []any{map[string]any{
				"text":       "long",
				"raw_output": map[string]any{"completion_token_ids": []any{10.0, 11.0, 12.0}},
			}}}, ServerMetrics{}, nil
		}),
	)
	pre, err := sampler.SampleWithPromptTokens(context.Background(), []int{1, 2, 3}, SampleOptions{MaxSeqLen: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(pre) != 0 {
		t.Fatalf("pre = %#v", pre)
	}
	post, err := sampler.SampleWithPromptTokens(context.Background(), []int{1, 2}, SampleOptions{MaxSeqLen: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(post) != 0 {
		t.Fatalf("post = %#v", post)
	}
}

func TestDeploymentSamplerStopConversion(t *testing.T) {
	tokenizer := &fakeDeploymentTokenizer{}
	var captured []string
	sampler := NewDeploymentSampler("https://api.example.com", "m", "key",
		WithDeploymentSamplerTokenizer(tokenizer),
		WithDeploymentSamplerRequester(func(_ context.Context, _ []int, opts CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			captured = opts.Stop
			return map[string]any{"choices": []any{map[string]any{
				"text":       "x",
				"raw_output": map[string]any{"completion_token_ids": []any{99.0}},
			}}}, ServerMetrics{}, nil
		}),
	)
	if _, err := sampler.SampleWithPromptTokens(context.Background(), []int{1, 2}, SampleOptions{Stop: []int{13, 42}}); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 2 || captured[0] != "<13>" || captured[1] != "<42>" {
		t.Fatalf("captured = %#v", captured)
	}
	if _, err := sampler.SampleWithPromptTokens(context.Background(), []int{1, 2}, SampleOptions{Stop: []string{"</answer>"}}); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 1 || captured[0] != "</answer>" {
		t.Fatalf("captured = %#v", captured)
	}
}

func TestDeploymentSamplerNAndMetricsDrain(t *testing.T) {
	callCount := 0
	pq := 0.2
	sampler := NewDeploymentSampler("https://api.example.com", "m", "key",
		WithDeploymentSamplerRequester(func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			callCount++
			return map[string]any{"choices": []any{map[string]any{
				"text":       "x",
				"raw_output": map[string]any{"completion_token_ids": []any{99.0}},
			}}}, ServerMetrics{PrefillQueueDuration: &pq}, nil
		}),
	)
	results, err := sampler.SampleWithPromptTokens(context.Background(), []int{1, 2}, SampleOptions{N: 4})
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 4 || len(results) != 4 {
		t.Fatalf("callCount=%d results=%d", callCount, len(results))
	}
	metrics := sampler.DrainMetrics()
	if len(metrics) != 4 || metrics[0].PrefillQueueDuration == nil || *metrics[0].PrefillQueueDuration != 0.2 {
		t.Fatalf("metrics = %#v", metrics)
	}
	if len(sampler.DrainMetrics()) != 0 {
		t.Fatal("metrics were not drained")
	}
}

func TestDeploymentSamplerDefaultRequesterPayload(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("prefill-queue-duration", "0.3")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"text":       "x",
			"raw_output": map[string]any{"completion_token_ids": []int{99}},
		}}})
	}))
	defer server.Close()

	sampler := NewDeploymentSampler(server.URL, "accounts/test/deployments/dep", "key")
	results, err := sampler.SampleWithPromptTokens(context.Background(), []int{1, 2}, SampleOptions{
		MaxTokens:            2,
		Temperature:          0.7,
		Logprobs:             true,
		IncludeRoutingMatrix: true,
		Stop:                 []string{"stop"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	if payload["model"] != "accounts/test/deployments/dep" || payload["raw_output"] != true || payload["logprobs"] != true || payload["include_routing_matrix"] != true {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["temperature"].(float64) != 0.7 || payload["max_tokens"].(float64) != 2 {
		t.Fatalf("payload = %#v", payload)
	}
	metrics := sampler.DrainMetrics()
	if len(metrics) != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
	assertFloatPtr(t, metrics[0].PrefillQueueDuration, 0.3)
}

func assertIntPtr(t *testing.T, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("value = %v, want %d", got, want)
	}
}

func assertFloatPtr(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("value = nil, want %f", want)
	}
	assertApprox(t, *got, want)
}

func assertApprox(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("value = %.12f, want %.12f", got, want)
	}
}

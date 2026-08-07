package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTrainingAPIErrorPreservesSafeErrorInfo(t *testing.T) {
	long := strings.Repeat("x", 200)
	body, _ := json.Marshal(map[string]any{"error": map[string]any{
		"message": "capacity exhausted",
		"details": []any{map[string]any{
			"@type":  errorInfoType,
			"reason": "INSUFFICIENT_CAPACITY",
			"metadata": map[string]any{
				"quota_required": long,
				"secret":         "drop-me",
			},
		}},
	}})
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Body: ioNopCloser(body)}
	err := ParseTrainingAPIError(resp, "trainer creation failed")
	if err.StatusCode != 429 || err.Reason != "INSUFFICIENT_CAPACITY" || len(err.Metadata["quota_required"]) != 128 {
		t.Fatalf("error = %#v", err)
	}
	if _, exists := err.Metadata["secret"]; exists {
		t.Fatalf("unsafe metadata survived: %#v", err.Metadata)
	}
}

func TestTrainingRestClientEnvironmentMetadata(t *testing.T) {
	t.Setenv("FIREWORKS_CLIENT_SOURCE", "fireworks-training-skill/grpo_1.2")
	t.Setenv("FIREWORKS_SESSION_ID", "123e4567-e89b-12d3-a456-426614174000")
	client := NewTrainingRestClient("key", "https://api.example.com")
	headers := client.Headers(map[string]string{"x-fireworks-client-source": "explicit"})
	if headers.Get("X-Fireworks-Client-Source") != "explicit" || headers.Get("X-Fireworks-Session-Id") == "" {
		t.Fatalf("headers = %#v", headers)
	}
	t.Setenv("FIREWORKS_SESSION_ID", "not-a-uuid")
	if ValidatedTrainingSessionID("not-a-uuid") != "" {
		t.Fatal("invalid session id should be discarded")
	}
}

func TestFireworksClientModelIsMoE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"base_model_details": map[string]any{"moe": true}})
	}))
	defer server.Close()
	moe, err := NewFireworksClient("key", server.URL).ModelIsMoE(context.Background(), "accounts/a/models/m")
	if err != nil || !moe {
		t.Fatalf("moe=%t err=%v", moe, err)
	}
}

func TestReattachTrainerTransitionOnlyPatch(t *testing.T) {
	mgr := NewDeploymentManager("key", "https://api.example.com")
	now := time.Unix(0, 0)
	identities := []string{"old", "new"}
	var body map[string]any
	var mask any
	_, err := mgr.ReattachTrainer(context.Background(), DeploymentInfo{
		DeploymentID:          "dep",
		HotLoadTrainerJob:     "job",
		HotLoadTransitionType: HotLoadTransitionTypeAsync,
	}, "model", "job", ReattachTrainerOptions{
		HotLoadTransitionType: "sync",
		Timeout:               time.Second,
		PollInterval:          time.Millisecond,
		Now:                   func() time.Time { return now },
		Sleep:                 func(d time.Duration) { now = now.Add(d) },
		ReadReplicaIdentity: func(context.Context, string, string) (string, error) {
			value := identities[0]
			identities = identities[1:]
			return value, nil
		},
		Update: func(_ context.Context, _ string, gotBody map[string]any, gotMask any) (DeploymentInfo, error) {
			body, mask = gotBody, gotMask
			return DeploymentInfo{DeploymentID: "dep"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body, map[string]any{"hotLoadTransitionType": "SYNC"}) || !reflect.DeepEqual(mask, []string{"hot_load_transition_type"}) {
		t.Fatalf("body=%#v mask=%#v", body, mask)
	}
}

func TestManagedMultiModelClientsShareCapacityAndAllowDuplicates(t *testing.T) {
	maxRank := 256
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{Config: FiretitanProvisioningConfig{
		BaseModel:   "accounts/a/models/base",
		MaxLoraRank: &maxRank,
	}})
	if err != nil {
		t.Fatal(err)
	}
	svc.ProvisionedHandle = &ManagedProvisionedHandle{}
	rank, alpha := 64, 128
	first, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{LoraRank: &rank, LoraAlpha: &alpha})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{LoraRank: &rank, LoraAlpha: &alpha})
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first.Config.LoraRank != rank || *first.Config.LoraAlpha != alpha || len(svc.ProvisionedHandle.TrainingClients) != 2 {
		t.Fatalf("first=%#v second=%#v clients=%d", first.Config, second.Config, len(svc.ProvisionedHandle.TrainingClients))
	}
	trainer := BuildManagedTrainerJobConfig(svc.Config, nil, nil)
	if trainer.LoraRank != maxRank {
		t.Fatalf("trainer capacity = %d", trainer.LoraRank)
	}
}

func TestTinkerSamplerBackendCMEKUsesExactSnapshotPathAndRank(t *testing.T) {
	backend := &TinkerSamplerBackend{
		DeploymentID:     "dep",
		BaseModel:        "base",
		HotLoadBucketURL: "gs://bucket/root/",
		CMEKResource:     "models/key",
		LoraRank:         8,
	}
	backend.RememberSavedSnapshot("account/run/step", "delta", 64)
	var options HotloadAndWaitOptions
	backend.HotloadAndWait = func(_ context.Context, _, _, _ string, opts ...HotloadAndWaitOptions) (bool, error) {
		options = opts[0]
		return true, nil
	}
	if _, err := backend.HotloadSavedSnapshot(context.Background(), "account/run/step"); err != nil {
		t.Fatal(err)
	}
	if options.Path != "gs://bucket/root/account/run/step/" || options.CMEKResource != "models/key" {
		t.Fatalf("options = %#v", options)
	}
}

func TestDeploymentSamplerStableRequestIDRetryAfterAndContext(t *testing.T) {
	var ids []string
	var sleeps []time.Duration
	attempt := 0
	sampler := NewDeploymentSampler("https://api.example.com", "model", "key",
		WithDeploymentSamplerRequestContext(map[string]any{"session": "s1", "prompt": "drop"}),
		WithDeploymentSamplerRetryJitter(func() float64 { return 0 }),
		WithDeploymentSamplerClock(time.Now, func(delay time.Duration) { sleeps = append(sleeps, delay) }),
		WithDeploymentSamplerRequester(func(_ context.Context, _ []int, opts CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			ids = append(ids, opts.LogicalRequestID)
			attempt++
			if attempt == 1 {
				return nil, ServerMetrics{}, &CompletionHTTPStatusError{StatusCode: 500, Headers: http.Header{"Retry-After": {"3"}}}
			}
			return completionResult([]int{2}, -0.1, -0.1), ServerMetrics{}, nil
		}),
	)
	_, err := sampler.SampleWithPromptTokens(context.Background(), []int{1}, SampleOptions{SamplingContext: map[string]any{"step": 2, "secret": "drop"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] || len(sleeps) != 1 || sleeps[0] != 3*time.Second {
		t.Fatalf("ids=%#v sleeps=%#v", ids, sleeps)
	}
}

func TestDeploymentSamplerTerminalErrorIsPayloadFree(t *testing.T) {
	sampler := NewDeploymentSampler("https://api.example.com", "model", "key",
		WithDeploymentSamplerRequestContext(map[string]any{"session": "s1", "prompt": "secret"}),
		WithDeploymentSamplerClock(time.Now, func(time.Duration) {}),
		WithDeploymentSamplerRequester(func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			return nil, ServerMetrics{}, &CompletionHTTPStatusError{StatusCode: 503, Headers: http.Header{"X-Fireworks-Request-Id": {"server-1"}}}
		}),
	)
	_, err := sampler.SampleWithPromptTokens(context.Background(), []int{123})
	var requestErr *SamplingRequestError
	if !errors.As(err, &requestErr) || requestErr.FinalStatus != 503 || requestErr.RequestID != "server-1" || requestErr.Context["session"] != "s1" {
		t.Fatalf("err = %#v", err)
	}
	if strings.Contains(requestErr.Error(), "123") || strings.Contains(requestErr.Error(), "secret") {
		t.Fatalf("error leaked payload: %s", requestErr)
	}
}

func TestDeploymentSamplerNonRetryableHTTPErrorRemainsRaw(t *testing.T) {
	sampler := NewDeploymentSampler("https://api.example.com", "model", "key",
		WithDeploymentSamplerRequester(func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			return nil, ServerMetrics{}, &CompletionHTTPStatusError{StatusCode: 400}
		}),
	)
	_, err := sampler.SampleWithPromptTokens(context.Background(), []int{1})
	var statusErr *CompletionHTTPStatusError
	var structured *SamplingRequestError
	if !errors.As(err, &statusErr) || errors.As(err, &structured) {
		t.Fatalf("err = %#v", err)
	}
}

func TestDeploymentSamplerTimeoutIsStructuredSubclass(t *testing.T) {
	sampler := NewDeploymentSampler("https://api.example.com", "model", "key",
		WithDeploymentSamplerClock(time.Now, func(time.Duration) {}),
		WithDeploymentSamplerRequester(func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			return nil, ServerMetrics{}, &CompletionHTTPStatusError{StatusCode: 504}
		}),
	)
	_, err := sampler.SampleWithPromptTokens(context.Background(), []int{1})
	var timeoutErr *DeploymentSamplerTimeoutError
	var structured *SamplingRequestError
	if !errors.As(err, &timeoutErr) || !errors.As(err, &structured) || structured.FinalErrorKind != "timeout" {
		t.Fatalf("err = %#v", err)
	}
}

func TestServerlessSamplingRouteAndAffinity(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		BaseURL:           "https://api.example.com/training/v1/serverless",
		TrainingSessionID: "ts-0123456789abcdef",
		Config:            FiretitanProvisioningConfig{BaseModel: "accounts/a/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.AccountIDCache = "a"
	if svc.Fireworks.BaseURL() != "https://api.example.com" {
		t.Fatalf("control-plane base URL = %q", svc.Fireworks.BaseURL())
	}
	client, err := svc.CreateSamplingClient(context.Background(), "a/run-1/step-2", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "accounts/a/trainingSessions/ts-0123456789abcdef/checkpoints/a/run-1/step-2"
	if client.DeploymentSampler.Model != want || client.DeploymentSampler.AdditionalHeaders["X-Session-Affinity"] != want {
		t.Fatalf("sampler = %#v", client.DeploymentSampler)
	}
}

func TestR3Helpers(t *testing.T) {
	input := ModelInputFromInts([]int{1, 2, 3}, []string{"a"})
	issues := R3RequestIssues([]TrainingDatum{{ModelInput: input}})
	if len(issues) != 1 || !strings.Contains(issues[0], "expected 3") {
		t.Fatalf("issues = %#v", issues)
	}
	if RoutingMatricesWireBytes(input) != len(`,"routing_matrices":["a"]`) {
		t.Fatalf("wire bytes = %d", RoutingMatricesWireBytes(input))
	}
	t.Setenv(ParallelChunkSendConcurrencyEnv, "17")
	if got, err := ParallelChunkSendConcurrency(); err != nil || got != 17 {
		t.Fatalf("concurrency=%d err=%v", got, err)
	}
}

func completionResult(ids []int, rawLogprob, samplingLogprob float64) map[string]any {
	content := make([]any, len(ids))
	for i := range ids {
		content[i] = map[string]any{"logprob": rawLogprob, "sampling_logprob": samplingLogprob}
	}
	return map[string]any{"choices": []any{map[string]any{
		"raw_output": map[string]any{"completion_token_ids": ids},
		"logprobs":   map[string]any{"content": content},
	}}}
}

func ioNopCloser(body []byte) *readCloser {
	return &readCloser{Reader: strings.NewReader(string(body))}
}

type readCloser struct{ *strings.Reader }

func (r *readCloser) Close() error { return nil }

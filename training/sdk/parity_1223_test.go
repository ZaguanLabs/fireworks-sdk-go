package sdk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func topRefs1223(length int, store string) *RoutingReferences {
	i, row := 0, 0
	return &RoutingReferences{Length: length, Files: []RoutingFile{{"format": "parquet_v1", "store_id": store, "row_count": length}}, Spans: []RoutingSpan{{Count: length, FileIndex: &i, FileRowStart: &row}}}
}
func Test1223TopSamplingStorageAndAlignment(t *testing.T) {
	refs := topRefs1223(2, "store")
	input, err := ModelInputFromIntsWithTopSampling([]int{1, 2}, refs)
	if err != nil {
		t.Fatal(err)
	}
	client := &FiretitanTrainingClient{R3StoreID: "store"}
	if err := client.ValidateRoutingData([]TrainingDatum{{ModelInput: input}}); err != nil {
		t.Fatal(err)
	}
	client.R3StoreID = "other"
	if err := client.ValidateRoutingData([]TrainingDatum{{ModelInput: input}}); err == nil {
		t.Fatal("accepted wrong storage")
	}
	if _, err := ModelInputFromIntsWithTopSampling([]int{1}, refs); err == nil {
		t.Fatal("accepted wrong length")
	}
	sampler := NewDeploymentSampler("http://example.test", "model", "key")
	sampler.NegotiateRoutingMatrixFormat(RoutingParquetV1, "store")
	payload := map[string]any{"top_sampling_format": "parquet_v1", "top_sampling_logprobs": 2}
	ttl := 60
	headers, err := sampler.routingRequest(context.Background(), payload, CompletionRequestOptions{R3TTLSeconds: &ttl})
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get(R3StoreHeader) != "store" || headers.Get(R3TTLHeader) != "60" {
		t.Fatal(headers)
	}
	choice := map[string]any{"raw_output": map[string]any{"completion_token_ids": []int{9, 1, 2}}, "r3_store_id": "store", "top_sampling_format": "parquet_v1", "top_sampling_references": refs}
	result := map[string]any{"choices": []any{choice}}
	values, err := sampler.parseCompletionsResult(result, []int{9}, 0, false, false, true, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if values[0].TopSamplingReferences.Length != 2 || values[0].LogprobsEchoed {
		t.Fatal(values)
	}
	choice["r3_store_id"] = "other"
	if _, err := sampler.parseCompletionsResult(result, []int{9}, 0, false, false, true, nil, true); err == nil {
		t.Fatal("accepted cross-store response")
	}
}
func Test1223CapabilitiesAndLossConfig(t *testing.T) {
	for _, wire := range []string{`{"supports_target_logprob_support":true}`, `{"supports_target_logprob_support":false}`, `{"supports_target_logprob_support":"true"}`} {
		var response CreateModelResponse
		if err := json.Unmarshal([]byte(wire), &response); err != nil {
			t.Fatal(err)
		}
		var c FiretitanTrainingClient
		c.ApplyModelCreationResponse(response)
		if c.SupportsTargetLogprobSupport != (wire == `{"supports_target_logprob_support":true}`) {
			t.Fatal(wire)
		}
	}
	for _, config := range []map[string]any{{"replay": true}, {"target_logprob_support": true}, {"target_logprob_support": "unknown"}} {
		if _, err := TargetLogprobSupport(config); err == nil {
			t.Fatal(config)
		}
	}
	backend := &parity1219Backend{}
	c := &FiretitanTrainingClient{ModelID: "model", ComputeBackend: backend}
	config := map[string]any{"target_logprob_support": "sampling_support", "modifier": true}
	if _, err := c.ForwardBackward(context.Background(), nil, "dppo", config); err == nil {
		t.Fatal("accepted unsupported sampling loss")
	}
	c.SupportsTargetLogprobSupport = true
	if _, err := c.ForwardBackward(context.Background(), nil, "dppo", config); err != nil {
		t.Fatal(err)
	}
	if backend.backwardOptions.LossFnConfig["modifier"] != true {
		t.Fatal(backend.backwardOptions)
	}
	payload := BuildTrainerCreatePayload(TrainerJobConfig{BaseModel: "base", ProjectionHeadDim: intPointer(4)})
	if payload["trainingConfig"].(map[string]any)["projectionHeadDim"] != 4 {
		t.Fatal(payload)
	}
}
func Test1223ScopedCapacityRetry(t *testing.T) {
	cases := []struct {
		path, body    string
		enabled, want bool
	}{
		{"/training/v1/serverless/api/v1/retrieve_future", `{"detail":"capacity_exhausted"}`, true, true},
		{"/training/v1/serverless/api/v1/retrieve_future", `{"detail":"capacity_exhausted"}`, false, false},
		{"/training/v1/rlorTrainerJobs/a/j/api/v1/retrieve_future", `{"detail":"capacity_exhausted"}`, true, false},
		{"/training/v1/serverless/api/v1/retrieve_future", `{"detail":"capacity_exhausted but idempotency conflict"}`, true, false},
		{"/training/v1/serverless/api/v1/retrieve_future", `{"detail":"FAILED"}`, true, false},
		{"/training/v1/serverless/api/v1/retrieve_future", `{}`, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.path+tc.body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					w.WriteHeader(409)
					w.Write([]byte(tc.body))
					return
				}
				w.Write([]byte(`{}`))
			}))
			defer server.Close()
			client := NewTrainingRestClient("test", server.URL, WithTrainingRetryOptions(RequestRetryOptions{Sleep: func(time.Duration) {}}))
			if tc.enabled {
				client.EnableServerlessSupervised409Retry()
			}
			resp, err := client.Post(context.Background(), tc.path, map[string]any{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if (calls == 2) != tc.want {
				t.Fatalf("calls=%d body=%s", calls, body)
			}
			if !tc.want && string(body) != tc.body {
				t.Fatalf("lost terminal body: %s", body)
			}
		})
	}
}
func Test1223MetricsSurviveDrainAndExpire(t *testing.T) {
	now := time.Unix(100, 0)
	s := NewDeploymentSampler("http://example.test", "m", "key", WithDeploymentSamplerClock(func() time.Time { return now }, nil))
	queue := 1.0
	s.recordAttemptMetrics(&ServerMetrics{PrefillQueueDuration: &queue})
	if len(s.DrainMetrics()) != 1 || len(s.recentMetricsDiagnostic()) == 0 {
		t.Fatal("drain lost diagnostics")
	}
	for i := 0; i < 65540; i++ {
		s.recordAttemptMetrics(&ServerMetrics{Error: "HTTP 400"})
	}
	if len(s.recentMetrics) != 65536 || len(s.diagnosticMetrics) != 1 {
		t.Fatal("unbounded or evicted pressure evidence")
	}
	now = now.Add(11 * time.Minute)
	if len(s.recentMetricsDiagnostic()) != 0 {
		t.Fatal("stale diagnostics")
	}
}

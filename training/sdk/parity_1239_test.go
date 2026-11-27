package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestReservedTrainingSessionBindsFreshCounters(t *testing.T) {
	const name = "accounts/acct/trainingSessions/ts-owned"
	var state atomic.Value
	state.Store("")
	var modelCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/trainingSessions":
			if r.URL.Query().Get("training_session_id") != "ts-owned" || body["displayName"] != "mine" || body["baseModel"] != "" {
				t.Error(r.URL, body)
			}
			json.NewEncoder(w).Encode(map[string]string{"name": name, "displayName": "mine"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/"+name:
			json.NewEncoder(w).Encode(map[string]string{"name": name, "displayName": "mine", "state": state.Load().(string)})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/trainingSessions/ts-gone":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/training/v1/serverless/api/v1/create_model":
			seq := modelCount.Add(1) - 1
			if body["session_id"] != "ts-owned" || body["model_seq_id"] != float64(seq) {
				t.Error(body)
			}
			fmt.Fprintf(w, `{"model_id":"run-first:train:%d"}`, seq)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx := context.Background()
	api := NewFireworksClient("key", server.URL)
	api.SetAccountID("acct")
	if _, err := api.ReserveTrainingSession(ctx, "ts-owned", "mine"); err != nil {
		t.Fatal(err)
	}
	if found, err := api.GetTrainingSession(ctx, "accounts/acct/trainingSessions/ts-gone"); err != nil || found != nil {
		t.Fatal(found, err)
	}
	if _, err := api.GetTrainingSession(ctx, "accounts/other/trainingSessions/ts-owned"); err == nil {
		t.Fatal("accepted foreign account")
	}
	if _, err := api.OpenReservedTrainingSession(ctx, name, "other-owner"); err == nil {
		t.Fatal("accepted foreign marker")
	}
	state.Store("READY")
	if _, err := api.OpenReservedTrainingSession(ctx, name, "mine"); err == nil {
		t.Fatal("accepted bound session")
	}
	state.Store("TRAINING_SESSION_STATE_UNSPECIFIED")
	svc, err := api.OpenReservedTrainingSession(ctx, name, "mine")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close(ctx)
	for i := 0; i < 2; i++ {
		client, err := svc.CreateTrainingClient(ctx, CreateFiretitanTrainingClientOptions{BaseModel: "accounts/acct/models/base", LoraRank: intPointer(4)})
		if err != nil || client.ModelID != fmt.Sprintf("run-first:train:%d", i) {
			t.Fatal(client, err)
		}
	}
}

func TestReservedHeartbeatStopsOnClose(t *testing.T) {
	seen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/api/v1/session_heartbeat" || body["session_id"] != "ts-owned" {
			t.Error(r.URL, body)
		}
		select {
		case seen <- struct{}{}:
		default:
		}
	}))
	defer server.Close()
	svc := &FiretitanServiceClient{reservedRuntime: NewNativeTrainingRuntime(NewTrainingRestClient("", server.URL), "ts-owned")}
	svc.startReservedHeartbeat(20 * time.Millisecond)
	select {
	case <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat did not run")
	}
	if err := svc.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-svc.heartbeatDone:
	default:
		t.Fatal("heartbeat still running after close")
	}
}

func TestTrainerMetricsFilterAndSingleAttempt(t *testing.T) {
	var appends, lookups atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/accounts":
			lookups.Add(1)
		case "/v1/accounts/acct/rlorTrainerJobs/train:logMetrics":
			appends.Add(1)
			var data struct {
				Name    string
				Records []TrainerMetricRecord
			}
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				t.Error(err)
			}
			want := TrainerMetricRecord{Step: 3, Metrics: map[string]float64{"loss": 0.5}, SessionID: "session"}
			if data.Name != "accounts/acct/rlorTrainerJobs/train" || !reflect.DeepEqual(data.Records, []TrainerMetricRecord{want}) {
				t.Error(data)
			}
		default:
			t.Error(r.URL)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	api := NewFireworksClient("key", server.URL)
	svc := &FiretitanServiceClient{Fireworks: api, Config: FiretitanProvisioningConfig{TrainerJobID: "train"}}
	ctx := context.Background()
	record := map[string]any{"step": 3, "loss": 0.5, "string": "skip", "bool": true, "nan": math.NaN(), "inf": math.Inf(1), "session_id": "session"}
	if err := svc.LogTrainingMetrics(ctx, record); err == nil || lookups.Load() != 1 {
		t.Fatal(err, lookups.Load())
	}
	api.SetAccountID("acct")
	if err := svc.LogTrainingMetrics(ctx, record); err == nil || appends.Load() != 1 {
		t.Fatal(err, appends.Load())
	}
	if err := svc.LogTrainingMetrics(ctx, map[string]any{"step": "x"}); err == nil {
		t.Fatal("accepted a nonnumeric step")
	}
	if err := svc.LogTrainingMetrics(ctx, map[string]any{"step": 2}); err != nil {
		t.Fatal(err)
	}
	svc.ServerlessSessionID = "ts-owned"
	if err := svc.LogTrainingMetrics(ctx, record); err != nil || appends.Load() != 1 {
		t.Fatal(err, appends.Load())
	}
}

func TestExtraSamplingUsesBoundCapture(t *testing.T) {
	sampler := NewDeploymentSampler("http://example.test", "model", "key")
	defer sampler.Close()
	payload := map[string]any{"sample_extra_tokens": 4, "logprobs": true, "sampling_mask": true}
	if _, err := sampler.routingRequest(context.Background(), payload, CompletionRequestOptions{}); err == nil {
		t.Fatal("accepted an unbound capture")
	}
	sampler.NegotiateRoutingMatrixFormat(RoutingParquetV1, "store")
	headers, err := sampler.routingRequest(context.Background(), payload, CompletionRequestOptions{})
	if err != nil || headers.Get(R3StoreHeader) != "store" || payload["sampling_mask"] != "parquet_v1" || payload["sample_extra_format"] != "parquet_v1" {
		t.Fatal(headers, payload, err)
	}
	for _, invalid := range []map[string]any{{"top_sampling_logprobs": 3}, {"temperature": 0.0}, {"top_k": 1}, {"sample_extra_tokens": -1}} {
		p := cloneAnyMap(payload)
		for k, v := range invalid {
			p[k] = v
		}
		if err := normalizeCaptureFormats(p); err == nil {
			t.Fatal("accepted", invalid)
		}
	}
	choice := map[string]any{"token_ids": []int{9, 4}, "prompt_token_ids": []int{9}, "r3_store_id": "store", "sample_extra_format": "parquet_v1", "sample_extra_references": testRouting(2), "sampling_mask_format": "parquet_v1", "sampling_mask_references": testRouting(2)}
	result := map[string]any{"choices": []any{choice}}
	rows, err := sampler.parseCompletionsResult(result, []int{9}, 0, false, false, true, nil, false, true, true)
	if err != nil || len(rows) != 1 || rows[0].SampleExtraReferences.Length != 1 || rows[0].SamplingMaskReferences.Length != 1 {
		t.Fatal(rows, err)
	}
	choice["r3_store_id"] = "other"
	if _, err := sampler.parseCompletionsResult(result, []int{9}, 0, false, false, true, nil, false, true, true); err == nil {
		t.Fatal("accepted a cross-store capture")
	}
	input, err := ModelInputFromIntsWithExtraSampling([]int{4}, rows[0].SampleExtraReferences)
	if err != nil {
		t.Fatal(err)
	}
	data := []TrainingDatum{{ModelInput: input}}
	config := map[string]any{"score_centering": true}
	if err := validateScoreCenteringInputs(data, config); err != nil {
		t.Fatal(err)
	}
	config["score_centering_estimator"] = "top_sampling"
	if err := validateScoreCenteringInputs(data, config); err == nil {
		t.Fatal("accepted mismatched capture and estimator")
	}
}

func TestAdaptiveWindowIgnoresPrefillFromBeforeCut(t *testing.T) {
	c := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 4, AdjustmentInterval: intPointer(1)})
	high := 10.0
	before := time.Now().Add(-time.Minute)
	c.Release(&ServerMetrics{ClientRequestStartedAt: before, PrefillQueueDuration: &high})
	c.Release(&ServerMetrics{ClientRequestStartedAt: before, PrefillQueueDuration: &high})
	if summary := c.StepCompleted(); summary["stale_prefill_observations"] != 1 {
		t.Fatal(summary)
	}
}

func TestFuturePollStopsRequestingMetadata(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/start" {
			io.WriteString(w, `{"request_id":"r1"}`)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		first := count.Add(1) == 1
		if req["allow_metadata_only"] != first || req["request_id"] != "r1" {
			t.Error(req)
		}
		if first {
			io.WriteString(w, `{"status":"complete_metadata","response_payload_size":42}`)
			return
		}
		io.WriteString(w, `{"value":1}`)
	}))
	defer server.Close()
	runtime := NewNativeTrainingRuntime(NewTrainingRestClient("", server.URL), "")
	runtime.PollInterval = time.Millisecond
	value, err := runtime.submit(context.Background(), "start", map[string]any{})
	if err != nil || value["value"] != float64(1) || count.Load() != 2 {
		t.Fatal(value, err, count.Load())
	}
}

func TestRetrieveFutureResetsIdleTimeoutOnBodyProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["mode"] == "stall-header" {
			<-r.Context().Done()
			return
		}
		io.WriteString(w, `{"value":"`)
		w.(http.Flusher).Flush()
		if body["mode"] == "stall-body" {
			<-r.Context().Done()
			return
		}
		for i := 0; i < 8; i++ {
			time.Sleep(12 * time.Millisecond)
			io.WriteString(w, "a")
			w.(http.Flusher).Flush()
		}
		io.WriteString(w, `"}`)
	}))
	defer server.Close()
	runtime := NewNativeTrainingRuntime(NewTrainingRestClient("", server.URL, WithTrainingHTTPClient(&http.Client{Timeout: 5 * time.Millisecond})), "")
	for _, mode := range []string{"progress", "stall-header", "stall-body"} {
		value, err := runtime.retrieveFuture(context.Background(), map[string]string{"mode": mode}, 80*time.Millisecond)
		if mode == "progress" {
			if err != nil || value["value"] != "aaaaaaaa" {
				t.Fatal(value, err)
			}
		} else if err != context.DeadlineExceeded {
			t.Fatal(mode, err)
		}
	}
}

func TestStreamTokenAlignmentRequiresConsistentIncrementalIDs(t *testing.T) {
	s := NewDeploymentSampler("https://example.test", "model", "key")
	defer s.Close()
	for _, row := range []string{
		`{"choices":[{"token_ids":[1.5],"prompt_token_ids":[9],"finish_reason":"stop"}]}`,
		`{"choices":[{"token_ids":[1],"prompt_token_ids":[9],"logprobs":{"content":[{"token_id":2}]},"finish_reason":"stop"}]}`,
		`{"choices":[{"token_ids":[1],"prompt_token_ids":[9],"finish_reason":"stop"}],"usage":{"completion_tokens":2}}`,
		`{"choices":[{"token_ids":[1],"finish_reason":"stop"}]}`,
		`{"choices":[{"text":"x","prompt_token_ids":[9],"finish_reason":"stop"}]}`,
		"{\"choices\":[{\"prompt_token_ids\":[9]}]}\n\ndata: {\"choices\":[{\"prompt_token_ids\":[10],\"token_ids\":[1],\"finish_reason\":\"stop\"}]}",
	} {
		if _, _, err := s.assembleStreamResponse([]byte("data: "+row+"\n\ndata: [DONE]\n\n"), http.Header{}, map[string]any{}); err == nil {
			t.Fatal("accepted", row)
		}
	}
}

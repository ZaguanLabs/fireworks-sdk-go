package sdk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func Test1230CapabilitiesAndCalibration(t *testing.T) {
	var response CreateModelResponse
	if err := json.Unmarshal([]byte(`{"supports_reinforce":true,"supports_score_centering":"true","supports_policy_importance_sampling":1,"supports_policy_diagnostics":false,"rdma_weight_sync_enabled":false}`), &response); err != nil {
		t.Fatal(err)
	}
	client := &FiretitanTrainingClient{}
	client.ApplyModelCreationResponse(response)
	if !client.SupportsReinforce || client.SupportsScoreCentering || client.SupportsPolicyImportanceSampling || client.trainerRDMAEnabled == nil || *client.trainerRDMAEnabled {
		t.Fatal(response)
	}
	if client.ValidateLossCapabilities("reinforce", map[string]any{"score_centering": true}) == nil {
		t.Fatal("unadvertised centering accepted")
	}
	if client.ValidateLossCapabilities("dppo", map[string]any{"weighting": "mis"}) == nil {
		t.Fatal("unadvertised MIS accepted")
	}
	if client.ValidateLossCapabilities("reinforce", nil) != nil {
		t.Fatal("advertised REINFORCE rejected")
	}
	if support, err := TargetLogprobSupport(map[string]any{"target_logprob_support": "candidates"}); err != nil || support != "candidates" {
		t.Fatal(support, err)
	}
	calibration := DecisionCalibration{Temperature: map[QuestionType]float64{QuestionTypeChoice: 0.7}, FitRows: map[QuestionType]int{QuestionTypeChoice: 0}}
	data, err := json.Marshal(calibration)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"contract":"systemone-letter-v1"`) || !strings.Contains(string(data), `"objective":"nll"`) {
		t.Fatal(string(data))
	}
	for _, invalid := range []float64{0, -1, 101, math.NaN(), math.Inf(1)} {
		calibration.Temperature[QuestionTypeChoice] = invalid
		if calibration.Validate() == nil {
			t.Fatal("accepted", invalid)
		}
	}
}

func Test1230SamplerHandleDoesNotCarryCredentials(t *testing.T) {
	sampler := NewDeploymentSampler("https://example.test", "model", "producer-secret", WithDeploymentSamplerAdditionalHeaders(map[string]string{"Authorization": "Bearer hidden", "X-API-Key": "hidden", "CF-Access-Client-Id": "producer", "CF-Access-Client-Secret": "hidden", "X-Custom-Token": "hidden", "Cookie": "hidden", "X-Trace": "safe"}), WithDeploymentSamplerConcurrencyController(NewFixedConcurrencyController(3)))
	sampler.NegotiateRoutingMatrixFormat(RoutingParquetV1, "store")
	handle, err := NewFiretitanSamplingClient(sampler).GetSamplerHandle()
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.URLEncoding.DecodeString(handle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hidden") || strings.Contains(string(data), "producer") {
		t.Fatal("handle leaked credentials")
	}
	t.Setenv("CLOUDFLARE_ACCESS_CLIENT_ID", "receiver")
	t.Setenv("CLOUDFLARE_ACCESS_CLIENT_SECRET", "receiver-secret")
	copy, err := FiretitanSamplingClientFromHandle(handle, "receiver-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if copy.DeploymentSampler.APIKey != "receiver-key" || copy.DeploymentSampler.AdditionalHeaders["CF-Access-Client-Id"] != "receiver" || copy.DeploymentSampler.AdditionalHeaders["X-Trace"] != "safe" || copy.DeploymentSampler.ConcurrencyController.WindowSize() != 3 || copy.DeploymentSampler.R3StoreID() != "store" {
		t.Fatal("handle restore mismatch")
	}
}

func Test1230SamplingMaskWire(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(R3StoreHeader) != "store" {
			t.Error("missing storage binding")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["sampling_mask"] != "parquet_v1" || body["top_sampling_logprobs"] != nil {
			t.Error(body)
		}
		choice := map[string]any{"finish_reason": "stop", "logprobs": map[string]any{"content": []any{map[string]any{"token_id": 4, "logprob": -0.2, "sampling_logprob": -0.2}}}, "token_ids": []int{4}, "prompt_token_ids": []int{1}, "sampling_mask_format": "parquet_v1", "r3_store_id": "store", "sampling_mask_references": testRouting(1)}
		data, _ := json.Marshal(map[string]any{"choices": []any{choice}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
	}))
	defer server.Close()
	sampler := NewDeploymentSampler(server.URL, "model", "key")
	sampler.NegotiateRoutingMatrixFormat(RoutingParquetV1, "store")
	result, err := NewFiretitanSamplingClient(sampler).Sample(context.Background(), []int{1}, 1, FiretitanSamplingParams{SamplingMask: "parquet_v1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Sequences[0].SamplingMaskReferences == nil || result.Sequences[0].SamplingMaskReferences.Length != 1 {
		t.Fatal(result)
	}
	input, err := ModelInputFromIntsWithSamplingMask([]int{1}, result.Sequences[0].SamplingMaskReferences)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRoutingData([]TrainingDatum{{ModelInput: input}}, RoutingParquetV1, "wrong"); err == nil {
		t.Fatal("foreign storage accepted")
	}
}

func Test1230StreamFailures(t *testing.T) {
	sampler := NewDeploymentSampler("https://example.test", "m", "key")
	for name, data := range map[string]string{"invalid_json": "data: {\n\n", "non_object": "data: []\n\n", "choices": "data: {\"choices\":[null]}\n\n", "usage": "data: {\"usage\":[]}\n\n"} {
		t.Run(name, func(t *testing.T) {
			_, _, err := sampler.assembleStreamResponse([]byte(data), nil)
			var protocol *SSEProtocolError
			if !errors.As(err, &protocol) {
				t.Fatal(err)
			}
		})
	}
	for _, data := range []string{"data: [DONE]\n\n", `data: {"choices":[{"raw_output":{"completion_token_ids":[2]}}]}` + "\n\n"} {
		_, _, err := sampler.assembleStreamResponse([]byte(data), nil)
		var truncation *SSETruncationError
		if !errors.As(err, &truncation) {
			t.Fatal(err)
		}
	}
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("X-Request-Id", "request")
		fmt.Fprint(w, "event: error\ndata: {\"id\":\"upstream\",\"error\":{\"status\":503,\"code\":\"overloaded\",\"type\":\"server_error\",\"message\":\"private payload\"}}\n\n")
	}))
	defer server.Close()
	sampler = NewDeploymentSampler(server.URL, "m", "key", WithDeploymentSamplerClock(time.Now, func(time.Duration) {}))
	_, err := sampler.SampleWithPromptTokensResult(context.Background(), []int{1})
	var failure *SamplingRequestError
	if !errors.As(err, &failure) {
		t.Fatal(err)
	}
	if attempts != SamplerRetryMaxAttempts || failure.FinalStatus != 503 || failure.WireStatus == nil || *failure.WireStatus != 200 || failure.UpstreamErrorCode != "overloaded" || failure.RequestID != "request" || failure.UpstreamResponseID != "upstream" || failure.RetryBudgetExhausted == nil || !*failure.RetryBudgetExhausted {
		t.Fatal(failure.AsErrorRecord(), attempts)
	}
	if strings.Contains(failure.Error(), "private") {
		t.Fatal("stream error exposed response payload")
	}
}

type unload1230Backend struct {
	started chan struct{}
	release chan struct{}
	unloads atomic.Int32
	fail    bool
}

func (b *unload1230Backend) ForwardBackward(context.Context, ForwardBackwardOptions) (ForwardBackwardOutput, error) {
	close(b.started)
	<-b.release
	return ForwardBackwardOutput{}, nil
}
func (b *unload1230Backend) OptimStep(context.Context, OptimStepOptions) (map[string]any, error) {
	return map[string]any{}, nil
}
func (b *unload1230Backend) UnloadModel(context.Context, string, int) (map[string]any, error) {
	b.unloads.Add(1)
	if b.fail {
		b.fail = false
		return nil, errors.New("retry cleanup")
	}
	return map[string]any{"type": "unload_model"}, nil
}

func Test1230UnloadAdmissionAndRetry(t *testing.T) {
	backend := &unload1230Backend{started: make(chan struct{}), release: make(chan struct{}), fail: true}
	client := &FiretitanTrainingClient{ModelID: "model", ComputeBackend: backend}
	accepted := client.ForwardBackwardFuture(context.Background(), nil, "cross_entropy", nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case <-backend.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	unload := client.UnloadModelFuture(ctx)
	if backend.unloads.Load() != 0 {
		t.Fatal("unloaded before accepted work drained")
	}
	if _, err := client.OptimStep(ctx, nil, nil); err == nil {
		t.Fatal("new operation admitted during unload")
	}
	close(backend.release)
	if _, err := accepted.Result(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := unload.Result(ctx); err == nil {
		t.Fatal("cleanup failure lost")
	}
	if _, err := client.UnloadModel(ctx); err != nil {
		t.Fatal(err)
	}
	if backend.unloads.Load() != 2 {
		t.Fatal("cleanup retry missing")
	}
	if _, err := client.OptimStep(ctx, nil, nil); err == nil {
		t.Fatal("training reopened after unload")
	}
}

func Test1230NativeTrainingRuntime(t *testing.T) {
	operations := []string{}
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method == "POST" {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		operation := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		if operation == "create_session" {
			fmt.Fprint(w, `{"session_id":"session"}`)
			return
		}
		if operation == "retrieve_future" {
			polls++
			if polls == 1 {
				fmt.Fprint(w, `{"type":"try_again"}`)
				return
			}
			fmt.Fprint(w, `{"model_id":"model","comms":"v2","supports_reinforce":true,"type":"create_model"}`)
			return
		}
		if operation == "create_model" {
			if body["lora_config"] != nil {
				t.Error("full-parameter model must use null LoRA")
			}
			fmt.Fprint(w, `{"request_id":"request"}`)
			return
		}
		operations = append(operations, operation)
		if operation == "save_weights_for_sampler" {
			if body["metadata"] == nil {
				t.Error("calibration metadata lost")
			}
			fmt.Fprint(w, `{"path":"account/run/checkpoint"}`)
			return
		}
		if sequence, ok := body["seq_id"].(float64); ok && sequence != float64(len(operations)) {
			t.Error("nonmonotonic sequence", body, operations)
		}
		fmt.Fprint(w, `{"loss_fn_outputs":[],"metrics":{},"path":"state"}`)
	}))
	defer server.Close()
	runtime := NewNativeTrainingRuntime(NewTrainingRestClient("key", server.URL), "")
	runtime.PollInterval = time.Millisecond
	response, err := runtime.CreateModel(context.Background(), FiretitanProvisioningConfig{BaseModel: "base"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.ModelID != "model" || response.Comms != CommsV2 || polls != 2 {
		t.Fatal(response, polls)
	}
	if _, err = runtime.ForwardBackward(context.Background(), ForwardBackwardOptions{ModelID: "model", LossFn: LossFnReinforce, Comms: CommsV2}); err != nil {
		t.Fatal(err)
	}
	model := runtime.ForModel("model")
	if _, err = model.SaveState(context.Background(), "step", SaveStateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.OptimStep(context.Background(), OptimStepOptions{ModelID: "model"}); err != nil {
		t.Fatal(err)
	}
	if _, err = model.SaveWeightsForSamplerExt(context.Background(), "step", SaveWeightsForSamplerOptions{Metadata: &SamplerCheckpointMetadata{DecisionCalibration: &DecisionCalibration{Temperature: map[QuestionType]float64{QuestionTypeNoul: 1}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.UnloadModel(context.Background(), "model", 1); err != nil {
		t.Fatal(err)
	}
}

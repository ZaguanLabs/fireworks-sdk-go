package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFutureReadyResult(t *testing.T) {
	future := ReadyFuture("ok", nil)
	if !future.Ready() {
		t.Fatal("ready future should report ready")
	}
	result, err := future.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("result = %q", result)
	}
}

func TestFutureResultCanTimeOutWithoutDroppingWork(t *testing.T) {
	release := make(chan struct{})
	future := SubmitFuture(func() (string, error) {
		<-release
		return "done", nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if result, err := future.Result(ctx); err == nil || result != "" {
		t.Fatalf("early result = %q err=%v", result, err)
	}
	close(release)
	result, err := future.Await()
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" {
		t.Fatalf("result = %q", result)
	}
}

func TestFuturePropagatesError(t *testing.T) {
	want := errors.New("boom")
	future := SubmitFuture(func() (int, error) {
		return 0, want
	})
	if _, err := future.Await(); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestNilFutureResult(t *testing.T) {
	var future *Future[int]
	if _, err := future.Result(context.Background()); !errors.Is(err, ErrNilFuture) {
		t.Fatalf("err = %v", err)
	}
	if future.Ready() {
		t.Fatal("nil future should not report ready")
	}
}

func TestFiretitanServiceClientCreateTrainingClientFuture(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	future := svc.CreateTrainingClientFuture(context.Background(), CreateFiretitanTrainingClientOptions{
		UserMetadata: map[string]string{"run": "future"},
	})
	client, err := future.Await()
	if err != nil {
		t.Fatal(err)
	}
	if client.UserMetadata["run"] != "future" {
		t.Fatalf("metadata = %#v", client.UserMetadata)
	}
}

func TestFiretitanTrainingClientWeightSyncerFutures(t *testing.T) {
	saver := &fakeSamplerSaver{
		results: []SaveSamplerResult{
			{Path: "raw/path-1", SnapshotName: "step-1-session"},
			{Path: "raw/path-2", SnapshotName: "step-2-session"},
		},
	}
	syncer := NewWeightSyncer(WeightSyncerConfig{
		PolicyClient: saver,
		BaseModel:    "accounts/acct/models/base",
	})
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{
		WeightSyncer:               syncer,
		RequiresInitialSamplerSync: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := client.SaveWeightsForSamplerFuture(context.Background(), "step-1").Await()
	if err != nil {
		t.Fatal(err)
	}
	if saved.SnapshotName != "step-1-session" {
		t.Fatalf("saved = %#v", saved)
	}
	hotloaded, err := client.SaveWeightsAndHotloadFuture(context.Background(), "step-2").Await()
	if err != nil {
		t.Fatal(err)
	}
	if hotloaded.SnapshotName != "step-2-session" || client.RequiresInitialSamplerSync() {
		t.Fatalf("hotloaded = %#v initialSync=%t", hotloaded, client.RequiresInitialSamplerSync())
	}
	if _, err := client.SaveWeightsAndGetSamplingClientFuture(context.Background(), "step-3", nil).Await(); err == nil || err.Error() != SamplingClientFromTrainerMessage {
		t.Fatalf("unsupported err = %v", err)
	}
}

func TestFiretitanTrainingClientStateAndAdapterFutures(t *testing.T) {
	state := &fakeTrainingStateBackend{}
	loader := &fakeTrainingAdapterLoader{}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{
		StateBackend:   state,
		AdapterLoader:  loader,
		ModelID:        "model-1",
		TrainerBaseURL: "https://trainer.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := client.SaveStateFuture(context.Background(), "step-1").Await()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Path != "state://step-1" {
		t.Fatalf("saved = %#v", saved)
	}
	if _, err := client.LoadStateFuture(context.Background(), "state://step-1", nil).Await(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.LoadStateWithOptimizerFuture(context.Background(), "state://step-2", nil).Await(); err != nil {
		t.Fatal(err)
	}
	if state.loadStatePath != "state://step-1" || state.loadStateOptimizerPath != "state://step-2" {
		t.Fatalf("state backend = %#v", state)
	}
	loaded, err := client.LoadAdapterFuture(context.Background(), "gs://bucket/adapter").Await()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ModelID != "model-1" || len(loader.calls) != 1 {
		t.Fatalf("loaded = %#v calls=%#v", loaded, loader.calls)
	}
}

func TestFiretitanSamplingClientFutures(t *testing.T) {
	sampler := NewDeploymentSampler("https://api.example.com", "accounts/acct/deployments/dep", "key",
		WithDeploymentSamplerRequester(func(_ context.Context, _ []int, opts CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
			if !opts.Logprobs {
				t.Fatalf("opts = %#v", opts)
			}
			return map[string]any{"choices": []any{map[string]any{
				"text":          "ok",
				"finish_reason": "stop",
				"raw_output":    map[string]any{"completion_token_ids": []any{3.0}},
				"logprobs":      map[string]any{"content": []any{map[string]any{"logprob": -0.4}}},
			}}}, ServerMetrics{}, nil
		}),
	)
	client := NewFiretitanSamplingClient(sampler)
	response, err := client.SampleFuture(context.Background(), []int{1, 2}, 1, FiretitanSamplingParams{}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Sequences) != 1 || response.Sequences[0].Tokens[0] != 3 {
		t.Fatalf("response = %#v", response)
	}
	model, err := client.GetBaseModelFuture(context.Background()).Await()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(model, "deployments/dep") {
		t.Fatalf("model = %q", model)
	}
}

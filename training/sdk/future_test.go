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

func TestFiretitanServiceClientManagedLifecycleFutures(t *testing.T) {
	trainer := &fakeManagedTrainer{}
	deployment := &fakeManagedDeployment{existing: map[string]DeploymentInfo{}}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:       "accounts/acct/models/base",
			DeploymentShape: "accounts/acct/deploymentShapes/serve",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := svc.ProvisionManagedHandleFuture(context.Background(), ManagedProvisionOptions{
		Trainer:    trainer,
		Deployment: deployment,
		Now:        func() time.Time { return time.Unix(456, 0) },
	}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if handle.TrainerEndpoint.JobID != "job-new" || handle.Deployment.DeploymentID != "base-456" {
		t.Fatalf("handle = %#v", handle)
	}

	trainer = &fakeManagedTrainer{}
	deployment = &fakeManagedDeployment{existing: map[string]DeploymentInfo{}}
	svc, err = NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:       "accounts/acct/models/base",
			DeploymentShape: "accounts/acct/deploymentShapes/serve",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateManagedTrainingClientFuture(context.Background(), ManagedProvisionOptions{
		Trainer:    trainer,
		Deployment: deployment,
		Now:        func() time.Time { return time.Unix(789, 0) },
	}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if jobID, err := client.TrainerJobID(); err != nil || jobID != "job-new" {
		t.Fatalf("jobID=%q err=%v", jobID, err)
	}
	if deploymentID, err := client.DeploymentID(); err != nil || deploymentID != "base-789" {
		t.Fatalf("deploymentID=%q err=%v", deploymentID, err)
	}
}

func TestFiretitanServiceClientCreateLoraTrainingClientFuture(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateLoraTrainingClientFuture(context.Background(), "accounts/acct/models/base", CreateLoraTrainingClientOptions{
		Rank:         12,
		UserMetadata: map[string]string{"run": "future-lora"},
	}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.BaseModel != "accounts/acct/models/base" || client.Config.LoraRank != 12 {
		t.Fatalf("client config = %#v", client.Config)
	}
	if client.UserMetadata["run"] != "future-lora" {
		t.Fatalf("metadata = %#v", client.UserMetadata)
	}
}

func TestFiretitanServiceClientCreateBaseTrainingClientFuture(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel: "accounts/acct/models/base",
			LoraRank:  8,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTrainingClient(context.Background()); err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateBaseTrainingClientFuture(context.Background(), "accounts/acct/models/base", map[string]string{"purpose": "base"}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.BaseModel != "accounts/acct/models/base" || client.Config.LoraRank != 0 || !client.Config.ForwardOnly {
		t.Fatalf("base client config = %#v", client.Config)
	}
	if client.Config.CreateDeployment == nil || *client.Config.CreateDeployment {
		t.Fatalf("base client should not request deployment: %#v", client.Config.CreateDeployment)
	}
	if client.UserMetadata["purpose"] != "base" {
		t.Fatalf("metadata = %#v", client.UserMetadata)
	}

	policy, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{
		ConfigOverride: &FiretitanProvisioningConfig{
			BaseModel: "accounts/acct/models/base",
			LoraRank:  4,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err = policy.CreateBaseTrainingClientFuture(context.Background(), "accounts/acct/models/base", map[string]string{"purpose": "training-base"}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.LoraRank != 0 || !client.Config.ForwardOnly || client.UserMetadata["purpose"] != "training-base" {
		t.Fatalf("training base client = %#v metadata=%#v", client.Config, client.UserMetadata)
	}
}

func TestFiretitanServiceClientCreateReferenceClientFuture(t *testing.T) {
	shared, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel: "accounts/acct/models/base",
			LoraRank:  8,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := shared.CreateReferenceClientFuture(context.Background(), "accounts/acct/models/base", CreateReferenceClientOptions{LoraRank: 8}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.LoraRank != 0 || !client.Config.ForwardOnly {
		t.Fatalf("shared reference config = %#v", client.Config)
	}

	trainer := &fakeManagedTrainer{}
	deployment := &fakeManagedDeployment{existing: map[string]DeploymentInfo{}}
	separate, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:                "accounts/acct/models/base",
			CreateDeployment:         boolPointer(false),
			ReferenceRequired:        true,
			ReferenceTrainingShapeID: "accounts/acct/trainingShapes/ref",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = separate.ProvisionManagedHandle(context.Background(), ManagedProvisionOptions{
		ProfileResolver: fakeProfileResolver{profiles: map[string]TrainingShapeProfile{
			"accounts/acct/trainingShapes/ref": {
				TrainingShapeVersion: "accounts/acct/trainingShapes/ref/versions/4",
				TrainerMode:          ForwardOnlyMode,
			},
		}},
		Trainer:    trainer,
		Deployment: deployment,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err = separate.CreateReferenceClientFuture(context.Background(), "accounts/acct/models/base", CreateReferenceClientOptions{
		UserMetadata: map[string]string{"purpose": "reference"},
	}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(client.Config.TrainerJobID, autoTrainerJobIDPrefix+"-") || !client.Config.ForwardOnly {
		t.Fatalf("separate reference config = %#v", client.Config)
	}
	if client.UserMetadata["purpose"] != "reference" {
		t.Fatalf("metadata = %#v", client.UserMetadata)
	}
}

func TestFiretitanServiceClientSamplerFutures(t *testing.T) {
	mgr := NewDeploymentManager("fw-key", "https://api.example.com", WithDeploymentInferenceURL("https://inference.example.com"))
	mgr.SetAccountID("acct")
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.AttachSamplerBackend(&TinkerSamplerBackend{
		DeployMgr:    mgr,
		DeploymentID: "dep-1",
		BaseModel:    "accounts/acct/models/base",
	})
	client, err := svc.CreateSamplingClientFuture(context.Background(), "", nil, nil).Await()
	if err != nil {
		t.Fatal(err)
	}
	if client.DeploymentSampler.Model != "accounts/acct/deployments/dep-1" {
		t.Fatalf("client sampler = %#v", client.DeploymentSampler)
	}
	sampler, err := svc.CreateDeploymentSamplerFuture(context.Background(), "", nil, nil).Await()
	if err != nil {
		t.Fatal(err)
	}
	if sampler.Model != "accounts/acct/deployments/dep-1" {
		t.Fatalf("deployment sampler = %#v", sampler)
	}
}

func TestFiretitanServiceClientGetServerCapabilitiesFuture(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := svc.GetServerCapabilitiesFuture(context.Background()).Await()
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilities.SupportedModels) != 1 || capabilities.SupportedModels[0].ModelName != "accounts/acct/models/base" {
		t.Fatalf("capabilities = %#v", capabilities)
	}
}

func TestFiretitanServiceClientCreateTrainingClientFromStateFuture(t *testing.T) {
	state := &fakeTrainingStateBackend{}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	info := WeightsInfo{BaseModel: "accounts/acct/models/resumed"}
	client, err := svc.CreateTrainingClientFromStateFuture(context.Background(), "state://step-1", CreateTrainingClientFromStateOptions{
		StateBackend: state,
		WeightsInfo:  &info,
	}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.BaseModel != "accounts/acct/models/resumed" || state.loadStatePath != "state://step-1" {
		t.Fatalf("client=%#v state=%#v", client.Config, state)
	}

	state2 := &fakeTrainingStateBackend{}
	svc2, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err = svc2.CreateTrainingClientFromStateWithOptimizerFuture(context.Background(), "state://step-2", CreateTrainingClientFromStateOptions{
		StateBackend: state2,
	}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.BaseModel != "accounts/acct/models/base-2" || state2.loadStateOptimizerPath != "state://step-2" {
		t.Fatalf("client=%#v state=%#v", client.Config, state2)
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
	saved, err = client.SaveWeightsForSamplerExtFuture(context.Background(), "step-ext", SaveWeightsForSamplerOptions{CheckpointType: "base", TTL: time.Minute}).Await()
	if err != nil {
		t.Fatal(err)
	}
	if saved.SnapshotName != "step-2-session" || len(saver.calls) < 2 || saver.calls[1].TTL != time.Minute {
		t.Fatalf("saved ext = %#v calls=%#v", saved, saver.calls)
	}
	hotloaded, err := client.SaveWeightsAndHotloadFuture(context.Background(), "step-2").Await()
	if err != nil {
		t.Fatal(err)
	}
	if hotloaded.SnapshotName != "step-2" || client.RequiresInitialSamplerSync() {
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

func TestFiretitanTrainingClientComputeFutures(t *testing.T) {
	backend := &fakeTrainingComputeBackend{
		optimResult:           map[string]any{"status": "ok"},
		forwardBackwardResult: ForwardBackwardOutput{Metrics: map[string]float64{}},
	}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{
		ComputeBackend: backend,
		ModelID:        "model-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	optim, err := client.OptimStepFuture(context.Background(), map[string]any{"beta1": 0.9}, "none").Await()
	if err != nil {
		t.Fatal(err)
	}
	if optim["status"] != "ok" || len(backend.optimCalls) != 1 || backend.optimCalls[0].GradAccumulationNormalization != "none" {
		t.Fatalf("optim=%#v calls=%#v", optim, backend.optimCalls)
	}
	output, err := client.ForwardBackwardFuture(context.Background(), []TrainingDatum{
		{LossFnInputs: map[string]TensorData{"target_tokens": {Data: []int{1, 2}}}},
	}, "cross_entropy", nil).Await()
	if err != nil {
		t.Fatal(err)
	}
	if output.Metrics[ResponseTokensMetric] != 2 || len(backend.forwardBackwardCalls) != 1 {
		t.Fatalf("output=%#v calls=%#v", output, backend.forwardBackwardCalls)
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

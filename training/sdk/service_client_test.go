package sdk

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewFiretitanServiceClientNormalizesConfigAndClonesMetadata(t *testing.T) {
	metadata := map[string]string{"owner": "cookbook"}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel: "accounts/acct/models/base",
		},
		DefaultUserMetadata: metadata,
		Now:                 func() time.Time { return time.Unix(123, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata["owner"] = "mutated"
	if svc.Config.CreateDeployment == nil || !*svc.Config.CreateDeployment {
		t.Fatalf("normalized config = %#v", svc.Config)
	}
	if svc.Config.ReplicaCount == nil || *svc.Config.ReplicaCount != 1 {
		t.Fatalf("replica count = %#v", svc.Config.ReplicaCount)
	}
	if svc.DefaultUserMetadata["owner"] != "cookbook" {
		t.Fatalf("metadata = %#v", svc.DefaultUserMetadata)
	}
}

func TestFiretitanServiceClientCreateTrainingClientRecordsCanonicalManagedKey(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel: "accounts/acct/models/base",
			LoraRank:  8,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	overrideRank := 8
	client, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{
		BaseModel: "accounts/acct/models/OTHER",
		LoraRank:  &overrideRank,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.BaseModel != "accounts/acct/models/base" || client.Config.LoraRank != 8 {
		t.Fatalf("client config = %#v", client.Config)
	}
	if len(client.Warnings) != 1 || !strings.Contains(client.Warnings[0], "base_model") {
		t.Fatalf("warnings = %#v", client.Warnings)
	}
	_, err = svc.CreateTrainingClient(context.Background())
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestFiretitanServiceClientCreateTrainingClientAllowsDifferentSeed(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel: "accounts/acct/models/base",
			Seed:      intPointer(1),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTrainingClient(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc.Config.Seed = intPointer(2)
	if _, err := svc.CreateTrainingClient(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFiretitanServiceClientResolvedMetadata(t *testing.T) {
	maxContext := 8192
	acceleratorCount := 8
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:      "accounts/acct/models/base",
			TrainerJobID:   "trainer-config",
			DeploymentID:   "deployment-config",
			TokenizerModel: "Qwen/Qwen3",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.ManagedHandle = &ManagedHandleMetadata{
		TrainerJobID:     "trainer-handle",
		DeploymentID:     "deployment-handle",
		MaxContextLength: &maxContext,
		DeploymentShape:  "accounts/acct/deploymentShapes/serve/versions/1",
		TrainingProfile: &TrainingShapeProfile{
			AcceleratorType:  "NVIDIA_H100_80GB",
			AcceleratorCount: acceleratorCount,
		},
	}
	if got := svc.ManagedTrainerJobID(); got != "trainer-handle" {
		t.Fatalf("managed trainer id = %q", got)
	}
	if got := svc.ManagedDeploymentID(); got != "deployment-handle" {
		t.Fatalf("managed deployment id = %q", got)
	}
	if got := svc.ManagedMaxContextLength(); got == nil || *got != 8192 {
		t.Fatalf("managed max context = %#v", got)
	}
	if got := svc.ManagedDeploymentShape(); got != "accounts/acct/deploymentShapes/serve/versions/1" {
		t.Fatalf("managed deployment shape = %q", got)
	}
	if got := svc.ManagedTrainingProfile(); got == nil || got.AcceleratorType != "NVIDIA_H100_80GB" {
		t.Fatalf("managed profile = %#v", got)
	}
	if got := svc.ManagedAcceleratorType(); got != "NVIDIA_H100_80GB" {
		t.Fatalf("managed accelerator type = %q", got)
	}
	if got := svc.ManagedAcceleratorCount(); got == nil || *got != acceleratorCount {
		t.Fatalf("managed accelerator count = %#v", got)
	}
	if got, err := svc.TrainerJobID(); err != nil || got != "trainer-handle" {
		t.Fatalf("trainer id = %q err=%v", got, err)
	}
	if got, err := svc.DeploymentID(); err != nil || got != "deployment-handle" {
		t.Fatalf("deployment id = %q err=%v", got, err)
	}
	if got, err := svc.MaxContextLength(); err != nil || got != 8192 {
		t.Fatalf("max context = %d err=%v", got, err)
	}
	if got, err := svc.DeploymentShape(); err != nil || got != "accounts/acct/deploymentShapes/serve/versions/1" {
		t.Fatalf("deployment shape = %q err=%v", got, err)
	}
	if got := svc.TrainingProfile(); got == nil || got.AcceleratorType != "NVIDIA_H100_80GB" {
		t.Fatalf("profile = %#v", got)
	}
	if got := svc.AcceleratorType(); got != "NVIDIA_H100_80GB" {
		t.Fatalf("accelerator type = %q", got)
	}
	if got := svc.AcceleratorCount(); got == nil || *got != acceleratorCount {
		t.Fatalf("accelerator count = %#v", got)
	}
}

func TestFiretitanServiceClientManagedAccessorsAreOptional(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.ManagedTrainerJobID(); got != "" {
		t.Fatalf("managed trainer id = %q", got)
	}
	if got := svc.ManagedDeploymentID(); got != "" {
		t.Fatalf("managed deployment id = %q", got)
	}
	if got := svc.ManagedMaxContextLength(); got != nil {
		t.Fatalf("managed max context = %#v", got)
	}
	if got := svc.ManagedDeploymentShape(); got != "" {
		t.Fatalf("managed deployment shape = %q", got)
	}
	if _, err := svc.TrainerJobID(); err == nil || !strings.Contains(err.Error(), "trainer job id") {
		t.Fatalf("trainer error = %v", err)
	}
	if _, err := svc.DeploymentID(); err == nil || !strings.Contains(err.Error(), "deployment id") {
		t.Fatalf("deployment error = %v", err)
	}
	if _, err := svc.MaxContextLength(); err == nil || !strings.Contains(err.Error(), "max context length") {
		t.Fatalf("max context error = %v", err)
	}
	if _, err := svc.DeploymentShape(); err == nil || !strings.Contains(err.Error(), "deployment shape") {
		t.Fatalf("deployment shape error = %v", err)
	}
}

func TestFiretitanServiceClientReferenceIDsAndRelease(t *testing.T) {
	trainer := &fakeManagedTrainer{}
	deployment := &fakeManagedDeployment{existing: map[string]DeploymentInfo{}}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
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
	_, err = svc.ProvisionManagedHandle(context.Background(), ManagedProvisionOptions{
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
	if svc.ReferenceJobID() != "job-new-2" || svc.ReferenceTrainerJobID() != "job-new-2" {
		t.Fatalf("reference ids = %q %q", svc.ReferenceJobID(), svc.ReferenceTrainerJobID())
	}
	if got, err := svc.ReferenceClientJobID(); err != nil || got != "job-new-2" {
		t.Fatalf("reference client job id = %q err=%v", got, err)
	}
	if err := svc.ReleaseReferences(context.Background()); err != nil {
		t.Fatal(err)
	}
	if svc.ReferenceJobID() != "" {
		t.Fatalf("reference id after release = %q", svc.ReferenceJobID())
	}
	if got, err := svc.ReferenceClientJobID(); err != nil || got != "job-new" {
		t.Fatalf("fallback reference client job id = %q err=%v", got, err)
	}
	if len(trainer.deleted) != 1 || trainer.deleted[0] != "job-new-2" {
		t.Fatalf("deleted trainers = %#v", trainer.deleted)
	}
}

func TestFiretitanServiceClientUtilityFacades(t *testing.T) {
	fireworks := NewFireworksClient("fw-key", "https://api.example.com")
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config:          FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
		FireworksClient: fireworks,
	})
	if err != nil {
		t.Fatal(err)
	}
	if svc.GetTelemetry() != nil {
		t.Fatalf("telemetry = %#v", svc.GetTelemetry())
	}
	if svc.CreateRestClient() != fireworks {
		t.Fatalf("rest client = %#v", svc.CreateRestClient())
	}
	capabilities := svc.GetServerCapabilities()
	if len(capabilities.SupportedModels) != 1 || capabilities.SupportedModels[0].ModelName != "accounts/acct/models/base" {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	var nilSvc *FiretitanServiceClient
	if nilSvc.CreateRestClient() != nil {
		t.Fatal("nil service should not return a rest client")
	}
	if len(nilSvc.GetServerCapabilities().SupportedModels) != 0 {
		t.Fatalf("nil capabilities = %#v", nilSvc.GetServerCapabilities())
	}
}

func TestFiretitanServiceClientCreateBaseTrainingClientSkipsDuplicateRegistry(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTrainingClient(context.Background()); err != nil {
		t.Fatal(err)
	}
	baseClient, err := svc.CreateBaseTrainingClient(context.Background(), "accounts/acct/models/base", map[string]string{"purpose": "reference"})
	if err != nil {
		t.Fatal(err)
	}
	if baseClient.Config.BaseModel != "accounts/acct/models/base" || baseClient.Config.LoraRank != 0 || !baseClient.Config.ForwardOnly {
		t.Fatalf("base client config = %#v", baseClient.Config)
	}
	if baseClient.Config.CreateDeployment == nil || *baseClient.Config.CreateDeployment {
		t.Fatalf("base client should not create deployment: %#v", baseClient.Config.CreateDeployment)
	}
	if baseClient.UserMetadata["purpose"] != "reference" {
		t.Fatalf("metadata = %#v", baseClient.UserMetadata)
	}
}

func TestFiretitanServiceClientCreateLoraTrainingClient(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateLoraTrainingClient(context.Background(), "accounts/acct/models/base", CreateLoraTrainingClientOptions{
		UserMetadata: map[string]string{"purpose": "lora"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.BaseModel != "accounts/acct/models/base" || client.Config.LoraRank != 32 {
		t.Fatalf("client config = %#v", client.Config)
	}
	if client.Config.TrainMLP == nil || !*client.Config.TrainMLP || client.Config.TrainAttn == nil || !*client.Config.TrainAttn || client.Config.TrainUnembed == nil || !*client.Config.TrainUnembed {
		t.Fatalf("train flags = mlp:%#v attn:%#v unembed:%#v", client.Config.TrainMLP, client.Config.TrainAttn, client.Config.TrainUnembed)
	}
	if client.UserMetadata["purpose"] != "lora" {
		t.Fatalf("metadata = %#v", client.UserMetadata)
	}
}

func TestFiretitanServiceClientCreateLoraTrainingClientOverrides(t *testing.T) {
	seed := 123
	trainMLP := false
	trainAttn := true
	trainUnembed := false
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateLoraTrainingClient(context.Background(), "accounts/acct/models/base", CreateLoraTrainingClientOptions{
		Rank:         16,
		Seed:         &seed,
		TrainMLP:     &trainMLP,
		TrainAttn:    &trainAttn,
		TrainUnembed: &trainUnembed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.LoraRank != 16 || client.Config.Seed == nil || *client.Config.Seed != 123 {
		t.Fatalf("client config = %#v", client.Config)
	}
	if client.Config.TrainMLP == nil || *client.Config.TrainMLP || client.Config.TrainAttn == nil || !*client.Config.TrainAttn || client.Config.TrainUnembed == nil || *client.Config.TrainUnembed {
		t.Fatalf("train flags = mlp:%#v attn:%#v unembed:%#v", client.Config.TrainMLP, client.Config.TrainAttn, client.Config.TrainUnembed)
	}
}

func TestFiretitanTrainingClientCreateBaseTrainingClientDelegatesToService(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base", LoraRank: 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := svc.CreateTrainingClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	baseClient, err := policy.CreateBaseTrainingClient(context.Background(), "accounts/acct/models/base", map[string]string{"purpose": "base"})
	if err != nil {
		t.Fatal(err)
	}
	if baseClient.Config.LoraRank != 0 || !baseClient.Config.ForwardOnly {
		t.Fatalf("base config = %#v", baseClient.Config)
	}
	if baseClient.UserMetadata["purpose"] != "base" {
		t.Fatalf("metadata = %#v", baseClient.UserMetadata)
	}
}

func TestFiretitanServiceClientCreateReferenceClientSharedBase(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel: "accounts/acct/models/base",
			LoraRank:  8,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateReferenceClient(context.Background(), "accounts/acct/models/base", CreateReferenceClientOptions{LoraRank: 8})
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.LoraRank != 0 || !client.Config.ForwardOnly {
		t.Fatalf("reference config = %#v", client.Config)
	}
	if svc.ReferenceHandle != nil {
		t.Fatalf("shared reference should not require separate handle: %#v", svc.ReferenceHandle)
	}
}

func TestFiretitanServiceClientCreateReferenceClientSeparateHandle(t *testing.T) {
	trainer := &fakeManagedTrainer{}
	deployment := &fakeManagedDeployment{existing: map[string]DeploymentInfo{}}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
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
	_, err = svc.ProvisionManagedHandle(context.Background(), ManagedProvisionOptions{
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
	client, err := svc.CreateReferenceClient(context.Background(), "accounts/acct/models/base", CreateReferenceClientOptions{UserMetadata: map[string]string{"purpose": "reference"}})
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.TrainerJobID != "job-new-2" || !client.Config.ForwardOnly {
		t.Fatalf("reference config = %#v", client.Config)
	}
	if client.HandleMetadata.TrainerJobID != "job-new-2" {
		t.Fatalf("handle metadata = %#v", client.HandleMetadata)
	}
	if client.UserMetadata["purpose"] != "reference" {
		t.Fatalf("metadata = %#v", client.UserMetadata)
	}
}

func TestFiretitanTrainingClientUsesDefaultAndCallMetadata(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config:              FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
		DefaultUserMetadata: map[string]string{"owner": "default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{
		UserMetadata: map[string]string{"owner": "call"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.UserMetadata["owner"] != "call" {
		t.Fatalf("metadata = %#v", client.UserMetadata)
	}
	client.UserMetadata["owner"] = "mutated"
	if svc.DefaultUserMetadata["owner"] != "default" {
		t.Fatalf("service metadata = %#v", svc.DefaultUserMetadata)
	}
}

func TestFiretitanTrainingClientCheckpointDelegation(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:    "accounts/acct/models/base",
			TrainerJobID: "trainer-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var listedJob string
	var listedPageSize int
	svc.ListCheckpointsFunc = func(_ context.Context, jobID string, pageSize int) ([]map[string]any, error) {
		listedJob = jobID
		listedPageSize = pageSize
		return []map[string]any{{"name": "cp"}}, nil
	}
	var promoted PromoteCheckpointOptions
	svc.PromoteCheckpointFunc = func(_ context.Context, opts PromoteCheckpointOptions) (map[string]any, error) {
		promoted = opts
		return map[string]any{"name": "accounts/acct/models/out"}, nil
	}
	client, err := svc.CreateTrainingClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := client.ListCheckpoints(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if listedJob != "trainer-1" || listedPageSize != 50 || rows[0]["name"] != "cp" {
		t.Fatalf("list job=%q page=%d rows=%#v", listedJob, listedPageSize, rows)
	}
	model, err := client.PromoteCheckpoint(context.Background(), PromoteCheckpointOptions{
		CheckpointID:  "step-1",
		OutputModelID: "out",
	})
	if err != nil {
		t.Fatal(err)
	}
	if promoted.JobID != "trainer-1" || promoted.BaseModel != "accounts/acct/models/base" || model["name"] != "accounts/acct/models/out" {
		t.Fatalf("promote opts=%#v model=%#v", promoted, model)
	}
}

func TestFiretitanServiceClientCheckpointDelegation(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:    "accounts/acct/models/base",
			TrainerJobID: "trainer-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var listedJob string
	var listedPageSize int
	svc.ListCheckpointsFunc = func(_ context.Context, jobID string, pageSize int) ([]map[string]any, error) {
		listedJob = jobID
		listedPageSize = pageSize
		return []map[string]any{{"name": "cp"}}, nil
	}
	var promoted PromoteCheckpointOptions
	svc.PromoteCheckpointFunc = func(_ context.Context, opts PromoteCheckpointOptions) (map[string]any, error) {
		promoted = opts
		return map[string]any{"name": "accounts/acct/models/out"}, nil
	}
	rows, err := svc.ListCheckpoints(context.Background(), "", 25)
	if err != nil {
		t.Fatal(err)
	}
	if listedJob != "trainer-1" || listedPageSize != 25 || rows[0]["name"] != "cp" {
		t.Fatalf("list job=%q page=%d rows=%#v", listedJob, listedPageSize, rows)
	}
	model, err := svc.PromoteCheckpoint(context.Background(), PromoteCheckpointOptions{
		CheckpointID:  "step-1",
		OutputModelID: "out",
	})
	if err != nil {
		t.Fatal(err)
	}
	if promoted.JobID != "trainer-1" || promoted.BaseModel != "accounts/acct/models/base" || model["name"] != "accounts/acct/models/out" {
		t.Fatalf("promote opts=%#v model=%#v", promoted, model)
	}
}

func TestFiretitanServiceClientCheckpointOpsRequireTrainer(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListCheckpoints(context.Background(), "", 0); err == nil || !strings.Contains(err.Error(), "provisioned trainer") {
		t.Fatalf("list error = %v", err)
	}
	if _, err := svc.PromoteCheckpoint(context.Background(), PromoteCheckpointOptions{}); err == nil || !strings.Contains(err.Error(), "provisioned trainer") {
		t.Fatalf("promote error = %v", err)
	}
}

func TestFiretitanTrainingClientCheckpointOpsRequireTrainer(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListCheckpoints(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "provisioned trainer") {
		t.Fatalf("list error = %v", err)
	}
	if _, err := client.PromoteCheckpoint(context.Background(), PromoteCheckpointOptions{}); err == nil || !strings.Contains(err.Error(), "provisioned trainer") {
		t.Fatalf("promote error = %v", err)
	}
}

type fakeTrainingStateBackend struct {
	saveCalls              []string
	saveOptions            []SaveStateOptions
	loadStatePath          string
	loadStateOptimizerPath string
}

func (b *fakeTrainingStateBackend) SaveState(_ context.Context, name string, opts SaveStateOptions) (SaveStateResult, error) {
	b.saveCalls = append(b.saveCalls, name)
	b.saveOptions = append(b.saveOptions, opts)
	return SaveStateResult{Name: name, Path: "state://" + name}, nil
}

func (b *fakeTrainingStateBackend) LoadState(_ context.Context, path string) error {
	b.loadStatePath = path
	return nil
}

func (b *fakeTrainingStateBackend) LoadStateWithOptimizer(_ context.Context, path string) error {
	b.loadStateOptimizerPath = path
	return nil
}

type fakeTrainingAdapterLoader struct {
	calls []LoadAdapterOptions
}

func (l *fakeTrainingAdapterLoader) LoadAdapter(_ context.Context, opts LoadAdapterOptions) (LoadAdapterResponse, error) {
	l.calls = append(l.calls, opts)
	return LoadAdapterResponse{ModelID: opts.ModelID, AdapterPath: opts.AdapterPath, Type: "load_adapter"}, nil
}

type fakeTrainingComputeBackend struct {
	optimCalls            []OptimStepOptions
	forwardBackwardCalls  []ForwardBackwardOptions
	optimResult           map[string]any
	forwardBackwardResult ForwardBackwardOutput
}

func (b *fakeTrainingComputeBackend) OptimStep(_ context.Context, opts OptimStepOptions) (map[string]any, error) {
	b.optimCalls = append(b.optimCalls, opts)
	if b.optimResult != nil {
		return cloneAnyMap(b.optimResult), nil
	}
	return map[string]any{"ok": true}, nil
}

func (b *fakeTrainingComputeBackend) ForwardBackward(_ context.Context, opts ForwardBackwardOptions) (ForwardBackwardOutput, error) {
	b.forwardBackwardCalls = append(b.forwardBackwardCalls, opts)
	return b.forwardBackwardResult, nil
}

func TestFiretitanServiceClientCreateTrainingClientFromStateProvider(t *testing.T) {
	state := &fakeTrainingStateBackend{}
	rank := 8
	trainUnembed := false
	trainMLP := true
	trainAttn := false
	var providerPath string
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClientFromState(context.Background(), "state://step-1", CreateTrainingClientFromStateOptions{
		UserMetadata: map[string]string{"run": "resume"},
		StateBackend: state,
		WeightsInfoProvider: func(_ context.Context, path string) (WeightsInfo, error) {
			providerPath = path
			return WeightsInfo{
				BaseModel:    "accounts/acct/models/resumed",
				IsLora:       true,
				LoraRank:     &rank,
				TrainUnembed: &trainUnembed,
				TrainMLP:     &trainMLP,
				TrainAttn:    &trainAttn,
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if providerPath != "state://step-1" || state.loadStatePath != "state://step-1" {
		t.Fatalf("providerPath=%q state=%#v", providerPath, state)
	}
	if client.Config.BaseModel != "accounts/acct/models/resumed" || client.Config.LoraRank != 8 {
		t.Fatalf("client config = %#v", client.Config)
	}
	if client.Config.TrainUnembed == nil || *client.Config.TrainUnembed || client.Config.TrainAttn == nil || *client.Config.TrainAttn {
		t.Fatalf("train flags = %#v", client.Config)
	}
	if client.UserMetadata["run"] != "resume" {
		t.Fatalf("metadata = %#v", client.UserMetadata)
	}
}

func TestFiretitanServiceClientCreateTrainingClientFromStateWithOptimizer(t *testing.T) {
	state := &fakeTrainingStateBackend{}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	info := WeightsInfo{BaseModel: "accounts/acct/models/resumed"}
	client, err := svc.CreateTrainingClientFromStateWithOptimizer(context.Background(), "state://step-2", CreateTrainingClientFromStateOptions{
		StateBackend: state,
		WeightsInfo:  &info,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.BaseModel != "accounts/acct/models/resumed" || client.Config.LoraRank != 0 {
		t.Fatalf("client config = %#v", client.Config)
	}
	if state.loadStateOptimizerPath != "state://step-2" || state.loadStatePath != "" {
		t.Fatalf("state backend = %#v", state)
	}
}

func TestFiretitanServiceClientCreateTrainingClientFromStateRejectsToken(t *testing.T) {
	state := &fakeTrainingStateBackend{}
	token := "token"
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateTrainingClientFromState(context.Background(), "state://step-1", CreateTrainingClientFromStateOptions{
		StateBackend:       state,
		WeightsAccessToken: &token,
	})
	if err == nil || !strings.Contains(err.Error(), "FiretitanServiceClient.create_training_client_from_state(weights_access_token=...)") {
		t.Fatalf("token error = %v", err)
	}
}

func TestFiretitanServiceClientCreateTrainingClientFromStateUsesManagedConfigFallback(t *testing.T) {
	state := &fakeTrainingStateBackend{}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel: "accounts/acct/models/base",
			LoraRank:  4,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClientFromState(context.Background(), "state://managed", CreateTrainingClientFromStateOptions{StateBackend: state})
	if err != nil {
		t.Fatal(err)
	}
	if client.Config.BaseModel != "accounts/acct/models/base" || client.Config.LoraRank != 4 {
		t.Fatalf("client config = %#v", client.Config)
	}
	if state.loadStatePath != "state://managed" {
		t.Fatalf("state backend = %#v", state)
	}
}

func TestFiretitanTrainingClientStateDelegationAndWarnings(t *testing.T) {
	state := &fakeTrainingStateBackend{}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{StateBackend: state})
	if err != nil {
		t.Fatal(err)
	}
	ttl := 60
	result, err := client.SaveState(context.Background(), " step-1 ", SaveStateOptions{TTLSeconds: &ttl})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != "state://step-1" || len(state.saveCalls) != 1 || state.saveCalls[0] != "step-1" {
		t.Fatalf("save result=%#v calls=%#v", result, state.saveCalls)
	}
	if state.saveOptions[0].TTLSeconds == nil || *state.saveOptions[0].TTLSeconds != 60 {
		t.Fatalf("save options = %#v", state.saveOptions[0])
	}
	if _, err := client.SaveState(context.Background(), "step-1"); err != nil {
		t.Fatal(err)
	}
	if len(client.Warnings) != 1 || !strings.Contains(client.Warnings[0], "DCP checkpoint name") {
		t.Fatalf("warnings = %#v", client.Warnings)
	}
	if _, err := client.SaveState(context.Background(), "bad", SaveStateOptions{Overwrite: true}); err == nil || !strings.Contains(err.Error(), "overwrite=True") {
		t.Fatalf("overwrite error = %v", err)
	}
	if err := client.LoadState(context.Background(), "state://step-1", nil); err != nil {
		t.Fatal(err)
	}
	if state.loadStatePath != "state://step-1" {
		t.Fatalf("load path = %q", state.loadStatePath)
	}
	if err := client.LoadStateWithOptimizer(context.Background(), "state://step-2", nil); err != nil {
		t.Fatal(err)
	}
	if state.loadStateOptimizerPath != "state://step-2" {
		t.Fatalf("optimizer load path = %q", state.loadStateOptimizerPath)
	}
	token := "token"
	if err := client.LoadState(context.Background(), "state://step-1", &token); err == nil || !strings.Contains(err.Error(), "weights_access_token") {
		t.Fatalf("token error = %v", err)
	}
}

func TestFiretitanTrainingClientLoadAdapterDelegates(t *testing.T) {
	loader := &fakeTrainingAdapterLoader{}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{
		AdapterLoader:  loader,
		ModelID:        "model-1",
		TrainerBaseURL: "https://trainer.example.com/",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.LoadAdapter(context.Background(), " gs://bucket/adapter ")
	if err != nil {
		t.Fatal(err)
	}
	if resp.ModelID != "model-1" || resp.AdapterPath != "gs://bucket/adapter" || resp.Type != "load_adapter" {
		t.Fatalf("response = %#v", resp)
	}
	if len(loader.calls) != 1 {
		t.Fatalf("calls = %#v", loader.calls)
	}
	call := loader.calls[0]
	if call.TrainerBaseURL != "https://trainer.example.com/" || call.ModelID != "model-1" || call.AdapterPath != "gs://bucket/adapter" || call.SeqID != 1 {
		t.Fatalf("call = %#v", call)
	}
}

func TestFiretitanTrainingClientOptimStepDelegates(t *testing.T) {
	backend := &fakeTrainingComputeBackend{optimResult: map[string]any{"status": "ok"}}
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
	result, err := client.OptimStep(context.Background(), map[string]any{"learning_rate": 0.1}, GradAccNormalizationNumLossTokens)
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "ok" || len(backend.optimCalls) != 1 {
		t.Fatalf("result=%#v calls=%#v", result, backend.optimCalls)
	}
	call := backend.optimCalls[0]
	if call.ModelID != "model-1" || call.SeqID != 1 || call.GradAccumulationNormalization != "num_loss_tokens" {
		t.Fatalf("call = %#v", call)
	}
	if client.AttachComputeBackend(backend) != client {
		t.Fatal("AttachComputeBackend should return client")
	}
}

func TestFiretitanTrainingClientForwardBackwardDelegatesAndCountsTokens(t *testing.T) {
	backend := &fakeTrainingComputeBackend{
		forwardBackwardResult: ForwardBackwardOutput{Metrics: map[string]float64{"loss:sum": 3}},
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
	data := []TrainingDatum{{LossFnInputs: map[string]TensorData{"target_tokens": {Data: []int{1, 2, 3}}}}}
	output, err := client.ForwardBackward(context.Background(), data, "cross_entropy", map[string]any{"temperature": 1.0})
	if err != nil {
		t.Fatal(err)
	}
	if output.Metrics[ResponseTokensMetric] != 3 {
		t.Fatalf("metrics = %#v", output.Metrics)
	}
	if len(backend.forwardBackwardCalls) != 1 {
		t.Fatalf("calls = %#v", backend.forwardBackwardCalls)
	}
	call := backend.forwardBackwardCalls[0]
	if call.ModelID != "model-1" || call.SeqID != 1 || call.LossFn != LossFnCrossEntropy || call.LossFnConfig["temperature"] != 1.0 {
		t.Fatalf("call = %#v", call)
	}
}

func TestFiretitanTrainingClientForwardBackwardContrastiveDelegates(t *testing.T) {
	backend := &fakeTrainingComputeBackend{
		forwardBackwardResult: ForwardBackwardOutput{Metrics: map[string]float64{"loss": 1.25}},
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
	data := []TrainingDatum{{}, {}, {}, {}, {}}
	output, err := client.ForwardBackwardContrastive(context.Background(), data, ForwardBackwardContrastiveOptions{
		NumQueries:        2,
		Temperature:       0.02,
		Pooling:           EmbeddingPoolingMean,
		NumExtraNegatives: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.Metrics["loss"] != 1.25 {
		t.Fatalf("output = %#v", output)
	}
	if len(backend.forwardBackwardCalls) != 1 {
		t.Fatalf("calls = %#v", backend.forwardBackwardCalls)
	}
	call := backend.forwardBackwardCalls[0]
	if call.LossFn != LossFnCrossEntropy || call.LossFnConfig["output"] != "contrastive_loss" || call.LossFnConfig["num_queries"] != 2 || call.LossFnConfig["temperature"] != 0.02 || call.LossFnConfig["pooling"] != "mean" || call.LossFnConfig["num_extra_negatives"] != 1 {
		t.Fatalf("call = %#v", call)
	}
}

func TestFiretitanTrainingClientForwardBackwardContrastiveValidatesLength(t *testing.T) {
	client := (&FiretitanTrainingClient{ModelID: "model-1"}).AttachComputeBackend(&fakeTrainingComputeBackend{})
	_, err := client.ForwardBackwardContrastive(context.Background(), []TrainingDatum{{}}, ForwardBackwardContrastiveOptions{
		NumQueries:        1,
		NumExtraNegatives: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "2*num_queries + num_extra_negatives") {
		t.Fatalf("err = %v", err)
	}
}

func TestFiretitanServiceClientSessionCheckpointWrappers(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var listedName string
	var promoted PromoteSessionCheckpointOptions
	svc.ListTrainingSessionCheckpointsFunc = func(_ context.Context, name string, pageSize int) ([]map[string]any, error) {
		listedName = name
		if pageSize != 123 {
			t.Fatalf("pageSize = %d", pageSize)
		}
		return []map[string]any{{"name": "cp-1"}}, nil
	}
	svc.PromoteSessionCheckpointFunc = func(_ context.Context, opts PromoteSessionCheckpointOptions) (map[string]any, error) {
		promoted = opts
		return map[string]any{"name": "accounts/acct/models/out"}, nil
	}
	rows, err := svc.ListTrainingSessionCheckpoints(context.Background(), "accounts/acct/trainingSessions/sess-1", 123)
	if err != nil {
		t.Fatal(err)
	}
	model, err := svc.PromoteSessionCheckpoint(context.Background(), PromoteSessionCheckpointOptions{
		Name:          "accounts/acct/trainingSessions/sess-1/checkpoints/cp-1",
		OutputModelID: "out",
	})
	if err != nil {
		t.Fatal(err)
	}
	if listedName != "accounts/acct/trainingSessions/sess-1" || rows[0]["name"] != "cp-1" {
		t.Fatalf("listedName=%q rows=%#v", listedName, rows)
	}
	if promoted.BaseModel != "accounts/acct/models/base" || model["name"] != "accounts/acct/models/out" {
		t.Fatalf("promoted=%#v model=%#v", promoted, model)
	}
}

func TestFiretitanTrainingClientComputeRequiresBackendAndModel(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.OptimStep(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "ComputeBackend") {
		t.Fatalf("optim error = %v", err)
	}
	client.AttachComputeBackend(&fakeTrainingComputeBackend{})
	if _, err := client.ForwardBackward(context.Background(), nil, "cross_entropy", nil); err == nil || !strings.Contains(err.Error(), "model_id") {
		t.Fatalf("forward_backward error = %v", err)
	}
}

func TestFiretitanTrainingClientSamplerSyncAndHotload(t *testing.T) {
	var calls []string
	backend := &TinkerSamplerBackend{
		DeploymentID: "dep-1",
		BaseModel:    "accounts/acct/models/base",
		HotloadAndWait: func(_ context.Context, deploymentID, baseModel, snapshotIdentity string, _ ...HotloadAndWaitOptions) (bool, error) {
			calls = append(calls, deploymentID+"|"+baseModel+"|"+snapshotIdentity)
			return true, nil
		},
	}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{
		SamplerBackend:             backend,
		RequiresInitialSamplerSync: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !client.RequiresInitialSamplerSync() {
		t.Fatal("expected initial sampler sync")
	}
	if err := client.HotloadSamplerSnapshot(context.Background(), "snap-1"); err != nil {
		t.Fatal(err)
	}
	if client.RequiresInitialSamplerSync() {
		t.Fatal("hotload should clear initial sampler sync")
	}
	if !reflect.DeepEqual(calls, []string{"dep-1|accounts/acct/models/base|snap-1"}) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestFiretitanServiceClientSamplerFacade(t *testing.T) {
	mgr := NewDeploymentManager("fw-key", "https://api.example.com", WithDeploymentInferenceURL("https://inference.example.com"))
	mgr.SetAccountID("acct")
	backend := &TinkerSamplerBackend{
		DeployMgr:    mgr,
		DeploymentID: "dep-1",
		BaseModel:    "accounts/acct/models/base",
	}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if svc.AttachSamplerBackend(backend) != svc {
		t.Fatal("AttachSamplerBackend should return the service")
	}
	tokenizer := &fakeDeploymentTokenizer{tokens: []int{1, 2}}
	controller := NewFixedConcurrencyController(2)
	client, err := svc.CreateSamplingClient(context.Background(), "", tokenizer, controller)
	if err != nil {
		t.Fatal(err)
	}
	if client.DeploymentSampler.Model != "accounts/acct/deployments/dep-1" {
		t.Fatalf("model = %q", client.DeploymentSampler.Model)
	}
	if client.DeploymentSampler.Tokenizer != tokenizer || client.DeploymentSampler.ConcurrencyController != controller {
		t.Fatalf("sampler wiring = %#v", client.DeploymentSampler)
	}
	sampler, err := svc.CreateDeploymentSampler(context.Background(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sampler.Model != "accounts/acct/deployments/dep-1" {
		t.Fatalf("deployment sampler model = %q", sampler.Model)
	}
}

func TestFiretitanServiceClientSamplerFacadeHotloadsSnapshot(t *testing.T) {
	mgr := NewDeploymentManager("fw-key", "https://api.example.com")
	mgr.SetAccountID("acct")
	var calls []string
	backend := &TinkerSamplerBackend{
		DeployMgr:    mgr,
		DeploymentID: "dep-1",
		BaseModel:    "accounts/acct/models/base",
		HotloadAndWait: func(_ context.Context, deploymentID, baseModel, snapshotIdentity string, _ ...HotloadAndWaitOptions) (bool, error) {
			calls = append(calls, deploymentID+"|"+baseModel+"|"+snapshotIdentity)
			return true, nil
		},
	}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.AttachSamplerBackend(backend)
	svc.SyncState = ManagedSamplerSyncState{RequiresInitialSamplerSync: true}
	if !svc.RequiresInitialSamplerSync() {
		t.Fatal("expected initial sync")
	}
	if _, err := svc.CreateSamplingClient(context.Background(), "snap-1", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"dep-1|accounts/acct/models/base|snap-1"}) {
		t.Fatalf("calls = %#v", calls)
	}
	if svc.RequiresInitialSamplerSync() {
		t.Fatal("hotload should clear initial sync")
	}
}

func TestFiretitanServiceClientCreateSamplingClientWrapsExplicitSampler(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tokenizer := &fakeDeploymentTokenizer{tokens: []int{1, 2}}
	controller := NewFixedConcurrencyController(1)
	sampler := NewDeploymentSampler("https://inference.example.com", "accounts/acct/deployments/dep-1", "key")
	client, err := svc.CreateSamplingClient(context.Background(), "", tokenizer, controller, sampler)
	if err != nil {
		t.Fatal(err)
	}
	if client.DeploymentSampler != sampler {
		t.Fatalf("wrapped sampler = %#v", client.DeploymentSampler)
	}
	if sampler.Tokenizer != tokenizer || sampler.ConcurrencyController != controller {
		t.Fatalf("sampler wiring = %#v", sampler)
	}
}

func TestFiretitanTrainingClientWeightSyncerSaveWeightsForSampler(t *testing.T) {
	saver := &fakeSamplerSaver{
		results: []SaveSamplerResult{{Path: "raw/path", SnapshotName: "step-1-session"}},
	}
	syncer := NewWeightSyncer(WeightSyncerConfig{
		PolicyClient: saver,
		BaseModel:    "accounts/acct/models/base",
		LoraRank:     0,
	})
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{
		WeightSyncer: syncer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := client.NextSamplerCheckpointType("delta"); err != nil || got != SamplerCheckpointTypeDelta {
		t.Fatalf("checkpoint type = %q err=%v", got, err)
	}
	result, err := client.SaveWeightsForSampler(context.Background(), "step-1", "base")
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != "step-1-session" || result.SnapshotName != "step-1-session" {
		t.Fatalf("result = %#v", result)
	}
	if len(saver.calls) != 1 || saver.calls[0].CheckpointType != "base" {
		t.Fatalf("save calls = %#v", saver.calls)
	}
}

func TestFiretitanTrainingClientSaveWeightsForSamplerExtRecordsSnapshotType(t *testing.T) {
	saver := &fakeSamplerSaver{
		results: []SaveSamplerResult{{Path: "storage/path", SnapshotName: "step-1-session"}},
	}
	syncer := NewWeightSyncer(WeightSyncerConfig{
		PolicyClient: saver,
		BaseModel:    "accounts/acct/models/base",
	})
	backend := &TinkerSamplerBackend{}
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background(), CreateFiretitanTrainingClientOptions{
		SamplerBackend: backend,
		WeightSyncer:   syncer,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.SaveWeightsForSamplerExt(context.Background(), "step-1", SaveWeightsForSamplerOptions{
		CheckpointType: "delta",
		TTL:            time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != "step-1-session" || result.SnapshotName != "step-1-session" {
		t.Fatalf("result = %#v", result)
	}
	if len(saver.calls) != 1 || saver.calls[0].CheckpointType != "delta" || saver.calls[0].TTL != time.Hour {
		t.Fatalf("save calls = %#v", saver.calls)
	}
	if backend.snapshotTypes["step-1-session"] != "delta" {
		t.Fatalf("snapshot types = %#v", backend.snapshotTypes)
	}
}

func TestFiretitanTrainingClientResolveCheckpointPath(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.ResolveCheckpointPath("checkpoint-1", "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "cross_job://job-1/checkpoint-1" {
		t.Fatalf("path = %q", got)
	}
}

func TestFiretitanTrainingClientSaveWeightsAndHotloadClearsInitialSync(t *testing.T) {
	saver := &fakeSamplerSaver{
		results: []SaveSamplerResult{{Path: "raw/path", SnapshotName: "step-1-session"}},
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
	if !client.RequiresInitialSamplerSync() {
		t.Fatal("expected initial sync")
	}
	result, err := client.SaveWeightsAndHotload(context.Background(), "step-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != "step-1-session" || client.RequiresInitialSamplerSync() {
		t.Fatalf("result = %#v initialSync=%t", result, client.RequiresInitialSamplerSync())
	}
}

func TestFiretitanTrainingClientWeightSyncerUnsupportedAndAttach(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SaveWeightsForSampler(context.Background(), "step-1"); err == nil || !strings.Contains(err.Error(), "WeightSyncer") {
		t.Fatalf("save error = %v", err)
	}
	syncer := NewWeightSyncer(WeightSyncerConfig{})
	if client.AttachWeightSyncer(syncer) != client || client.WeightSyncer != syncer {
		t.Fatal("AttachWeightSyncer should set and return the client")
	}
	if _, err := client.SaveWeightsAndGetSamplingClient(context.Background(), "step-1", nil); err == nil || err.Error() != SamplingClientFromTrainerMessage {
		t.Fatalf("unsupported error = %v", err)
	}
}

func TestFiretitanTrainingClientSamplerRequiresBackend(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.HotloadSamplerSnapshot(context.Background(), "snap-1"); err == nil || !strings.Contains(err.Error(), "SDK-managed sampler state") {
		t.Fatalf("hotload error = %v", err)
	}
	if _, err := client.CreateSamplingClient(context.Background(), "", nil, nil); err == nil || !strings.Contains(err.Error(), "SDK-managed sampler state") {
		t.Fatalf("sampling error = %v", err)
	}
	if client.AttachSamplerBackend(&TinkerSamplerBackend{}) != client {
		t.Fatal("AttachSamplerBackend should return the client")
	}
}

func TestFiretitanTrainingClientGetTokenizerModel(t *testing.T) {
	svc, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:      "accounts/acct/models/base",
			TokenizerModel: " Qwen/Qwen3 ",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.CreateTrainingClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := client.GetTokenizerModel(); err != nil || got != "Qwen/Qwen3" {
		t.Fatalf("tokenizer = %q err=%v", got, err)
	}
}

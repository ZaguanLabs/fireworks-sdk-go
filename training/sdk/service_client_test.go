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
	}
	if got, err := svc.ManagedTrainerJobID(); err != nil || got != "trainer-handle" {
		t.Fatalf("trainer id = %q err=%v", got, err)
	}
	if got, err := svc.ManagedDeploymentID(); err != nil || got != "deployment-handle" {
		t.Fatalf("deployment id = %q err=%v", got, err)
	}
	if got, err := svc.ManagedMaxContextLength(); err != nil || got != 8192 {
		t.Fatalf("max context = %d err=%v", got, err)
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

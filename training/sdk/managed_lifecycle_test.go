package sdk

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fakeProfileResolver struct {
	profiles map[string]TrainingShapeProfile
}

func (r fakeProfileResolver) ResolveTrainingProfile(_ context.Context, id string) (TrainingShapeProfile, error) {
	return r.profiles[id], nil
}

type fakeManagedTrainer struct {
	accountID       string
	created         []TrainerJobConfig
	waited          []string
	reconnected     []string
	deleted         []string
	endpointBaseURL string
}

func (t *fakeManagedTrainer) AccountID(context.Context) (string, error) {
	if t.accountID == "" {
		return "acct", nil
	}
	return t.accountID, nil
}

func (t *fakeManagedTrainer) Create(_ context.Context, config TrainerJobConfig) (CreatedTrainerJob, error) {
	t.created = append(t.created, config)
	jobID := "job-new"
	if len(t.created) > 1 {
		jobID = "job-new-" + strconv.Itoa(len(t.created))
	}
	return CreatedTrainerJob{
		JobName: "accounts/acct/rlorTrainerJobs/" + jobID,
		JobID:   jobID,
	}, nil
}

func (t *fakeManagedTrainer) WaitForReady(_ context.Context, jobID, jobName string, _ ...TrainerPollOptions) (TrainerServiceEndpoint, error) {
	t.waited = append(t.waited, jobID)
	baseURL := t.endpointBaseURL
	if baseURL == "" {
		baseURL = "https://trainer.example.com"
	}
	return TrainerServiceEndpoint{JobName: jobName, JobID: jobID, BaseURL: baseURL}, nil
}

func (t *fakeManagedTrainer) ReconnectAndWait(_ context.Context, jobID string, _ ...TrainerReconnectOptions) (TrainerServiceEndpoint, error) {
	t.reconnected = append(t.reconnected, jobID)
	return TrainerServiceEndpoint{
		JobName: "accounts/acct/rlorTrainerJobs/" + jobID,
		JobID:   jobID,
		BaseURL: "https://trainer.example.com",
	}, nil
}

func (t *fakeManagedTrainer) DeleteJob(_ context.Context, jobID string) error {
	t.deleted = append(t.deleted, jobID)
	return nil
}

type fakeManagedDeployment struct {
	existing     map[string]DeploymentInfo
	created      []DeploymentConfig
	waited       []string
	reattached   []string
	deleted      []string
	scaledToZero []string
}

func (d *fakeManagedDeployment) GetInfo(_ context.Context, deploymentID string) (DeploymentInfo, bool, error) {
	info, ok := d.existing[deploymentID]
	return info, ok, nil
}

func (d *fakeManagedDeployment) CreateOrGet(_ context.Context, config DeploymentConfig, _ bool) (DeploymentInfo, error) {
	d.created = append(d.created, config)
	return DeploymentInfo{
		DeploymentID:           config.DeploymentID,
		State:                  "CREATING",
		HotLoadTrainerJob:      config.HotLoadTrainerJob,
		DeploymentShapeVersion: config.DeploymentShape,
	}, nil
}

func (d *fakeManagedDeployment) WaitForReady(_ context.Context, deploymentID string, _ ...DeploymentWaitOptions) (DeploymentInfo, error) {
	d.waited = append(d.waited, deploymentID)
	info := d.existing[deploymentID]
	if info.DeploymentID == "" {
		info.DeploymentID = deploymentID
	}
	info.State = DeploymentStateReady
	return info, nil
}

func (d *fakeManagedDeployment) ReattachTrainer(_ context.Context, deployment DeploymentInfo, _ string, trainerJobName string, _ ...ReattachTrainerOptions) (DeploymentInfo, error) {
	d.reattached = append(d.reattached, deployment.DeploymentID)
	deployment.HotLoadTrainerJob = trainerJobName
	return deployment, nil
}

func (d *fakeManagedDeployment) DeleteInfo(_ context.Context, deploymentID string) error {
	d.deleted = append(d.deleted, deploymentID)
	return nil
}

func (d *fakeManagedDeployment) ScaleToZero(_ context.Context, deploymentID string) error {
	d.scaledToZero = append(d.scaledToZero, deploymentID)
	return nil
}

func TestProvisionManagedHandleCreatesTrainerAndDeployment(t *testing.T) {
	trainer := &fakeManagedTrainer{}
	deployment := &fakeManagedDeployment{existing: map[string]DeploymentInfo{}}
	profile := TrainingShapeProfile{
		TrainingShapeVersion:      "accounts/acct/trainingShapes/policy/versions/7",
		MaxSupportedContextLength: 4096,
		DeploymentShapeVersion:    "accounts/acct/deploymentShapes/serve/versions/3",
	}
	handle, err := ProvisionManagedHandle(context.Background(), ManagedProvisionOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:       "accounts/acct/models/base",
			TrainingShapeID: "accounts/acct/trainingShapes/policy",
			LoraRank:        8,
		},
		ProfileResolver: fakeProfileResolver{profiles: map[string]TrainingShapeProfile{
			"accounts/acct/trainingShapes/policy": profile,
		}},
		Trainer:    trainer,
		Deployment: deployment,
		Now:        func() time.Time { return time.Unix(123, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(trainer.created) != 1 {
		t.Fatalf("created trainers = %#v", trainer.created)
	}
	if trainer.created[0].TrainingShapeRef != profile.TrainingShapeVersion {
		t.Fatalf("trainer config = %#v", trainer.created[0])
	}
	if trainer.created[0].MaxContextLength == nil || *trainer.created[0].MaxContextLength != 4096 {
		t.Fatalf("max context = %#v", trainer.created[0].MaxContextLength)
	}
	if len(deployment.created) != 1 {
		t.Fatalf("created deployments = %#v", deployment.created)
	}
	if deployment.created[0].DeploymentID != "base-123" || deployment.created[0].HotLoadTrainerJob != "accounts/acct/rlorTrainerJobs/job-new" {
		t.Fatalf("deployment config = %#v", deployment.created[0])
	}
	if deployment.created[0].DeploymentShape != profile.DeploymentShapeVersion {
		t.Fatalf("deployment shape = %q", deployment.created[0].DeploymentShape)
	}
	if handle.Metadata.TrainerJobID != "job-new" || handle.Metadata.DeploymentID != "base-123" {
		t.Fatalf("metadata = %#v", handle.Metadata)
	}
	if handle.Metadata.MaxContextLength == nil || *handle.Metadata.MaxContextLength != 4096 {
		t.Fatalf("metadata max context = %#v", handle.Metadata.MaxContextLength)
	}
	if handle.RequiresInitialSamplerSync {
		t.Fatal("new deployment should not require initial sampler sync")
	}
}

func TestProvisionManagedHandleReattachesExistingDeployment(t *testing.T) {
	trainer := &fakeManagedTrainer{}
	deployment := &fakeManagedDeployment{existing: map[string]DeploymentInfo{
		"dep-1": {
			DeploymentID:           "dep-1",
			State:                  DeploymentStateReady,
			HotLoadTrainerJob:      "accounts/acct/rlorTrainerJobs/old",
			DeploymentShapeVersion: "accounts/acct/deploymentShapes/serve/versions/1",
		},
	}}
	handle, err := ProvisionManagedHandle(context.Background(), ManagedProvisionOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:                 "accounts/acct/models/base",
			TrainerJobID:              "job-existing",
			DeploymentID:              "dep-1",
			DeploymentShape:           "accounts/acct/deploymentShapes/serve/versions/2",
			CleanupDeploymentOnClose:  CleanupDeploymentScaleToZero,
			CleanupTrainerOnClose:     true,
			GradientAccumulationSteps: 1,
		},
		Trainer:              trainer,
		Deployment:           deployment,
		ReconnectExistingJob: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(trainer.reconnected) != 1 || trainer.reconnected[0] != "job-existing" {
		t.Fatalf("reconnected = %#v", trainer.reconnected)
	}
	if len(deployment.reattached) != 1 || deployment.reattached[0] != "dep-1" {
		t.Fatalf("reattached = %#v", deployment.reattached)
	}
	if !handle.RequiresInitialSamplerSync {
		t.Fatal("reattach should require initial sampler sync")
	}
	if len(handle.CleanupPlan) != 2 {
		t.Fatalf("cleanup plan = %#v", handle.CleanupPlan)
	}
	if err := handle.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deployment.scaledToZero) != 1 || len(trainer.deleted) != 1 {
		t.Fatalf("scaled=%#v deleted=%#v", deployment.scaledToZero, trainer.deleted)
	}
}

func TestProvisionManagedHandleRejectsDeploymentShapeConflict(t *testing.T) {
	trainer := &fakeManagedTrainer{}
	deployment := &fakeManagedDeployment{existing: map[string]DeploymentInfo{
		"dep-1": {
			DeploymentID:           "dep-1",
			State:                  DeploymentStateReady,
			HotLoadTrainerJob:      "accounts/acct/rlorTrainerJobs/old",
			DeploymentShapeVersion: "accounts/acct/deploymentShapes/other/versions/1",
		},
	}}
	_, err := ProvisionManagedHandle(context.Background(), ManagedProvisionOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:       "accounts/acct/models/base",
			TrainerJobID:    "job-existing",
			DeploymentID:    "dep-1",
			DeploymentShape: "accounts/acct/deploymentShapes/serve/versions/2",
		},
		Trainer:    trainer,
		Deployment: deployment,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v", err)
	}
}

func TestProvisionManagedHandleProvisionsSeparateReference(t *testing.T) {
	trainer := &fakeManagedTrainer{}
	deployment := &fakeManagedDeployment{existing: map[string]DeploymentInfo{}}
	referenceProfile := TrainingShapeProfile{
		TrainingShapeVersion: "accounts/acct/trainingShapes/ref/versions/4",
		TrainerMode:          ForwardOnlyMode,
	}
	handle, err := ProvisionManagedHandle(context.Background(), ManagedProvisionOptions{
		Config: FiretitanProvisioningConfig{
			BaseModel:                "accounts/acct/models/base",
			CreateDeployment:         boolPointer(false),
			ReferenceRequired:        true,
			ReferenceTrainingShapeID: "accounts/acct/trainingShapes/ref",
		},
		ProfileResolver: fakeProfileResolver{profiles: map[string]TrainingShapeProfile{
			"accounts/acct/trainingShapes/ref": referenceProfile,
		}},
		Trainer:    trainer,
		Deployment: deployment,
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.ReferenceHandle == nil {
		t.Fatal("expected separate reference handle")
	}
	if len(trainer.created) != 2 {
		t.Fatalf("created trainers = %#v", trainer.created)
	}
	referenceConfig := trainer.created[1]
	if !referenceConfig.ForwardOnly || referenceConfig.TrainingShapeRef != referenceProfile.TrainingShapeVersion {
		t.Fatalf("reference config = %#v", referenceConfig)
	}
	if len(handle.ReferenceHandle.CleanupPlan) != 1 || handle.ReferenceHandle.CleanupPlan[0].Operation != ManagedCleanupDeleteTrainer {
		t.Fatalf("reference cleanup = %#v", handle.ReferenceHandle.CleanupPlan)
	}
	if err := handle.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(trainer.deleted) != 1 || trainer.deleted[0] != "job-new-2" {
		t.Fatalf("deleted trainers = %#v", trainer.deleted)
	}
}

func TestFiretitanServiceClientCreateManagedTrainingClientWiresHandle(t *testing.T) {
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
	client, err := svc.CreateManagedTrainingClient(context.Background(), ManagedProvisionOptions{
		Trainer:    trainer,
		Deployment: deployment,
		Now:        func() time.Time { return time.Unix(456, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := client.TrainerJobID()
	if err != nil || jobID != "job-new" {
		t.Fatalf("jobID=%q err=%v", jobID, err)
	}
	deploymentID, err := client.DeploymentID()
	if err != nil || deploymentID != "base-456" {
		t.Fatalf("deploymentID=%q err=%v", deploymentID, err)
	}
	if svc.ManagedHandle == nil || svc.ManagedHandle.TrainerJobID != "job-new" {
		t.Fatalf("service handle = %#v", svc.ManagedHandle)
	}
}

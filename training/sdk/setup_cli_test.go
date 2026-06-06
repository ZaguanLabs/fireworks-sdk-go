package sdk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeSetupTrainerManager struct {
	configs []TrainerJobConfig
	polls   []TrainerPollOptions
}

func (m *fakeSetupTrainerManager) CreateAndWait(_ context.Context, config TrainerJobConfig, opts ...TrainerPollOptions) (TrainerServiceEndpoint, error) {
	m.configs = append(m.configs, config)
	if len(opts) > 0 {
		m.polls = append(m.polls, opts[0])
	}
	return TrainerServiceEndpoint{
		JobID:   "job-1",
		JobName: "accounts/acct/rlorTrainerJobs/job-1",
		BaseURL: "https://trainer.example.com",
	}, nil
}

type fakeSetupDeploymentManager struct {
	created []DeploymentConfig
	waits   []DeploymentWaitOptions
	info    DeploymentInfo
	waited  DeploymentInfo
}

func (m *fakeSetupDeploymentManager) CreateOrGet(_ context.Context, config DeploymentConfig, _ bool) (DeploymentInfo, error) {
	m.created = append(m.created, config)
	return m.info, nil
}

func (m *fakeSetupDeploymentManager) WaitForReady(_ context.Context, _ string, opts ...DeploymentWaitOptions) (DeploymentInfo, error) {
	if len(opts) > 0 {
		m.waits = append(m.waits, opts[0])
	}
	return m.waited, nil
}

func TestSetupTrainerCreatesConfigAndWritesJSON(t *testing.T) {
	manager := &fakeSetupTrainerManager{}
	nowValues := []time.Time{
		time.Unix(10, 0),
		time.Unix(15, 500_000_000),
	}
	outputPath := filepath.Join(t.TempDir(), "trainer.json")
	acceleratorCount := 8
	output, err := SetupTrainer(context.Background(), SetupTrainerOptions{
		DisplayName:      "ablation-eager",
		ExtraArgs:        []string{"--forward-only", "--no-compile"},
		CustomImageTag:   "trainer-tag",
		AcceleratorType:  "NVIDIA_H200_141GB",
		AcceleratorCount: &acceleratorCount,
		OutputFile:       outputPath,
		Manager:          manager,
		Now: func() time.Time {
			value := nowValues[0]
			nowValues = nowValues[1:]
			return value
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.JobID != "job-1" || output.ElapsedS != 5.5 {
		t.Fatalf("output = %#v", output)
	}
	if len(manager.configs) != 1 {
		t.Fatalf("configs = %#v", manager.configs)
	}
	config := manager.configs[0]
	if config.BaseModel != DefaultSetupBaseModel || config.DisplayName != "ablation-eager" || config.Region != DefaultSetupRegion {
		t.Fatalf("config = %#v", config)
	}
	if config.NodeCount == nil || *config.NodeCount != 2 {
		t.Fatalf("node count = %#v", config.NodeCount)
	}
	if config.MaxContextLength == nil || *config.MaxContextLength != 4096 {
		t.Fatalf("max context = %#v", config.MaxContextLength)
	}
	if config.GradientAccumulationSteps == nil || *config.GradientAccumulationSteps != 1 {
		t.Fatalf("grad accumulation = %#v", config.GradientAccumulationSteps)
	}
	if config.AcceleratorCount == nil || *config.AcceleratorCount != 8 {
		t.Fatalf("accelerator count = %#v", config.AcceleratorCount)
	}
	if !reflect.DeepEqual(config.ExtraArgs, []string{"--forward-only", "--no-compile"}) {
		t.Fatalf("extra args = %#v", config.ExtraArgs)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved SetupTrainerOutput
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.JobName != "accounts/acct/rlorTrainerJobs/job-1" || saved.ElapsedS != 5.5 {
		t.Fatalf("saved = %#v", saved)
	}
}

func TestSetupTrainerValidatesDisplayNameAndAPIKey(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "")
	if _, err := SetupTrainer(context.Background(), SetupTrainerOptions{OutputFile: "-"}); err == nil {
		t.Fatal("expected display-name error")
	}
	if _, err := SetupTrainer(context.Background(), SetupTrainerOptions{DisplayName: "run", OutputFile: filepath.Join(t.TempDir(), "trainer.json")}); err == nil {
		t.Fatal("expected API key error")
	}
}

func TestSetupDeploymentCreatesWaitsAndWritesJSON(t *testing.T) {
	manager := &fakeSetupDeploymentManager{
		info: DeploymentInfo{
			DeploymentID:      "dep-1",
			State:             "CREATING",
			HotLoadTrainerJob: "accounts/acct/rlorTrainerJobs/job-1",
		},
		waited: DeploymentInfo{
			DeploymentID:   "dep-1",
			State:          DeploymentStateReady,
			InferenceModel: "accounts/acct/deployments/dep-1",
		},
	}
	nowValues := []time.Time{
		time.Unix(20, 0),
		time.Unix(23, 0),
	}
	outputPath := filepath.Join(t.TempDir(), "deployment.json")
	output, err := SetupDeployment(context.Background(), SetupDeploymentOptions{
		DeploymentID:        "dep-1",
		DeploymentShape:     "accounts/acct/deploymentShapes/serve",
		OutputFile:          outputPath,
		SkipShapeValidation: true,
		AcceleratorType:     "NVIDIA_H200_141GB",
		Manager:             manager,
		Now: func() time.Time {
			value := nowValues[0]
			nowValues = nowValues[1:]
			return value
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.State != DeploymentStateReady || output.ElapsedS != 3 {
		t.Fatalf("output = %#v", output)
	}
	if len(manager.created) != 1 {
		t.Fatalf("created = %#v", manager.created)
	}
	config := manager.created[0]
	if config.BaseModel != DefaultSetupBaseModel || config.Region != DefaultSetupRegion || config.MinReplicaCount != 1 {
		t.Fatalf("config = %#v", config)
	}
	if !config.SkipShapeValidation || config.AcceleratorType != "NVIDIA_H200_141GB" {
		t.Fatalf("config = %#v", config)
	}
	if len(manager.waits) != 1 || manager.waits[0].Timeout != 1800*time.Second {
		t.Fatalf("waits = %#v", manager.waits)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved SetupDeploymentOutput
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.InferenceModel != "accounts/acct/deployments/dep-1" {
		t.Fatalf("saved = %#v", saved)
	}
}

func TestSetupDeploymentSkipsWaitWhenReady(t *testing.T) {
	manager := &fakeSetupDeploymentManager{
		info: DeploymentInfo{DeploymentID: "dep-1", State: DeploymentStateReady},
	}
	if _, err := SetupDeployment(context.Background(), SetupDeploymentOptions{
		DeploymentID:    "dep-1",
		DeploymentShape: "accounts/acct/deploymentShapes/serve",
		OutputFile:      filepath.Join(t.TempDir(), "deployment.json"),
		Manager:         manager,
	}); err != nil {
		t.Fatal(err)
	}
	if len(manager.waits) != 0 {
		t.Fatalf("waits = %#v", manager.waits)
	}
}

func TestSetupHelpersParseHeadersAndExtraArgs(t *testing.T) {
	headers, err := ParseAdditionalHeadersJSON(`{"X-Test":"1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if headers["X-Test"] != "1" {
		t.Fatalf("headers = %#v", headers)
	}
	if args := SplitSetupExtraArgs(" --forward-only   --no-compile "); !reflect.DeepEqual(args, []string{"--forward-only", "--no-compile"}) {
		t.Fatalf("args = %#v", args)
	}
}

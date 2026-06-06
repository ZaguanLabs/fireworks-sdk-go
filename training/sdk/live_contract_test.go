package sdk

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLiveTrainerReconnectAndListCheckpoints(t *testing.T) {
	ctx := liveContext(t)
	apiKey := liveRequiredEnv(t, "FIREWORKS_API_KEY")
	jobID := liveRequiredEnv(t, "FIREWORKS_LIVE_TRAINER_JOB_ID")
	baseURL := os.Getenv("FIREWORKS_BASE_URL")

	trainer := NewTrainerJobManager(apiKey, baseURL)
	var endpoint TrainerServiceEndpoint
	var err error
	if os.Getenv("FIREWORKS_LIVE_RECONNECT") == "1" {
		endpoint, err = trainer.ReconnectAndWait(ctx, jobID, TrainerReconnectOptions{
			PollOptions: TrainerPollOptions{Timeout: liveTimeout(t, "FIREWORKS_LIVE_TRAINER_TIMEOUT_S", 120)},
		})
	} else {
		endpoint, err = trainer.WaitForExisting(ctx, jobID, TrainerPollOptions{
			Timeout: liveTimeout(t, "FIREWORKS_LIVE_TRAINER_TIMEOUT_S", 120),
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.JobID != jobID || endpoint.BaseURL == "" {
		t.Fatalf("endpoint = %#v", endpoint)
	}

	fireworks := NewFireworksClient(apiKey, baseURL)
	rows, err := fireworks.ListCheckpoints(ctx, jobID, 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("listed %d checkpoints for trainer %s", len(rows), jobID)
}

func TestLivePromoteCheckpoint(t *testing.T) {
	if os.Getenv("FIREWORKS_LIVE_PROMOTE") != "1" {
		t.Skip("set FIREWORKS_LIVE_PROMOTE=1 to run checkpoint promotion")
	}
	ctx := liveContext(t)
	apiKey := liveRequiredEnv(t, "FIREWORKS_API_KEY")
	baseURL := os.Getenv("FIREWORKS_BASE_URL")
	client := NewFireworksClient(apiKey, baseURL)
	model, err := client.PromoteCheckpoint(ctx, PromoteCheckpointOptions{
		Name:                os.Getenv("FIREWORKS_LIVE_CHECKPOINT_NAME"),
		JobID:               os.Getenv("FIREWORKS_LIVE_TRAINER_JOB_ID"),
		CheckpointID:        os.Getenv("FIREWORKS_LIVE_CHECKPOINT_ID"),
		OutputModelID:       liveRequiredEnv(t, "FIREWORKS_LIVE_OUTPUT_MODEL_ID"),
		BaseModel:           liveRequiredEnv(t, "FIREWORKS_LIVE_BASE_MODEL"),
		HotLoadDeploymentID: os.Getenv("FIREWORKS_LIVE_DEPLOYMENT_ID"),
		WaitOptions: OperationWaitOptions{
			Timeout: liveTimeout(t, "FIREWORKS_LIVE_PROMOTE_TIMEOUT_S", 7200),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(model) == 0 {
		t.Fatal("promote returned empty model payload")
	}
}

func TestLiveDeploymentHotloadAndSample(t *testing.T) {
	ctx := liveContext(t)
	apiKey := liveRequiredEnv(t, "FIREWORKS_API_KEY")
	baseURL := os.Getenv("FIREWORKS_BASE_URL")
	deploymentID := liveRequiredEnv(t, "FIREWORKS_LIVE_DEPLOYMENT_ID")
	baseModel := liveRequiredEnv(t, "FIREWORKS_LIVE_BASE_MODEL")
	snapshot := liveRequiredEnv(t, "FIREWORKS_LIVE_SNAPSHOT_IDENTITY")

	manager := NewDeploymentManager(
		apiKey,
		baseURL,
		WithDeploymentInferenceURL(os.Getenv("FIREWORKS_LIVE_INFERENCE_URL")),
		WithDeploymentHotloadAPIURL(os.Getenv("FIREWORKS_LIVE_HOTLOAD_API_URL")),
	)
	ok, err := manager.HotloadAndWait(ctx, deploymentID, baseModel, snapshot, HotloadAndWaitOptions{
		RequestTimeout: liveTimeout(t, "FIREWORKS_LIVE_HOTLOAD_TIMEOUT_S", 400),
		Wait: HotloadWaitOptions{
			Timeout: liveTimeout(t, "FIREWORKS_LIVE_HOTLOAD_TIMEOUT_S", 400),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("hotload did not report success")
	}

	if os.Getenv("FIREWORKS_LIVE_SAMPLE") != "1" {
		return
	}
	accountID, err := manager.AccountID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sampler := NewDeploymentSampler(
		manager.InferenceURL,
		"accounts/"+accountID+"/deployments/"+deploymentID,
		apiKey,
	)
	results, err := sampler.SampleWithPromptTokens(ctx, livePromptTokens(), SampleOptions{
		N:           1,
		MaxTokens:   1,
		Temperature: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("sample returned no completions")
	}
}

func TestLiveManagedProvisioning(t *testing.T) {
	if os.Getenv("FIREWORKS_LIVE_MANAGED_CREATE") != "1" {
		t.Skip("set FIREWORKS_LIVE_MANAGED_CREATE=1 to create managed trainer/deployment resources")
	}
	ctx := liveContext(t)
	apiKey := liveRequiredEnv(t, "FIREWORKS_API_KEY")
	createDeployment := os.Getenv("FIREWORKS_LIVE_CREATE_DEPLOYMENT") != "0"
	service, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{
		APIKey:  apiKey,
		BaseURL: os.Getenv("FIREWORKS_BASE_URL"),
		Config: FiretitanProvisioningConfig{
			BaseModel:                  liveRequiredEnv(t, "FIREWORKS_LIVE_BASE_MODEL"),
			TrainingShapeID:            os.Getenv("FIREWORKS_LIVE_TRAINING_SHAPE_ID"),
			DeploymentShape:            os.Getenv("FIREWORKS_LIVE_DEPLOYMENT_SHAPE"),
			CreateDeployment:           &createDeployment,
			DeploymentID:               os.Getenv("FIREWORKS_LIVE_DEPLOYMENT_ID"),
			CleanupTrainerOnClose:      os.Getenv("FIREWORKS_LIVE_CLEANUP_TRAINER") == "1",
			CleanupDeploymentOnClose:   os.Getenv("FIREWORKS_LIVE_CLEANUP_DEPLOYMENT"),
			DisplayName:                os.Getenv("FIREWORKS_LIVE_DISPLAY_NAME"),
			SkipValidations:            os.Getenv("FIREWORKS_LIVE_SKIP_VALIDATIONS") == "1",
			DisableSpeculativeDecoding: os.Getenv("FIREWORKS_LIVE_DISABLE_SPECULATIVE_DECODING") == "1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := service.CreateManagedTrainingClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.TrainerJobID(); err != nil {
		t.Fatal(err)
	}
	if service.ManagedHandle != nil {
		t.Logf("managed handle: trainer=%s deployment=%s", service.ManagedHandle.TrainerJobID, service.ManagedHandle.DeploymentID)
	}
}

func liveContext(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("FIREWORKS_SDK_GO_LIVE") != "1" {
		t.Skip("set FIREWORKS_SDK_GO_LIVE=1 to run live Fireworks contract tests")
	}
	timeout := liveTimeout(t, "FIREWORKS_LIVE_TIMEOUT_S", 900)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

func liveRequiredEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		t.Skipf("set %s to run this live contract test", key)
	}
	return value
}

func liveTimeout(t *testing.T, key string, defaultSeconds int) time.Duration {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		return time.Duration(defaultSeconds) * time.Second
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("invalid %s=%q: %v", key, raw, err)
	}
	return time.Duration(seconds * float64(time.Second))
}

func livePromptTokens() []int {
	raw := os.Getenv("FIREWORKS_LIVE_PROMPT_TOKENS")
	if raw == "" {
		return []int{1}
	}
	parts := strings.Split(raw, ",")
	tokens := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil {
			tokens = append(tokens, value)
		}
	}
	if len(tokens) == 0 {
		return []int{1}
	}
	return tokens
}

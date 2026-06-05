package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTrainerJobCreateReturnsJobIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/accounts/test/rlorTrainerJobs" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "accounts/test/rlorTrainerJobs/job-1"})
	}))
	defer server.Close()

	mgr := NewTrainerJobManager("test-key", server.URL)
	mgr.SetAccountID("test")
	created, err := mgr.Create(context.Background(), TrainerJobConfig{BaseModel: "accounts/test/models/m"})
	if err != nil {
		t.Fatal(err)
	}
	if created.JobName != "accounts/test/rlorTrainerJobs/job-1" || created.JobID != "job-1" {
		t.Fatalf("created = %#v", created)
	}
}

func TestTrainerJobPayloadConstruction(t *testing.T) {
	maxContext := 4096
	nodeCount := 2
	var seenPath string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "accounts/test/rlorTrainerJobs/job-1"})
	}))
	defer server.Close()

	mgr := NewTrainerJobManager("test-key", server.URL)
	mgr.SetAccountID("test")
	_, err := mgr.CreateRaw(context.Background(), TrainerJobConfig{
		BaseModel:           "accounts/test/models/qwen3-1p7b",
		NodeCount:           &nodeCount,
		Region:              "US_OHIO_1",
		LoraRank:            0,
		MaxContextLength:    &maxContext,
		LearningRate:        1e-5,
		HotLoadDeploymentID: "my-deploy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seenPath, "deploymentId=my-deploy") || !strings.Contains(seenPath, "skipValidations=true") {
		t.Fatalf("path = %q", seenPath)
	}
	if payload["serviceMode"] != true || payload["nodeCount"].(float64) != 2 {
		t.Fatalf("payload = %#v", payload)
	}
	tc := payload["trainingConfig"].(map[string]any)
	if tc["baseModel"] != "accounts/test/models/qwen3-1p7b" || tc["region"] != "US_OHIO_1" {
		t.Fatalf("training config = %#v", tc)
	}
	if payload["hotLoadDeploymentId"] != "my-deploy" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestTrainerJobShapePathOmitsInfraFields(t *testing.T) {
	payload := BuildTrainerCreatePayload(TrainerJobConfig{
		BaseModel:        "accounts/test/models/m",
		TrainingShapeRef: "accounts/test-account/trainingShapes/ts-test/versions/shape-v1",
		Region:           "US_OHIO_1",
		ExtraArgs:        []string{"--flag"},
	})
	tc := payload["trainingConfig"].(map[string]any)
	for _, key := range []string{"acceleratorType", "acceleratorCount", "customImageTag", "maxContextLength"} {
		if _, ok := tc[key]; ok {
			t.Fatalf("training config unexpectedly includes %s: %#v", key, tc)
		}
	}
	if _, ok := payload["nodeCount"]; ok {
		t.Fatalf("payload unexpectedly includes nodeCount: %#v", payload)
	}
	if tc["region"] != "US_OHIO_1" {
		t.Fatalf("training config = %#v", tc)
	}
	extra := tc["extraArgs"].([]string)
	if len(extra) != 1 || extra[0] != "--flag" {
		t.Fatalf("extra args = %#v", extra)
	}
}

func TestTrainerJobManualPathSendsAllFields(t *testing.T) {
	maxContext := 8192
	nodeCount := 4
	accelCount := 8
	payload := BuildTrainerCreatePayload(TrainerJobConfig{
		BaseModel:        "accounts/test/models/m",
		AcceleratorType:  "NVIDIA_H100_80GB",
		AcceleratorCount: &accelCount,
		CustomImageTag:   "0.33.0",
		NodeCount:        &nodeCount,
		MaxContextLength: &maxContext,
		Region:           "US_OHIO_1",
	})
	tc := payload["trainingConfig"].(map[string]any)
	if tc["acceleratorType"] != "NVIDIA_H100_80GB" || tc["acceleratorCount"] != 8 || tc["customImageTag"] != "0.33.0" || tc["maxContextLength"] != 8192 {
		t.Fatalf("training config = %#v", tc)
	}
	if payload["nodeCount"] != 4 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestTrainerReplicaCountSentOnShapePath(t *testing.T) {
	replicas := 3
	config := TrainerJobConfig{
		BaseModel:           "accounts/test/models/m",
		TrainingShapeRef:    "accounts/test-account/trainingShapes/ts-test/versions/shape-v1",
		TrainerReplicaCount: &replicas,
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	payload := BuildTrainerCreatePayload(config)
	if payload["trainerReplicaCount"] != 3 {
		t.Fatalf("payload = %#v", payload)
	}
	if _, ok := payload["nodeCount"]; ok {
		t.Fatalf("payload unexpectedly includes nodeCount: %#v", payload)
	}
}

func TestTrainerReplicaCountOmittedWhenUnset(t *testing.T) {
	payload := BuildTrainerCreatePayload(TrainerJobConfig{BaseModel: "accounts/test/models/m"})
	if _, ok := payload["trainerReplicaCount"]; ok {
		t.Fatalf("payload unexpectedly includes trainerReplicaCount: %#v", payload)
	}
}

func TestTrainerInactivityCleanupFields(t *testing.T) {
	payload := BuildTrainerCreatePayload(TrainerJobConfig{
		BaseModel:                "accounts/test/models/m",
		TrainingShapeRef:         "accounts/test-account/trainingShapes/ts-test/versions/shape-v1",
		InactivityTimeout:        30 * time.Minute,
		DisableInactivityCleanup: true,
	})
	if payload["inactivityTimeout"] != "1800s" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["disableInactivityCleanup"] != true {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestTrainerInactivityTimeoutAcceptsProtoDurationString(t *testing.T) {
	payload := BuildTrainerCreatePayload(TrainerJobConfig{
		BaseModel:         "accounts/test/models/m",
		InactivityTimeout: "7200s",
	})
	if payload["inactivityTimeout"] != "7200s" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestTrainerExtraArgsFlattened(t *testing.T) {
	payload := BuildTrainerCreatePayload(TrainerJobConfig{
		BaseModel: "accounts/test/models/m",
		ExtraArgs: []string{"--pp 8", "--ep=4", "--flag"},
	})
	tc := payload["trainingConfig"].(map[string]any)
	extra := tc["extraArgs"].([]string)
	want := []string{"--pp", "8", "--ep=4", "--flag"}
	if len(extra) != len(want) {
		t.Fatalf("extra args = %#v", extra)
	}
	for i := range want {
		if extra[i] != want[i] {
			t.Fatalf("extra args = %#v, want %#v", extra, want)
		}
	}
}

func TestTrainerJobConfigValidate(t *testing.T) {
	if err := (TrainerJobConfig{
		BaseModel:        "accounts/test/models/m",
		TrainingShapeRef: "accounts/fw/trainingShapes/ts-x/versions/1",
	}).Validate(); err != nil {
		t.Fatal(err)
	}

	accelCount := 8
	nodeCount := 4
	config := TrainerJobConfig{
		BaseModel:        "accounts/test/models/m",
		TrainingShapeRef: "accounts/fw/trainingShapes/ts-x/versions/1",
		AcceleratorType:  "NVIDIA_H100_80GB",
		AcceleratorCount: &accelCount,
		CustomImageTag:   "0.33.0",
		NodeCount:        &nodeCount,
	}
	err := config.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"accelerator_type", "accelerator_count", "custom_image_tag", "node_count"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %s", err, want)
		}
	}

	if err := (TrainerJobConfig{
		BaseModel:        "accounts/test/models/m",
		AcceleratorType:  "NVIDIA_H100_80GB",
		AcceleratorCount: &accelCount,
		CustomImageTag:   "0.33.0",
		NodeCount:        &nodeCount,
	}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTrainerJobConfigValidateRejectsBaseAndDeprecatedGradientAccumulation(t *testing.T) {
	if err := (TrainerJobConfig{BaseModel: ""}).Validate(); err == nil || !strings.Contains(err.Error(), "base_model") {
		t.Fatalf("error = %v", err)
	}
	steps := 4
	if err := (TrainerJobConfig{BaseModel: "accounts/test/models/m", GradientAccumulationSteps: &steps}).Validate(); err == nil || !strings.Contains(err.Error(), "gradient_accumulation_steps") {
		t.Fatalf("error = %v", err)
	}
	steps = 1
	if err := (TrainerJobConfig{BaseModel: "accounts/test/models/m", GradientAccumulationSteps: &steps}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTrainerJobConfigValidateRejectsBadInactivityTimeout(t *testing.T) {
	if err := (TrainerJobConfig{BaseModel: "accounts/test/models/m", InactivityTimeout: -time.Second}).Validate(); err == nil || !strings.Contains(err.Error(), "inactivity_timeout") {
		t.Fatalf("error = %v", err)
	}
	if err := (TrainerJobConfig{BaseModel: "accounts/test/models/m", InactivityTimeout: "30m"}).Validate(); err == nil || !strings.Contains(err.Error(), "protobuf JSON duration") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateTrainingShapeRef(t *testing.T) {
	if err := ValidateTrainingShapeRef("accounts/fw/trainingShapes/ts-x/versions/1"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTrainingShapeRef("short-id"); err == nil || !strings.Contains(err.Error(), "not a valid training shape resource name") {
		t.Fatalf("error = %v", err)
	}
}

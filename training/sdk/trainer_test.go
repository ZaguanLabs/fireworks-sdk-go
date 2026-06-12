package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTrainerJobCreateReturnsJobIdentity(t *testing.T) {
	var seenJobID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/accounts/test/rlorTrainerJobs" {
			t.Errorf("path = %q", r.URL.Path)
		}
		seenJobID = r.URL.Query().Get("rlorTrainerJobId")
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
	if !strings.HasPrefix(seenJobID, autoTrainerJobIDPrefix+"-") {
		t.Fatalf("rlorTrainerJobId = %q", seenJobID)
	}
}

func TestTrainerJobLoadAdapterValidation(t *testing.T) {
	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	for _, adapterPath := range []string{"", "   "} {
		_, err := mgr.LoadAdapter(context.Background(), LoadAdapterOptions{
			TrainerBaseURL: "https://trainer.example.com",
			ModelID:        "model-1",
			AdapterPath:    adapterPath,
		})
		if err == nil || !strings.Contains(err.Error(), "adapter_path must be a non-empty string") {
			t.Fatalf("adapter path %q err = %v", adapterPath, err)
		}
	}
	_, err := mgr.LoadAdapter(context.Background(), LoadAdapterOptions{
		TrainerBaseURL: "https://trainer.example.com",
		AdapterPath:    "gs://bucket/adapter",
	})
	if err == nil || !strings.Contains(err.Error(), "model_id") {
		t.Fatalf("missing model error = %v", err)
	}
}

func TestTrainerJobLoadAdapterPostsGatewayRequest(t *testing.T) {
	var seenPath string
	var seenAuth string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model_id":     "model-1",
			"adapter_path": "gs://bucket/adapter-dir",
		})
	}))
	defer server.Close()

	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	resp, err := mgr.LoadAdapter(context.Background(), LoadAdapterOptions{
		TrainerBaseURL: server.URL + "/",
		ModelID:        "model-1",
		AdapterPath:    "  gs://bucket/adapter-dir  ",
		SeqID:          43,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenPath != "/api/v1/load_adapter" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenAuth != "Bearer test-key" {
		t.Fatalf("authorization = %q", seenAuth)
	}
	if payload["adapter_path"] != "gs://bucket/adapter-dir" || payload["model_id"] != "model-1" || payload["seq_id"].(float64) != 43 {
		t.Fatalf("payload = %#v", payload)
	}
	if resp.ModelID != "model-1" || resp.AdapterPath != "gs://bucket/adapter-dir" || resp.Type != "load_adapter" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestTrainerJobLoadAdapterDefaultsEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	resp, err := mgr.LoadAdapter(context.Background(), LoadAdapterOptions{
		TrainerBaseURL: server.URL,
		ModelID:        "model-1",
		AdapterPath:    "gs://bucket/adapter-dir",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ModelID != "model-1" || resp.AdapterPath != "gs://bucket/adapter-dir" || resp.Type != "load_adapter" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestTrainerJobLoadAdapterHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad adapter"}`))
	}))
	defer server.Close()

	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	_, err := mgr.LoadAdapter(context.Background(), LoadAdapterOptions{
		TrainerBaseURL: server.URL,
		ModelID:        "model-1",
		AdapterPath:    "gs://bucket/adapter-dir",
	})
	if err == nil || !strings.Contains(err.Error(), "Load adapter failed") || !strings.Contains(err.Error(), "bad adapter") {
		t.Fatalf("error = %v", err)
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

func TestTrainerJobCreateSendsRequestedJobID(t *testing.T) {
	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "accounts/test/rlorTrainerJobs/stable-id"})
	}))
	defer server.Close()

	mgr := NewTrainerJobManager("test-key", server.URL)
	mgr.SetAccountID("test")
	created, err := mgr.Create(context.Background(), TrainerJobConfig{
		BaseModel:      "accounts/test/models/m",
		RequestedJobID: "stable-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.JobID != "stable-id" {
		t.Fatalf("created = %#v", created)
	}
	if !strings.Contains(seenPath, "rlorTrainerJobId=stable-id") {
		t.Fatalf("path = %q", seenPath)
	}
}

func TestTrainerJobCreateConflictFetchesExisting(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.String())
		switch r.Method {
		case http.MethodPost:
			http.Error(w, `{"error":"already exists"}`, http.StatusConflict)
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "accounts/test/rlorTrainerJobs/stable-id"})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	mgr := NewTrainerJobManager("test-key", server.URL)
	mgr.SetAccountID("test")
	created, err := mgr.Create(context.Background(), TrainerJobConfig{
		BaseModel:      "accounts/test/models/m",
		RequestedJobID: "stable-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.JobID != "stable-id" {
		t.Fatalf("created = %#v", created)
	}
	if len(calls) != 2 || !strings.Contains(calls[0], "rlorTrainerJobId=stable-id") || calls[1] != "GET /v1/accounts/test/rlorTrainerJobs/stable-id" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestTrainerTryGetJobReturnsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	mgr := NewTrainerJobManager("test-key", server.URL)
	mgr.SetAccountID("test")
	job, found, err := mgr.TryGetJob(context.Background(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if found || job != nil {
		t.Fatalf("job=%#v found=%t", job, found)
	}
	_, err = mgr.GetJob(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("err = %v", err)
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

func TestTrainerJobConfigValidationWarnings(t *testing.T) {
	if warnings := (TrainerJobConfig{BaseModel: "accounts/test/models/m"}).ValidationWarnings(); len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	steps := 4
	if warnings := (TrainerJobConfig{BaseModel: "accounts/test/models/m", GradientAccumulationSteps: &steps}).ValidationWarnings(); len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	steps = 1
	warnings := (TrainerJobConfig{BaseModel: "accounts/test/models/m", GradientAccumulationSteps: &steps}).ValidationWarnings()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "gradient_accumulation_steps=1 is deprecated") {
		t.Fatalf("warnings = %#v", warnings)
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

func TestExtractJobStatusMessage(t *testing.T) {
	tests := []struct {
		job  map[string]any
		want string
	}{
		{job: map[string]any{"statusMessage": "top"}, want: "top"},
		{job: map[string]any{"message": "msg"}, want: "msg"},
		{job: map[string]any{"status": map[string]any{"message": "nested"}}, want: "nested"},
		{job: map[string]any{"status": "plain"}, want: "plain"},
		{job: map[string]any{"status": map[string]any{}}, want: ""},
	}
	for _, test := range tests {
		if got := ExtractJobStatusMessage(test.job); got != test.want {
			t.Fatalf("ExtractJobStatusMessage(%#v) = %q, want %q", test.job, got, test.want)
		}
	}
}

func TestTrainerGatewayURL(t *testing.T) {
	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	mgr.SetAccountID("test-account")
	got, err := mgr.TrainerGatewayURL(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://api.example.com/training/v1/rlorTrainerJobs/test-account/job-1"
	if got != want {
		t.Fatalf("gateway URL = %q, want %q", got, want)
	}
}

func TestTrainerManagerGetDeleteResume(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/a/rlorTrainerJobs/j":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "JOB_STATE_RUNNING"})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/a/rlorTrainerJobs/j":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/a/rlorTrainerJobs/j:resume":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "accounts/a/rlorTrainerJobs/j"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mgr := NewTrainerJobManager("test-key", server.URL)
	mgr.SetAccountID("a")
	job, err := mgr.GetJob(context.Background(), "j")
	if err != nil {
		t.Fatal(err)
	}
	if job["state"] != "JOB_STATE_RUNNING" {
		t.Fatalf("job = %#v", job)
	}
	if err := mgr.DeleteJob(context.Background(), "j"); err != nil {
		t.Fatal(err)
	}
	resumed, err := mgr.Resume(context.Background(), "j")
	if err != nil {
		t.Fatal(err)
	}
	if resumed["name"] != "accounts/a/rlorTrainerJobs/j" {
		t.Fatalf("resumed = %#v", resumed)
	}
	if len(seen) != 3 {
		t.Fatalf("seen = %#v", seen)
	}
}

func TestTrainerPollUntilReadyRunningUsesGatewayEndpoint(t *testing.T) {
	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	mgr.SetAccountID("test-account")
	endpoint, err := mgr.PollUntilReady(
		context.Background(),
		"job-1",
		"accounts/test/rlorTrainerJobs/job-1",
		TrainerPollOptions{
			Timeout:      time.Second,
			PollInterval: time.Millisecond,
			GetJob: func(context.Context, string) (map[string]any, error) {
				return map[string]any{
					"state":             "JOB_STATE_RUNNING",
					"directRouteHandle": "https://trainer.internal:8080",
				}, nil
			},
			HealthCheck: func(context.Context, string) bool { return true },
			Sleep:       func(time.Duration) {},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.JobID != "job-1" {
		t.Fatalf("endpoint = %#v", endpoint)
	}
	want := "https://api.example.com/training/v1/rlorTrainerJobs/test-account/job-1"
	if endpoint.BaseURL != want {
		t.Fatalf("base URL = %q, want %q", endpoint.BaseURL, want)
	}
}

func TestTrainerPollUntilReadyFailedRaisesRuntimeError(t *testing.T) {
	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	mgr.SetAccountID("a")
	_, err := mgr.PollUntilReady(
		context.Background(),
		"job-1",
		"name",
		TrainerPollOptions{
			Timeout: time.Second,
			GetJob: func(context.Context, string) (map[string]any, error) {
				return map[string]any{
					"state":  "JOB_STATE_FAILED",
					"status": map[string]any{"message": "GPU OOM"},
				}, nil
			},
			Sleep: func(time.Duration) {},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "GPU OOM") {
		t.Fatalf("error = %v", err)
	}
}

func TestTrainerPollUntilReadyTimeoutRaises(t *testing.T) {
	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	mgr.SetAccountID("a")
	now := time.Unix(0, 0)
	_, err := mgr.PollUntilReady(
		context.Background(),
		"job-1",
		"name",
		TrainerPollOptions{
			Timeout:      5 * time.Second,
			PollInterval: time.Second,
			Now: func() time.Time {
				current := now
				now = now.Add(2 * time.Second)
				return current
			},
			GetJob: func(context.Context, string) (map[string]any, error) {
				return map[string]any{"state": "JOB_STATE_CREATING"}, nil
			},
			HealthCheck: func(context.Context, string) bool { return false },
			Sleep:       func(time.Duration) {},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("error = %v", err)
	}
}

func TestTrainerCreateAndWaitDelegates(t *testing.T) {
	var created bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/accounts/a/rlorTrainerJobs" {
			created = true
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "accounts/a/rlorTrainerJobs/job-1"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	mgr := NewTrainerJobManager("test-key", server.URL)
	mgr.SetAccountID("a")
	endpoint, err := mgr.CreateAndWait(
		context.Background(),
		TrainerJobConfig{BaseModel: "accounts/a/models/m"},
		TrainerPollOptions{
			Timeout: time.Second,
			GetJob: func(context.Context, string) (map[string]any, error) {
				return map[string]any{"state": "JOB_STATE_RUNNING"}, nil
			},
			HealthCheck: func(context.Context, string) bool { return true },
			Sleep:       func(time.Duration) {},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !created || endpoint.JobID != "job-1" {
		t.Fatalf("created = %t endpoint = %#v", created, endpoint)
	}
}

func TestTrainerReconnectAndWaitFailedTriggersResume(t *testing.T) {
	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	expected := TrainerServiceEndpoint{JobName: "n", JobID: "j", BaseURL: "https://u"}
	resumed := false
	got, err := mgr.ReconnectAndWait(
		context.Background(),
		"j",
		TrainerReconnectOptions{
			GetJob: func(context.Context, string) (map[string]any, error) {
				return map[string]any{"state": "JOB_STATE_FAILED"}, nil
			},
			ResumeAndWait: func(_ context.Context, jobID string, _ TrainerPollOptions) (TrainerServiceEndpoint, error) {
				resumed = true
				if jobID != "j" {
					t.Fatalf("jobID = %q", jobID)
				}
				return expected, nil
			},
			Sleep: func(time.Duration) {},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !resumed || got != expected {
		t.Fatalf("resumed = %t endpoint = %#v", resumed, got)
	}
}

func TestTrainerReconnectAndWaitRunningWaitsForExisting(t *testing.T) {
	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	expected := TrainerServiceEndpoint{JobName: "n", JobID: "j", BaseURL: "https://u"}
	waited := false
	got, err := mgr.ReconnectAndWait(
		context.Background(),
		"j",
		TrainerReconnectOptions{
			GetJob: func(context.Context, string) (map[string]any, error) {
				return map[string]any{"state": "JOB_STATE_RUNNING"}, nil
			},
			WaitForExisting: func(_ context.Context, jobID string, _ TrainerPollOptions) (TrainerServiceEndpoint, error) {
				waited = true
				if jobID != "j" {
					t.Fatalf("jobID = %q", jobID)
				}
				return expected, nil
			},
			Sleep: func(time.Duration) {},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !waited || got != expected {
		t.Fatalf("waited = %t endpoint = %#v", waited, got)
	}
}

func TestTrainerReconnectAndWaitRetriesTransientGetError(t *testing.T) {
	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	calls := 0
	expected := TrainerServiceEndpoint{JobName: "n", JobID: "j", BaseURL: "https://u"}
	got, err := mgr.ReconnectAndWait(
		context.Background(),
		"j",
		TrainerReconnectOptions{
			MaxWaitForResumable: time.Minute,
			Now: func() time.Time {
				return time.Unix(0, int64(calls))
			},
			GetJob: func(context.Context, string) (map[string]any, error) {
				calls++
				if calls == 1 {
					return nil, errors.New("temporary")
				}
				return map[string]any{"state": "JOB_STATE_FAILED"}, nil
			},
			ResumeAndWait: func(context.Context, string, TrainerPollOptions) (TrainerServiceEndpoint, error) {
				return expected, nil
			},
			Sleep: func(time.Duration) {},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || got != expected {
		t.Fatalf("calls = %d endpoint = %#v", calls, got)
	}
}

func TestTrainerReconnectAndWaitStuckStateRaises(t *testing.T) {
	mgr := NewTrainerJobManager("test-key", "https://api.example.com")
	now := time.Unix(0, 0)
	_, err := mgr.ReconnectAndWait(
		context.Background(),
		"j",
		TrainerReconnectOptions{
			MaxWaitForResumable: 5 * time.Second,
			Now: func() time.Time {
				current := now
				now = now.Add(3 * time.Second)
				return current
			},
			GetJob: func(context.Context, string) (map[string]any, error) {
				return map[string]any{"state": "JOB_STATE_CREATING"}, nil
			},
			Sleep: func(time.Duration) {},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "stuck") {
		t.Fatalf("error = %v", err)
	}
}

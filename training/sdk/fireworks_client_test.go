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

func TestValidateOutputModelID(t *testing.T) {
	if errs := ValidateOutputModelID("valid-model-1"); len(errs) != 0 {
		t.Fatalf("errors = %#v", errs)
	}
	if errs := ValidateOutputModelID(""); len(errs) == 0 || !strings.Contains(errs[0], "required") {
		t.Fatalf("errors = %#v", errs)
	}
	if errs := ValidateOutputModelID("Bad_Model"); len(errs) == 0 || !strings.Contains(errs[0], "invalid characters") {
		t.Fatalf("errors = %#v", errs)
	}
	if errs := ValidateOutputModelID(strings.Repeat("a", 64)); len(errs) == 0 || !strings.Contains(errs[0], "too long") {
		t.Fatalf("errors = %#v", errs)
	}
}

func TestParseCheckpointName(t *testing.T) {
	accountID, jobID, checkpointID, ok := ParseCheckpointName("accounts/acct/rlorTrainerJobs/job-1/checkpoints/cp-1")
	if !ok {
		t.Fatal("expected parse success")
	}
	if accountID != "acct" || jobID != "job-1" || checkpointID != "cp-1" {
		t.Fatalf("parsed = %q %q %q", accountID, jobID, checkpointID)
	}
	if _, _, _, ok := ParseCheckpointName("accounts/acct/checkpoints/cp-1"); ok {
		t.Fatal("unexpected parse success")
	}
}

func TestBuildPromoteRequest(t *testing.T) {
	path, body := BuildPromoteRequest(
		"acct",
		"job-1",
		"cp-1",
		"out",
		"accounts/fireworks/models/base",
		"dep-1",
	)
	if path != "/v1/accounts/acct/checkpoints/cp-1:promote" {
		t.Fatalf("path = %q", path)
	}
	if body["output_model"] != "accounts/acct/models/out" {
		t.Fatalf("body = %#v", body)
	}
	if body["trainer_job_id"] != "accounts/acct/rlorTrainerJobs/job-1" {
		t.Fatalf("body = %#v", body)
	}
	if body["base_model"] != "accounts/fireworks/models/base" {
		t.Fatalf("body = %#v", body)
	}
	if body["hot_load_deployment_id"] != "accounts/acct/deployments/dep-1" {
		t.Fatalf("body = %#v", body)
	}
}

func TestFireworksClientResolvePromoteTarget(t *testing.T) {
	client := NewFireworksClient("test-key", "https://api.example.com")
	accountID, jobID, checkpointID, err := client.ResolvePromoteTarget(
		context.Background(),
		"accounts/acct/rlorTrainerJobs/job-1/checkpoints/cp-1",
		"",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if accountID != "acct" || jobID != "job-1" || checkpointID != "cp-1" {
		t.Fatalf("target = %q %q %q", accountID, jobID, checkpointID)
	}

	_, _, _, err = client.ResolvePromoteTarget(context.Background(), "bad", "", "")
	if err == nil || !strings.Contains(err.Error(), "Invalid checkpoint name") && !strings.Contains(err.Error(), "invalid checkpoint name") {
		t.Fatalf("error = %v", err)
	}
	_, _, _, err = client.ResolvePromoteTarget(
		context.Background(),
		"accounts/acct/rlorTrainerJobs/job-1/checkpoints/cp-1",
		"other-job",
		"",
	)
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("error = %v", err)
	}
}

func TestFireworksClientGetOperationSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/accounts/x/operations/op-1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "accounts/x/operations/op-1", "done": false})
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	result, err := client.GetOperation(context.Background(), "accounts/x/operations/op-1")
	if err != nil {
		t.Fatal(err)
	}
	if result["done"] != false {
		t.Fatalf("result = %#v", result)
	}
}

func TestFireworksClientGetOperationPermanentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"missing"}}`, http.StatusNotFound)
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	_, err := client.GetOperation(context.Background(), "accounts/x/operations/op-1")
	if err == nil || !strings.Contains(err.Error(), "Failed to poll operation") {
		t.Fatalf("error = %v", err)
	}
}

func TestFireworksClientWaitForOperationCompletesAfterTransientPollError(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, `{"error":"retry"}`, http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":     "accounts/x/operations/op-1",
			"done":     true,
			"response": map[string]any{"name": "accounts/x/models/out"},
		})
	}))
	defer server.Close()

	now := time.Unix(0, 0)
	client := NewFireworksClient("test-key", server.URL, WithTrainingRetryOptions(RequestRetryOptions{
		MaxWaitTime: time.Nanosecond,
		Now: func() time.Time {
			current := now
			now = now.Add(time.Second)
			return current
		},
		Sleep: func(time.Duration) {},
	}))
	result, err := client.WaitForOperation(
		context.Background(),
		map[string]any{"name": "accounts/x/operations/op-1", "done": false},
		OperationWaitOptions{
			Timeout:      time.Minute,
			PollInterval: 0,
			Now: func() time.Time {
				return time.Unix(0, 0)
			},
			Sleep: func(time.Duration) {},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result["done"] != true || calls != 2 {
		t.Fatalf("result = %#v calls = %d", result, calls)
	}
}

func TestFireworksClientWaitForOperationDoneError(t *testing.T) {
	client := NewFireworksClient("test-key", "https://api.example.com")
	_, err := client.WaitForOperation(
		context.Background(),
		map[string]any{
			"name": "accounts/x/operations/op-1",
			"done": true,
			"error": map[string]any{
				"message": "promotion failed",
			},
		},
		OperationWaitOptions{Sleep: func(time.Duration) {}},
	)
	if err == nil || !strings.Contains(err.Error(), "promotion failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestFireworksClientPromoteCheckpointSendsAsyncField(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/accounts/acct/checkpoints/cp-1:promote" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": map[string]any{
				"name":  "accounts/acct/models/out",
				"state": "READY",
				"kind":  "HF_BASE_MODEL",
			},
		})
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	model, err := client.PromoteCheckpoint(context.Background(), PromoteCheckpointOptions{
		Name:          "accounts/acct/rlorTrainerJobs/job-1/checkpoints/cp-1",
		OutputModelID: "out",
		BaseModel:     "accounts/fireworks/models/base",
	})
	if err != nil {
		t.Fatal(err)
	}
	if model["name"] != "accounts/acct/models/out" {
		t.Fatalf("model = %#v", model)
	}
	if body["async_promotion"] != true {
		t.Fatalf("body = %#v", body)
	}
}

func TestFireworksClientPromoteCheckpointRetriesWithoutAsyncField(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			http.Error(w, `{"error":{"message":"unknown field \"async_promotion\""}}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": map[string]any{"name": "accounts/acct/models/out"},
		})
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	model, err := client.PromoteCheckpoint(context.Background(), PromoteCheckpointOptions{
		Name:          "accounts/acct/rlorTrainerJobs/job-1/checkpoints/cp-1",
		OutputModelID: "out",
		BaseModel:     "accounts/fireworks/models/base",
	})
	if err != nil {
		t.Fatal(err)
	}
	if model["name"] != "accounts/acct/models/out" {
		t.Fatalf("model = %#v", model)
	}
	if len(bodies) != 2 || bodies[0]["async_promotion"] != true {
		t.Fatalf("bodies = %#v", bodies)
	}
	if _, ok := bodies[1]["async_promotion"]; ok {
		t.Fatalf("second body contains async_promotion: %#v", bodies[1])
	}
}

func TestFireworksClientPromoteCheckpointFailsWhenOperationResponseEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/accounts/acct/checkpoints/cp-1:promote":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"operation": map[string]any{
					"name": "accounts/acct/operations/op-1",
					"done": true,
				},
			})
		case "/v1/accounts/acct/models/out":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":  "accounts/acct/models/out",
				"state": "READY",
				"kind":  "HF_PEFT_ADDON",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	_, err := client.PromoteCheckpoint(context.Background(), PromoteCheckpointOptions{
		Name:          "accounts/acct/rlorTrainerJobs/job-1/checkpoints/cp-1",
		OutputModelID: "out",
		BaseModel:     "accounts/fireworks/models/base",
	})
	if err == nil || !strings.Contains(err.Error(), "server response did not contain the promoted model payload") {
		t.Fatalf("err = %v", err)
	}
}

func TestFireworksClientPromoteCheckpointRaisesCleanErrorWhenModelUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/accounts/acct/checkpoints/cp-1:promote":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"operation": map[string]any{
					"name": "accounts/acct/operations/op-1",
					"done": true,
				},
			})
		case "/v1/accounts/acct/models/out":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	_, err := client.PromoteCheckpoint(context.Background(), PromoteCheckpointOptions{
		Name:          "accounts/acct/rlorTrainerJobs/job-1/checkpoints/cp-1",
		OutputModelID: "out",
		BaseModel:     "accounts/fireworks/models/base",
	})
	if err == nil || !strings.Contains(err.Error(), "without a model response") {
		t.Fatalf("error = %v", err)
	}
}

func TestFireworksClientResolveTrainingProfileParsesSharding(t *testing.T) {
	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"trainingShapeVersions": []map[string]any{
				{
					"name": "accounts/a/trainingShapes/ts-test/versions/ver-123",
					"snapshot": map[string]any{
						"name":                      "accounts/a/trainingShapes/ts-test",
						"trainerImageTag":           "0.33.0",
						"maxSupportedContextLength": 8192,
						"nodeCount":                 2,
						"deploymentShapeVersion":    "dsv",
						"deploymentImageTag":        "img",
						"acceleratorType":           "NVIDIA_H100_80GB",
						"acceleratorCount":          8,
						"baseModelWeightPrecision":  "bfloat16",
						"trainerMode":               "LORA_TRAINER",
						"trainerShardingScheme": map[string]any{
							"tensorParallelism":   1,
							"pipelineParallelism": 4,
							"contextParallelism":  1,
							"expertParallelism":   1,
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	profile, err := client.ResolveTrainingProfile(context.Background(), "accounts/a/trainingShapes/ts-test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seenPath, "/trainingShapes/ts-test/versions") {
		t.Fatalf("path = %q", seenPath)
	}
	if profile.PipelineParallelism != 4 {
		t.Fatalf("pipeline parallelism = %d", profile.PipelineParallelism)
	}
	if profile.MaxSupportedContextLength != 8192 {
		t.Fatalf("max context = %d", profile.MaxSupportedContextLength)
	}
	if profile.TrainingShapeVersion != "accounts/a/trainingShapes/ts-test/versions/ver-123" {
		t.Fatalf("training shape version = %q", profile.TrainingShapeVersion)
	}
	if profile.TrainingShape() != "accounts/a/trainingShapes/ts-test" {
		t.Fatalf("training shape = %q", profile.TrainingShape())
	}
	if profile.TrainerMode != "LORA_TRAINER" || !profile.SupportsLora() {
		t.Fatalf("trainer mode = %q supports lora = %t", profile.TrainerMode, profile.SupportsLora())
	}
}

func TestFireworksClientResolveTrainingProfileRejectsBareShapeID(t *testing.T) {
	client := NewFireworksClient("test-key", "https://api.example.com")
	_, err := client.ResolveTrainingProfile(context.Background(), "ts-test")
	if err == nil || !strings.Contains(err.Error(), "not a valid training shape resource name") {
		t.Fatalf("error = %v", err)
	}
}

func TestFireworksClientResolveTrainingProfileUsesFullyQualifiedPath(t *testing.T) {
	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"trainingShapeVersions": []map[string]any{
				{"name": "accounts/fireworks/trainingShapes/ts-test/versions/ver-123"},
			},
		})
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	_, err := client.ResolveTrainingProfile(context.Background(), "accounts/fireworks/trainingShapes/ts-test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(seenPath, "/v1/accounts/fireworks/trainingShapes/ts-test/versions?") {
		t.Fatalf("path = %q", seenPath)
	}
}

func TestFireworksClientResolveTrainingProfile401MentionsTrainingScopedKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	_, err := client.ResolveTrainingProfile(context.Background(), "accounts/a/trainingShapes/ts-test")
	if err == nil || !strings.Contains(err.Error(), "training-scoped Fireworks API key") {
		t.Fatalf("error = %v", err)
	}
}

func TestTrainingShapeProfileProperties(t *testing.T) {
	profile := TrainingShapeProfile{
		TrainingShapeVersion:   "accounts/fw/trainingShapes/ts-x/versions/1",
		DeploymentShapeVersion: "accounts/fw/deploymentShapes/ds-x/versions/1",
		TrainerMode:            "LORA_TRAINER",
	}
	if profile.TrainingShape() != "accounts/fw/trainingShapes/ts-x" {
		t.Fatalf("training shape = %q", profile.TrainingShape())
	}
	if profile.DeploymentShape() != "accounts/fw/deploymentShapes/ds-x/versions/1" {
		t.Fatalf("deployment shape = %q", profile.DeploymentShape())
	}
	if !profile.SupportsLora() {
		t.Fatal("expected SupportsLora")
	}
	profile.TrainingShapeVersion = ""
	if profile.TrainingShape() != "" {
		t.Fatalf("training shape = %q, want empty", profile.TrainingShape())
	}
	profile.DeploymentShapeVersion = ""
	if profile.DeploymentShape() != "" {
		t.Fatalf("deployment shape = %q, want empty", profile.DeploymentShape())
	}
	profile.TrainerMode = "POLICY_TRAINER"
	if profile.SupportsLora() {
		t.Fatal("unexpected SupportsLora")
	}
}

func TestFireworksClientListCheckpointsSinglePage(t *testing.T) {
	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"checkpoints": []map[string]any{
				{
					"name":           "accounts/a/rlorTrainerJobs/j/checkpoints/step-1",
					"createTime":     "2026-04-16T10:00:00Z",
					"checkpointType": "INFERENCE_BASE",
					"promotable":     true,
				},
			},
		})
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	client.SetAccountID("a")
	rows, err := client.ListCheckpoints(context.Background(), "j", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["checkpointType"] != "INFERENCE_BASE" || rows[0]["promotable"] != true {
		t.Fatalf("rows = %#v", rows)
	}
	if !strings.HasPrefix(seenPath, "/v1/accounts/a/rlorTrainerJobs/j/checkpoints?") {
		t.Fatalf("path = %q", seenPath)
	}
	if !strings.Contains(seenPath, "pageSize=200") || strings.Contains(seenPath, "pageToken") {
		t.Fatalf("path = %q", seenPath)
	}
}

func TestFireworksClientListCheckpointsAutoPaginates(t *testing.T) {
	var seenPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.URL.String())
		if r.URL.Query().Get("pageToken") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"checkpoints":   []map[string]any{{"name": "c/1", "promotable": true}},
				"nextPageToken": "tok-2",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"checkpoints":   []map[string]any{{"name": "c/2", "promotable": false}},
			"nextPageToken": "",
		})
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	client.SetAccountID("a")
	rows, err := client.ListCheckpoints(context.Background(), "j", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["name"] != "c/1" || rows[1]["name"] != "c/2" {
		t.Fatalf("rows = %#v", rows)
	}
	if len(seenPaths) != 2 || !strings.Contains(seenPaths[1], "pageToken=tok-2") || !strings.Contains(seenPaths[1], "pageSize=1") {
		t.Fatalf("paths = %#v", seenPaths)
	}
}

func TestFireworksClientListCheckpointsAlternateKeyAndEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") == "empty" {
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rlorTrainerJobCheckpoints": []map[string]any{{"name": "c/1", "promotable": true}},
		})
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	client.SetAccountID("a")
	rows, err := client.ListCheckpoints(context.Background(), "j", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["name"] != "c/1" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestFireworksClientListCheckpointsErrors(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{status: http.StatusNotFound, want: "was not found in this account"},
		{status: http.StatusForbidden, want: "does not have access"},
	}
	for _, test := range tests {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"nope"}`, test.status)
			}))
			defer server.Close()

			client := NewFireworksClient("test-key", server.URL)
			client.SetAccountID("a")
			_, err := client.ListCheckpoints(context.Background(), "j", 200)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestParseSessionCheckpointName(t *testing.T) {
	accountID, sessionID, checkpointID, ok := ParseSessionCheckpointName("accounts/acct/trainingSessions/sess-1/checkpoints/cp-1")
	if !ok {
		t.Fatal("expected parse success")
	}
	if accountID != "acct" || sessionID != "sess-1" || checkpointID != "cp-1" {
		t.Fatalf("parsed = %q %q %q", accountID, sessionID, checkpointID)
	}
	if _, _, _, ok := ParseSessionCheckpointName("accounts/acct/rlorTrainerJobs/job/checkpoints/cp"); ok {
		t.Fatal("unexpected parse success")
	}
}

func TestFireworksClientListTrainingSessionCheckpointsAutoPaginates(t *testing.T) {
	var seenPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.URL.String())
		if r.URL.Query().Get("pageToken") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"trainingSessionCheckpoints": []map[string]any{{"name": "c/1", "promotable": true}},
				"nextPageToken":              "tok-2",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"checkpoints": []map[string]any{{"name": "c/2", "promotable": false}},
		})
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	rows, err := client.ListTrainingSessionCheckpoints(context.Background(), "accounts/acct/trainingSessions/sess-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["name"] != "c/1" || rows[1]["name"] != "c/2" {
		t.Fatalf("rows = %#v", rows)
	}
	if len(seenPaths) != 2 || !strings.Contains(seenPaths[1], "pageToken=tok-2") || !strings.Contains(seenPaths[1], "pageSize=1") {
		t.Fatalf("paths = %#v", seenPaths)
	}
}

func TestFireworksClientPromoteSessionCheckpoint(t *testing.T) {
	var body map[string]any
	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": map[string]any{"name": "accounts/acct/models/out", "state": "READY"},
		})
	}))
	defer server.Close()

	client := NewFireworksClient("test-key", server.URL)
	model, err := client.PromoteSessionCheckpoint(context.Background(), PromoteSessionCheckpointOptions{
		Name:          "accounts/acct/trainingSessions/sess-1/checkpoints/cp-1",
		OutputModelID: "out",
		BaseModel:     "accounts/fireworks/models/base",
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenPath != "/v1/accounts/acct/trainingSessions/sess-1/checkpoints/cp-1:promote" {
		t.Fatalf("path = %q", seenPath)
	}
	if body["output_model"] != "accounts/acct/models/out" || body["base_model"] != "accounts/fireworks/models/base" {
		t.Fatalf("body = %#v", body)
	}
	if model["name"] != "accounts/acct/models/out" {
		t.Fatalf("model = %#v", model)
	}
}

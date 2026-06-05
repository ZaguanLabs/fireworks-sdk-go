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

func TestFireworksClientPromoteCheckpointFetchesModelWhenOperationResponseEmpty(t *testing.T) {
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
	model, err := client.PromoteCheckpoint(context.Background(), PromoteCheckpointOptions{
		Name:          "accounts/acct/rlorTrainerJobs/job-1/checkpoints/cp-1",
		OutputModelID: "out",
		BaseModel:     "accounts/fireworks/models/base",
	})
	if err != nil {
		t.Fatal(err)
	}
	if model["state"] != "READY" {
		t.Fatalf("model = %#v", model)
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

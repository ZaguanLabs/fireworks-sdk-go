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

func TestDeploymentParseInfo(t *testing.T) {
	mgr := NewDeploymentManager("test-key", "https://api.example.com")
	mgr.SetAccountID("test-acct")

	info := mgr.ParseDeploymentInfo("dep-1", map[string]any{
		"name":              "accounts/test-acct/deployments/dep-1",
		"state":             "READY",
		"hotLoadBucketUrl":  "gs://bucket",
		"hotLoadTrainerJob": "accounts/test-acct/rlorTrainerJobs/job-1",
		"deploymentShape":   "accounts/test-acct/deploymentShapes/s/versions/v",
	})
	if info.DeploymentID != "dep-1" || info.State != "READY" || info.HotLoadBucketURL != "gs://bucket" {
		t.Fatalf("info = %#v", info)
	}
	if info.HotLoadTrainerJob != "accounts/test-acct/rlorTrainerJobs/job-1" {
		t.Fatalf("trainer job = %q", info.HotLoadTrainerJob)
	}
	if info.DeploymentShapeVersion != "accounts/test-acct/deploymentShapes/s/versions/v" {
		t.Fatalf("shape = %q", info.DeploymentShapeVersion)
	}
	if info.InferenceModel != "accounts/test-acct/deployments/dep-1" {
		t.Fatalf("inference model = %q", info.InferenceModel)
	}

	missing := mgr.ParseDeploymentInfo("dep-2", map[string]any{"name": "accounts/test-acct/deployments/dep-2"})
	if missing.State != "UNKNOWN" || missing.DeploymentShapeVersion != "" {
		t.Fatalf("missing = %#v", missing)
	}
}

func TestDeploymentHotLoadTrainerJob(t *testing.T) {
	deployment := DeploymentInfo{
		DeploymentID:      "dep-1",
		Name:              "accounts/test-acct/deployments/dep-1",
		State:             "READY",
		HotLoadTrainerJob: "accounts/test-acct/rlorTrainerJobs/job-1",
	}
	if DeploymentHotLoadTrainerJob(deployment) != "accounts/test-acct/rlorTrainerJobs/job-1" {
		t.Fatalf("deployment = %#v", deployment)
	}
}

func TestBuildDeploymentBodyDefaults(t *testing.T) {
	body := BuildDeploymentBody(DeploymentConfig{
		DeploymentID: "dep-1",
		BaseModel:    "accounts/test/models/qwen3-1p7b",
		Region:       "US_OHIO_1",
	})
	if body["description"] != DefaultDeploymentDescription {
		t.Fatalf("body = %#v", body)
	}
	if body["enableHotLoad"] != true || body["forTraining"] != true {
		t.Fatalf("body = %#v", body)
	}
	if body["maxReplicaCount"] != 1 || body["acceleratorType"] != "NVIDIA_H200_141GB" || body["hotLoadBucketType"] != "FW_HOSTED" {
		t.Fatalf("body = %#v", body)
	}
	if placement := body["placement"].(map[string]any); placement["region"] != "US_OHIO_1" {
		t.Fatalf("placement = %#v", placement)
	}
}

func TestBuildDeploymentBodyCanDisableHotload(t *testing.T) {
	enable := false
	maxReplicas := 0
	omitBucketType := ""
	body := BuildDeploymentBody(DeploymentConfig{
		DeploymentID:      "dep-1",
		BaseModel:         "accounts/test/models/qwen3-1p7b",
		MaxReplicaCount:   &maxReplicas,
		HotLoadBucketType: &omitBucketType,
		EnableHotLoad:     &enable,
	})
	if body["enableHotLoad"] != false || body["forTraining"] != false {
		t.Fatalf("body = %#v", body)
	}
	if body["maxReplicaCount"] != 0 {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["hotLoadBucketType"]; ok {
		t.Fatalf("body = %#v", body)
	}
}

func TestBuildDeploymentBodyShapeOmitsAcceleratorType(t *testing.T) {
	body := BuildDeploymentBody(DeploymentConfig{
		DeploymentID:    "dep-1",
		BaseModel:       "accounts/test/models/qwen3-1p7b",
		DeploymentShape: "accounts/test/deploymentShapes/shape/versions/v1",
		AcceleratorType: "NVIDIA_H100_80GB",
		ExtraArgs:       []string{"--pp 8", "--flag"},
		ExtraValues:     map[string]string{"k": "v"},
		Annotations:     map[string]string{"purpose": "test"},
	})
	if _, ok := body["acceleratorType"]; ok {
		t.Fatalf("body unexpectedly includes acceleratorType: %#v", body)
	}
	if body["deploymentShape"] != "accounts/test/deploymentShapes/shape/versions/v1" {
		t.Fatalf("body = %#v", body)
	}
	extra := body["extraArgs"].([]string)
	want := []string{"--pp", "8", "--flag"}
	for i := range want {
		if extra[i] != want[i] {
			t.Fatalf("extra args = %#v", extra)
		}
	}
	if body["annotations"].(map[string]string)["purpose"] != "test" || body["extraValues"].(map[string]string)["k"] != "v" {
		t.Fatalf("body = %#v", body)
	}
}

func TestDeploymentHotloadHeaders(t *testing.T) {
	mgr := NewDeploymentManager(
		"test-key",
		"https://api.example.com",
		WithDeploymentAdditionalHeaders(map[string]string{"X-Secret": "s"}),
	)
	mgr.SetAccountID("test-acct")

	headers, err := mgr.HotloadHeaders(context.Background(), "dep-1", "accounts/test/models/m", "gs://bucket/snap/")
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("X-Secret") != "s" || headers.Get("Authorization") != "Bearer test-key" {
		t.Fatalf("headers = %#v", headers)
	}
	if headers.Get("fireworks-model") != "accounts/test/models/m" {
		t.Fatalf("headers = %#v", headers)
	}
	if headers.Get("fireworks-deployment") != "accounts/test-acct/deployments/dep-1" {
		t.Fatalf("headers = %#v", headers)
	}
	if headers.Get(HotloadSourceURLHeader) != "gs://bucket/snap/" {
		t.Fatalf("headers = %#v", headers)
	}
}

func TestCreateDeploymentPostsCorrectPathAndBody(t *testing.T) {
	var seenPath string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":  "accounts/test-acct/deployments/dep-1",
			"state": "CREATING",
		})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", server.URL)
	mgr.SetAccountID("test-acct")
	_, err := mgr.CreateDeployment(context.Background(), DeploymentConfig{
		DeploymentID:               "dep-1",
		BaseModel:                  "accounts/test/models/qwen3-1p7b",
		Region:                     "US_OHIO_1",
		SkipShapeValidation:        true,
		DisableSpeculativeDecoding: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(seenPath, "/v1/accounts/test-acct/deployments?") {
		t.Fatalf("path = %q", seenPath)
	}
	if !strings.Contains(seenPath, "deploymentId=dep-1") || !strings.Contains(seenPath, "skipShapeValidation=true") || !strings.Contains(seenPath, "disableSpeculativeDecoding=true") {
		t.Fatalf("path = %q", seenPath)
	}
	if body["description"] != DefaultDeploymentDescription || body["enableHotLoad"] != true || body["forTraining"] != true {
		t.Fatalf("body = %#v", body)
	}
}

func TestCreateDeploymentOmitsPlacementWhenRegionUnset(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":  "accounts/test-acct/deployments/dep-1",
			"state": "CREATING",
		})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", server.URL)
	mgr.SetAccountID("test-acct")
	_, err := mgr.CreateDeployment(context.Background(), DeploymentConfig{
		DeploymentID: "dep-1",
		BaseModel:    "accounts/test/models/qwen3-1p7b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["placement"]; ok {
		t.Fatalf("body = %#v", body)
	}
}

func TestCreateDeploymentColocatesWithTrainerRegionWhenUnset(t *testing.T) {
	var body map[string]any
	var trainerPath string
	var postCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			trainerPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{"trainingConfig": map[string]any{"region": "US_OHIO_1"}})
		case http.MethodPost:
			postCount++
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":  "accounts/test-acct/deployments/dep-1",
				"state": "CREATING",
			})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", server.URL)
	mgr.SetAccountID("test-acct")
	_, err := mgr.CreateDeployment(context.Background(), DeploymentConfig{
		DeploymentID:      "dep-1",
		BaseModel:         "accounts/test/models/qwen3-1p7b",
		HotLoadTrainerJob: "accounts/test-acct/rlorTrainerJobs/job-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if trainerPath != "/v1/accounts/test-acct/rlorTrainerJobs/job-1" || postCount != 1 {
		t.Fatalf("trainerPath=%q postCount=%d", trainerPath, postCount)
	}
	if placement := body["placement"].(map[string]any); placement["region"] != "US_OHIO_1" {
		t.Fatalf("body = %#v", body)
	}
}

func TestCreateDeploymentRejectsConflictingTrainerRegion(t *testing.T) {
	var postCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount++
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"trainingConfig": map[string]any{"region": "US_OHIO_1"}})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", server.URL)
	mgr.SetAccountID("test-acct")
	_, err := mgr.CreateDeployment(context.Background(), DeploymentConfig{
		DeploymentID:      "dep-1",
		BaseModel:         "accounts/test/models/qwen3-1p7b",
		Region:            "US_VIRGINIA_1",
		HotLoadTrainerJob: "accounts/test-acct/rlorTrainerJobs/job-1",
	})
	if err == nil || !strings.Contains(err.Error(), "colocated") {
		t.Fatalf("err = %v", err)
	}
	if postCount != 0 {
		t.Fatalf("postCount = %d", postCount)
	}
}

func TestCreateDeploymentUnresolvableTrainerRegionProceeds(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":  "accounts/test-acct/deployments/dep-1",
			"state": "CREATING",
		})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", server.URL)
	mgr.SetAccountID("test-acct")
	_, err := mgr.CreateDeployment(context.Background(), DeploymentConfig{
		DeploymentID:      "dep-1",
		BaseModel:         "accounts/test/models/qwen3-1p7b",
		HotLoadTrainerJob: "accounts/test-acct/rlorTrainerJobs/job-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["placement"]; ok {
		t.Fatalf("body = %#v", body)
	}
}

func TestCreateDeploymentConflictFetchesExisting(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.String())
		if r.Method == http.MethodPost {
			http.Error(w, `{"error":"already exists"}`, http.StatusConflict)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":  "accounts/test-acct/deployments/dep-1",
			"state": "READY",
		})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", server.URL)
	mgr.SetAccountID("test-acct")
	created, err := mgr.CreateDeployment(context.Background(), DeploymentConfig{
		DeploymentID: "dep-1",
		BaseModel:    "accounts/test/models/qwen3-1p7b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created["state"] != "READY" {
		t.Fatalf("created = %#v", created)
	}
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "POST ") || calls[1] != "GET /v1/accounts/test-acct/deployments/dep-1" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestCreateOrGetReturnsExistingReadyDeployment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":  "accounts/test-acct/deployments/dep-1",
			"state": "READY",
		})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", server.URL)
	mgr.SetAccountID("test-acct")
	info, err := mgr.CreateOrGet(context.Background(), DeploymentConfig{
		DeploymentID: "dep-1",
		BaseModel:    "accounts/test/models/qwen3-1p7b",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if info.State != "READY" || info.Name != "accounts/test-acct/deployments/dep-1" {
		t.Fatalf("info = %#v", info)
	}
}

func TestUpdateDeploymentUsesUpdateMask(t *testing.T) {
	var seenPath string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":              "accounts/test-acct/deployments/dep-1",
			"state":             "READY",
			"hotLoadTrainerJob": "accounts/test-acct/rlorTrainerJobs/job-1",
		})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", server.URL)
	mgr.SetAccountID("test-acct")
	info, err := mgr.Update(context.Background(), "dep-1", map[string]any{"hotLoadTrainerJob": "accounts/test-acct/rlorTrainerJobs/job-1"}, []string{"hot_load_trainer_job"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seenPath, "updateMask=hot_load_trainer_job") {
		t.Fatalf("path = %q", seenPath)
	}
	if body["hotLoadTrainerJob"] != "accounts/test-acct/rlorTrainerJobs/job-1" || info.HotLoadTrainerJob == "" {
		t.Fatalf("body=%#v info=%#v", body, info)
	}
}

func TestWaitForReadyAcceptsReadyAndCreatingProbe(t *testing.T) {
	now := time.Unix(0, 0)
	mgr := NewDeploymentManager("test-key", "https://api.example.com")
	mgr.SetAccountID("test-acct")

	info, err := mgr.WaitForReady(context.Background(), "dep-1", DeploymentWaitOptions{
		Timeout:      time.Second,
		PollInterval: time.Nanosecond,
		Now:          func() time.Time { return now },
		Sleep:        func(d time.Duration) { now = now.Add(d) },
		Get: func(context.Context, string) (map[string]any, error) {
			return map[string]any{"name": "accounts/test-acct/deployments/dep-1", "state": "CREATING"}, nil
		},
		Probe: func(context.Context, string) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.State != "CREATING" || mgr.BootTime != 0 {
		t.Fatalf("info=%#v boot=%s", info, mgr.BootTime)
	}
}

func TestHotloadSendsIdentityMetadataAndPathHeader(t *testing.T) {
	var body map[string]any
	var sourceURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hot_load/v1/models/hot_load" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		sourceURL = r.Header.Get(HotloadSourceURLHeader)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", "https://api.example.com", WithDeploymentHotloadAPIURL(server.URL))
	mgr.SetAccountID("test-acct")
	got, err := mgr.Hotload(context.Background(), "dep-1", "accounts/test/models/m", "snap-2", HotloadOptions{
		Path: "gs://bucket/snap/",
		IncrementalSnapshotMetadata: map[string]any{
			"previous_snapshot_identity": "snap-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["ok"] != true {
		t.Fatalf("got = %#v", got)
	}
	if body["identity"] != "snap-2" || body["reset_prompt_cache"] != true {
		t.Fatalf("body = %#v", body)
	}
	metadata := body["incremental_snapshot_metadata"].(map[string]any)
	if metadata["previous_snapshot_identity"] != "snap-1" {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["path"]; ok {
		t.Fatalf("body includes path: %#v", body)
	}
	if sourceURL != "gs://bucket/snap/" {
		t.Fatalf("sourceURL = %q", sourceURL)
	}
}

func TestHotloadRetriesWithoutResetPromptCacheWhenRejected(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Extra inputs are not permitted, field: 'reset_prompt_cache', value: True"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", "https://api.example.com", WithDeploymentHotloadAPIURL(server.URL))
	mgr.SetAccountID("test-acct")
	if _, err := mgr.Hotload(context.Background(), "dep-1", "accounts/test/models/m", "snap-123"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Hotload(context.Background(), "dep-1", "accounts/test/models/m", "snap-456"); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 3 {
		t.Fatalf("bodies = %#v", bodies)
	}
	if _, ok := bodies[0]["reset_prompt_cache"]; !ok {
		t.Fatalf("first body = %#v", bodies[0])
	}
	if _, ok := bodies[1]["reset_prompt_cache"]; ok {
		t.Fatalf("retry body = %#v", bodies[1])
	}
	if _, ok := bodies[2]["reset_prompt_cache"]; ok {
		t.Fatalf("second hotload body = %#v", bodies[2])
	}
	if mgr.HotloadResetPromptCacheSupported == nil || *mgr.HotloadResetPromptCacheSupported {
		t.Fatalf("reset prompt cache support = %#v", mgr.HotloadResetPromptCacheSupported)
	}
}

func TestHotloadCheckStatus(t *testing.T) {
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.Method != http.MethodGet || r.URL.Path != "/hot_load/v1/models/hot_load" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"replicas": []any{map[string]any{"current_snapshot_identity": "snap-1"}}})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", "https://api.example.com", WithDeploymentHotloadAPIURL(server.URL))
	mgr.SetAccountID("test-acct")
	status, err := mgr.HotloadCheckStatus(context.Background(), "dep-1", "accounts/test/models/m")
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer test-key" {
		t.Fatalf("auth = %q", auth)
	}
	replicas := status["replicas"].([]any)
	if len(replicas) != 1 {
		t.Fatalf("status = %#v", status)
	}
}

func TestWaitForHotloadImmediateSuccess(t *testing.T) {
	mgr := NewDeploymentManager("test-key", "https://api.example.com")
	ok, err := mgr.WaitForHotload(context.Background(), "dep-1", "m", "snap-1", HotloadWaitOptions{
		CheckStatus: func(context.Context, string, string) (map[string]any, error) {
			return map[string]any{"replicas": []any{map[string]any{
				"current_snapshot_identity": "snap-1",
				"loading_state":             map[string]any{"stage": "idle"},
				"readiness":                 true,
			}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected success")
	}
}

func TestWaitForHotloadLoadedAdaptersSignalCompletion(t *testing.T) {
	mgr := NewDeploymentManager("test-key", "https://api.example.com")
	ok, err := mgr.WaitForHotload(context.Background(), "dep-1", "m", "snap-1", HotloadWaitOptions{
		CheckStatus: func(context.Context, string, string) (map[string]any, error) {
			return map[string]any{"replicas": []any{map[string]any{
				"current_snapshot_identity": nil,
				"loading_state":             map[string]any{"stage": "idle"},
				"readiness":                 true,
				"loaded_adapters": []any{
					map[string]any{"identity": "snap-1", "status": "loaded"},
				},
			}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected success")
	}
}

func TestWaitForHotloadTimeoutRecordsError(t *testing.T) {
	now := time.Unix(0, 0)
	mgr := NewDeploymentManager("test-key", "https://api.example.com")
	ok, err := mgr.WaitForHotload(context.Background(), "dep-1", "m", "snap-x", HotloadWaitOptions{
		Timeout:      10 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
		Now:          func() time.Time { return now },
		Sleep:        func(d time.Duration) { now = now.Add(d) },
		CheckStatus: func(context.Context, string, string) (map[string]any, error) {
			return map[string]any{"replicas": []any{map[string]any{
				"identity": "old",
				"stage":    "downloading",
				"ready":    false,
			}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(mgr.LastHotloadErrorMessage, "Hotload did not complete") || !strings.Contains(mgr.LastHotloadErrorMessage, "Expected client snapshot: snap-x") {
		t.Fatalf("message = %q", mgr.LastHotloadErrorMessage)
	}
}

func TestWaitForHotloadLoadedAdaptersNonLoadedDoesNotComplete(t *testing.T) {
	now := time.Unix(0, 0)
	mgr := NewDeploymentManager("test-key", "https://api.example.com")
	ok, err := mgr.WaitForHotload(context.Background(), "dep-1", "m", "snap-1", HotloadWaitOptions{
		Timeout:      10 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
		Now:          func() time.Time { return now },
		Sleep:        func(d time.Duration) { now = now.Add(d) },
		CheckStatus: func(context.Context, string, string) (map[string]any, error) {
			return map[string]any{"replicas": []any{map[string]any{
				"current_snapshot_identity": nil,
				"readiness":                 true,
				"loading_state":             map[string]any{"stage": "downloading"},
				"loaded_adapters":           []any{map[string]any{"identity": "snap-1", "status": "loading"}},
			}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected non-loaded adapter to keep waiting")
	}
}

func TestWaitForHotloadErrorStatusRecordsSnapshotState(t *testing.T) {
	mgr := NewDeploymentManager("test-key", "https://api.example.com")
	ok, err := mgr.WaitForHotload(context.Background(), "dep-1", "m", "snap-1", HotloadWaitOptions{
		CheckStatus: func(context.Context, string, string) (map[string]any, error) {
			return map[string]any{"replicas": []any{map[string]any{
				"current_snapshot_identity": "old-snap",
				"readiness":                 false,
				"loading_state": map[string]any{
					"stage":                    "error",
					"target_snapshot_identity": "snap-1",
				},
			}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected failure")
	}
	for _, want := range []string{
		"reported an error for the requested snapshot",
		"Expected client snapshot: snap-1",
		"current deployment snapshot: old-snap",
		"Use the Fireworks training cookbook skill's hotload recovery self-check",
		"reattach or recreate a stale deployment",
		"full-parameter training",
		"for LoRA, fix deployment attachment",
		"First search the Fireworks training cookbook skill",
		"https://github.com/fw-ai/cookbook",
	} {
		if !strings.Contains(mgr.LastHotloadErrorMessage, want) {
			t.Fatalf("message missing %q: %s", want, mgr.LastHotloadErrorMessage)
		}
	}
}

func TestWaitForHotloadRejectsUnrecognizedStatusFormat(t *testing.T) {
	mgr := NewDeploymentManager("test-key", "https://api.example.com")
	_, err := mgr.WaitForHotload(context.Background(), "dep-1", "m", "snap-1", HotloadWaitOptions{
		CheckStatus: func(context.Context, string, string) (map[string]any, error) {
			return map[string]any{"state": "loaded"}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "Unrecognized hotload status response format") {
		t.Fatalf("err = %v", err)
	}
}

func TestHotloadAndWaitForwardsPath(t *testing.T) {
	var sourceURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceURL = r.Header.Get(HotloadSourceURLHeader)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", "https://api.example.com", WithDeploymentHotloadAPIURL(server.URL))
	mgr.SetAccountID("test-acct")
	ok, err := mgr.HotloadAndWait(context.Background(), "dep-1", "accounts/test/models/m", "snap-123", HotloadAndWaitOptions{
		Path: "gs://bucket/snap/",
		Wait: HotloadWaitOptions{
			CheckStatus: func(context.Context, string, string) (map[string]any, error) {
				return map[string]any{"replicas": []any{map[string]any{
					"current_snapshot_identity": "snap-123",
					"loading_state":             map[string]any{"stage": "idle"},
					"readiness":                 true,
				}}}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || sourceURL != "gs://bucket/snap/" {
		t.Fatalf("ok=%v sourceURL=%q", ok, sourceURL)
	}
}

func TestReattachTrainerAlreadyAttachedReturnsExisting(t *testing.T) {
	mgr := NewDeploymentManager("test-key", "https://api.example.com")
	existing := DeploymentInfo{
		DeploymentID:      "dep-1",
		Name:              "accounts/test-acct/deployments/dep-1",
		State:             "READY",
		HotLoadTrainerJob: "accounts/test-acct/rlorTrainerJobs/job-1",
	}
	result, err := mgr.ReattachTrainer(context.Background(), existing, "accounts/test-acct/models/base", "accounts/test-acct/rlorTrainerJobs/job-1", ReattachTrainerOptions{
		ReadReplicaIdentity: func(context.Context, string, string) (string, error) {
			t.Fatal("should not read replica identity")
			return "", nil
		},
		Update: func(context.Context, string, map[string]any, any) (DeploymentInfo, error) {
			t.Fatal("should not update")
			return DeploymentInfo{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != existing {
		t.Fatalf("result = %#v", result)
	}
}

func TestReattachTrainerPatchesAndWaitsForNewReplica(t *testing.T) {
	now := time.Unix(0, 0)
	identities := []string{"old-pod", "", "new-pod"}
	var updateBody map[string]any
	mgr := NewDeploymentManager("test-key", "https://api.example.com")
	existing := DeploymentInfo{
		DeploymentID:      "dep-1",
		Name:              "accounts/test-acct/deployments/dep-1",
		State:             "READY",
		HotLoadTrainerJob: "accounts/test-acct/rlorTrainerJobs/old-job",
	}
	updated := DeploymentInfo{
		DeploymentID:      "dep-1",
		Name:              "accounts/test-acct/deployments/dep-1",
		State:             "READY",
		HotLoadTrainerJob: "accounts/test-acct/rlorTrainerJobs/job-1",
	}
	result, err := mgr.ReattachTrainer(context.Background(), existing, "accounts/test-acct/models/base", "accounts/test-acct/rlorTrainerJobs/job-1", ReattachTrainerOptions{
		Timeout:      time.Second,
		PollInterval: time.Millisecond,
		Now:          func() time.Time { return now },
		Sleep:        func(d time.Duration) { now = now.Add(d) },
		ReadReplicaIdentity: func(context.Context, string, string) (string, error) {
			if len(identities) == 0 {
				return "new-pod", nil
			}
			got := identities[0]
			identities = identities[1:]
			return got, nil
		},
		Update: func(_ context.Context, deploymentID string, body map[string]any, updateMask any) (DeploymentInfo, error) {
			if deploymentID != "dep-1" || updateMask != "hot_load_trainer_job" {
				t.Fatalf("deploymentID=%q updateMask=%#v", deploymentID, updateMask)
			}
			updateBody = body
			return updated, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != updated {
		t.Fatalf("result = %#v", result)
	}
	if updateBody["hotLoadTrainerJob"] != "accounts/test-acct/rlorTrainerJobs/job-1" {
		t.Fatalf("updateBody = %#v", updateBody)
	}
}

func TestProbeInferenceUsesTokenPrompt(t *testing.T) {
	var body map[string]any
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := NewDeploymentManager("test-key", "https://api.example.com", WithDeploymentInferenceURL(server.URL))
	if !mgr.ProbeInference(context.Background(), "accounts/test/deployments/dep-1") {
		t.Fatal("probe failed")
	}
	if auth != "Bearer test-key" || body["model"] != "accounts/test/deployments/dep-1" {
		t.Fatalf("auth=%q body=%#v", auth, body)
	}
	prompt := body["prompt"].([]any)
	if len(prompt) != 2 || prompt[0].(float64) != 1 || prompt[1].(float64) != 2 {
		t.Fatalf("body = %#v", body)
	}
}

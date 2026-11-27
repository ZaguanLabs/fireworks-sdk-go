package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func clearMLflowEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"MLFLOW_TRACKING_URI", "MLFLOW_TRACKING_TOKEN", "MLFLOW_EXPERIMENT_NAME", "MLFLOW_RUN_NAME", "FIREWORKS_JOB_ID", "FIREWORKS_REQUIRE_MLFLOW", "FIREWORKS_SKIP_MLFLOW_REASON"} {
		t.Setenv(k, "")
	}
	t.Setenv("FIREWORKS_MLFLOW_CONFIG_DIR", t.TempDir())
}

func TestMLflowConfigAndTransport(t *testing.T) {
	clearMLflowEnv(t)
	ctx := context.Background()
	if c, err := NewMLflowFromEnv(ctx, MLflowOptions{}); c != nil || err != nil {
		t.Fatal("unexpected optional mirror", err)
	}
	if !MLflowRequiredByServer(http.Header{"x-fireworks-require-mlflow": {" YeS "}}) {
		t.Fatal("header was ignored")
	}
	if _, err := NewMLflowFromEnv(ctx, MLflowOptions{ServerRequired: true}); err == nil {
		t.Fatal("missing required credentials accepted")
	}
	t.Setenv("MLFLOW_TRACKING_URI", "http://example.org")
	t.Setenv("MLFLOW_TRACKING_TOKEN", "unused")
	if _, err := NewMLflowFromEnv(ctx, MLflowOptions{}); err == nil {
		t.Fatal("insecure transport accepted")
	}
	t.Setenv("FIREWORKS_SKIP_MLFLOW_REASON", "disabled for test")
	if c, err := NewMLflowFromEnv(ctx, MLflowOptions{ServerRequired: true}); c != nil || err != nil {
		t.Fatal("skip override ignored", err)
	}
}

func TestMLflowIngestLifecycle(t *testing.T) {
	clearMLflowEnv(t)
	var mu sync.Mutex
	metrics := 0
	params := map[string]string{}
	updated := 0
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer local-test" || r.Method != "POST" {
			t.Error("missing auth or invalid HTTP method")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/proxy/ingest/experiments":
			attempts++
			if attempts == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(503)
				return
			}
			fmt.Fprint(w, `{"experiment_id":1}`)
		case "/proxy/api/2.0/mlflow/runs/create":
			if body["experiment_id"] != "1" || body["run_name"] != "test" {
				t.Errorf("run = %v", body)
			}
			fmt.Fprint(w, `{"run":{"info":{"run_id":"r"}}}`)
		case "/proxy/api/2.0/mlflow/runs/log-batch":
			batch := body["metrics"].([]any)
			if len(batch) > 1000 {
				t.Error("oversized metric batch")
			}
			metrics += len(batch)
			for _, item := range body["params"].([]any) {
				e := item.(map[string]any)
				k := e["key"].(string)
				if _, exists := params[k]; exists {
					t.Error("duplicate parameter", k)
				}
				params[k] = e["value"].(string)
			}
		case "/proxy/api/2.0/mlflow/runs/update":
			if body["status"] != "KILLED" {
				t.Errorf("terminal status = %v", body["status"])
			}
			updated++
		default:
			t.Error("unexpected route", r.URL.Path)
		}
	}))
	defer server.Close()
	dir := os.Getenv("FIREWORKS_MLFLOW_CONFIG_DIR")
	if err := os.WriteFile(filepath.Join(dir, "tracking_uri"), []byte(server.URL+"/proxy\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MLFLOW_TRACKING_TOKEN", "local-test")
	c, err := NewMLflowFromEnv(context.Background(), MLflowOptions{RunName: "test", Params: map[string]any{"client": map[string]string{"token": "sensitive", "count": "2"}}})
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"session_id": 2, "bad@key": 2, "infinite": math.Inf(1), "step": 123}
	for i := 0; i < 1001; i++ {
		data[fmt.Sprintf("pass@%d", i)] = i
	}
	c.Log(data, 123)
	c.LogParams(map[string]any{"client.count": 5})
	if err := c.Finish(context.Background(), "KILLED"); err != nil {
		t.Fatal(err)
	}
	c.Log(map[string]any{"late": 1}, 1)
	if err := c.Finish(context.Background(), "FINISHED"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if updated != 1 || metrics != 1001 || params["client.token"] != "[REDACTED]" || params["client.count"] != "2" {
		t.Fatalf("updates=%d metrics=%d params=%v", updated, metrics, params)
	}
}

func TestMLflowCredentialFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "original")
	if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "token")); err != nil {
		t.Skip(err)
	}
	if got := mlflowFile(dir, "token"); got != "" {
		t.Fatal("followed symlink")
	}
	if got, ok := TranslateMLflowKey("eval/pass^8_score"); !ok || got != "eval/pass_all_8_score" {
		t.Fatal(got)
	}
}

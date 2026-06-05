package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fwtypes "github.com/ZaguanLabs/fireworks-sdk-go/types"
)

func TestVersionMatchesPythonSDK(t *testing.T) {
	if Version != "1.2.0-alpha.75" {
		t.Fatalf("Version = %q, want %q", Version, "1.2.0-alpha.75")
	}
}

func TestNewClientRequiresAPIKey(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "")

	_, err := NewClient()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInferenceRequestUsesPythonCompatibleEnvironmentAndHeaders(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "env-key")
	t.Setenv("FIREWORKS_CUSTOM_HEADERS", "X-From-Env: yes\nIgnored\nX-Second: value")

	var seenPath string
	var seenBody JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer env-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Stainless-Async"); got != "false" {
			t.Errorf("X-Stainless-Async = %q", got)
		}
		if got := r.Header.Get("X-Stainless-Lang"); got != "go" {
			t.Errorf("X-Stainless-Lang = %q", got)
		}
		if got := r.Header.Get("X-Stainless-Package-Version"); got != Version {
			t.Errorf("X-Stainless-Package-Version = %q", got)
		}
		if got := r.Header.Get("X-From-Env"); got != "yes" {
			t.Errorf("X-From-Env = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Completions.Create(context.Background(), JSON{"model": "m", "prompt": "p"})
	if err != nil {
		t.Fatal(err)
	}

	if seenPath != "/v1/completions" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenBody["model"] != "m" || seenBody["prompt"] != "p" {
		t.Fatalf("body = %#v", seenBody)
	}
	if out["ok"] != true {
		t.Fatalf("response = %#v", out)
	}
}

func TestAccountResourcePathAndCommaQuery(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")
	t.Setenv("FIREWORKS_ACCOUNT_ID", "default-account")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/accounts/override-account/models/model-1" {
			t.Errorf("path = %q", got)
		}
		if got := r.URL.Query().Get("read_mask"); got != "a,b" {
			t.Errorf("read_mask = %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"name": "model-1"})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Models.Get(
		context.Background(),
		"model-1",
		WithAccountID("override-account"),
		WithQuery(map[string]any{"read_mask": []string{"a", "b"}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if out["name"] != "model-1" {
		t.Fatalf("response = %#v", out)
	}
}

func TestStatusErrorMapping(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"limited"}`, http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Completions.Create(context.Background(), JSON{"model": "m", "prompt": "p"})
	if err == nil {
		t.Fatal("expected error")
	}
	var rateLimit *RateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("error type = %T", err)
	}
}

func TestStreamReadsServerSentEvents(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chunk-1\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Chat.Completions.CreateStream(context.Background(), JSON{"stream": true})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	if !stream.Next() {
		t.Fatalf("expected first chunk, err=%v", stream.Err())
	}
	if got := stream.Current()["id"]; got != "chunk-1" {
		t.Fatalf("chunk id = %q", got)
	}
	if stream.Next() {
		t.Fatal("expected stream to stop at [DONE]")
	}
	if err := stream.Err(); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("stream err = %v", err)
	}
}

func TestChatCompletionCreateTyped(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if got := body["model"]; got != "accounts/fireworks/models/test" {
			t.Errorf("model = %q", got)
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("messages = %#v", body["messages"])
		}
		message, ok := messages[0].(map[string]any)
		if !ok || message["role"] != "user" || message["content"] != "hello" {
			t.Fatalf("message = %#v", messages[0])
		}
		_ = json.NewEncoder(w).Encode(JSON{
			"id":      "chatcmpl-1",
			"created": 123,
			"model":   "accounts/fireworks/models/test",
			"choices": []JSON{
				{
					"index": 0,
					"message": JSON{
						"role":    "assistant",
						"content": "world",
					},
					"finish_reason": "stop",
				},
			},
			"usage": JSON{
				"prompt_tokens":     1,
				"completion_tokens": 1,
				"total_tokens":      2,
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Chat.Completions.CreateTyped(context.Background(), fwtypes.ChatCompletionCreateParams{
		Model: "accounts/fireworks/models/test",
		Messages: []fwtypes.SharedParamsChatMessage{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "chatcmpl-1" {
		t.Fatalf("ID = %q", out.ID)
	}
	if len(out.Choices) != 1 || out.Choices[0].Message.Role != "assistant" || out.Choices[0].Message.Content != "world" {
		t.Fatalf("choices = %#v", out.Choices)
	}
	if out.Usage == nil || out.Usage.TotalTokens != 2 {
		t.Fatalf("usage = %#v", out.Usage)
	}
}

func TestCompletionCreateTypedStream(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"cmpl-1\",\"created\":123,\"model\":\"m\",\"choices\":[{\"index\":0,\"text\":\"hi\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Completions.CreateTypedStream(context.Background(), fwtypes.CompletionCreateParams{
		Model:  "m",
		Prompt: "prompt",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	if !stream.Next() {
		t.Fatalf("expected typed chunk, err=%v", stream.Err())
	}
	chunk := stream.Current()
	if chunk.ID != "cmpl-1" || len(chunk.Choices) != 1 || chunk.Choices[0].Text != "hi" {
		t.Fatalf("chunk = %#v", chunk)
	}
}

func TestMessagesCreateTyped(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/messages" {
			t.Errorf("path = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if got := body["model"]; got != "accounts/fireworks/models/test" {
			t.Errorf("model = %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{
			"id":    "msg-1",
			"model": "accounts/fireworks/models/test",
			"role":  "assistant",
			"type":  "message",
			"content": []JSON{
				{"type": "text", "text": "hello"},
			},
			"stop_reason": "end_turn",
		})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Messages.CreateTyped(context.Background(), fwtypes.MessageCreateParams{
		Model: "accounts/fireworks/models/test",
		Messages: []fwtypes.MessageCreateParamsMessage{
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.MessageCreateResponse = out
	if out.ID != "msg-1" || out.Role != "assistant" {
		t.Fatalf("response = %#v", out)
	}
}

func TestModelsListTypedUsesPythonPaginationShape(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")
	t.Setenv("FIREWORKS_ACCOUNT_ID", "acct")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/accounts/acct/models" {
			t.Errorf("path = %q", got)
		}
		if got := r.URL.Query().Get("pageToken"); got != "cursor-1" {
			t.Errorf("pageToken = %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{
			"models": []JSON{
				{
					"name":          "accounts/acct/models/model-1",
					"displayName":   "Model One",
					"contextLength": 8192,
				},
			},
			"nextPageToken": "cursor-2",
		})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.Models.ListTyped(context.Background(), map[string]any{"pageToken": "cursor-1"})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.ModelsPage = page
	if len(page.Models) != 1 {
		t.Fatalf("models = %#v", page.Models)
	}
	if page.Models[0].Name == nil || *page.Models[0].Name != "accounts/acct/models/model-1" {
		t.Fatalf("model name = %#v", page.Models[0].Name)
	}
	if page.Models[0].ContextLength == nil || *page.Models[0].ContextLength != 8192 {
		t.Fatalf("context length = %#v", page.Models[0].ContextLength)
	}
	if page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("next page token = %#v", page.NextPageToken)
	}
}

func TestDeploymentsScaleTypedUsesActionPath(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodPatch {
			t.Errorf("method = %q", got)
		}
		if got := r.URL.Path; got != "/v1/accounts/acct/deployments/dep-1:scale" {
			t.Errorf("path = %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{
			"baseModel":           "accounts/fireworks/models/test",
			"name":                "accounts/acct/deployments/dep-1",
			"desiredReplicaCount": 3,
		})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithDefaultAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := client.Deployments.ScaleTyped(context.Background(), "dep-1", JSON{"replicaCount": 3})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.Deployment = deployment
	if deployment.DesiredReplicaCount == nil || *deployment.DesiredReplicaCount != 3 {
		t.Fatalf("deployment = %#v", deployment)
	}
}

func TestEvaluatorBuildLogEndpointTyped(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/accounts/acct/evaluators/eval-1:getBuildLogEndpoint" {
			t.Errorf("path = %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"buildLogSignedUri": "gs://logs/build.txt"})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithDefaultAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := client.Evaluators.GetBuildLogEndpointTyped(context.Background(), "eval-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.EvaluatorGetBuildLogEndpointResponse = endpoint
	if endpoint.BuildLogSignedURI == nil || *endpoint.BuildLogSignedURI != "gs://logs/build.txt" {
		t.Fatalf("endpoint = %#v", endpoint)
	}
}

package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestWithOptionsClonesClientAndPreservesDefaultInferenceBaseURLMode(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")
	t.Setenv("FIREWORKS_BASE_URL", "")

	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	clone, err := client.WithOptions(WithDefaultHeader("X-Clone", "yes"))
	if err != nil {
		t.Fatal(err)
	}

	if clone == client {
		t.Fatal("expected a distinct client")
	}
	if clone.baseURLOverridden {
		t.Fatal("clone unexpectedly marked the default base URL as overridden")
	}
	if got := clone.inferencePath("/v1/completions"); got != defaultBaseURL+"/inference/v1/completions" {
		t.Fatalf("inference path = %q", got)
	}
	if got := clone.defaultHeaders.Get("X-Clone"); got != "yes" {
		t.Fatalf("clone header = %q", got)
	}
	if got := client.defaultHeaders.Get("X-Clone"); got != "" {
		t.Fatalf("original client header mutated to %q", got)
	}
}

func TestWithOptionsOverridesClientSettingsForRequests(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "env-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/accounts/acct-2/models/model-1" {
			t.Errorf("path = %q", got)
		}
		if got := r.URL.Query().Get("source"); got != "clone" {
			t.Errorf("source query = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key-2" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Scope"); got != "clone" {
			t.Errorf("X-Scope = %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"name": "model-1"})
	}))
	defer server.Close()

	client, err := NewClient(
		WithAPIKey("key-1"),
		WithBaseURL(server.URL),
		WithDefaultAccountID("acct-1"),
		WithDefaultHeader("X-Scope", "original"),
	)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := client.WithOptions(
		WithAPIKey("key-2"),
		WithDefaultAccountID("acct-2"),
		WithDefaultHeader("X-Scope", "clone"),
		WithDefaultQuery(map[string]any{"source": "clone"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	out, err := clone.Models.Get(context.Background(), "model-1")
	if err != nil {
		t.Fatal(err)
	}
	if out["name"] != "model-1" {
		t.Fatalf("response = %#v", out)
	}
	if client.APIKey() != "key-1" || client.AccountID() != "acct-1" || client.defaultHeaders.Get("X-Scope") != "original" {
		t.Fatalf("original client mutated: apiKey=%q accountID=%q header=%q", client.APIKey(), client.AccountID(), client.defaultHeaders.Get("X-Scope"))
	}
}

func TestHeaderOmitOptionsRemoveDefaultHeaders(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	var seenContentType string
	var seenUserAgent string
	var seenCustom string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenContentType = r.Header.Get("Content-Type")
		seenUserAgent = r.Header.Get("User-Agent")
		seenCustom = r.Header.Get("X-Custom")
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(
		WithBaseURL(server.URL),
		WithDefaultHeader("X-Custom", "present"),
		WithDefaultOmitHeader("User-Agent"),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Post(context.Background(), "/headers", JSON{"ok": true}, WithOmitHeader("Content-Type"), WithOmitHeader("X-Custom"))
	if err != nil {
		t.Fatal(err)
	}
	if seenContentType != "" {
		t.Fatalf("Content-Type = %q", seenContentType)
	}
	if seenUserAgent != "" {
		t.Fatalf("User-Agent = %q", seenUserAgent)
	}
	if seenCustom != "" {
		t.Fatalf("X-Custom = %q", seenCustom)
	}
}

func TestRawHonorsRequestTimeout(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Raw(context.Background(), http.MethodGet, "/slow", nil, WithTimeout(time.Millisecond))
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientHTTPVerbHelpers(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	seen := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.Method] = true
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("source"); got != "helper" {
			t.Errorf("source query = %q", got)
		}
		var body map[string]any
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
				t.Errorf("decode body: %v", err)
			}
		}
		if r.Method != http.MethodGet && body["ok"] != true {
			t.Errorf("%s body = %#v", r.Method, body)
		}
		_ = json.NewEncoder(w).Encode(JSON{"method": r.Method})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithDefaultQuery(map[string]any{"source": "helper"}))
	if err != nil {
		t.Fatal(err)
	}
	calls := []struct {
		method string
		call   func() (Response, error)
	}{
		{http.MethodGet, func() (Response, error) { return client.Get(context.Background(), "/low-level") }},
		{http.MethodPost, func() (Response, error) { return client.Post(context.Background(), "/low-level", JSON{"ok": true}) }},
		{http.MethodPatch, func() (Response, error) { return client.Patch(context.Background(), "/low-level", JSON{"ok": true}) }},
		{http.MethodPut, func() (Response, error) { return client.Put(context.Background(), "/low-level", JSON{"ok": true}) }},
		{http.MethodDelete, func() (Response, error) { return client.Delete(context.Background(), "/low-level", JSON{"ok": true}) }},
	}
	for _, call := range calls {
		out, err := call.call()
		if err != nil {
			t.Fatalf("%s: %v", call.method, err)
		}
		if out["method"] != call.method {
			t.Fatalf("%s response = %#v", call.method, out)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		if !seen[method] {
			t.Fatalf("did not see %s", method)
		}
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

func TestRetryAfterHeaderControlsRetryDelay(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("retry-after-ms", "1")
			http.Error(w, `{"error":"retry"}`, http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithMaxRetries(1))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, err := client.Get(context.Background(), "/retry")
	if err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("response = %#v", out)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
	if elapsed := time.Since(start); elapsed >= 250*time.Millisecond {
		t.Fatalf("retry-after-ms was not honored; elapsed = %s", elapsed)
	}
}

func TestRetryAfterHeaderIgnoresUnreasonableDelay(t *testing.T) {
	if delay, ok := parseRetryAfter(http.Header{"Retry-After": {"61"}}); ok || delay != 0 {
		t.Fatalf("parseRetryAfter = %s, %t", delay, ok)
	}
}

func TestRequestMaxRetriesOverridesClientDefault(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, `{"error":"retry"}`, http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithMaxRetries(2))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(context.Background(), "/retry", WithRequestMaxRetries(0))
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRequestMaxRetriesCanIncreaseClientDefault(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("retry-after-ms", "1")
			http.Error(w, `{"error":"retry"}`, http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Get(context.Background(), "/retry", WithRequestMaxRetries(1))
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if out["ok"] != true {
		t.Fatalf("response = %#v", out)
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

func TestWithExtraBodyMergesIntoJSONRequest(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if got := body["model"]; got != "override-model" {
			t.Errorf("model = %q", got)
		}
		if got := body["prompt"]; got != "prompt" {
			t.Errorf("prompt = %q", got)
		}
		if got := body["custom"]; got != "value" {
			t.Errorf("custom = %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{
			"id":      "cmpl-1",
			"created": 123,
			"model":   "override-model",
			"choices": []JSON{
				{"index": 0, "text": "ok"},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Completions.CreateTyped(
		context.Background(),
		fwtypes.CompletionCreateParams{Model: "base-model", Prompt: "prompt"},
		WithExtraBody(map[string]any{"model": "override-model", "custom": "value"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if out.Model != "override-model" {
		t.Fatalf("model = %q", out.Model)
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

func TestTypedManagementActionParity(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/accounts/acct/models/model-1:prepare":
			if got := r.Method; got != http.MethodPost {
				t.Errorf("prepare method = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode prepare body: %v", err)
			}
			if got := body["precision"]; got != "FP8" {
				t.Errorf("precision = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{"name": "model-1", "prepared": true})
		case "/v1/accounts/acct/datasets/ds-1:validateUpload":
			if got := r.Method; got != http.MethodPost {
				t.Errorf("validate method = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode validate body: %v", err)
			}
			if got := body["body"]; got != "dataset spec" {
				t.Errorf("body = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{"valid": true})
		case "/v1/accounts/acct/dpoJobs/dpo-1:getMetricsFileEndpoint":
			if got := r.Method; got != http.MethodGet {
				t.Errorf("metrics method = %q", got)
			}
			if got := r.URL.Query().Get("read_mask"); got != "signedUrl" {
				t.Errorf("read_mask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{"signedUrl": "gs://metrics.jsonl"})
		default:
			t.Errorf("unexpected path = %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithDefaultAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}

	prepared, err := client.Models.PrepareTyped(context.Background(), "model-1", fwtypes.ModelPrepareParams{Precision: "FP8"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared["prepared"] != true {
		t.Fatalf("prepared = %#v", prepared)
	}

	validated, err := client.Datasets.ValidateUploadTyped(context.Background(), "ds-1", fwtypes.DatasetValidateUploadParams{Body: "dataset spec"})
	if err != nil {
		t.Fatal(err)
	}
	if validated["valid"] != true {
		t.Fatalf("validated = %#v", validated)
	}

	metrics, err := client.DPOJobs.GetMetricsFileEndpointTyped(context.Background(), "dpo-1", map[string]any{"read_mask": "signedUrl"})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.DPOJobGetMetricsFileEndpointResponse = metrics
	if metrics.SignedURL == nil || *metrics.SignedURL != "gs://metrics.jsonl" {
		t.Fatalf("metrics = %#v", metrics)
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

func TestDatasetsUploadFileTypedUsesMultipart(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/accounts/acct/datasets/ds-1:upload" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "multipart/form-data; boundary=") {
			t.Errorf("Content-Type = %q", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		defer file.Close()
		payload, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read uploaded file: %v", err)
		}
		if header.Filename != "train.jsonl" {
			t.Errorf("filename = %q", header.Filename)
		}
		if string(payload) != "{\"prompt\":\"hi\"}\n" {
			t.Errorf("payload = %q", payload)
		}
		_ = json.NewEncoder(w).Encode(JSON{
			"id":       "file-1",
			"bytes":    len(payload),
			"filename": header.Filename,
			"object":   "file",
			"purpose":  "dataset",
		})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithDefaultAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Datasets.UploadTyped(
		context.Background(),
		"ds-1",
		fwtypes.DatasetUploadParams{
			File: NewFileFromBytes("train.jsonl", []byte("{\"prompt\":\"hi\"}\n")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.DatasetUploadResponse = out
	if out.ID == nil || *out.ID != "file-1" || out.Filename == nil || *out.Filename != "train.jsonl" {
		t.Fatalf("response = %#v", out)
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

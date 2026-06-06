package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fwtypes "github.com/ZaguanLabs/fireworks-sdk-go/types"
)

func TestVersionMatchesPythonSDK(t *testing.T) {
	if Version != "1.2.0-alpha.76" {
		t.Fatalf("Version = %q, want %q", Version, "1.2.0-alpha.76")
	}
}

func TestDefaultConstantsMatchPythonSDK(t *testing.T) {
	if DefaultTimeout != time.Minute {
		t.Fatalf("DefaultTimeout = %s", DefaultTimeout)
	}
	if DefaultConnectTimeout != 5*time.Second {
		t.Fatalf("DefaultConnectTimeout = %s", DefaultConnectTimeout)
	}
	if DefaultMaxRetries != 2 {
		t.Fatalf("DefaultMaxRetries = %d", DefaultMaxRetries)
	}
	if DefaultMaxConnectionsPerHost != 1000 {
		t.Fatalf("DefaultMaxConnectionsPerHost = %d", DefaultMaxConnectionsPerHost)
	}
	if DefaultMaxIdleConnectionsPerHost != 20 {
		t.Fatalf("DefaultMaxIdleConnectionsPerHost = %d", DefaultMaxIdleConnectionsPerHost)
	}
}

func TestDefaultHTTPClientFactoriesUseSDKDefaults(t *testing.T) {
	client := DefaultHTTPClient()
	if client.Timeout != DefaultTimeout {
		t.Fatalf("Timeout = %s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", client.Transport)
	}
	if transport.MaxConnsPerHost != DefaultMaxConnectionsPerHost {
		t.Fatalf("MaxConnsPerHost = %d", transport.MaxConnsPerHost)
	}
	if transport.MaxIdleConnsPerHost != DefaultMaxIdleConnectionsPerHost {
		t.Fatalf("MaxIdleConnsPerHost = %d", transport.MaxIdleConnsPerHost)
	}

	custom := DefaultHTTPClientWithTimeout(2 * time.Second)
	if custom.Timeout != 2*time.Second {
		t.Fatalf("custom Timeout = %s", custom.Timeout)
	}
}

func TestFireworksAliasMatchesClient(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	var fireworks *Fireworks = client
	if fireworks.APIKey() != "test-key" {
		t.Fatalf("api key = %q", fireworks.APIKey())
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
		if got := r.Header.Get("X-Stainless-Read-Timeout"); got != "60" {
			t.Errorf("X-Stainless-Read-Timeout = %q", got)
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

func TestResourceMethodsRejectMissingPathArguments(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithDefaultAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	calls := []struct {
		name string
		call func() error
	}{
		{"accounts get", func() error {
			_, err := client.Accounts.Get(context.Background(), "")
			return err
		}},
		{"accounts get typed", func() error {
			_, err := client.Accounts.GetTyped(context.Background(), "")
			return err
		}},
		{"models get", func() error {
			_, err := client.Models.Get(context.Background(), "")
			return err
		}},
		{"models get typed", func() error {
			_, err := client.Models.GetTyped(context.Background(), "")
			return err
		}},
		{"models prepare typed", func() error {
			_, err := client.Models.PrepareTyped(context.Background(), "", JSON{})
			return err
		}},
		{"deployment shape versions list", func() error {
			_, err := client.DeploymentShapeVersions.List(context.Background(), "", nil)
			return err
		}},
		{"deployment shape versions get typed", func() error {
			_, err := client.DeploymentShapeVersions.GetTyped(context.Background(), "shape-1", "")
			return err
		}},
	}
	for _, call := range calls {
		err := call.call()
		if err == nil {
			t.Fatalf("%s: expected error", call.name)
		}
		if !strings.Contains(err.Error(), "missing required path argument") {
			t.Fatalf("%s: error = %v", call.name, err)
		}
	}
	if requests != 0 {
		t.Fatalf("sent %d unexpected requests", requests)
	}
}

func TestQueryEncodingUsesCommaForArbitrarySlicesAndArrays(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if got := query.Get("float32s"); got != "1.25,2.5" {
			t.Errorf("float32s = %q", got)
		}
		if got := query.Get("uints"); got != "7,8" {
			t.Errorf("uints = %q", got)
		}
		if got := query.Get("array"); got != "a,b" {
			t.Errorf("array = %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Get(
		context.Background(),
		"/query",
		WithQueryParam("float32s", []float32{1.25, 2.5}),
		WithQueryParam("uints", []uint{7, 8}),
		WithQueryParam("array", [2]string{"a", "b"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
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

func TestWithOptionsCanReplaceDefaultHeadersAndQuery(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Original"); got != "" {
			t.Errorf("X-Original = %q", got)
		}
		if got := r.Header.Get("X-Replaced"); got != "yes" {
			t.Errorf("X-Replaced = %q", got)
		}
		if got := r.URL.Query().Get("original"); got != "" {
			t.Errorf("original query = %q", got)
		}
		if got := r.URL.Query().Get("replaced"); got != "yes" {
			t.Errorf("replaced query = %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(
		WithBaseURL(server.URL),
		WithDefaultHeader("X-Original", "yes"),
		WithDefaultQuery(map[string]any{"original": "yes"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := client.WithOptions(
		WithSetDefaultHeaders(map[string]string{"X-Replaced": "yes"}),
		WithSetDefaultQuery(map[string]any{"replaced": "yes"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	out, err := clone.Get(context.Background(), "/replace")
	if err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("response = %#v", out)
	}
	if client.defaultHeaders.Get("X-Original") != "yes" || client.defaultQuery.Get("original") != "yes" {
		t.Fatalf("original client defaults were mutated")
	}
}

func TestWithOptionsCanReplaceDefaultTimeout(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	client, err := NewClient(WithDefaultTimeout(3 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	clone, err := client.WithOptions(WithDefaultTimeout(1500 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout() != 3*time.Second {
		t.Fatalf("original timeout = %s", client.Timeout())
	}
	if clone.Timeout() != 1500*time.Millisecond {
		t.Fatalf("clone timeout = %s", clone.Timeout())
	}
	if clone.httpClient.Timeout != 1500*time.Millisecond {
		t.Fatalf("http client timeout = %s", clone.httpClient.Timeout)
	}
}

func TestSetDefaultHeadersReplacesEnvironmentCustomHeaders(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")
	t.Setenv("FIREWORKS_CUSTOM_HEADERS", "X-From-Env: yes")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-From-Env"); got != "" {
			t.Errorf("X-From-Env = %q", got)
		}
		if got := r.Header.Get("X-Replaced"); got != "yes" {
			t.Errorf("X-Replaced = %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(
		WithBaseURL(server.URL),
		WithSetDefaultHeaders(map[string]string{"X-Replaced": "yes"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Get(context.Background(), "/replace")
	if err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("response = %#v", out)
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

func TestReadTimeoutHeaderUsesRequestTimeoutAndCanBeOmitted(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Stainless-Read-Timeout"))
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/timeout", WithTimeout(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/timeout", WithOmitHeader("X-Stainless-Read-Timeout")); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("seen = %#v", seen)
	}
	if seen[0] != "1.5" {
		t.Fatalf("request timeout header = %q", seen[0])
	}
	if seen[1] != "" {
		t.Fatalf("omitted timeout header = %q", seen[1])
	}
}

func TestReadTimeoutHeaderUsesClientDefaultTimeout(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Stainless-Read-Timeout")
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithDefaultTimeout(2500*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/timeout"); err != nil {
		t.Fatal(err)
	}
	if seen != "2.5" {
		t.Fatalf("timeout header = %q", seen)
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
	var timeoutErr *APITimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error type = %T, want APITimeoutError", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientDefaultTimeoutIsAppliedByDefaultHTTPClient(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithDefaultTimeout(time.Millisecond), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(context.Background(), "/slow")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	var timeoutErr *APITimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error type = %T, want APITimeoutError", err)
	}
}

func TestConnectionErrorsAreWrapped(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	client, err := NewClient(WithBaseURL("http://127.0.0.1:1"), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(context.Background(), "/unreachable")
	if err == nil {
		t.Fatal("expected error")
	}
	var connErr *APIConnectionError
	if !errors.As(err, &connErr) {
		t.Fatalf("error type = %T, want APIConnectionError", err)
	}
	if connErr.Request == nil || connErr.Request.URL.Path != "/unreachable" {
		t.Fatalf("request = %#v", connErr.Request)
	}
	if errors.Unwrap(connErr) == nil {
		t.Fatal("expected wrapped transport error")
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

func TestClientHTTPByteContentHelpers(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	attempts := 0
	var seenBody []byte
	var seenContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		seenBody = body
		seenContentType = r.Header.Get("Content-Type")
		if attempts == 1 {
			w.Header().Set("retry-after-ms", "1")
			http.Error(w, `{"error":"retry"}`, http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(JSON{"size": len(body), "method": r.Method})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithMaxRetries(1))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.PostBytes(context.Background(), "/bytes", []byte("raw bytes"), WithHeader("Content-Type", "text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
	if string(seenBody) != "raw bytes" {
		t.Fatalf("body = %q", seenBody)
	}
	if seenContentType != "text/plain" {
		t.Fatalf("Content-Type = %q", seenContentType)
	}
	if out["size"].(float64) != 9 {
		t.Fatalf("response = %#v", out)
	}
}

func TestClientRawAndTextResponseHelpers(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("retry-after-ms", "1")
			http.Error(w, `{"error":"retry"}`, http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("plain response"))
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithMaxRetries(1))
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.GetText(context.Background(), "/plain")
	if err != nil {
		t.Fatal(err)
	}
	if text != "plain response" {
		t.Fatalf("text = %q", text)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}

	raw, err := client.GetRaw(context.Background(), "/plain", WithRequestMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "plain response" {
		t.Fatalf("raw = %q", raw)
	}
}

func TestClientAPIResponseHelperExposesMetadataAndParsing(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("retry-after-ms", "1")
			http.Error(w, `{"error":"retry"}`, http.StatusTooManyRequests)
			return
		}
		if got := r.URL.Query().Get("source"); got != "response" {
			t.Errorf("source query = %q", got)
		}
		w.Header().Set("X-Request-ID", "req-123")
		w.Header().Set("X-Custom", "value")
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithMaxRetries(1))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.GetResponse(context.Background(), "/metadata", WithQueryParam("source", "response"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Request == nil || resp.Request.Method != http.MethodGet {
		t.Fatalf("request = %#v", resp.Request)
	}
	if resp.RequestID != "req-123" {
		t.Fatalf("request id = %q", resp.RequestID)
	}
	if resp.Header.Get("X-Custom") != "value" {
		t.Fatalf("header = %q", resp.Header.Get("X-Custom"))
	}
	if resp.RetriesTaken != 1 {
		t.Fatalf("retries taken = %d", resp.RetriesTaken)
	}
	if resp.Text() == "" {
		t.Fatal("expected response text")
	}
	parsed, err := resp.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if parsed.(map[string]any)["ok"] != true {
		t.Fatalf("json = %#v", parsed)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := resp.ParseJSON(&out); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("parsed struct = %#v", out)
	}

	copied := resp.Bytes()
	if len(resp.Body) == 0 {
		t.Fatal("missing body")
	}
	copied[0] = 'X'
	if resp.Body[0] == 'X' {
		t.Fatal("Bytes returned the internal body slice")
	}
}

func TestAPIResponseValidationErrorForInvalidSuccessBody(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{invalid"))
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(context.Background(), "/invalid")
	if err == nil {
		t.Fatal("expected error")
	}
	var validationErr *APIResponseValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error type = %T", err)
	}
	if string(validationErr.Body) != "{invalid" {
		t.Fatalf("body = %q", validationErr.Body)
	}
}

func TestAPIResponseParsingUsesValidationError(t *testing.T) {
	resp := &APIResponse{Body: []byte("{invalid")}

	if _, err := resp.JSON(); err == nil {
		t.Fatal("expected JSON error")
	} else {
		var validationErr *APIResponseValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("JSON error type = %T", err)
		}
	}

	var out map[string]any
	err := resp.ParseJSON(&out)
	if err == nil {
		t.Fatal("expected ParseJSON error")
	}
	var validationErr *APIResponseValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("ParseJSON error type = %T", err)
	}
	if string(validationErr.Body) != "{invalid" {
		t.Fatalf("body = %q", validationErr.Body)
	}
}

func TestClientRawByteContentResponseHelper(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	var seenBody string
	var seenContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		seenBody = string(body)
		seenContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("accepted"))
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.PostBytesText(context.Background(), "/bytes", []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if text != "accepted" {
		t.Fatalf("text = %q", text)
	}
	if seenBody != "payload" {
		t.Fatalf("body = %q", seenBody)
	}
	if seenContentType != "application/octet-stream" {
		t.Fatalf("Content-Type = %q", seenContentType)
	}
}

func TestClientAPIResponseByteContentHelper(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	var seenBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		seenBody = string(body)
		w.Header().Set("X-Request-ID", "req-bytes")
		_, _ = w.Write([]byte("accepted"))
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.PostBytesResponse(context.Background(), "/bytes", []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if seenBody != "payload" {
		t.Fatalf("body = %q", seenBody)
	}
	if resp.RequestID != "req-bytes" {
		t.Fatalf("request id = %q", resp.RequestID)
	}
	if resp.Text() != "accepted" {
		t.Fatalf("text = %q", resp.Text())
	}
}

func TestNewFileFromPathUsesBasenameAndBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "train.jsonl")
	if err := os.WriteFile(path, []byte("{\"prompt\":\"hi\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	file, err := NewFileFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if file.Filename != "train.jsonl" {
		t.Fatalf("filename = %q", file.Filename)
	}
	content, err := io.ReadAll(file.Content)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "{\"prompt\":\"hi\"}\n" {
		t.Fatalf("content = %q", content)
	}

	alias, err := FileFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if alias.Filename != file.Filename {
		t.Fatalf("alias filename = %q", alias.Filename)
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
	body, ok := rateLimit.BodyJSON.(map[string]any)
	if !ok || body["error"] != "limited" {
		t.Fatalf("BodyJSON = %#v", rateLimit.BodyJSON)
	}
}

func TestStatusErrorKeepsRawNonJSONBody(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "plain error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(context.Background(), "/plain")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *InternalServerError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.BodyJSON != nil {
		t.Fatalf("BodyJSON = %#v", apiErr.BodyJSON)
	}
	if !strings.Contains(string(apiErr.Body), "plain error") {
		t.Fatalf("Body = %q", apiErr.Body)
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

func TestStreamHandlesPythonSSEDecoderSemantics(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": keepalive\n"))
		_, _ = w.Write([]byte("event: ping\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"skip\"}\n\n"))
		_, _ = w.Write([]byte("event: completion\n"))
		_, _ = w.Write([]byte("retry: 1000\n"))
		_, _ = w.Write([]byte("data: {\"id\":\n"))
		_, _ = w.Write([]byte("data: \"chunk-2\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE] trailing metadata\n\n"))
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
		t.Fatalf("expected completion chunk, err=%v", stream.Err())
	}
	if got := stream.Current()["id"]; got != "chunk-2" {
		t.Fatalf("chunk id = %q", got)
	}
	if raw := string(stream.RawCurrent()); raw != "{\"id\":\n\"chunk-2\"}" {
		t.Fatalf("raw chunk = %q", raw)
	}
	if stream.Next() {
		t.Fatal("expected stream to stop at [DONE] prefix")
	}
	if err := stream.Err(); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("stream err = %v", err)
	}
}

func TestStreamStopsAtMessageStopEvent(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"unexpected\"}\n\n"))
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Completions.CreateStream(context.Background(), JSON{"stream": true})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	if stream.Next() {
		t.Fatalf("unexpected chunk = %#v", stream.Current())
	}
	if err := stream.Err(); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("stream err = %v", err)
	}
}

func TestStreamExposesRawAndNonObjectJSONEvents(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [\"a\",\"b\"]\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Completions.CreateStream(context.Background(), JSON{"stream": true})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	if !stream.Next() {
		t.Fatalf("expected first chunk, err=%v", stream.Err())
	}
	if stream.Current() != nil {
		t.Fatalf("Current = %#v, want nil for non-object JSON", stream.Current())
	}
	values, ok := stream.CurrentJSON().([]any)
	if !ok || len(values) != 2 || values[0] != "a" || values[1] != "b" {
		t.Fatalf("CurrentJSON = %#v", stream.CurrentJSON())
	}
	raw := stream.RawCurrent()
	raw[0] = '{'
	if string(stream.RawCurrent()) != "[\"a\",\"b\"]" {
		t.Fatalf("RawCurrent was not copied: %q", stream.RawCurrent())
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

func TestTypedListParamsUsePythonQueryAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/accounts/acct-from-params/models" {
			t.Errorf("path = %q", got)
		}
		query := r.URL.Query()
		if got := query.Get("pageToken"); got != "cursor-1" {
			t.Errorf("pageToken = %q", got)
		}
		if got := query.Get("pageSize"); got != "25" {
			t.Errorf("pageSize = %q", got)
		}
		if got := query.Get("orderBy"); got != "name desc" {
			t.Errorf("orderBy = %q", got)
		}
		if got := query.Get("readMask"); got != "name,displayName" {
			t.Errorf("readMask = %q", got)
		}
		if got := query.Get("account_id"); got != "" {
			t.Errorf("account_id should not be query param, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"models": []JSON{}})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Models.ListTyped(context.Background(), fwtypes.ModelListParams{
		AccountID: "acct-from-params",
		PageToken: "cursor-1",
		PageSize:  25,
		OrderBy:   "name desc",
		ReadMask:  []string{"name", "displayName"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGenericListMapUsesPythonQueryAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/accounts/acct-from-map/models" {
			t.Errorf("path = %q", got)
		}
		query := r.URL.Query()
		if got := query.Get("pageToken"); got != "cursor-1" {
			t.Errorf("pageToken = %q", got)
		}
		if got := query.Get("pageSize"); got != "25" {
			t.Errorf("pageSize = %q", got)
		}
		if got := query.Get("orderBy"); got != "name desc" {
			t.Errorf("orderBy = %q", got)
		}
		if got := query.Get("readMask"); got != "name,displayName" {
			t.Errorf("readMask = %q", got)
		}
		if got := query.Get("account_id"); got != "" {
			t.Errorf("account_id should not be query param, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"models": []JSON{}})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Models.List(context.Background(), map[string]any{
		"account_id": "acct-from-map",
		"page_token": "cursor-1",
		"page_size":  25,
		"order_by":   "name desc",
		"read_mask":  []string{"name", "displayName"},
	})
	if err != nil {
		t.Fatal(err)
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
			if got := r.URL.RawQuery; got != "" {
				t.Errorf("metrics query = %q", got)
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

	metrics, err := client.DPOJobs.GetMetricsFileEndpointTyped(context.Background(), "dpo-1")
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
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if got := body["replicaCount"]; got != float64(3) {
			t.Errorf("replicaCount = %#v", got)
		}
		if _, ok := body["replica_count"]; ok {
			t.Errorf("unexpected replica_count body key: %#v", body)
		}
		if _, ok := body["account_id"]; ok {
			t.Errorf("unexpected account_id body key: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(JSON{
			"baseModel":           "accounts/fireworks/models/test",
			"name":                "accounts/acct/deployments/dep-1",
			"desiredReplicaCount": 3,
		})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := client.Deployments.ScaleTyped(context.Background(), "dep-1", fwtypes.DeploymentScaleParams{
		AccountID:    "acct",
		ReplicaCount: 3,
	})
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

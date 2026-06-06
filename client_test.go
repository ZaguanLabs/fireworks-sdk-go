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
	if Title != "fireworks" {
		t.Fatalf("Title = %q, want %q", Title, "fireworks")
	}
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

func TestDefaultHeadersCanOverridePlatformHeaders(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	var seenLang string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenLang = r.Header.Get("X-Stainless-Lang")
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(
		WithBaseURL(server.URL),
		WithDefaultHeader("X-Stainless-Lang", "my-overriding-header"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/headers"); err != nil {
		t.Fatal(err)
	}
	if seenLang != "my-overriding-header" {
		t.Fatalf("X-Stainless-Lang = %q", seenLang)
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
			_, err := client.Accounts.GetTyped(context.Background(), "", nil)
			return err
		}},
		{"models get", func() error {
			_, err := client.Models.Get(context.Background(), "")
			return err
		}},
		{"models get typed", func() error {
			_, err := client.Models.GetTyped(context.Background(), "", nil)
			return err
		}},
		{"models prepare typed", func() error {
			_, err := client.Models.PrepareTyped(context.Background(), "", JSON{})
			return err
		}},
		{"users get", func() error {
			_, err := client.Users.Get(context.Background(), "")
			return err
		}},
		{"users get typed", func() error {
			_, err := client.Users.GetTyped(context.Background(), "", nil)
			return err
		}},
		{"users update", func() error {
			_, err := client.Users.Update(context.Background(), "", JSON{"role": "user"})
			return err
		}},
		{"users update typed", func() error {
			_, err := client.Users.UpdateTyped(context.Background(), "", fwtypes.UserUpdateParams{Role: "user"})
			return err
		}},
		{"api keys create", func() error {
			_, err := client.APIKeys.Create(context.Background(), "", JSON{})
			return err
		}},
		{"api keys create typed", func() error {
			_, err := client.APIKeys.CreateTyped(context.Background(), "", fwtypes.APIKeyCreateParams{})
			return err
		}},
		{"api keys list", func() error {
			_, err := client.APIKeys.List(context.Background(), "", nil)
			return err
		}},
		{"api keys list typed", func() error {
			_, err := client.APIKeys.ListTyped(context.Background(), "", nil)
			return err
		}},
		{"api keys delete", func() error {
			_, err := client.APIKeys.Delete(context.Background(), "", JSON{})
			return err
		}},
		{"api keys delete typed", func() error {
			_, err := client.APIKeys.DeleteTyped(context.Background(), "", fwtypes.APIKeyDeleteParams{})
			return err
		}},
		{"deployment shape versions list", func() error {
			_, err := client.DeploymentShapeVersions.List(context.Background(), "", nil)
			return err
		}},
		{"deployment shape versions get typed", func() error {
			_, err := client.DeploymentShapeVersions.GetTyped(context.Background(), "shape-1", "", nil)
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
		if got := query.Get("nested[enabled]"); got != "true" {
			t.Errorf("nested[enabled] = %q", got)
		}
		if got := query.Get("nested[inner][value]"); got != "x" {
			t.Errorf("nested[inner][value] = %q", got)
		}
		if got := query.Get("maybe"); got != "a,true" {
			t.Errorf("maybe = %q", got)
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
		WithQueryParam("nested", map[string]any{
			"enabled": true,
			"inner":   map[string]any{"value": "x"},
		}),
		WithQueryParam("maybe", []any{"a", nil, true}),
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

func TestBaseURLJoiningMatchesPythonClient(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	for _, baseURL := range []string{"http://localhost:5000/custom/path/", "http://localhost:5000/custom/path"} {
		client, err := NewClient(WithBaseURL(baseURL))
		if err != nil {
			t.Fatal(err)
		}

		req, err := client.NewRequest(context.Background(), http.MethodPost, "/foo", JSON{"foo": "bar"})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := req.URL.String(), "http://localhost:5000/custom/path/foo"; got != want {
			t.Fatalf("base %q url = %q, want %q", baseURL, got, want)
		}

		req, err = client.NewRequest(
			context.Background(),
			http.MethodGet,
			"/files/a%2Fb?beta=true",
			nil,
			WithQuery(map[string]any{"limit": "10"}),
		)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := req.URL.String(), "http://localhost:5000/custom/path/files/a%2Fb?beta=true&limit=10"; got != want {
			t.Fatalf("base %q url = %q, want %q", baseURL, got, want)
		}

		req, err = client.NewRequest(context.Background(), http.MethodPost, "https://myapi.com/foo", JSON{"foo": "bar"})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := req.URL.String(), "https://myapi.com/foo"; got != want {
			t.Fatalf("absolute url = %q, want %q", got, want)
		}
	}
}

func TestBaseURLAccessorMatchesPythonTrailingSlash(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	client, err := NewClient(WithBaseURL("https://example.com/from_init"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := client.BaseURL(), "https://example.com/from_init/"; got != want {
		t.Fatalf("BaseURL = %q, want %q", got, want)
	}
}

func TestBaseURLAccessorUsesEnvironmentWithTrailingSlash(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")
	t.Setenv("FIREWORKS_BASE_URL", "http://localhost:5000/from/env")

	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := client.BaseURL(), "http://localhost:5000/from/env/"; got != want {
		t.Fatalf("BaseURL = %q, want %q", got, want)
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

func TestClientCloseIsIdempotentAndPreventsRequests(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if client.IsClosed() {
		t.Fatal("new client is closed")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if !client.IsClosed() {
		t.Fatal("client is not closed")
	}
	_, err = client.Get(context.Background(), "/closed")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "client is closed") {
		t.Fatalf("error = %v", err)
	}
}

func TestWithOptionsSharesClientLifecycleWhenHTTPClientIsShared(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	clone, err := client.WithOptions(WithDefaultHeader("X-Clone", "yes"))
	if err != nil {
		t.Fatal(err)
	}
	if clone.IsClosed() || client.IsClosed() {
		t.Fatal("clients unexpectedly closed")
	}
	if err := clone.Close(); err != nil {
		t.Fatal(err)
	}
	if !clone.IsClosed() || !client.IsClosed() {
		t.Fatalf("closed state was not shared: clone=%t client=%t", clone.IsClosed(), client.IsClosed())
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

func TestGetRequestDropsBodyAndContentType(t *testing.T) {
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
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	var out Response
	err = client.Request(
		context.Background(),
		http.MethodGet,
		"/get-with-body",
		JSON{"ignored": true},
		&out,
		WithHeader("Content-Type", "application/json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if seenBody != "" {
		t.Fatalf("body = %q", seenBody)
	}
	if seenContentType != "" {
		t.Fatalf("Content-Type = %q", seenContentType)
	}
	if out["ok"] != true {
		t.Fatalf("response = %#v", out)
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
	if resp.Method() != http.MethodGet {
		t.Fatalf("method = %q", resp.Method())
	}
	if !strings.HasPrefix(resp.URL(), server.URL+"/metadata?") {
		t.Fatalf("url = %q", resp.URL())
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
		w.Header().Set("X-Request-ID", "req_invalid")
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
	if validationErr.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d", validationErr.StatusCode)
	}
	if validationErr.Request == nil || validationErr.Request.URL.Path != "/invalid" {
		t.Fatalf("request = %#v", validationErr.Request)
	}
	if validationErr.Response == nil || validationErr.Response.RequestID != "req_invalid" {
		t.Fatalf("response = %#v", validationErr.Response)
	}
	if got := validationErr.Header.Get("X-Request-ID"); got != "req_invalid" {
		t.Fatalf("header X-Request-ID = %q", got)
	}
}

func TestAPIResponseParsingUsesValidationError(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.test/invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp := &APIResponse{
		Request:    req,
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"X-Request-ID": []string{"req_parse"}},
		Body:       []byte("{invalid"),
		RequestID:  "req_parse",
	}

	if _, err := resp.JSON(); err == nil {
		t.Fatal("expected JSON error")
	} else {
		var validationErr *APIResponseValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("JSON error type = %T", err)
		}
		if validationErr.Response != resp || validationErr.Request != req {
			t.Fatalf("validation response/request = %#v / %#v", validationErr.Response, validationErr.Request)
		}
	}

	var out map[string]any
	err = resp.ParseJSON(&out)
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

func TestMultipartFieldsUsePythonBracketSerialization(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "multipart/form-data; boundary=") {
			t.Errorf("Content-Type = %q", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		values := r.MultipartForm.Value
		if got := values["meta[enabled]"]; len(got) != 1 || got[0] != "true" {
			t.Errorf("meta[enabled] = %#v", got)
		}
		if got := values["meta[tags][]"]; len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Errorf("meta[tags][] = %#v", got)
		}
		if got := values["ids[]"]; len(got) != 2 || got[0] != "1" || got[1] != "2" {
			t.Errorf("ids[] = %#v", got)
		}
		if got := values["raw"]; len(got) != 1 || got[0] != "abc" {
			t.Errorf("raw = %#v", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	var out Response
	err = client.MultipartRequest(
		context.Background(),
		http.MethodPost,
		"/multipart",
		map[string]any{
			"ids": []int{1, 2},
			"meta": map[string]any{
				"enabled": true,
				"tags":    []any{"a", nil, "b"},
			},
			"raw": []byte("abc"),
		},
		nil,
		&out,
	)
	if err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("response = %#v", out)
	}
}

func TestMultipartRequestHonorsExplicitContentTypeBoundary(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	const boundary = "6b7ba517decee4a450543ea6ae821c82"
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "multipart/form-data; boundary="+boundary {
			t.Errorf("Content-Type = %q", got)
		}
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	var out Response
	err = client.MultipartRequest(
		context.Background(),
		http.MethodPost,
		"/multipart",
		map[string]any{"array": []string{"foo", "bar"}},
		map[string]File{"foo.txt": NewFileFromBytes("upload", []byte("hello world"))},
		&out,
		WithHeader("Content-Type", "multipart/form-data; boundary="+boundary),
	)
	if err != nil {
		t.Fatal(err)
	}
	bodyText := string(body)
	if !strings.HasPrefix(bodyText, "--"+boundary+"\r\n") {
		t.Fatalf("body starts with %q, want boundary %q", bodyText, boundary)
	}
	if !strings.Contains(bodyText, `name="array[]"`) {
		t.Fatalf("body does not contain array field: %q", bodyText)
	}
	if !strings.Contains(bodyText, `name="foo.txt"; filename="upload"`) {
		t.Fatalf("body does not contain file field: %q", bodyText)
	}
}

func TestMultipartRequestReplacesBareMultipartContentType(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "multipart/form-data; boundary=") {
			t.Fatalf("Content-Type = %q", contentType)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if got := r.MultipartForm.Value["name"]; len(got) != 1 || got[0] != "value" {
			t.Fatalf("name = %#v", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	var out Response
	err = client.MultipartRequest(
		context.Background(),
		http.MethodPost,
		"/multipart",
		map[string]any{"name": "value"},
		nil,
		&out,
		WithHeader("Content-Type", "multipart/form-data"),
	)
	if err != nil {
		t.Fatal(err)
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
	var statusErr *APIStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error type = %T, want APIStatusError", err)
	}
	if statusErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status code = %d", statusErr.StatusCode)
	}
	var baseErr *APIError
	if !errors.As(err, &baseErr) {
		t.Fatalf("error type = %T, want APIError", err)
	}
	if baseErr.Request == nil || baseErr.Request.URL.Path != "/v1/completions" {
		t.Fatalf("request = %#v", baseErr.Request)
	}
	body, ok := rateLimit.BodyJSON.(map[string]any)
	if !ok || body["error"] != "limited" {
		t.Fatalf("BodyJSON = %#v", rateLimit.BodyJSON)
	}
}

func TestGenericStatusErrorMapsToAPIStatusError(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"teapot"}`, http.StatusTeapot)
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(context.Background(), "/teapot")
	if err == nil {
		t.Fatal("expected error")
	}
	var statusErr *APIStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error type = %T, want APIStatusError", err)
	}
	if statusErr.StatusCode != http.StatusTeapot {
		t.Fatalf("status code = %d", statusErr.StatusCode)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want APIError", err)
	}
	if !apiErr.IsStatus(http.StatusTeapot) {
		t.Fatalf("IsStatus(%d) = false", http.StatusTeapot)
	}
	if apiErr.Request == nil || apiErr.Request.URL.Path != "/teapot" {
		t.Fatalf("request = %#v", apiErr.Request)
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

func TestRetryAfterHeaderAcceptsHTTPDate(t *testing.T) {
	retryAt := time.Now().Add(5 * time.Second).UTC()
	delay, ok := parseRetryAfter(http.Header{"Retry-After": {retryAt.Format(http.TimeFormat)}})
	if !ok {
		t.Fatal("expected retry-after date to be accepted")
	}
	if delay < 3*time.Second || delay > 5*time.Second {
		t.Fatalf("delay = %s", delay)
	}
}

func TestRetryAfterHeaderIgnoresPastHTTPDate(t *testing.T) {
	retryAt := time.Now().Add(-time.Second).UTC()
	if delay, ok := parseRetryAfter(http.Header{"Retry-After": {retryAt.Format(http.TimeFormat)}}); ok || delay != 0 {
		t.Fatalf("parseRetryAfter = %s, %t", delay, ok)
	}
}

func TestRequestsFollowRedirectsByDefault(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/redirected", http.StatusFound)
		case "/redirected":
			_ = json.NewEncoder(w).Encode(JSON{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Post(context.Background(), "/redirect", JSON{"key": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if out["status"] != "ok" {
		t.Fatalf("response = %#v", out)
	}
}

func TestRequestsCanDisableRedirectFollowing(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			w.Header().Set("Location", "/redirected")
			w.WriteHeader(http.StatusFound)
		case "/redirected":
			_ = json.NewEncoder(w).Encode(JSON{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Post(context.Background(), "/redirect", JSON{"key": "value"}, WithFollowRedirects(false))
	if err == nil {
		t.Fatal("expected error")
	}
	var statusErr *APIStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error type = %T, want APIStatusError", err)
	}
	if statusErr.StatusCode != http.StatusFound {
		t.Fatalf("status code = %d", statusErr.StatusCode)
	}
	if statusErr.Header.Get("Location") != "/redirected" {
		t.Fatalf("Location = %q", statusErr.Header.Get("Location"))
	}
}

func TestRetryCountHeaderCanBeOverriddenOrOmitted(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	var seen [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Values("X-Stainless-Retry-Count"))
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/default"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/override", WithHeader("X-Stainless-Retry-Count", "42")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background(), "/omit", WithOmitHeader("X-Stainless-Retry-Count")); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 3 {
		t.Fatalf("seen = %#v", seen)
	}
	if got := seen[0]; len(got) != 1 || got[0] != "0" {
		t.Fatalf("default retry count = %#v", got)
	}
	if got := seen[1]; len(got) != 1 || got[0] != "42" {
		t.Fatalf("overridden retry count = %#v", got)
	}
	if got := seen[2]; len(got) != 0 {
		t.Fatalf("omitted retry count = %#v", got)
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

func TestChatCompletionCreateTypedPreservesAllPythonOptionalParams(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(JSON{
			"id":      "chatcmpl-1",
			"created": 123,
			"model":   "model",
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
		})
	}))
	defer server.Close()

	functionCall := fwtypes.ChatCompletionCreateParamsFunctionCall("auto")
	prediction := fwtypes.ChatCompletionCreateParamsPrediction(map[string]any{
		"content": "string",
		"type":    "content",
	})
	thinking := fwtypes.ChatCompletionCreateParamsThinking(map[string]any{
		"type":          "enabled",
		"budget_tokens": 0,
	})
	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Chat.Completions.CreateTyped(context.Background(), fwtypes.ChatCompletionCreateParams{
		Messages: []fwtypes.SharedParamsChatMessage{
			{
				Role:             "role",
				Content:          "string",
				ReasoningContent: testStringPtr("reasoning_content"),
				ToolCallID:       testStringPtr("tool_call_id"),
				ToolCalls: []fwtypes.SharedParamsChatCompletionMessageToolCall{
					{
						Function: map[string]any{
							"arguments": "string",
							"name":      "name",
						},
						ID:   testStringPtr("id"),
						Type: "type",
					},
				},
			},
		},
		Model:                         "model",
		ContextLengthExceededBehavior: "error",
		Echo:                          testBoolPtr(true),
		EchoLast:                      testIntPtr(0),
		FrequencyPenalty:              testFloatPtr(0),
		FunctionCall:                  &functionCall,
		Functions: []fwtypes.ChatCompletionCreateParamsFunction{
			{
				Name:        "name",
				Description: testStringPtr("description"),
				Parameters:  map[string]any{"foo": "bar"},
				Strict:      testBoolPtr(true),
			},
		},
		IgnoreEos:               true,
		LogitBias:               map[string]float64{"foo": 0},
		Logprobs:                0,
		MaxCompletionTokens:     testIntPtr(0),
		MaxTokens:               testIntPtr(0),
		Metadata:                map[string]string{"foo": "string"},
		MinP:                    testFloatPtr(0),
		MirostatLr:              testFloatPtr(0),
		MirostatTarget:          testFloatPtr(0),
		N:                       0,
		ParallelToolCalls:       testBoolPtr(true),
		PerfMetricsInResponse:   testBoolPtr(true),
		Prediction:              &prediction,
		PresencePenalty:         testFloatPtr(0),
		PromptCacheIsolationKey: testStringPtr("prompt_cache_isolation_key"),
		PromptCacheKey:          testStringPtr("prompt_cache_key"),
		PromptTruncateLen:       testIntPtr(0),
		RawOutput:               testBoolPtr(true),
		ReasoningEffort:         "low",
		ReasoningHistory:        testStringPtr("disabled"),
		RepetitionPenalty:       testFloatPtr(0),
		ResponseFormat: &fwtypes.ChatCompletionCreateParamsResponseFormat{
			Type:       "json_object",
			Grammar:    testStringPtr("grammar"),
			JsonSchema: "string",
			Schema:     "string",
		},
		ReturnTokenIds:   testBoolPtr(true),
		SafeTokenization: testBoolPtr(true),
		Seed:             testIntPtr(0),
		ServiceTier:      "auto",
		Speculation:      "string",
		Stop:             "string",
		Temperature:      testFloatPtr(0),
		Thinking:         &thinking,
		ToolChoice:       "auto",
		Tools: []fwtypes.SharedParamsChatCompletionTool{
			{
				Type: "function",
				Function: &fwtypes.SharedParamsChatCompletionToolFunction{
					Name:        "name",
					Description: testStringPtr("description"),
					Parameters:  map[string]any{"foo": "bar"},
					Strict:      testBoolPtr(true),
				},
			},
		},
		TopK:        testIntPtr(0),
		TopLogprobs: testIntPtr(0),
		TopP:        testFloatPtr(0),
		TypicalP:    testFloatPtr(0),
		User:        testStringPtr("user"),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"context_length_exceeded_behavior",
		"echo",
		"echo_last",
		"frequency_penalty",
		"function_call",
		"functions",
		"ignore_eos",
		"logit_bias",
		"logprobs",
		"max_completion_tokens",
		"max_tokens",
		"metadata",
		"min_p",
		"mirostat_lr",
		"mirostat_target",
		"n",
		"parallel_tool_calls",
		"perf_metrics_in_response",
		"prediction",
		"presence_penalty",
		"prompt_cache_isolation_key",
		"prompt_cache_key",
		"prompt_truncate_len",
		"raw_output",
		"reasoning_effort",
		"reasoning_history",
		"repetition_penalty",
		"response_format",
		"return_token_ids",
		"safe_tokenization",
		"seed",
		"service_tier",
		"speculation",
		"stop",
		"temperature",
		"thinking",
		"tool_choice",
		"tools",
		"top_k",
		"top_logprobs",
		"top_p",
		"typical_p",
		"user",
	} {
		if _, ok := body[key]; !ok {
			t.Fatalf("%q missing from body %#v", key, body)
		}
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v", body["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["reasoning_content"] != "reasoning_content" || message["tool_call_id"] != "tool_call_id" {
		t.Fatalf("message = %#v", messages[0])
	}
	if body["echo_last"] != float64(0) || body["temperature"] != float64(0) || body["prompt_truncate_len"] != float64(0) {
		t.Fatalf("zero-valued params were not preserved: %#v", body)
	}
	if body["parallel_tool_calls"] != true || body["safe_tokenization"] != true || body["return_token_ids"] != true {
		t.Fatalf("boolean params = %#v", body)
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

func TestCompletionCreateTypedPreservesAllPythonOptionalParams(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(JSON{
			"id":      "cmpl-1",
			"created": 123,
			"model":   "model",
			"choices": []JSON{
				{"index": 0, "text": "ok"},
			},
		})
	}))
	defer server.Close()

	prediction := fwtypes.CompletionCreateParamsPrediction(map[string]any{
		"content": "string",
		"type":    "content",
	})
	thinking := fwtypes.CompletionCreateParamsThinking(map[string]any{
		"type":          "enabled",
		"budget_tokens": 0,
	})
	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Completions.CreateTyped(context.Background(), fwtypes.CompletionCreateParams{
		Model:                         "model",
		Prompt:                        "string",
		ContextLengthExceededBehavior: "error",
		Echo:                          testBoolPtr(true),
		EchoLast:                      testIntPtr(0),
		FrequencyPenalty:              testFloatPtr(0),
		IgnoreEos:                     true,
		Images:                        []string{"string"},
		LogitBias:                     map[string]float64{"foo": 0},
		Logprobs:                      0,
		MaxCompletionTokens:           testIntPtr(0),
		MaxTokens:                     testIntPtr(0),
		Metadata:                      map[string]string{"foo": "string"},
		MinP:                          testFloatPtr(0),
		MirostatLr:                    testFloatPtr(0),
		MirostatTarget:                testFloatPtr(0),
		N:                             0,
		PerfMetricsInResponse:         testBoolPtr(true),
		Prediction:                    &prediction,
		PresencePenalty:               testFloatPtr(0),
		PromptCacheIsolationKey:       testStringPtr("prompt_cache_isolation_key"),
		PromptCacheKey:                testStringPtr("prompt_cache_key"),
		RawOutput:                     testBoolPtr(true),
		ReasoningEffort:               "low",
		ReasoningHistory:              testStringPtr("disabled"),
		RepetitionPenalty:             testFloatPtr(0),
		ResponseFormat: &fwtypes.CompletionCreateParamsResponseFormat{
			Type:       "json_object",
			Grammar:    testStringPtr("grammar"),
			JsonSchema: "string",
			Schema:     "string",
		},
		ReturnTokenIds: testBoolPtr(true),
		Seed:           testIntPtr(0),
		ServiceTier:    "auto",
		Speculation:    "string",
		Stop:           "string",
		Temperature:    testFloatPtr(0),
		Thinking:       &thinking,
		TopK:           testIntPtr(0),
		TopLogprobs:    testIntPtr(0),
		TopP:           testFloatPtr(0),
		TypicalP:       testFloatPtr(0),
		User:           testStringPtr("user"),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"context_length_exceeded_behavior",
		"echo",
		"echo_last",
		"frequency_penalty",
		"ignore_eos",
		"images",
		"logit_bias",
		"logprobs",
		"max_completion_tokens",
		"max_tokens",
		"metadata",
		"min_p",
		"mirostat_lr",
		"mirostat_target",
		"n",
		"perf_metrics_in_response",
		"prediction",
		"presence_penalty",
		"prompt_cache_isolation_key",
		"prompt_cache_key",
		"raw_output",
		"reasoning_effort",
		"reasoning_history",
		"repetition_penalty",
		"response_format",
		"return_token_ids",
		"seed",
		"service_tier",
		"speculation",
		"stop",
		"temperature",
		"thinking",
		"top_k",
		"top_logprobs",
		"top_p",
		"typical_p",
		"user",
	} {
		if _, ok := body[key]; !ok {
			t.Fatalf("%q missing from body %#v", key, body)
		}
	}
	if body["echo_last"] != float64(0) || body["temperature"] != float64(0) || body["top_k"] != float64(0) {
		t.Fatalf("zero-valued params were not preserved: %#v", body)
	}
	if body["perf_metrics_in_response"] != true || body["raw_output"] != true || body["return_token_ids"] != true {
		t.Fatalf("boolean params = %#v", body)
	}
	responseFormat, ok := body["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_object" || responseFormat["grammar"] != "grammar" {
		t.Fatalf("response_format = %#v", body["response_format"])
	}
}

func TestCompletionCreateTypedStream(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if got := body["stream"]; got != true {
			t.Errorf("stream = %#v", got)
		}
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
		ReadMask:  "name,displayName",
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

func TestTypedGetUsesPythonQueryAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/accounts/acct-from-get/models/model-1" {
			t.Errorf("path = %q", got)
		}
		query := r.URL.Query()
		if got := query.Get("readMask"); got != "displayName" {
			t.Errorf("readMask = %q", got)
		}
		if got := query.Get("account_id"); got != "" {
			t.Errorf("account_id should not be query param, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"name": "accounts/acct-from-get/models/model-1"})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Models.GetTyped(context.Background(), "model-1", fwtypes.ModelGetParams{
		AccountID: "acct-from-get",
		ReadMask:  "displayName",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Name == nil || *out.Name != "accounts/acct-from-get/models/model-1" {
		t.Fatalf("model = %#v", out)
	}
}

func TestTypedCreateSplitsPythonQueryFields(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodPost {
			t.Errorf("method = %q", got)
		}
		if got := r.URL.Path; got != "/v1/accounts/acct-from-create/deployments" {
			t.Errorf("path = %q", got)
		}
		query := r.URL.Query()
		if got := query.Get("deploymentId"); got != "dep-1" {
			t.Errorf("deploymentId = %q", got)
		}
		if got := query.Get("skipShapeValidation"); got != "true" {
			t.Errorf("skipShapeValidation = %q", got)
		}
		if got := query.Get("validateOnly"); got != "true" {
			t.Errorf("validateOnly = %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if got := body["baseModel"]; got != "accounts/fireworks/models/base" {
			t.Errorf("baseModel = %q", got)
		}
		for _, key := range []string{"account_id", "deploymentId", "deployment_id", "skipShapeValidation", "skip_shape_validation", "validateOnly", "validate_only"} {
			if _, ok := body[key]; ok {
				t.Errorf("body should not contain %q: %#v", key, body)
			}
		}
		_ = json.NewEncoder(w).Encode(JSON{"name": "accounts/acct-from-create/deployments/dep-1"})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Deployments.CreateTyped(context.Background(), fwtypes.DeploymentCreateParams{
		AccountID:           "acct-from-create",
		BaseModel:           "accounts/fireworks/models/base",
		DeploymentID:        "dep-1",
		SkipShapeValidation: true,
		ValidateOnly:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Name == nil || *out.Name != "accounts/acct-from-create/deployments/dep-1" {
		t.Fatalf("deployment = %#v", out)
	}
}

func TestTypedJSONBodyUsesPythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodPatch {
			t.Errorf("method = %q", got)
		}
		if got := r.URL.Path; got != "/v1/accounts/acct-from-json/models/model-1" {
			t.Errorf("path = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if got := body["displayName"]; got != "Model One" {
			t.Errorf("displayName = %q", got)
		}
		if got := body["baseModel"]; got != "accounts/fireworks/models/base" {
			t.Errorf("baseModel = %q", got)
		}
		for _, key := range []string{"account_id", "display_name", "base_model"} {
			if _, ok := body[key]; ok {
				t.Errorf("body should not contain %q: %#v", key, body)
			}
		}
		_ = json.NewEncoder(w).Encode(JSON{"name": "accounts/acct-from-json/models/model-1"})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Models.UpdateTyped(context.Background(), "model-1", JSON{
		"account_id":   "acct-from-json",
		"display_name": "Model One",
		"base_model":   "accounts/fireworks/models/base",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Name == nil || *out.Name != "accounts/acct-from-json/models/model-1" {
		t.Fatalf("model = %#v", out)
	}
}

func TestTypedDeleteUsesPythonQueryAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodDelete {
			t.Errorf("method = %q", got)
		}
		if got := r.URL.Path; got != "/v1/accounts/acct-from-delete/deployments/dep-1" {
			t.Errorf("path = %q", got)
		}
		query := r.URL.Query()
		if got := query.Get("hard"); got != "true" {
			t.Errorf("hard = %q", got)
		}
		if got := query.Get("ignoreChecks"); got != "true" {
			t.Errorf("ignoreChecks = %q", got)
		}
		if got := query.Get("account_id"); got != "" {
			t.Errorf("account_id should not be query param, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(JSON{"name": "accounts/acct-from-delete/deployments/dep-1"})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Deployments.DeleteTyped(context.Background(), "dep-1", fwtypes.DeploymentDeleteParams{
		AccountID:    "acct-from-delete",
		Hard:         true,
		IgnoreChecks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["name"] != "accounts/acct-from-delete/deployments/dep-1" {
		t.Fatalf("deployment response = %#v", out)
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
	if deployment["desiredReplicaCount"] != float64(3) {
		t.Fatalf("deployment = %#v", deployment)
	}
}

func TestBatchInferenceJobsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/batchInferenceJobs":
			query := r.URL.Query()
			if got := query.Get("batchInferenceJobId"); got != "batch-job-1" {
				t.Errorf("batchInferenceJobId = %q", got)
			}
			if got := query.Get("batch_inference_job_id"); got != "" {
				t.Errorf("unexpected snake query = %q", got)
			}

			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			for _, key := range []string{"account_id", "batchInferenceJobId", "batch_inference_job_id"} {
				if _, ok := body[key]; ok {
					t.Errorf("create body should not contain %q: %#v", key, body)
				}
			}
			if got := body["continuedFromJobName"]; got != "accounts/acct/batchInferenceJobs/previous" {
				t.Errorf("continuedFromJobName = %q", got)
			}
			if got := body["displayName"]; got != "Display Name" {
				t.Errorf("displayName = %q", got)
			}
			inferenceParams, ok := body["inferenceParameters"].(map[string]any)
			if !ok {
				t.Fatalf("inferenceParameters = %#v", body["inferenceParameters"])
			}
			for _, key := range []string{"extraBody", "maxTokens", "n", "temperature", "topK", "topP"} {
				if _, ok := inferenceParams[key]; !ok {
					t.Errorf("inferenceParameters missing %q: %#v", key, inferenceParams)
				}
			}
			if inferenceParams["maxTokens"] != float64(0) || inferenceParams["temperature"] != float64(0) || inferenceParams["topK"] != float64(0) {
				t.Errorf("zero inference params were not preserved: %#v", inferenceParams)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/batchInferenceJobs/batch-job-1",
				"displayName": "Display Name",
				"state":       "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/batchInferenceJobs":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "state=running" {
				t.Errorf("filter = %q", got)
			}
			if got := query.Get("orderBy"); got != "create_time desc" {
				t.Errorf("orderBy = %q", got)
			}
			if got := query.Get("pageSize"); got != "0" {
				t.Errorf("pageSize = %q", got)
			}
			if got := query.Get("pageToken"); got != "cursor-1" {
				t.Errorf("pageToken = %q", got)
			}
			if got := query.Get("readMask"); got != "name,state" {
				t.Errorf("readMask = %q", got)
			}
			if got := query.Get("account_id"); got != "" {
				t.Errorf("account_id should not be query param, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"batchInferenceJobs": []JSON{
					{"name": "accounts/acct/batchInferenceJobs/batch-job-1", "state": "JOB_STATE_RUNNING"},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/batchInferenceJobs/batch-job-1":
			query := r.URL.Query()
			if got := query.Get("readMask"); got != "name,state" {
				t.Errorf("readMask = %q", got)
			}
			if got := query.Get("account_id"); got != "" {
				t.Errorf("account_id should not be query param, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":  "accounts/acct/batchInferenceJobs/batch-job-1",
				"state": "JOB_STATE_RUNNING",
			})

		default:
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.BatchInferenceJobs.CreateTyped(context.Background(), fwtypes.BatchInferenceJobCreateParams{
		AccountID:            "acct",
		BatchInferenceJobID:  "batch-job-1",
		ContinuedFromJobName: "accounts/acct/batchInferenceJobs/previous",
		DisplayName:          "Display Name",
		InferenceParameters: fwtypes.BatchInferenceJobCreateParamsInferenceParameters{
			ExtraBody:   "extra",
			MaxTokens:   0,
			N:           0,
			Temperature: 0,
			TopK:        0,
			TopP:        0,
		},
		InputDatasetID:  "input-dataset",
		Model:           "accounts/fireworks/models/model",
		OutputDatasetID: "output-dataset",
		Precision:       "PRECISION_UNSPECIFIED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/batchInferenceJobs/batch-job-1" {
		t.Fatalf("created = %#v", created)
	}

	page, err := client.BatchInferenceJobs.ListTyped(context.Background(), fwtypes.BatchInferenceJobListParams{
		AccountID: "acct",
		Filter:    "state=running",
		OrderBy:   "create_time desc",
		PageSize:  0,
		PageToken: "cursor-1",
		ReadMask:  "name,state",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.BatchInferenceJobsPage = page
	if len(page.BatchInferenceJobs) != 1 {
		t.Fatalf("batchInferenceJobs = %#v", page.BatchInferenceJobs)
	}
	if page.BatchInferenceJobs[0].Name == nil || *page.BatchInferenceJobs[0].Name != "accounts/acct/batchInferenceJobs/batch-job-1" {
		t.Fatalf("batch job name = %#v", page.BatchInferenceJobs[0].Name)
	}
	if page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("nextPageToken = %#v", page.NextPageToken)
	}

	got, err := client.BatchInferenceJobs.GetTyped(context.Background(), "batch-job-1", fwtypes.BatchInferenceJobGetParams{
		AccountID: "acct",
		ReadMask:  "name,state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/batchInferenceJobs/batch-job-1" {
		t.Fatalf("got = %#v", got)
	}
}

func TestUsersTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/users":
			query := r.URL.Query()
			if got := query.Get("userId"); got != "user-1" {
				t.Errorf("userId = %q", got)
			}
			if got := query.Get("user_id"); got != "" {
				t.Errorf("unexpected snake query = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			for _, key := range []string{"account_id", "userId", "user_id"} {
				if _, ok := body[key]; ok {
					t.Errorf("create body should not contain %q: %#v", key, body)
				}
			}
			if body["role"] != "admin" || body["displayName"] != "Display Name" || body["email"] != "user@example.com" || body["serviceAccount"] != true {
				t.Errorf("create body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":           "accounts/acct/users/user-1",
				"role":           "admin",
				"displayName":    "Display Name",
				"serviceAccount": true,
			})

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/accounts/acct/users/user-1":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("update body should not contain account_id: %#v", body)
			}
			if body["role"] != "viewer" || body["displayName"] != "Updated Name" || body["email"] != "updated@example.com" || body["serviceAccount"] != false {
				t.Errorf("update body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/users/user-1",
				"role":        "viewer",
				"displayName": "Updated Name",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/users":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "role=viewer" {
				t.Errorf("filter = %q", got)
			}
			if got := query.Get("orderBy"); got != "create_time desc" {
				t.Errorf("orderBy = %q", got)
			}
			if got := query.Get("pageSize"); got != "0" {
				t.Errorf("pageSize = %q", got)
			}
			if got := query.Get("pageToken"); got != "cursor-1" {
				t.Errorf("pageToken = %q", got)
			}
			if got := query.Get("readMask"); got != "name,role" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"users":         []JSON{{"name": "accounts/acct/users/user-1", "role": "viewer"}},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/users/user-1":
			if got := r.URL.Query().Get("readMask"); got != "name,role" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{"name": "accounts/acct/users/user-1", "role": "viewer"})

		default:
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.Users.CreateTyped(context.Background(), fwtypes.UserCreateParams{
		AccountID:      "acct",
		Role:           "admin",
		UserID:         "user-1",
		DisplayName:    "Display Name",
		Email:          "user@example.com",
		ServiceAccount: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/users/user-1" {
		t.Fatalf("created = %#v", created)
	}

	updated, err := client.Users.UpdateTyped(context.Background(), "user-1", fwtypes.UserUpdateParams{
		AccountID:      "acct",
		Role:           "viewer",
		DisplayName:    "Updated Name",
		Email:          "updated@example.com",
		ServiceAccount: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Role != "viewer" {
		t.Fatalf("updated = %#v", updated)
	}

	page, err := client.Users.ListTyped(context.Background(), fwtypes.UserListParams{
		AccountID: "acct",
		Filter:    "role=viewer",
		OrderBy:   "create_time desc",
		PageSize:  0,
		PageToken: "cursor-1",
		ReadMask:  "name,role",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.UsersPage = page
	if len(page.Users) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.Users.GetTyped(context.Background(), "user-1", fwtypes.UserGetParams{
		AccountID: "acct",
		ReadMask:  "name,role",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/users/user-1" {
		t.Fatalf("got = %#v", got)
	}
}

func TestAPIKeysTypedAndGenericAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/users/user-1/apiKeys":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("create body should not contain account_id: %#v", body)
			}
			apiKey, ok := body["apiKey"].(map[string]any)
			if !ok {
				t.Fatalf("apiKey body = %#v", body["apiKey"])
			}
			if _, ok := body["api_key"]; ok {
				t.Errorf("unexpected api_key body key: %#v", body)
			}
			displayName, _ := apiKey["displayName"].(string)
			if displayName != "Typed Key" && displayName != "Generic Key" {
				t.Errorf("displayName = %q", displayName)
			}
			if _, ok := apiKey["display_name"]; ok {
				t.Errorf("unexpected display_name body key: %#v", apiKey)
			}
			if _, ok := apiKey["expireTime"]; !ok {
				t.Errorf("expireTime missing: %#v", apiKey)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"keyId":       "key-1",
				"displayName": displayName,
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/users/user-1/apiKeys":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "secure=true" {
				t.Errorf("filter = %q", got)
			}
			if got := query.Get("orderBy"); got != "create_time desc" {
				t.Errorf("orderBy = %q", got)
			}
			if got := query.Get("pageSize"); got != "0" {
				t.Errorf("pageSize = %q", got)
			}
			if got := query.Get("pageToken"); got != "cursor-1" {
				t.Errorf("pageToken = %q", got)
			}
			if got := query.Get("readMask"); got != "keyId,displayName" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"apiKeys":       []JSON{{"keyId": "key-1", "displayName": "Typed Key"}},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/users/user-1/apiKeys:delete":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode delete body: %v", err)
			}
			if body["keyId"] != "key-1" {
				t.Errorf("keyId = %#v", body["keyId"])
			}
			for _, key := range []string{"account_id", "key_id"} {
				if _, ok := body[key]; ok {
					t.Errorf("delete body should not contain %q: %#v", key, body)
				}
			}
			_ = json.NewEncoder(w).Encode(JSON{"deleted": true})

		default:
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	typedKey, err := client.APIKeys.CreateTyped(context.Background(), "user-1", fwtypes.APIKeyCreateParams{
		AccountID: "acct",
		APIKey: fwtypes.APIKeyParam{
			DisplayName: "Typed Key",
			ExpireTime:  "2019-12-27T18:11:19.117Z",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if typedKey.KeyID == nil || *typedKey.KeyID != "key-1" {
		t.Fatalf("typedKey = %#v", typedKey)
	}

	genericKey, err := client.APIKeys.Create(context.Background(), "user-1", JSON{
		"account_id": "acct",
		"api_key": JSON{
			"display_name": "Generic Key",
			"expire_time":  "2019-12-27T18:11:19.117Z",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if genericKey["keyId"] != "key-1" {
		t.Fatalf("genericKey = %#v", genericKey)
	}

	page, err := client.APIKeys.ListTyped(context.Background(), "user-1", fwtypes.APIKeyListParams{
		AccountID: "acct",
		Filter:    "secure=true",
		OrderBy:   "create_time desc",
		PageSize:  0,
		PageToken: "cursor-1",
		ReadMask:  "keyId,displayName",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.APIKeysPage = page
	if len(page.APIKeys) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	deleted, err := client.APIKeys.DeleteTyped(context.Background(), "user-1", fwtypes.APIKeyDeleteParams{
		AccountID: "acct",
		KeyID:     "key-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
	}

	deleted, err = client.APIKeys.Delete(context.Background(), "user-1", JSON{
		"account_id": "acct",
		"key_id":     "key-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("generic deleted = %#v", deleted)
	}
}

func TestDatasetsUploadFileTypedUsesMultipart(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
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

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Datasets.UploadTyped(
		context.Background(),
		"ds-1",
		fwtypes.DatasetUploadParams{
			AccountID: "acct",
			File:      NewFileFromBytes("train.jsonl", []byte("{\"prompt\":\"hi\"}\n")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.DatasetUploadResponse = out
	if out.ID == nil || *out.ID != "file-1" || out.Filename == nil || *out.Filename != "train.jsonl" {
		t.Fatalf("response = %#v", out)
	}

	raw, err := client.Datasets.Upload(
		context.Background(),
		"ds-1",
		JSON{
			"account_id": "acct",
			"file":       NewFileFromBytes("train.jsonl", []byte("{\"prompt\":\"hi\"}\n")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if raw["id"] != "file-1" || raw["filename"] != "train.jsonl" {
		t.Fatalf("raw upload response = %#v", raw)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
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

func testStringPtr(value string) *string {
	return &value
}

func testBoolPtr(value bool) *bool {
	return &value
}

func testIntPtr(value int) *int {
	return &value
}

func testFloatPtr(value float64) *float64 {
	return &value
}

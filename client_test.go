package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestPathTemplateMatchesPythonInterpolation(t *testing.T) {
	cases := []struct {
		template string
		values   JSON
		want     string
	}{
		{"/v1/{id}", JSON{"id": "abc"}, "/v1/abc"},
		{"/v1/{a}/{b}", JSON{"a": "x", "b": "y"}, "/v1/x/y"},
		{"/v1/{a}{b}/path/{c}?val={d}#{e}", JSON{"a": "x", "b": "y", "c": "z", "d": "u", "e": "v"}, "/v1/xy/path/z?val=u#v"},
		{"/{w}/{w}", JSON{"w": "echo"}, "/echo/echo"},
		{"/v1/static", JSON{}, "/v1/static"},
		{"", JSON{}, ""},
		{"/v1/?q={n}&count=10", JSON{"n": 42}, "/v1/?q=42&count=10"},
		{"/v1/{v}", JSON{"v": nil}, "/v1/null"},
		{"/v1/{v}", JSON{"v": true}, "/v1/true"},
		{"/v1/{v}", JSON{"v": false}, "/v1/false"},
		{"/v1/{v}", JSON{"v": ".hidden"}, "/v1/.hidden"},
		{"/v1/{v}", JSON{"v": "file.txt"}, "/v1/file.txt"},
		{"/v1/{v}", JSON{"v": "..."}, "/v1/..."},
		{"/v1/{a}{b}", JSON{"a": ".", "b": "txt"}, "/v1/.txt"},
		{"/items?q={v}#{f}", JSON{"v": ".", "f": ".."}, "/items?q=.#.."},
		{"/v1/{a}?query={b}", JSON{"a": "../../other/endpoint", "b": "a&bad=true"}, "/v1/..%2F..%2Fother%2Fendpoint?query=a%26bad%3Dtrue"},
		{"/v1/{val}", JSON{"val": "a/b/c"}, "/v1/a%2Fb%2Fc"},
		{"/v1/{val}", JSON{"val": "a/b/c?query=value"}, "/v1/a%2Fb%2Fc%3Fquery=value"},
		{"/v1/{val}", JSON{"val": "a/b/c?query=value&bad=true"}, "/v1/a%2Fb%2Fc%3Fquery=value&bad=true"},
		{"/v1/{val}", JSON{"val": "%20"}, "/v1/%2520"},
		{"/items?q={v}", JSON{"v": "a/b"}, "/items?q=a/b"},
		{"/items?q={v}", JSON{"v": "a?b"}, "/items?q=a?b"},
		{"/items?q={v}", JSON{"v": "a#b"}, "/items?q=a%23b"},
		{"/items?q={v}", JSON{"v": "a b"}, "/items?q=a%20b"},
		{"/docs#{v}", JSON{"v": "a/b"}, "/docs#a/b"},
		{"/docs#{v}", JSON{"v": "a?b"}, "/docs#a?b"},
		{"/v1/{v}", JSON{"v": "a?b"}, "/v1/a%3Fb"},
		{"/v1/{v}", JSON{"v": "a#b"}, "/v1/a%23b"},
		{"/v1/{v}?q={v}#{v}", JSON{"v": "a/b?c#d"}, "/v1/a%2Fb%3Fc%23d?q=a/b?c%23d#a/b?c%23d"},
		{"/v1/{val}", JSON{"val": "x?admin=true"}, "/v1/x%3Fadmin=true"},
		{"/v1/{val}", JSON{"val": "x#admin"}, "/v1/x%23admin"},
	}

	for _, tc := range cases {
		t.Run(tc.template+" -> "+tc.want, func(t *testing.T) {
			got, err := pathTemplate(tc.template, tc.values)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("pathTemplate(%q) = %q, want %q", tc.template, got, tc.want)
			}
		})
	}
}

func TestPathTemplateMissingPlaceholderAndDotSegments(t *testing.T) {
	if _, err := pathTemplate("/v1/{org_id}", JSON{}); err == nil || !strings.Contains(err.Error(), "{org_id}") {
		t.Fatalf("missing placeholder err = %v", err)
	}

	for _, tc := range []struct {
		template string
		values   JSON
	}{
		{"{a}/path", JSON{"a": "."}},
		{"{a}/path", JSON{"a": ".."}},
		{"/v1/{a}", JSON{"a": "."}},
		{"/v1/{a}", JSON{"a": ".."}},
		{"/v1/{a}/path", JSON{"a": "."}},
		{"/v1/{a}/path", JSON{"a": ".."}},
		{"/v1/{a}{b}", JSON{"a": ".", "b": "."}},
		{"/v1/{a}.", JSON{"a": "."}},
		{"/v1/{a}{b}", JSON{"a": "", "b": "."}},
		{"/v1/%2e/{x}", JSON{"x": "ok"}},
		{"/v1/%2e./{x}", JSON{"x": "ok"}},
		{"/v1/.%2E/{x}", JSON{"x": "ok"}},
		{"/v1/{v}?q=1", JSON{"v": ".."}},
		{"/v1/{v}#frag", JSON{"v": ".."}},
	} {
		t.Run(tc.template, func(t *testing.T) {
			_, err := pathTemplate(tc.template, tc.values)
			if err == nil || !strings.Contains(err.Error(), "dot-segment") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestResourcePathEscapingUsesPythonPathSegmentSafeSet(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")
	t.Setenv("FIREWORKS_ACCOUNT_ID", "acct")

	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.EscapedPath()
		_ = json.NewEncoder(w).Encode(JSON{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Models.Get(context.Background(), "model/with space/%20/!$&'()*+,;=:@")
	if err != nil {
		t.Fatal(err)
	}
	want := "/v1/accounts/acct/models/model%2Fwith%20space%2F%2520%2F!$&'()*+,;=:@"
	if seenPath != want {
		t.Fatalf("path = %q, want %q", seenPath, want)
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

func TestStringifyQueryMatchesPythonQS(t *testing.T) {
	cases := []struct {
		name         string
		params       JSON
		arrayFormat  string
		nestedFormat string
		want         string
	}{
		{name: "empty", params: JSON{}, want: ""},
		{name: "empty nested", params: JSON{"a": JSON{}}, want: ""},
		{name: "basic int", params: JSON{"a": 1}, want: "a=1"},
		{name: "basic string", params: JSON{"a": "b"}, want: "a=b"},
		{name: "basic true", params: JSON{"a": true}, want: "a=true"},
		{name: "basic false", params: JSON{"a": false}, want: "a=false"},
		{name: "basic float", params: JSON{"a": 1.23456}, want: "a=1.23456"},
		{name: "basic nil", params: JSON{"a": nil}, want: ""},
		{name: "nested dotted", params: JSON{"a": JSON{"b": JSON{"c": JSON{"d": "e"}}}}, nestedFormat: "dots", want: "a.b.c.d=e"},
		{name: "nested brackets", params: JSON{"a": JSON{"b": JSON{"c": JSON{"d": "e"}}}}, want: "a[b][c][d]=e"},
		{name: "nested bool", params: JSON{"a": JSON{"b": true}}, want: "a[b]=true"},
		{name: "array comma", params: JSON{"in": []any{"foo", "bar"}}, arrayFormat: "comma", want: "in=foo,bar"},
		{name: "array comma nested", params: JSON{"a": JSON{"b": []any{true, false, nil, true}}}, arrayFormat: "comma", want: "a[b]=true,false,true"},
		{name: "array repeat", params: JSON{"in": []any{"foo", "bar"}}, want: "in=foo&in=bar"},
		{name: "array repeat nested", params: JSON{"a": JSON{"b": []any{true, false, nil, true}}}, want: "a[b]=true&a[b]=false&a[b]=true"},
		{name: "array repeat object", params: JSON{"in": []any{"foo", JSON{"b": JSON{"c": []any{"d", "e"}}}}}, want: "in=foo&in[b][c]=d&in[b][c]=e"},
		{name: "array brackets", params: JSON{"in": []any{"foo", "bar"}}, arrayFormat: "brackets", want: "in[]=foo&in[]=bar"},
		{name: "array brackets nested", params: JSON{"a": JSON{"b": []any{true, false, nil, true}}}, arrayFormat: "brackets", want: "a[b][]=true&a[b][]=false&a[b][]=true"},
		{name: "array indices", params: JSON{"in": []any{"foo", "bar"}}, arrayFormat: "indices", want: "in[0]=foo&in[1]=bar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stringifyQuery(tc.params, tc.arrayFormat, tc.nestedFormat)
			if err != nil {
				t.Fatal(err)
			}
			if got = queryUnescape(t, got); got != tc.want {
				t.Fatalf("query = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStringifyQueryRejectsUnknownArrayFormat(t *testing.T) {
	_, err := stringifyQuery(JSON{"a": []any{"foo", "bar"}}, "foo", "")
	if err == nil || !strings.Contains(err.Error(), "unknown array_format value: foo") {
		t.Fatalf("err = %v", err)
	}
}

func queryUnescape(t *testing.T, value string) string {
	t.Helper()
	out, err := url.QueryUnescape(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
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

func TestNewRequestUsesPythonOpenAPIDumpsJSONSemantics(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	client, err := NewClient(WithBaseURL("https://example.test"))
	if err != nil {
		t.Fatal(err)
	}
	req, err := client.NewRequest(context.Background(), http.MethodPost, "/json", JSON{
		"text":    "<tag>&value",
		"unicode": "åß∂",
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), `{"text":"<tag>&value","unicode":"åß∂"}`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if strings.Contains(string(body), `\u003c`) || strings.Contains(string(body), `\u0026`) || strings.Contains(string(body), `\u00e5`) {
		t.Fatalf("body unexpectedly escaped like default json.Marshal: %q", body)
	}
}

func TestNewRequestRejectsNaNLikePythonOpenAPIDumps(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	client, err := NewClient(WithBaseURL("https://example.test"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.NewRequest(context.Background(), http.MethodPost, "/json", JSON{"nan": math.NaN()})
	if err == nil || !strings.Contains(err.Error(), "unsupported value: NaN") {
		t.Fatalf("err = %v", err)
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

func TestAPIResponseBoolMatchesPythonParseBool(t *testing.T) {
	for _, tc := range []struct {
		content string
		want    bool
	}{
		{content: "false", want: false},
		{content: "true", want: true},
		{content: "False", want: false},
		{content: "True", want: true},
		{content: "TrUe", want: true},
		{content: "FalSe", want: false},
	} {
		t.Run(tc.content, func(t *testing.T) {
			resp := &APIResponse{Body: []byte(tc.content)}
			got, err := resp.Bool()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("Bool() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAPIResponseBoolValidationErrorKeepsMetadata(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.fireworks.ai/v1/bool", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp := &APIResponse{
		Request:    req,
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"req-bool"}},
		Body:       []byte("maybe"),
		RequestID:  "req-bool",
	}
	_, err = resp.Bool()
	if err == nil {
		t.Fatal("expected error")
	}
	var validationErr *APIResponseValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error type = %T, want APIResponseValidationError", err)
	}
	if validationErr.Response != resp || validationErr.Request != req || validationErr.Header.Get("X-Request-Id") != "req-bool" {
		t.Fatalf("validation metadata = %#v", validationErr)
	}
}

func TestAPIResponseJSONOrTextUsesContentType(t *testing.T) {
	jsonResp := &APIResponse{
		Header: http.Header{"Content-Type": []string{"application/vnd.fireworks+json; charset=utf-8"}},
		Body:   []byte(`{"ok":true}`),
	}
	if !jsonResp.IsJSON() {
		t.Fatal("expected JSON content type")
	}
	parsed, err := jsonResp.JSONOrText()
	if err != nil {
		t.Fatal(err)
	}
	if parsed.(map[string]any)["ok"] != true {
		t.Fatalf("parsed = %#v", parsed)
	}

	textResp := &APIResponse{
		Header: http.Header{"Content-Type": []string{"application/text"}},
		Body:   []byte("foo"),
	}
	if textResp.IsJSON() {
		t.Fatal("did not expect JSON content type")
	}
	parsed, err = textResp.JSONOrText()
	if err != nil {
		t.Fatal(err)
	}
	if parsed != "foo" {
		t.Fatalf("parsed = %#v", parsed)
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

func TestExtractFilesMutatesInputLikePythonHelper(t *testing.T) {
	query := map[string]any{"foo": []byte("Bar"), "hello": "world"}
	files, err := extractFiles(query, [][]string{{"foo"}}, "brackets")
	if err != nil {
		t.Fatal(err)
	}
	assertFileParts(t, files, []string{"foo"}, []string{"Bar"})
	if _, ok := query["foo"]; ok || query["hello"] != "world" {
		t.Fatalf("query = %#v", query)
	}

	nested := map[string]any{
		"foo":   map[string]any{"foo": map[string]any{"bar": []byte("Nested")}},
		"hello": "world",
	}
	files, err = extractFiles(nested, [][]string{{"foo", "foo", "bar"}}, "brackets")
	if err != nil {
		t.Fatal(err)
	}
	assertFileParts(t, files, []string{"foo[foo][bar]"}, []string{"Nested"})
	if got := nested["foo"].(map[string]any)["foo"].(map[string]any); len(got) != 0 {
		t.Fatalf("nested foo = %#v", got)
	}
	if nested["hello"] != "world" {
		t.Fatalf("nested = %#v", nested)
	}
}

func TestExtractFilesArraysUsePythonFieldNames(t *testing.T) {
	query := map[string]any{
		"documents": []any{
			map[string]any{"file": []byte("first")},
			map[string]any{"file": []byte("second")},
		},
	}
	files, err := extractFiles(query, [][]string{{"documents", "<array>", "file"}}, "brackets")
	if err != nil {
		t.Fatal(err)
	}
	assertFileParts(t, files, []string{"documents[][file]", "documents[][file]"}, []string{"first", "second"})
	docs := query["documents"].([]any)
	if len(docs[0].(map[string]any)) != 0 || len(docs[1].(map[string]any)) != 0 {
		t.Fatalf("documents = %#v", docs)
	}

	for _, tc := range []struct {
		name        string
		arrayFormat string
		wantTop     []string
		wantNested  []string
	}{
		{name: "brackets", arrayFormat: "brackets", wantTop: []string{"files[]", "files[]"}, wantNested: []string{"items[][file]", "items[][file]"}},
		{name: "repeat", arrayFormat: "repeat", wantTop: []string{"files", "files"}, wantNested: []string{"items[file]", "items[file]"}},
		{name: "comma", arrayFormat: "comma", wantTop: []string{"files", "files"}, wantNested: []string{"items[file]", "items[file]"}},
		{name: "indices", arrayFormat: "indices", wantTop: []string{"files[0]", "files[1]"}, wantNested: []string{"items[0][file]", "items[1][file]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			top := map[string]any{"files": [][]byte{[]byte("a"), []byte("b")}, "title": "hello"}
			files, err := extractFiles(top, [][]string{{"files", "<array>"}}, tc.arrayFormat)
			if err != nil {
				t.Fatal(err)
			}
			assertFileParts(t, files, tc.wantTop, []string{"a", "b"})
			if _, ok := top["files"]; ok || top["title"] != "hello" {
				t.Fatalf("top = %#v", top)
			}

			nested := map[string]any{
				"items": []any{
					map[string]any{"file": []byte("a")},
					map[string]any{"file": []byte("b")},
				},
			}
			files, err = extractFiles(nested, [][]string{{"items", "<array>", "file"}}, tc.arrayFormat)
			if err != nil {
				t.Fatal(err)
			}
			assertFileParts(t, files, tc.wantNested, []string{"a", "b"})
		})
	}
}

func TestExtractFilesIgnoresIncorrectPaths(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query map[string]any
		paths [][]string
	}{
		{name: "dict expecting array", query: map[string]any{"foo": map[string]any{"bar": "baz"}}, paths: [][]string{{"foo", "<array>", "bar"}}},
		{name: "array expecting dict", query: map[string]any{"foo": []any{"bar", "baz"}}, paths: [][]string{{"foo", "bar"}}},
		{name: "unknown keys", query: map[string]any{"foo": map[string]any{"bar": "baz"}}, paths: [][]string{{"foo", "foo"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, err := extractFiles(tc.query, tc.paths, "brackets")
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 0 {
				t.Fatalf("files = %#v", files)
			}
		})
	}
}

func TestExtractFilesRejectsUnknownArrayFormat(t *testing.T) {
	query := map[string]any{"files": [][]byte{[]byte("a")}}
	_, err := extractFiles(query, [][]string{{"files", "<array>"}}, "foo")
	if err == nil || !strings.Contains(err.Error(), "unknown array_format value: foo") {
		t.Fatalf("err = %v", err)
	}
}

func assertFileParts(t *testing.T, files []filePart, wantKeys, wantBodies []string) {
	t.Helper()
	if len(files) != len(wantKeys) || len(files) != len(wantBodies) {
		t.Fatalf("files = %#v, want %d", files, len(wantKeys))
	}
	for i, part := range files {
		if part.Key != wantKeys[i] {
			t.Fatalf("files[%d].Key = %q, want %q", i, part.Key, wantKeys[i])
		}
		body, err := io.ReadAll(part.File.Content)
		if err != nil {
			t.Fatalf("read file %d: %v", i, err)
		}
		if string(body) != wantBodies[i] {
			t.Fatalf("files[%d] body = %q, want %q", i, body, wantBodies[i])
		}
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

func TestAccountsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "state=ACTIVE" {
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
			if got := query.Get("readMask"); got != "name,displayName" {
				t.Errorf("readMask = %q", got)
			}
			for _, key := range []string{"account_id", "order_by", "page_size", "page_token", "read_mask"} {
				if got := query.Get(key); got != "" {
					t.Errorf("unexpected query %s=%q", key, got)
				}
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"accounts": []JSON{
					{"name": "accounts/acct", "displayName": "Account One", "email": "owner@example.com", "state": "ACTIVE"},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct":
			query := r.URL.Query()
			if got := query.Get("readMask"); got != "name,displayName" {
				t.Errorf("readMask = %q", got)
			}
			for _, key := range []string{"account_id", "read_mask"} {
				if got := query.Get(key); got != "" {
					t.Errorf("unexpected query %s=%q", key, got)
				}
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct",
				"displayName": "Account One",
				"email":       "owner@example.com",
				"state":       "ACTIVE",
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

	page, err := client.Accounts.ListTyped(context.Background(), fwtypes.AccountListParams{
		Filter:    "state=ACTIVE",
		OrderBy:   "create_time desc",
		PageSize:  0,
		PageToken: "cursor-1",
		ReadMask:  "name,displayName",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.AccountsPage = page
	if len(page.Accounts) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}
	if page.Accounts[0].Name == nil || *page.Accounts[0].Name != "accounts/acct" {
		t.Fatalf("account = %#v", page.Accounts[0])
	}

	got, err := client.Accounts.GetTyped(context.Background(), "acct", fwtypes.AccountGetParams{
		AccountID: "acct",
		ReadMask:  "name,displayName",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct" || got.DisplayName == nil || *got.DisplayName != "Account One" {
		t.Fatalf("got = %#v", got)
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

func TestMessagesCreateTypedPreservesAllPythonOptionalParams(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/messages" {
			t.Errorf("path = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(JSON{
			"id":    "msg-1",
			"model": "claude-opus-4-6",
			"role":  "assistant",
			"type":  "message",
			"content": []JSON{
				{"type": "text", "text": "hello"},
			},
			"stop_reason": "end_turn",
		})
	}))
	defer server.Close()

	toolType := "custom"
	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Messages.CreateTyped(context.Background(), fwtypes.MessageCreateParams{
		Messages: []fwtypes.MessageCreateParamsMessage{
			{Role: "user", Content: "Hello, world"},
		},
		Model:     "claude-opus-4-6",
		MaxTokens: 1024,
		Metadata: fwtypes.MessageCreateParamsMetadata{
			UserID: testStringPtr("13803d75-b4b5-4c3e-b2a2-6f21399b021b"),
		},
		OutputConfig: fwtypes.MessageCreateParamsOutputConfig{
			Effort: testStringPtr("low"),
			Format: &fwtypes.MessageCreateParamsOutputConfigFormat{
				Schema: map[string]any{"foo": "bar"},
				Type:   "json_schema",
			},
		},
		RawOutput:     testBoolPtr(true),
		StopSequences: []string{"string"},
		Stream:        true,
		System: []fwtypes.RequestTextBlockParam{
			{
				Text: "Today's date is 2024-06-01.",
				Type: "text",
				CacheControl: &fwtypes.CacheControlEphemeralParam{
					Type: "ephemeral",
					Ttl:  "5m",
				},
				Citations: []fwtypes.RequestTextBlockParamCitation{
					map[string]any{
						"cited_text":       "cited_text",
						"document_index":   0,
						"document_title":   "x",
						"end_char_index":   0,
						"start_char_index": 0,
						"type":             "char_location",
					},
				},
			},
		},
		Temperature: 1,
		Thinking: fwtypes.MessageCreateParamsThinking(map[string]any{
			"budget_tokens": 1024,
			"type":          "enabled",
		}),
		ToolChoice: fwtypes.MessageCreateParamsToolChoice(map[string]any{
			"type":                      "auto",
			"disable_parallel_tool_use": true,
		}),
		Tools: []fwtypes.MessageCreateParamsTool{
			{
				InputSchema: fwtypes.MessageCreateParamsToolInputSchema{
					Type: "object",
					Properties: map[string]any{
						"location": "bar",
						"unit":     "bar",
					},
					Required: []string{"location"},
				},
				Name:        "name",
				Description: "Get the current weather in a given location",
				Strict:      true,
				Type:        &toolType,
			},
		},
		TopK: 5,
		TopP: 0.7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "msg-1" || out.Role != "assistant" {
		t.Fatalf("response = %#v", out)
	}

	for _, key := range []string{
		"messages",
		"model",
		"max_tokens",
		"metadata",
		"output_config",
		"raw_output",
		"stop_sequences",
		"stream",
		"system",
		"temperature",
		"thinking",
		"tool_choice",
		"tools",
		"top_k",
		"top_p",
	} {
		if _, ok := body[key]; !ok {
			t.Fatalf("%q missing from body %#v", key, body)
		}
	}
	for _, key := range []string{"maxTokens", "outputConfig", "rawOutput", "stopSequences", "toolChoice", "topK", "topP"} {
		if _, ok := body[key]; ok {
			t.Fatalf("unexpected camel key %q in body %#v", key, body)
		}
	}
	if body["max_tokens"] != float64(1024) || body["raw_output"] != true || body["stream"] != true || body["top_k"] != float64(5) || body["top_p"] != 0.7 {
		t.Fatalf("primitive params = %#v", body)
	}

	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v", body["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["role"] != "user" || message["content"] != "Hello, world" {
		t.Fatalf("message = %#v", messages[0])
	}

	metadata, ok := body["metadata"].(map[string]any)
	if !ok || metadata["user_id"] != "13803d75-b4b5-4c3e-b2a2-6f21399b021b" {
		t.Fatalf("metadata = %#v", body["metadata"])
	}
	outputConfig, ok := body["output_config"].(map[string]any)
	if !ok || outputConfig["effort"] != "low" {
		t.Fatalf("output_config = %#v", body["output_config"])
	}
	format, ok := outputConfig["format"].(map[string]any)
	if !ok || format["type"] != "json_schema" {
		t.Fatalf("format = %#v", outputConfig["format"])
	}
	schema, ok := format["schema"].(map[string]any)
	if !ok || schema["foo"] != "bar" {
		t.Fatalf("schema = %#v", format["schema"])
	}

	system, ok := body["system"].([]any)
	if !ok || len(system) != 1 {
		t.Fatalf("system = %#v", body["system"])
	}
	systemBlock, ok := system[0].(map[string]any)
	if !ok || systemBlock["type"] != "text" || systemBlock["text"] != "Today's date is 2024-06-01." {
		t.Fatalf("system block = %#v", system[0])
	}
	cacheControl, ok := systemBlock["cache_control"].(map[string]any)
	if !ok || cacheControl["type"] != "ephemeral" || cacheControl["ttl"] != "5m" {
		t.Fatalf("cache_control = %#v", systemBlock["cache_control"])
	}
	citations, ok := systemBlock["citations"].([]any)
	if !ok || len(citations) != 1 {
		t.Fatalf("citations = %#v", systemBlock["citations"])
	}
	citation, ok := citations[0].(map[string]any)
	if !ok || citation["cited_text"] != "cited_text" || citation["start_char_index"] != float64(0) {
		t.Fatalf("citation = %#v", citations[0])
	}

	toolChoice, ok := body["tool_choice"].(map[string]any)
	if !ok || toolChoice["type"] != "auto" || toolChoice["disable_parallel_tool_use"] != true {
		t.Fatalf("tool_choice = %#v", body["tool_choice"])
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok || tool["type"] != "custom" || tool["strict"] != true || tool["name"] != "name" {
		t.Fatalf("tool = %#v", tools[0])
	}
	inputSchema, ok := tool["input_schema"].(map[string]any)
	if !ok || inputSchema["type"] != "object" {
		t.Fatalf("input_schema = %#v", tool["input_schema"])
	}
	if _, ok := tool["inputSchema"]; ok {
		t.Fatalf("unexpected camel inputSchema in tool %#v", tool)
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

func TestModelsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/models":
			if got := r.URL.RawQuery; got != "" {
				t.Errorf("create query = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("create body should not contain account_id: %#v", body)
			}
			if body["modelId"] != "model-1" || body["cluster"] != "accounts/acct/clusters/cluster-1" {
				t.Errorf("create aliases = %#v", body)
			}
			if _, ok := body["model_id"]; ok {
				t.Errorf("unexpected model_id body key: %#v", body)
			}
			model, ok := body["model"].(map[string]any)
			if !ok {
				t.Fatalf("model = %#v", body["model"])
			}
			assertModelPayloadUsesAliases(t, model)
			_ = json.NewEncoder(w).Encode(JSON{
				"name":          "accounts/acct/models/model-1",
				"displayName":   "Model One",
				"contextLength": 8192,
				"public":        true,
			})

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/accounts/acct/models/model-1":
			if got := r.URL.RawQuery; got != "" {
				t.Errorf("update query = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("update body should not contain account_id: %#v", body)
			}
			assertModelPayloadUsesAliases(t, body)
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/models/model-1",
				"displayName": "Model One Updated",
				"public":      true,
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/models":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "kind=HF_BASE_MODEL" {
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
			if got := query.Get("readMask"); got != "name,displayName" {
				t.Errorf("readMask = %q", got)
			}
			if got := query.Get("account_id"); got != "" {
				t.Errorf("account_id should not be query param, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"models": []JSON{
					{"name": "accounts/acct/models/model-1", "displayName": "Model One", "contextLength": 8192},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/models/model-1":
			if got := r.URL.Query().Get("readMask"); got != "name,displayName" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/models/model-1",
				"displayName": "Model One",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/models/model-1:getDownloadEndpoint":
			if got := r.URL.Query().Get("readMask"); got != "filenameToSignedUrls" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"filenameToSignedUrls": map[string]string{"model.safetensors": "gs://download/model.safetensors"},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/models/model-1:getUploadEndpoint":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode get upload body: %v", err)
			}
			for _, key := range []string{"account_id", "filename_to_size", "enable_resumable_upload", "read_mask"} {
				if _, ok := body[key]; ok {
					t.Errorf("get upload body should not contain %q: %#v", key, body)
				}
			}
			filenameToSize, ok := body["filenameToSize"].(map[string]any)
			if !ok || filenameToSize["model.safetensors"] != "123" {
				t.Errorf("filenameToSize = %#v", body["filenameToSize"])
			}
			if body["enableResumableUpload"] != true || body["readMask"] != "filenameToUnsignedUris" {
				t.Errorf("get upload aliases = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"filenameToUnsignedUris": map[string]string{"model.safetensors": "gs://upload/model.safetensors"},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/models/model-1:prepare":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode prepare body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("prepare body should not contain account_id: %#v", body)
			}
			if body["precision"] != "FP8" || body["readMask"] != "state" {
				t.Errorf("prepare body = %#v", body)
			}
			if _, ok := body["read_mask"]; ok {
				t.Errorf("unexpected read_mask body key: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{"prepared": true})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/models/model-1:validateUpload":
			query := r.URL.Query()
			if got := query.Get("configOnly"); got != "true" {
				t.Errorf("configOnly = %q", got)
			}
			if got := query.Get("skipHfConfigValidation"); got != "true" {
				t.Errorf("skipHfConfigValidation = %q", got)
			}
			if got := query.Get("trustRemoteCode"); got != "true" {
				t.Errorf("trustRemoteCode = %q", got)
			}
			for _, key := range []string{"account_id", "config_only", "skip_hf_config_validation", "trust_remote_code"} {
				if got := query.Get(key); got != "" {
					t.Errorf("unexpected query %s=%q", key, got)
				}
			}
			_ = json.NewEncoder(w).Encode(JSON{"warnings": []string{"ok"}})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/models/model-1":
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
	created, err := client.Models.CreateTyped(context.Background(), fwtypes.ModelCreateParams{
		AccountID: "acct",
		ModelID:   "model-1",
		Cluster:   "accounts/acct/clusters/cluster-1",
		Model:     testModelParam("Model One"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/models/model-1" {
		t.Fatalf("created = %#v", created)
	}

	updated, err := client.Models.UpdateTyped(context.Background(), "model-1", testModelUpdateParams("acct", "Model One Updated"))
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName == nil || *updated.DisplayName != "Model One Updated" {
		t.Fatalf("updated = %#v", updated)
	}

	page, err := client.Models.ListTyped(context.Background(), fwtypes.ModelListParams{
		AccountID: "acct",
		Filter:    "kind=HF_BASE_MODEL",
		OrderBy:   "create_time desc",
		PageSize:  0,
		PageToken: "cursor-1",
		ReadMask:  "name,displayName",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.ModelsPage = page
	if len(page.Models) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.Models.GetTyped(context.Background(), "model-1", fwtypes.ModelGetParams{
		AccountID: "acct",
		ReadMask:  "name,displayName",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/models/model-1" {
		t.Fatalf("got = %#v", got)
	}

	download, err := client.Models.GetDownloadEndpointTyped(context.Background(), "model-1", fwtypes.ModelGetDownloadEndpointParams{
		AccountID: "acct",
		ReadMask:  "filenameToSignedUrls",
	})
	if err != nil {
		t.Fatal(err)
	}
	if download.FilenameToSignedUrls["model.safetensors"] != "gs://download/model.safetensors" {
		t.Fatalf("download = %#v", download)
	}

	upload, err := client.Models.GetUploadEndpointTyped(context.Background(), "model-1", fwtypes.ModelGetUploadEndpointParams{
		AccountID:             "acct",
		FilenameToSize:        map[string]string{"model.safetensors": "123"},
		EnableResumableUpload: true,
		ReadMask:              "filenameToUnsignedUris",
	})
	if err != nil {
		t.Fatal(err)
	}
	if upload.FilenameToUnsignedUris["model.safetensors"] != "gs://upload/model.safetensors" {
		t.Fatalf("upload = %#v", upload)
	}

	prepared, err := client.Models.PrepareTyped(context.Background(), "model-1", fwtypes.ModelPrepareParams{
		AccountID: "acct",
		Precision: "FP8",
		ReadMask:  "state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared["prepared"] != true {
		t.Fatalf("prepared = %#v", prepared)
	}

	validated, err := client.Models.ValidateUploadTyped(context.Background(), "model-1", fwtypes.ModelValidateUploadParams{
		AccountID:              "acct",
		ConfigOnly:             true,
		SkipHfConfigValidation: true,
		TrustRemoteCode:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(validated.Warnings) != 1 || validated.Warnings[0] != "ok" {
		t.Fatalf("validated = %#v", validated)
	}

	deleted, err := client.Models.DeleteTyped(context.Background(), "model-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
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

func TestDeploymentsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/deployments":
			query := r.URL.Query()
			for key, want := range map[string]string{
				"deploymentId":               "dep-1",
				"disableAutoDeploy":          "true",
				"disableSpeculativeDecoding": "true",
				"skipImageTagValidation":     "true",
				"skipShapeValidation":        "true",
				"validateOnly":               "true",
			} {
				if got := query.Get(key); got != want {
					t.Errorf("%s = %q", key, got)
				}
			}
			for _, key := range []string{"account_id", "deployment_id", "disable_auto_deploy", "disable_speculative_decoding", "skip_image_tag_validation", "skip_shape_validation", "validate_only"} {
				if got := query.Get(key); got != "" {
					t.Errorf("unexpected query %s=%q", key, got)
				}
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			for _, key := range []string{"account_id", "deploymentId", "deployment_id", "disableAutoDeploy", "disableSpeculativeDecoding", "skipImageTagValidation", "skipShapeValidation", "validateOnly"} {
				if _, ok := body[key]; ok {
					t.Errorf("create body should not contain %q: %#v", key, body)
				}
			}
			assertDeploymentPayloadUsesAliases(t, body)
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/deployments/dep-1",
				"baseModel":   "accounts/fireworks/models/base",
				"displayName": "Deployment One",
			})

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/accounts/acct/deployments/dep-1":
			query := r.URL.Query()
			if got := query.Get("skipShapeValidation"); got != "true" {
				t.Errorf("skipShapeValidation = %q", got)
			}
			if got := query.Get("skip_shape_validation"); got != "" {
				t.Errorf("unexpected skip_shape_validation = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			for _, key := range []string{"account_id", "skipShapeValidation", "skip_shape_validation"} {
				if _, ok := body[key]; ok {
					t.Errorf("update body should not contain %q: %#v", key, body)
				}
			}
			assertDeploymentPayloadUsesAliases(t, body)
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/deployments/dep-1",
				"baseModel":   "accounts/fireworks/models/base",
				"displayName": "Deployment One Updated",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/deployments":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "state=READY" {
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
			if got := query.Get("showDeleted"); got != "true" {
				t.Errorf("showDeleted = %q", got)
			}
			if got := query.Get("show_deleted"); got != "" {
				t.Errorf("unexpected show_deleted = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"deployments": []JSON{
					{"name": "accounts/acct/deployments/dep-1", "baseModel": "accounts/fireworks/models/base", "state": "READY"},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/deployments/dep-1":
			if got := r.URL.Query().Get("readMask"); got != "name,state" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":      "accounts/acct/deployments/dep-1",
				"baseModel": "accounts/fireworks/models/base",
				"state":     "READY",
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/deployments/dep-1":
			query := r.URL.Query()
			if got := query.Get("hard"); got != "true" {
				t.Errorf("hard = %q", got)
			}
			if got := query.Get("ignoreChecks"); got != "true" {
				t.Errorf("ignoreChecks = %q", got)
			}
			if got := query.Get("ignore_checks"); got != "" {
				t.Errorf("unexpected ignore_checks = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{"deleted": true})

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/accounts/acct/deployments/dep-1:scale":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode scale body: %v", err)
			}
			if body["replicaCount"] != float64(0) {
				t.Errorf("replicaCount = %#v", body["replicaCount"])
			}
			for _, key := range []string{"account_id", "replica_count"} {
				if _, ok := body[key]; ok {
					t.Errorf("scale body should not contain %q: %#v", key, body)
				}
			}
			_ = json.NewEncoder(w).Encode(JSON{"scaled": true})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/deployments/dep-1:undelete":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode undelete body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("undelete body should not contain account_id: %#v", body)
			}
			if body["restoreReason"] != "manual" {
				t.Errorf("restoreReason = %#v", body["restoreReason"])
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":      "accounts/acct/deployments/dep-1",
				"baseModel": "accounts/fireworks/models/base",
				"state":     "READY",
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
	created, err := client.Deployments.CreateTyped(context.Background(), testDeploymentCreateParams("acct", "Deployment One"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/deployments/dep-1" {
		t.Fatalf("created = %#v", created)
	}

	updated, err := client.Deployments.UpdateTyped(context.Background(), "dep-1", testDeploymentUpdateParams("acct", "Deployment One Updated"))
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName == nil || *updated.DisplayName != "Deployment One Updated" {
		t.Fatalf("updated = %#v", updated)
	}

	page, err := client.Deployments.ListTyped(context.Background(), fwtypes.DeploymentListParams{
		AccountID:   "acct",
		Filter:      "state=READY",
		OrderBy:     "create_time desc",
		PageSize:    0,
		PageToken:   "cursor-1",
		ReadMask:    "name,state",
		ShowDeleted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.DeploymentsPage = page
	if len(page.Deployments) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.Deployments.GetTyped(context.Background(), "dep-1", fwtypes.DeploymentGetParams{
		AccountID: "acct",
		ReadMask:  "name,state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/deployments/dep-1" {
		t.Fatalf("got = %#v", got)
	}

	deleted, err := client.Deployments.DeleteTyped(context.Background(), "dep-1", fwtypes.DeploymentDeleteParams{
		AccountID:    "acct",
		Hard:         true,
		IgnoreChecks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
	}

	scaled, err := client.Deployments.ScaleTyped(context.Background(), "dep-1", fwtypes.DeploymentScaleParams{
		AccountID:    "acct",
		ReplicaCount: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if scaled["scaled"] != true {
		t.Fatalf("scaled = %#v", scaled)
	}

	undeleted, err := client.Deployments.UndeleteTyped(context.Background(), "dep-1", JSON{
		"account_id":     "acct",
		"restore_reason": "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if undeleted.Name == nil || *undeleted.Name != "accounts/acct/deployments/dep-1" {
		t.Fatalf("undeleted = %#v", undeleted)
	}
}

func TestDeploymentShapesTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/deploymentShapes":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "accelerator_type=A100" {
				t.Errorf("filter = %q", got)
			}
			if got := query.Get("orderBy"); got != "name desc" {
				t.Errorf("orderBy = %q", got)
			}
			if got := query.Get("pageSize"); got != "0" {
				t.Errorf("pageSize = %q", got)
			}
			if got := query.Get("pageToken"); got != "cursor-1" {
				t.Errorf("pageToken = %q", got)
			}
			if got := query.Get("readMask"); got != "name,displayName" {
				t.Errorf("readMask = %q", got)
			}
			if got := query.Get("targetModel"); got != "accounts/acct/models/model-1" {
				t.Errorf("targetModel = %q", got)
			}
			for _, key := range []string{"account_id", "order_by", "page_size", "page_token", "read_mask", "target_model"} {
				if got := query.Get(key); got != "" {
					t.Errorf("unexpected query %s=%q", key, got)
				}
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"deploymentShapes": []JSON{
					{
						"name":             "accounts/acct/deploymentShapes/shape-1",
						"baseModel":        "accounts/fireworks/models/base",
						"displayName":      "Shape One",
						"acceleratorCount": 1,
					},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/deploymentShapes/shape-1":
			if got := r.URL.Query().Get("readMask"); got != "name,displayName" {
				t.Errorf("readMask = %q", got)
			}
			if got := r.URL.Query().Get("read_mask"); got != "" {
				t.Errorf("unexpected read_mask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":             "accounts/acct/deploymentShapes/shape-1",
				"baseModel":        "accounts/fireworks/models/base",
				"displayName":      "Shape One",
				"acceleratorCount": 1,
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/deploymentShapes/shape-1/versions":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "validated=true" {
				t.Errorf("filter = %q", got)
			}
			if got := query.Get("orderBy"); got != "create_time desc" {
				t.Errorf("orderBy = %q", got)
			}
			if got := query.Get("pageSize"); got != "0" {
				t.Errorf("pageSize = %q", got)
			}
			if got := query.Get("pageToken"); got != "version-cursor-1" {
				t.Errorf("pageToken = %q", got)
			}
			if got := query.Get("readMask"); got != "name,validated" {
				t.Errorf("readMask = %q", got)
			}
			for _, key := range []string{"account_id", "deployment_shape_id", "order_by", "page_size", "page_token", "read_mask"} {
				if got := query.Get(key); got != "" {
					t.Errorf("unexpected query %s=%q", key, got)
				}
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"deploymentShapeVersions": []JSON{
					{
						"name":      "accounts/acct/deploymentShapes/shape-1/versions/version-1",
						"validated": true,
						"snapshot":  JSON{"name": "accounts/acct/deploymentShapes/shape-1"},
					},
				},
				"nextPageToken": "version-cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/deploymentShapes/shape-1/versions/version-1":
			if got := r.URL.Query().Get("readMask"); got != "name,validated" {
				t.Errorf("readMask = %q", got)
			}
			for _, key := range []string{"account_id", "deployment_shape_id", "read_mask"} {
				if got := r.URL.Query().Get(key); got != "" {
					t.Errorf("unexpected query %s=%q", key, got)
				}
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":      "accounts/acct/deploymentShapes/shape-1/versions/version-1",
				"validated": true,
				"snapshot":  JSON{"name": "accounts/acct/deploymentShapes/shape-1"},
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

	shapes, err := client.DeploymentShapes.ListTyped(context.Background(), fwtypes.DeploymentShapeListParams{
		AccountID:   "acct",
		Filter:      "accelerator_type=A100",
		OrderBy:     "name desc",
		PageSize:    0,
		PageToken:   "cursor-1",
		ReadMask:    "name,displayName",
		TargetModel: "accounts/acct/models/model-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.DeploymentShapesPage = shapes
	if len(shapes.DeploymentShapes) != 1 || shapes.NextPageToken == nil || *shapes.NextPageToken != "cursor-2" {
		t.Fatalf("shapes = %#v", shapes)
	}

	shape, err := client.DeploymentShapes.GetTyped(context.Background(), "shape-1", fwtypes.DeploymentShapeGetParams{
		AccountID: "acct",
		ReadMask:  "name,displayName",
	})
	if err != nil {
		t.Fatal(err)
	}
	if shape.Name == nil || *shape.Name != "accounts/acct/deploymentShapes/shape-1" {
		t.Fatalf("shape = %#v", shape)
	}

	versions, err := client.DeploymentShapeVersions.ListTyped(context.Background(), "shape-1", fwtypes.DeploymentShapeVersionListParams{
		AccountID: "acct",
		Filter:    "validated=true",
		OrderBy:   "create_time desc",
		PageSize:  0,
		PageToken: "version-cursor-1",
		ReadMask:  "name,validated",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.DeploymentShapeVersionsPage = versions
	if len(versions.DeploymentShapeVersions) != 1 || versions.NextPageToken == nil || *versions.NextPageToken != "version-cursor-2" {
		t.Fatalf("versions = %#v", versions)
	}

	version, err := client.DeploymentShapeVersions.GetTyped(context.Background(), "shape-1", "version-1", fwtypes.DeploymentShapeVersionGetParams{
		AccountID:         "acct",
		DeploymentShapeID: "shape-1",
		ReadMask:          "name,validated",
	})
	if err != nil {
		t.Fatal(err)
	}
	if version.Name == nil || *version.Name != "accounts/acct/deploymentShapes/shape-1/versions/version-1" {
		t.Fatalf("version = %#v", version)
	}
}

func TestLoraTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/accounts/acct/deployedModels/lora-1":
			if got := r.URL.RawQuery; got != "" {
				t.Errorf("update query = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("update body should not contain account_id: %#v", body)
			}
			assertLoraPayloadUsesAliases(t, body)
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/deployedModels/lora-1",
				"displayName": "LoRA One Updated",
				"model":       "accounts/acct/models/lora",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/deployedModels":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "serverless=true" {
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
			if got := query.Get("readMask"); got != "name,model" {
				t.Errorf("readMask = %q", got)
			}
			for _, key := range []string{"account_id", "order_by", "page_size", "page_token", "read_mask"} {
				if got := query.Get(key); got != "" {
					t.Errorf("unexpected query %s=%q", key, got)
				}
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"deployedModels": []JSON{
					{"name": "accounts/acct/deployedModels/lora-1", "displayName": "LoRA One", "model": "accounts/acct/models/lora"},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/deployedModels/lora-1":
			if got := r.URL.Query().Get("readMask"); got != "name,model" {
				t.Errorf("readMask = %q", got)
			}
			if got := r.URL.Query().Get("read_mask"); got != "" {
				t.Errorf("unexpected read_mask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/deployedModels/lora-1",
				"displayName": "LoRA One",
				"model":       "accounts/acct/models/lora",
			})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/deployedModels":
			query := r.URL.Query()
			if got := query.Get("replaceMergedAddon"); got != "true" {
				t.Errorf("replaceMergedAddon = %q", got)
			}
			if got := query.Get("replace_merged_addon"); got != "" {
				t.Errorf("unexpected replace_merged_addon = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode load body: %v", err)
			}
			for _, key := range []string{"account_id", "replaceMergedAddon", "replace_merged_addon"} {
				if _, ok := body[key]; ok {
					t.Errorf("load body should not contain %q: %#v", key, body)
				}
			}
			assertLoraPayloadUsesAliases(t, body)
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/deployedModels/lora-1",
				"displayName": "LoRA One",
				"model":       "accounts/acct/models/lora",
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/deployedModels/lora-1":
			if got := r.URL.RawQuery; got != "" {
				t.Errorf("unload query = %q", got)
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

	updated, err := client.Lora.UpdateTyped(context.Background(), "lora-1", testLoraUpdateParams("acct", "LoRA One Updated"))
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName == nil || *updated.DisplayName != "LoRA One Updated" {
		t.Fatalf("updated = %#v", updated)
	}

	page, err := client.Lora.ListTyped(context.Background(), fwtypes.LoraListParams{
		AccountID: "acct",
		Filter:    "serverless=true",
		OrderBy:   "create_time desc",
		PageSize:  0,
		PageToken: "cursor-1",
		ReadMask:  "name,model",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.LoraPage = page
	if len(page.DeployedModels) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.Lora.GetTyped(context.Background(), "lora-1", fwtypes.LoraGetParams{
		AccountID: "acct",
		ReadMask:  "name,model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/deployedModels/lora-1" {
		t.Fatalf("got = %#v", got)
	}

	loaded, err := client.Lora.LoadTyped(context.Background(), testLoraLoadParams("acct", "LoRA One"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name == nil || *loaded.Name != "accounts/acct/deployedModels/lora-1" {
		t.Fatalf("loaded = %#v", loaded)
	}

	deleted, err := client.Lora.UnloadTyped(context.Background(), "lora-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
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

func TestSecretsTypedAndGenericAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/secrets":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("create body should not contain account_id: %#v", body)
			}
			if _, ok := body["key_name"]; ok {
				t.Errorf("unexpected key_name body key: %#v", body)
			}
			if body["keyName"] != "api-key" || body["name"] != "secret-1" || body["value"] != "sk-1234567890abcdef" {
				t.Errorf("create body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"keyName": "api-key",
				"name":    "accounts/acct/secrets/secret-1",
				"value":   "sk-1234567890abcdef",
			})

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/accounts/acct/secrets/secret-1":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("update body should not contain account_id: %#v", body)
			}
			if _, ok := body["key_name"]; ok {
				t.Errorf("unexpected key_name body key: %#v", body)
			}
			if body["keyName"] != "api-key" || body["value"] != "sk-updated" {
				t.Errorf("update body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"keyName": "api-key",
				"name":    "accounts/acct/secrets/secret-1",
				"value":   "sk-updated",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/secrets":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "name=secret-1" {
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
			if got := query.Get("readMask"); got != "name,keyName" {
				t.Errorf("readMask = %q", got)
			}
			if got := query.Get("account_id"); got != "" {
				t.Errorf("account_id should not be query param, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"secrets":       []JSON{{"name": "accounts/acct/secrets/secret-1", "keyName": "api-key"}},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/secrets/secret-1":
			if got := r.URL.Query().Get("readMask"); got != "name,keyName" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"keyName": "api-key",
				"name":    "accounts/acct/secrets/secret-1",
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/secrets/secret-1":
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
	created, err := client.Secrets.CreateTyped(context.Background(), fwtypes.SecretCreateParams{
		AccountID: "acct",
		KeyName:   "api-key",
		Name:      "secret-1",
		Value:     "sk-1234567890abcdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "accounts/acct/secrets/secret-1" || created.KeyName != "api-key" {
		t.Fatalf("created = %#v", created)
	}

	genericCreated, err := client.Secrets.Create(context.Background(), JSON{
		"account_id": "acct",
		"key_name":   "api-key",
		"name":       "secret-1",
		"value":      "sk-1234567890abcdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if genericCreated["name"] != "accounts/acct/secrets/secret-1" {
		t.Fatalf("genericCreated = %#v", genericCreated)
	}

	updated, err := client.Secrets.UpdateTyped(context.Background(), "secret-1", fwtypes.SecretUpdateParams{
		AccountID: "acct",
		KeyName:   "api-key",
		Value:     "sk-updated",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "accounts/acct/secrets/secret-1" {
		t.Fatalf("updated = %#v", updated)
	}

	genericUpdated, err := client.Secrets.Update(context.Background(), "secret-1", JSON{
		"account_id": "acct",
		"key_name":   "api-key",
		"value":      "sk-updated",
	})
	if err != nil {
		t.Fatal(err)
	}
	if genericUpdated["name"] != "accounts/acct/secrets/secret-1" {
		t.Fatalf("genericUpdated = %#v", genericUpdated)
	}

	page, err := client.Secrets.ListTyped(context.Background(), fwtypes.SecretListParams{
		AccountID: "acct",
		Filter:    "name=secret-1",
		OrderBy:   "create_time desc",
		PageSize:  0,
		PageToken: "cursor-1",
		ReadMask:  "name,keyName",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.SecretsPage = page
	if len(page.Secrets) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.Secrets.GetTyped(context.Background(), "secret-1", fwtypes.SecretGetParams{
		AccountID: "acct",
		ReadMask:  "name,keyName",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "accounts/acct/secrets/secret-1" {
		t.Fatalf("got = %#v", got)
	}

	deleted, err := client.Secrets.DeleteTyped(context.Background(), "secret-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
	}
}

func TestSupervisedFineTuningJobsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/supervisedFineTuningJobs":
			query := r.URL.Query()
			if got := query.Get("supervisedFineTuningJobId"); got != "sft-1" {
				t.Errorf("supervisedFineTuningJobId = %q", got)
			}
			if got := query.Get("supervised_fine_tuning_job_id"); got != "" {
				t.Errorf("unexpected snake query = %q", got)
			}

			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			for _, key := range []string{"account_id", "supervisedFineTuningJobId", "supervised_fine_tuning_job_id"} {
				if _, ok := body[key]; ok {
					t.Errorf("create body should not contain %q: %#v", key, body)
				}
			}
			for _, key := range []string{
				"dataset",
				"awsS3Config",
				"azureBlobStorageConfig",
				"baseModel",
				"batchSize",
				"batchSizeSamples",
				"displayName",
				"earlyStop",
				"epochs",
				"evalAutoCarveout",
				"evaluationDataset",
				"gradientAccumulationSteps",
				"isTurbo",
				"jinjaTemplate",
				"learningRate",
				"learningRateWarmupSteps",
				"loraRank",
				"maxContextLength",
				"metricsFileSignedUrl",
				"mtpEnabled",
				"mtpFreezeBaseModel",
				"mtpNumDraftTokens",
				"nodes",
				"optimizerWeightDecay",
				"outputModel",
				"region",
				"usePurpose",
				"wandbConfig",
				"warmStartFrom",
			} {
				if _, ok := body[key]; !ok {
					t.Errorf("create body missing %q: %#v", key, body)
				}
			}
			if body["dataset"] != "dataset-1" || body["baseModel"] != "accounts/fireworks/models/base" || body["region"] != "REGION_UNSPECIFIED" {
				t.Errorf("create scalar body = %#v", body)
			}
			if body["batchSize"] != float64(0) || body["learningRate"] != float64(0) || body["nodes"] != float64(0) {
				t.Errorf("zero-valued numeric params were not preserved: %#v", body)
			}
			if body["earlyStop"] != true || body["evalAutoCarveout"] != true || body["isTurbo"] != true || body["mtpEnabled"] != true || body["mtpFreezeBaseModel"] != true {
				t.Errorf("boolean params = %#v", body)
			}

			aws, ok := body["awsS3Config"].(map[string]any)
			if !ok || aws["credentialsSecret"] != "aws-secret" || aws["iamRoleArn"] != "arn:aws:iam::123:role/fireworks" {
				t.Errorf("awsS3Config = %#v", body["awsS3Config"])
			}
			azure, ok := body["azureBlobStorageConfig"].(map[string]any)
			if !ok || azure["credentialsSecret"] != "azure-secret" || azure["managedIdentityClientId"] != "client-id" || azure["tenantId"] != "tenant-id" {
				t.Errorf("azureBlobStorageConfig = %#v", body["azureBlobStorageConfig"])
			}
			wandb, ok := body["wandbConfig"].(map[string]any)
			if !ok || wandb["apiKey"] != "wandb-key" || wandb["enabled"] != true || wandb["runId"] != "run-1" {
				t.Errorf("wandbConfig = %#v", body["wandbConfig"])
			}

			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/supervisedFineTuningJobs/sft-1",
				"dataset":     "dataset-1",
				"displayName": "Display Name",
				"state":       "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/supervisedFineTuningJobs":
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
				"supervisedFineTuningJobs": []JSON{
					{"name": "accounts/acct/supervisedFineTuningJobs/sft-1", "dataset": "dataset-1", "state": "JOB_STATE_RUNNING"},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/supervisedFineTuningJobs/sft-1":
			if got := r.URL.Query().Get("readMask"); got != "name,state" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":    "accounts/acct/supervisedFineTuningJobs/sft-1",
				"dataset": "dataset-1",
				"state":   "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/supervisedFineTuningJobs/sft-1":
			_ = json.NewEncoder(w).Encode(JSON{"deleted": true})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/supervisedFineTuningJobs/sft-1:resume":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode resume body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("resume body should not contain account_id: %#v", body)
			}
			if body["reason"] != "manual" {
				t.Errorf("resume body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":    "accounts/acct/supervisedFineTuningJobs/sft-1",
				"dataset": "dataset-1",
				"state":   "JOB_STATE_RUNNING",
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
	created, err := client.SupervisedFineTuningJobs.CreateTyped(context.Background(), fwtypes.SupervisedFineTuningJobCreateParams{
		AccountID:                 "acct",
		Dataset:                   "dataset-1",
		SupervisedFineTuningJobID: "sft-1",
		AwsS3Config: fwtypes.SupervisedFineTuningJobCreateParamsAwsS3Config{
			CredentialsSecret: "aws-secret",
			IamRoleArn:        "arn:aws:iam::123:role/fireworks",
		},
		AzureBlobStorageConfig: fwtypes.SupervisedFineTuningJobCreateParamsAzureBlobStorageConfig{
			CredentialsSecret:       "azure-secret",
			ManagedIdentityClientID: "client-id",
			TenantID:                "tenant-id",
		},
		BaseModel:                 "accounts/fireworks/models/base",
		BatchSize:                 0,
		BatchSizeSamples:          0,
		DisplayName:               "Display Name",
		EarlyStop:                 true,
		Epochs:                    0,
		EvalAutoCarveout:          true,
		EvaluationDataset:         "eval-dataset",
		GradientAccumulationSteps: 0,
		IsTurbo:                   true,
		JinjaTemplate:             "template",
		LearningRate:              0,
		LearningRateWarmupSteps:   0,
		LoraRank:                  0,
		MaxContextLength:          0,
		MetricsFileSignedURL:      "gs://metrics.jsonl",
		MtpEnabled:                true,
		MtpFreezeBaseModel:        true,
		MtpNumDraftTokens:         0,
		Nodes:                     0,
		OptimizerWeightDecay:      0,
		OutputModel:               "output-model",
		Region:                    "REGION_UNSPECIFIED",
		UsePurpose:                "pilot",
		WandbConfig: fwtypes.SharedParamsWandbConfig{
			APIKey:  "wandb-key",
			Enabled: true,
			Entity:  "entity",
			Project: "project",
			RunID:   "run-1",
		},
		WarmStartFrom: "accounts/acct/models/warm-start",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/supervisedFineTuningJobs/sft-1" {
		t.Fatalf("created = %#v", created)
	}

	page, err := client.SupervisedFineTuningJobs.ListTyped(context.Background(), fwtypes.SupervisedFineTuningJobListParams{
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
	var _ *fwtypes.SupervisedFineTuningJobsPage = page
	if len(page.SupervisedFineTuningJobs) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.SupervisedFineTuningJobs.GetTyped(context.Background(), "sft-1", fwtypes.SupervisedFineTuningJobGetParams{
		AccountID: "acct",
		ReadMask:  "name,state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/supervisedFineTuningJobs/sft-1" {
		t.Fatalf("got = %#v", got)
	}

	deleted, err := client.SupervisedFineTuningJobs.DeleteTyped(context.Background(), "sft-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
	}

	resumed, err := client.SupervisedFineTuningJobs.ResumeTyped(context.Background(), "sft-1", JSON{
		"account_id": "acct",
		"reason":     "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Name == nil || *resumed.Name != "accounts/acct/supervisedFineTuningJobs/sft-1" {
		t.Fatalf("resumed = %#v", resumed)
	}
}

func TestDPOJobsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/dpoJobs":
			query := r.URL.Query()
			if got := query.Get("dpoJobId"); got != "dpo-1" {
				t.Errorf("dpoJobId = %q", got)
			}
			if got := query.Get("dpo_job_id"); got != "" {
				t.Errorf("unexpected snake query = %q", got)
			}

			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			for _, key := range []string{"account_id", "dpoJobId", "dpo_job_id"} {
				if _, ok := body[key]; ok {
					t.Errorf("create body should not contain %q: %#v", key, body)
				}
			}
			for _, key := range []string{
				"dataset",
				"awsS3Config",
				"azureBlobStorageConfig",
				"displayName",
				"lossConfig",
				"trainingConfig",
				"wandbConfig",
			} {
				if _, ok := body[key]; !ok {
					t.Errorf("create body missing %q: %#v", key, body)
				}
			}
			if body["dataset"] != "dataset-1" || body["displayName"] != "Display Name" {
				t.Errorf("create scalar body = %#v", body)
			}
			aws, ok := body["awsS3Config"].(map[string]any)
			if !ok || aws["credentialsSecret"] != "aws-secret" || aws["iamRoleArn"] != "arn:aws:iam::123:role/fireworks" {
				t.Errorf("awsS3Config = %#v", body["awsS3Config"])
			}
			azure, ok := body["azureBlobStorageConfig"].(map[string]any)
			if !ok || azure["credentialsSecret"] != "azure-secret" || azure["managedIdentityClientId"] != "client-id" || azure["tenantId"] != "tenant-id" {
				t.Errorf("azureBlobStorageConfig = %#v", body["azureBlobStorageConfig"])
			}
			loss, ok := body["lossConfig"].(map[string]any)
			if !ok || loss["klBeta"] != float64(0) || loss["method"] != "METHOD_UNSPECIFIED" {
				t.Errorf("lossConfig = %#v", body["lossConfig"])
			}
			training, ok := body["trainingConfig"].(map[string]any)
			if !ok {
				t.Fatalf("trainingConfig = %#v", body["trainingConfig"])
			}
			for _, key := range []string{
				"baseModel",
				"batchSize",
				"batchSizeSamples",
				"epochs",
				"gradientAccumulationSteps",
				"jinjaTemplate",
				"learningRate",
				"learningRateWarmupSteps",
				"loraRank",
				"maxContextLength",
				"optimizerWeightDecay",
				"outputModel",
				"region",
				"warmStartFrom",
			} {
				if _, ok := training[key]; !ok {
					t.Errorf("trainingConfig missing %q: %#v", key, training)
				}
			}
			if training["batchSize"] != float64(0) || training["learningRate"] != float64(0) || training["optimizerWeightDecay"] != float64(0) {
				t.Errorf("zero training params were not preserved: %#v", training)
			}
			if training["baseModel"] != "accounts/fireworks/models/base" || training["region"] != "REGION_UNSPECIFIED" {
				t.Errorf("trainingConfig scalars = %#v", training)
			}
			wandb, ok := body["wandbConfig"].(map[string]any)
			if !ok || wandb["apiKey"] != "wandb-key" || wandb["enabled"] != true || wandb["runId"] != "run-1" {
				t.Errorf("wandbConfig = %#v", body["wandbConfig"])
			}

			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/dpoJobs/dpo-1",
				"dataset":     "dataset-1",
				"displayName": "Display Name",
				"state":       "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/dpoJobs":
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
				"dpoJobs":       []JSON{{"name": "accounts/acct/dpoJobs/dpo-1", "dataset": "dataset-1", "state": "JOB_STATE_RUNNING"}},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/dpoJobs/dpo-1":
			if got := r.URL.Query().Get("readMask"); got != "name,state" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":    "accounts/acct/dpoJobs/dpo-1",
				"dataset": "dataset-1",
				"state":   "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/dpoJobs/dpo-1:getMetricsFileEndpoint":
			if got := r.URL.RawQuery; got != "" {
				t.Errorf("metrics query = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{"signedUrl": "gs://metrics.jsonl"})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/dpoJobs/dpo-1":
			_ = json.NewEncoder(w).Encode(JSON{"deleted": true})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/dpoJobs/dpo-1:resume":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode resume body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("resume body should not contain account_id: %#v", body)
			}
			if body["reason"] != "manual" {
				t.Errorf("resume body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":    "accounts/acct/dpoJobs/dpo-1",
				"dataset": "dataset-1",
				"state":   "JOB_STATE_RUNNING",
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
	created, err := client.DPOJobs.CreateTyped(context.Background(), fwtypes.DpoJobCreateParams{
		AccountID: "acct",
		Dataset:   "dataset-1",
		DPOJobID:  "dpo-1",
		AwsS3Config: fwtypes.DPOJobCreateParamsAwsS3Config{
			CredentialsSecret: "aws-secret",
			IamRoleArn:        "arn:aws:iam::123:role/fireworks",
		},
		AzureBlobStorageConfig: fwtypes.DPOJobCreateParamsAzureBlobStorageConfig{
			CredentialsSecret:       "azure-secret",
			ManagedIdentityClientID: "client-id",
			TenantID:                "tenant-id",
		},
		DisplayName: "Display Name",
		LossConfig: fwtypes.SharedParamsReinforcementLearningLossConfig{
			KlBeta: 0,
			Method: "METHOD_UNSPECIFIED",
		},
		TrainingConfig: fwtypes.SharedParamsTrainingConfig{
			BaseModel:                 "accounts/fireworks/models/base",
			BatchSize:                 0,
			BatchSizeSamples:          0,
			Epochs:                    0,
			GradientAccumulationSteps: 0,
			JinjaTemplate:             "template",
			LearningRate:              0,
			LearningRateWarmupSteps:   0,
			LoraRank:                  0,
			MaxContextLength:          0,
			OptimizerWeightDecay:      0,
			OutputModel:               "output-model",
			Region:                    "REGION_UNSPECIFIED",
			WarmStartFrom:             "accounts/acct/models/warm-start",
		},
		WandbConfig: fwtypes.SharedParamsWandbConfig{
			APIKey:  "wandb-key",
			Enabled: true,
			Entity:  "entity",
			Project: "project",
			RunID:   "run-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/dpoJobs/dpo-1" {
		t.Fatalf("created = %#v", created)
	}

	page, err := client.DPOJobs.ListTyped(context.Background(), fwtypes.DpoJobListParams{
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
	var _ *fwtypes.DPOJobsPage = page
	if len(page.DPOJobs) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.DPOJobs.GetTyped(context.Background(), "dpo-1", fwtypes.DpoJobGetParams{
		AccountID: "acct",
		ReadMask:  "name,state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/dpoJobs/dpo-1" {
		t.Fatalf("got = %#v", got)
	}

	metrics, err := client.DPOJobs.GetMetricsFileEndpointTyped(context.Background(), "dpo-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if metrics.SignedURL == nil || *metrics.SignedURL != "gs://metrics.jsonl" {
		t.Fatalf("metrics = %#v", metrics)
	}

	deleted, err := client.DPOJobs.DeleteTyped(context.Background(), "dpo-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
	}

	resumed, err := client.DPOJobs.ResumeTyped(context.Background(), "dpo-1", JSON{
		"account_id": "acct",
		"reason":     "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Name == nil || *resumed.Name != "accounts/acct/dpoJobs/dpo-1" {
		t.Fatalf("resumed = %#v", resumed)
	}
}

func TestReinforcementFineTuningJobsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/reinforcementFineTuningJobs":
			query := r.URL.Query()
			if got := query.Get("reinforcementFineTuningJobId"); got != "rft-1" {
				t.Errorf("reinforcementFineTuningJobId = %q", got)
			}
			if got := query.Get("reinforcement_fine_tuning_job_id"); got != "" {
				t.Errorf("unexpected snake query = %q", got)
			}

			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			for _, key := range []string{"account_id", "reinforcementFineTuningJobId", "reinforcement_fine_tuning_job_id"} {
				if _, ok := body[key]; ok {
					t.Errorf("create body should not contain %q: %#v", key, body)
				}
			}
			for _, key := range []string{
				"dataset",
				"evaluator",
				"awsS3Config",
				"azureBlobStorageConfig",
				"chunkSize",
				"displayName",
				"evalAutoCarveout",
				"evaluationDataset",
				"inferenceParameters",
				"lossConfig",
				"maxConcurrentEvaluations",
				"maxConcurrentRollouts",
				"maxInferenceReplicaCount",
				"mcpServer",
				"nodeCount",
				"trainingConfig",
				"wandbConfig",
			} {
				if _, ok := body[key]; !ok {
					t.Errorf("create body missing %q: %#v", key, body)
				}
			}
			if body["dataset"] != "dataset-1" || body["evaluator"] != "evaluator-1" || body["displayName"] != "Display Name" {
				t.Errorf("create scalar body = %#v", body)
			}
			if body["chunkSize"] != float64(0) || body["maxConcurrentEvaluations"] != float64(0) || body["nodeCount"] != float64(0) {
				t.Errorf("zero-valued top-level params were not preserved: %#v", body)
			}
			if body["evalAutoCarveout"] != true {
				t.Errorf("evalAutoCarveout = %#v", body["evalAutoCarveout"])
			}
			aws, ok := body["awsS3Config"].(map[string]any)
			if !ok || aws["credentialsSecret"] != "aws-secret" || aws["iamRoleArn"] != "arn:aws:iam::123:role/fireworks" {
				t.Errorf("awsS3Config = %#v", body["awsS3Config"])
			}
			azure, ok := body["azureBlobStorageConfig"].(map[string]any)
			if !ok || azure["credentialsSecret"] != "azure-secret" || azure["managedIdentityClientId"] != "client-id" || azure["tenantId"] != "tenant-id" {
				t.Errorf("azureBlobStorageConfig = %#v", body["azureBlobStorageConfig"])
			}
			inference, ok := body["inferenceParameters"].(map[string]any)
			if !ok {
				t.Fatalf("inferenceParameters = %#v", body["inferenceParameters"])
			}
			for _, key := range []string{"extraBody", "maxOutputTokens", "responseCandidatesCount", "temperature", "topK", "topP"} {
				if _, ok := inference[key]; !ok {
					t.Errorf("inferenceParameters missing %q: %#v", key, inference)
				}
			}
			if inference["maxOutputTokens"] != float64(0) || inference["temperature"] != float64(0) || inference["topK"] != float64(0) {
				t.Errorf("zero inference params were not preserved: %#v", inference)
			}
			loss, ok := body["lossConfig"].(map[string]any)
			if !ok || loss["klBeta"] != float64(0) || loss["method"] != "METHOD_UNSPECIFIED" {
				t.Errorf("lossConfig = %#v", body["lossConfig"])
			}
			training, ok := body["trainingConfig"].(map[string]any)
			if !ok {
				t.Fatalf("trainingConfig = %#v", body["trainingConfig"])
			}
			for _, key := range []string{
				"baseModel",
				"batchSize",
				"batchSizeSamples",
				"epochs",
				"gradientAccumulationSteps",
				"jinjaTemplate",
				"learningRate",
				"learningRateWarmupSteps",
				"loraRank",
				"maxContextLength",
				"optimizerWeightDecay",
				"outputModel",
				"region",
				"warmStartFrom",
			} {
				if _, ok := training[key]; !ok {
					t.Errorf("trainingConfig missing %q: %#v", key, training)
				}
			}
			if training["batchSize"] != float64(0) || training["learningRate"] != float64(0) || training["optimizerWeightDecay"] != float64(0) {
				t.Errorf("zero training params were not preserved: %#v", training)
			}
			wandb, ok := body["wandbConfig"].(map[string]any)
			if !ok || wandb["apiKey"] != "wandb-key" || wandb["enabled"] != true || wandb["runId"] != "run-1" {
				t.Errorf("wandbConfig = %#v", body["wandbConfig"])
			}

			_ = json.NewEncoder(w).Encode(JSON{
				"name":      "accounts/acct/reinforcementFineTuningJobs/rft-1",
				"dataset":   "dataset-1",
				"evaluator": "evaluator-1",
				"state":     "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/reinforcementFineTuningJobs":
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
				"reinforcementFineTuningJobs": []JSON{
					{"name": "accounts/acct/reinforcementFineTuningJobs/rft-1", "dataset": "dataset-1", "evaluator": "evaluator-1"},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/reinforcementFineTuningJobs/rft-1":
			if got := r.URL.Query().Get("readMask"); got != "name,state" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":      "accounts/acct/reinforcementFineTuningJobs/rft-1",
				"dataset":   "dataset-1",
				"evaluator": "evaluator-1",
				"state":     "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/reinforcementFineTuningJobs/rft-1":
			_ = json.NewEncoder(w).Encode(JSON{"deleted": true})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/reinforcementFineTuningJobs/rft-1:cancel":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode cancel body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("cancel body should not contain account_id: %#v", body)
			}
			if body["reason"] != "manual" {
				t.Errorf("cancel body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{"cancelled": true})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/reinforcementFineTuningJobs/rft-1:resume":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode resume body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("resume body should not contain account_id: %#v", body)
			}
			if body["reason"] != "manual" {
				t.Errorf("resume body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":      "accounts/acct/reinforcementFineTuningJobs/rft-1",
				"dataset":   "dataset-1",
				"evaluator": "evaluator-1",
				"state":     "JOB_STATE_RUNNING",
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
	created, err := client.ReinforcementFineTuningJobs.CreateTyped(context.Background(), fwtypes.ReinforcementFineTuningJobCreateParams{
		AccountID:                    "acct",
		Dataset:                      "dataset-1",
		Evaluator:                    "evaluator-1",
		ReinforcementFineTuningJobID: "rft-1",
		AwsS3Config: fwtypes.ReinforcementFineTuningJobCreateParamsAwsS3Config{
			CredentialsSecret: "aws-secret",
			IamRoleArn:        "arn:aws:iam::123:role/fireworks",
		},
		AzureBlobStorageConfig: fwtypes.ReinforcementFineTuningJobCreateParamsAzureBlobStorageConfig{
			CredentialsSecret:       "azure-secret",
			ManagedIdentityClientID: "client-id",
			TenantID:                "tenant-id",
		},
		ChunkSize:         0,
		DisplayName:       "Display Name",
		EvalAutoCarveout:  true,
		EvaluationDataset: "eval-dataset",
		InferenceParameters: fwtypes.ReinforcementFineTuningJobCreateParamsInferenceParameters{
			ExtraBody:               "extra",
			MaxOutputTokens:         0,
			ResponseCandidatesCount: 0,
			Temperature:             0,
			TopK:                    0,
			TopP:                    0,
		},
		LossConfig: fwtypes.SharedParamsReinforcementLearningLossConfig{
			KlBeta: 0,
			Method: "METHOD_UNSPECIFIED",
		},
		MaxConcurrentEvaluations: 0,
		MaxConcurrentRollouts:    0,
		MaxInferenceReplicaCount: 0,
		McpServer:                "mcp-server",
		NodeCount:                0,
		TrainingConfig: fwtypes.SharedParamsTrainingConfig{
			BaseModel:                 "accounts/fireworks/models/base",
			BatchSize:                 0,
			BatchSizeSamples:          0,
			Epochs:                    0,
			GradientAccumulationSteps: 0,
			JinjaTemplate:             "template",
			LearningRate:              0,
			LearningRateWarmupSteps:   0,
			LoraRank:                  0,
			MaxContextLength:          0,
			OptimizerWeightDecay:      0,
			OutputModel:               "output-model",
			Region:                    "REGION_UNSPECIFIED",
			WarmStartFrom:             "accounts/acct/models/warm-start",
		},
		WandbConfig: fwtypes.SharedParamsWandbConfig{
			APIKey:  "wandb-key",
			Enabled: true,
			Entity:  "entity",
			Project: "project",
			RunID:   "run-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/reinforcementFineTuningJobs/rft-1" {
		t.Fatalf("created = %#v", created)
	}

	page, err := client.ReinforcementFineTuningJobs.ListTyped(context.Background(), fwtypes.ReinforcementFineTuningJobListParams{
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
	var _ *fwtypes.ReinforcementFineTuningJobsPage = page
	if len(page.ReinforcementFineTuningJobs) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.ReinforcementFineTuningJobs.GetTyped(context.Background(), "rft-1", fwtypes.ReinforcementFineTuningJobGetParams{
		AccountID: "acct",
		ReadMask:  "name,state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/reinforcementFineTuningJobs/rft-1" {
		t.Fatalf("got = %#v", got)
	}

	deleted, err := client.ReinforcementFineTuningJobs.DeleteTyped(context.Background(), "rft-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
	}

	cancelled, err := client.ReinforcementFineTuningJobs.CancelTyped(context.Background(), "rft-1", JSON{
		"account_id": "acct",
		"reason":     "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled["cancelled"] != true {
		t.Fatalf("cancelled = %#v", cancelled)
	}

	resumed, err := client.ReinforcementFineTuningJobs.ResumeTyped(context.Background(), "rft-1", JSON{
		"account_id": "acct",
		"reason":     "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Name == nil || *resumed.Name != "accounts/acct/reinforcementFineTuningJobs/rft-1" {
		t.Fatalf("resumed = %#v", resumed)
	}
}

func TestReinforcementFineTuningStepsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/rlorTrainerJobs":
			query := r.URL.Query()
			if got := query.Get("rlorTrainerJobId"); got != "rlor-1" {
				t.Errorf("rlorTrainerJobId = %q", got)
			}
			if got := query.Get("rlor_trainer_job_id"); got != "" {
				t.Errorf("unexpected snake query = %q", got)
			}

			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			for _, key := range []string{"account_id", "rlorTrainerJobId", "rlor_trainer_job_id"} {
				if _, ok := body[key]; ok {
					t.Errorf("create body should not contain %q: %#v", key, body)
				}
			}
			for _, key := range []string{
				"awsS3Config",
				"azureBlobStorageConfig",
				"dataset",
				"displayName",
				"evalAutoCarveout",
				"evaluationDataset",
				"forwardOnly",
				"hotLoadDeploymentId",
				"keepAlive",
				"lossConfig",
				"nodeCount",
				"rewardWeights",
				"rolloutDeploymentName",
				"serviceMode",
				"trainingConfig",
				"usePurpose",
				"wandbConfig",
			} {
				if _, ok := body[key]; !ok {
					t.Errorf("create body missing %q: %#v", key, body)
				}
			}
			if body["dataset"] != "dataset-1" || body["displayName"] != "Display Name" || body["hotLoadDeploymentId"] != "dep-1" {
				t.Errorf("create scalar body = %#v", body)
			}
			if body["evalAutoCarveout"] != true || body["forwardOnly"] != true || body["keepAlive"] != true || body["serviceMode"] != true {
				t.Errorf("boolean params = %#v", body)
			}
			if body["nodeCount"] != float64(0) {
				t.Errorf("nodeCount = %#v", body["nodeCount"])
			}
			rewardWeights, ok := body["rewardWeights"].([]any)
			if !ok || len(rewardWeights) != 1 || rewardWeights[0] != "reward-1" {
				t.Errorf("rewardWeights = %#v", body["rewardWeights"])
			}
			aws, ok := body["awsS3Config"].(map[string]any)
			if !ok || aws["credentialsSecret"] != "aws-secret" || aws["iamRoleArn"] != "arn:aws:iam::123:role/fireworks" {
				t.Errorf("awsS3Config = %#v", body["awsS3Config"])
			}
			azure, ok := body["azureBlobStorageConfig"].(map[string]any)
			if !ok || azure["credentialsSecret"] != "azure-secret" || azure["managedIdentityClientId"] != "client-id" || azure["tenantId"] != "tenant-id" {
				t.Errorf("azureBlobStorageConfig = %#v", body["azureBlobStorageConfig"])
			}
			loss, ok := body["lossConfig"].(map[string]any)
			if !ok || loss["klBeta"] != float64(0) || loss["method"] != "METHOD_UNSPECIFIED" {
				t.Errorf("lossConfig = %#v", body["lossConfig"])
			}
			training, ok := body["trainingConfig"].(map[string]any)
			if !ok {
				t.Fatalf("trainingConfig = %#v", body["trainingConfig"])
			}
			for _, key := range []string{
				"baseModel",
				"batchSize",
				"batchSizeSamples",
				"epochs",
				"gradientAccumulationSteps",
				"jinjaTemplate",
				"learningRate",
				"learningRateWarmupSteps",
				"loraRank",
				"maxContextLength",
				"optimizerWeightDecay",
				"outputModel",
				"region",
				"warmStartFrom",
			} {
				if _, ok := training[key]; !ok {
					t.Errorf("trainingConfig missing %q: %#v", key, training)
				}
			}
			if training["batchSize"] != float64(0) || training["learningRate"] != float64(0) || training["optimizerWeightDecay"] != float64(0) {
				t.Errorf("zero training params were not preserved: %#v", training)
			}
			wandb, ok := body["wandbConfig"].(map[string]any)
			if !ok || wandb["apiKey"] != "wandb-key" || wandb["enabled"] != true || wandb["runId"] != "run-1" {
				t.Errorf("wandbConfig = %#v", body["wandbConfig"])
			}

			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/rlorTrainerJobs/rlor-1",
				"dataset":     "dataset-1",
				"displayName": "Display Name",
				"state":       "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/rlorTrainerJobs":
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
				"rlorTrainerJobs": []JSON{
					{"name": "accounts/acct/rlorTrainerJobs/rlor-1", "dataset": "dataset-1", "state": "JOB_STATE_RUNNING"},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/rlorTrainerJobs/rlor-1":
			if got := r.URL.Query().Get("readMask"); got != "name,state" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":    "accounts/acct/rlorTrainerJobs/rlor-1",
				"dataset": "dataset-1",
				"state":   "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/rlorTrainerJobs/rlor-1":
			_ = json.NewEncoder(w).Encode(JSON{"deleted": true})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/rlorTrainerJobs/rlor-1:executeTrainStep":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode execute body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("execute body should not contain account_id: %#v", body)
			}
			if body["dataset"] != "dataset-iteration" || body["outputModel"] != "output-model" {
				t.Errorf("execute body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{"executed": true})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/rlorTrainerJobs/rlor-1:resume":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode resume body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("resume body should not contain account_id: %#v", body)
			}
			if body["reason"] != "manual" {
				t.Errorf("resume body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":    "accounts/acct/rlorTrainerJobs/rlor-1",
				"dataset": "dataset-1",
				"state":   "JOB_STATE_RUNNING",
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
	created, err := client.ReinforcementFineTuningSteps.CreateTyped(context.Background(), fwtypes.ReinforcementFineTuningStepCreateParams{
		AccountID:        "acct",
		RLORTrainerJobID: "rlor-1",
		AwsS3Config: fwtypes.ReinforcementFineTuningStepCreateParamsAwsS3Config{
			CredentialsSecret: "aws-secret",
			IamRoleArn:        "arn:aws:iam::123:role/fireworks",
		},
		AzureBlobStorageConfig: fwtypes.ReinforcementFineTuningStepCreateParamsAzureBlobStorageConfig{
			CredentialsSecret:       "azure-secret",
			ManagedIdentityClientID: "client-id",
			TenantID:                "tenant-id",
		},
		Dataset:               "dataset-1",
		DisplayName:           "Display Name",
		EvalAutoCarveout:      true,
		EvaluationDataset:     "eval-dataset",
		ForwardOnly:           true,
		HotLoadDeploymentID:   "dep-1",
		KeepAlive:             true,
		NodeCount:             0,
		RewardWeights:         []string{"reward-1"},
		RolloutDeploymentName: "rollout-deployment",
		ServiceMode:           true,
		LossConfig: fwtypes.SharedParamsReinforcementLearningLossConfig{
			KlBeta: 0,
			Method: "METHOD_UNSPECIFIED",
		},
		TrainingConfig: fwtypes.SharedParamsTrainingConfig{
			BaseModel:                 "accounts/fireworks/models/base",
			BatchSize:                 0,
			BatchSizeSamples:          0,
			Epochs:                    0,
			GradientAccumulationSteps: 0,
			JinjaTemplate:             "template",
			LearningRate:              0,
			LearningRateWarmupSteps:   0,
			LoraRank:                  0,
			MaxContextLength:          0,
			OptimizerWeightDecay:      0,
			OutputModel:               "output-model",
			Region:                    "REGION_UNSPECIFIED",
			WarmStartFrom:             "accounts/acct/models/warm-start",
		},
		UsePurpose: "pilot",
		WandbConfig: fwtypes.SharedParamsWandbConfig{
			APIKey:  "wandb-key",
			Enabled: true,
			Entity:  "entity",
			Project: "project",
			RunID:   "run-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/rlorTrainerJobs/rlor-1" {
		t.Fatalf("created = %#v", created)
	}

	page, err := client.ReinforcementFineTuningSteps.ListTyped(context.Background(), fwtypes.ReinforcementFineTuningStepListParams{
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
	var _ *fwtypes.ReinforcementFineTuningStepsPage = page
	if len(page.RLORTrainerJobs) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.ReinforcementFineTuningSteps.GetTyped(context.Background(), "rlor-1", fwtypes.ReinforcementFineTuningStepGetParams{
		AccountID: "acct",
		ReadMask:  "name,state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/rlorTrainerJobs/rlor-1" {
		t.Fatalf("got = %#v", got)
	}

	deleted, err := client.ReinforcementFineTuningSteps.DeleteTyped(context.Background(), "rlor-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
	}

	executed, err := client.ReinforcementFineTuningSteps.ExecuteTyped(context.Background(), "rlor-1", fwtypes.ReinforcementFineTuningStepExecuteParams{
		AccountID:   "acct",
		Dataset:     "dataset-iteration",
		OutputModel: "output-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if executed["executed"] != true {
		t.Fatalf("executed = %#v", executed)
	}

	resumed, err := client.ReinforcementFineTuningSteps.ResumeTyped(context.Background(), "rlor-1", JSON{
		"account_id": "acct",
		"reason":     "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Name == nil || *resumed.Name != "accounts/acct/rlorTrainerJobs/rlor-1" {
		t.Fatalf("resumed = %#v", resumed)
	}
}

func TestEvaluationJobsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/evaluationJobs":
			if got := r.URL.RawQuery; got != "" {
				t.Errorf("create query = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("create body should not contain account_id: %#v", body)
			}
			if body["evaluationJobId"] != "eval-job-1" {
				t.Errorf("evaluationJobId = %#v", body["evaluationJobId"])
			}
			if _, ok := body["evaluation_job_id"]; ok {
				t.Errorf("unexpected evaluation_job_id body key: %#v", body)
			}
			leaderboardIDs, ok := body["leaderboardIds"].([]any)
			if !ok || len(leaderboardIDs) != 1 || leaderboardIDs[0] != "leaderboard-1" {
				t.Errorf("leaderboardIds = %#v", body["leaderboardIds"])
			}
			evaluationJob, ok := body["evaluationJob"].(map[string]any)
			if !ok {
				t.Fatalf("evaluationJob = %#v", body["evaluationJob"])
			}
			for _, key := range []string{"evaluator", "inputDataset", "outputDataset", "awsS3Config", "displayName", "outputStats"} {
				if _, ok := evaluationJob[key]; !ok {
					t.Errorf("evaluationJob missing %q: %#v", key, evaluationJob)
				}
			}
			for _, key := range []string{"input_dataset", "output_dataset", "aws_s3_config", "display_name", "output_stats"} {
				if _, ok := evaluationJob[key]; ok {
					t.Errorf("unexpected snake evaluationJob key %q: %#v", key, evaluationJob)
				}
			}
			if evaluationJob["evaluator"] != "evaluator-1" || evaluationJob["inputDataset"] != "input-dataset" || evaluationJob["outputDataset"] != "output-dataset" {
				t.Errorf("evaluationJob scalar fields = %#v", evaluationJob)
			}
			aws, ok := evaluationJob["awsS3Config"].(map[string]any)
			if !ok || aws["credentialsSecret"] != "aws-secret" || aws["iamRoleArn"] != "arn:aws:iam::123:role/fireworks" {
				t.Errorf("awsS3Config = %#v", evaluationJob["awsS3Config"])
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":          "accounts/acct/evaluationJobs/eval-job-1",
				"evaluator":     "evaluator-1",
				"inputDataset":  "input-dataset",
				"outputDataset": "output-dataset",
				"state":         "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/evaluationJobs":
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
				"evaluationJobs": []JSON{
					{"name": "accounts/acct/evaluationJobs/eval-job-1", "evaluator": "evaluator-1", "state": "JOB_STATE_RUNNING"},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/evaluationJobs/eval-job-1":
			if got := r.URL.Query().Get("readMask"); got != "name,state" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":      "accounts/acct/evaluationJobs/eval-job-1",
				"evaluator": "evaluator-1",
				"state":     "JOB_STATE_RUNNING",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/evaluationJobs/eval-job-1:getExecutionLogEndpoint":
			if got := r.URL.Query().Get("readMask"); got != "executionLogSignedUri" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"contentType":           "text/plain",
				"executionLogSignedUri": "gs://logs/eval.txt",
			})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/evaluationJobs/eval-job-1":
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
	created, err := client.EvaluationJobs.CreateTyped(context.Background(), fwtypes.EvaluationJobCreateParams{
		AccountID:       "acct",
		EvaluationJobID: "eval-job-1",
		LeaderboardIds:  []string{"leaderboard-1"},
		EvaluationJob: fwtypes.EvaluationJobCreateParamsEvaluationJob{
			Evaluator:     "evaluator-1",
			InputDataset:  "input-dataset",
			OutputDataset: "output-dataset",
			AwsS3Config: fwtypes.EvaluationJobCreateParamsEvaluationJobAwsS3Config{
				CredentialsSecret: "aws-secret",
				IamRoleArn:        "arn:aws:iam::123:role/fireworks",
			},
			DisplayName: "Display Name",
			OutputStats: "output-stats",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/evaluationJobs/eval-job-1" {
		t.Fatalf("created = %#v", created)
	}

	page, err := client.EvaluationJobs.ListTyped(context.Background(), fwtypes.EvaluationJobListParams{
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
	var _ *fwtypes.EvaluationJobsPage = page
	if len(page.EvaluationJobs) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.EvaluationJobs.GetTyped(context.Background(), "eval-job-1", fwtypes.EvaluationJobGetParams{
		AccountID: "acct",
		ReadMask:  "name,state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/evaluationJobs/eval-job-1" {
		t.Fatalf("got = %#v", got)
	}

	logEndpoint, err := client.EvaluationJobs.GetLogEndpointTyped(context.Background(), "eval-job-1", fwtypes.EvaluationJobGetParams{
		AccountID: "acct",
		ReadMask:  "executionLogSignedUri",
	})
	if err != nil {
		t.Fatal(err)
	}
	if logEndpoint.ExecutionLogSignedURI == nil || *logEndpoint.ExecutionLogSignedURI != "gs://logs/eval.txt" {
		t.Fatalf("logEndpoint = %#v", logEndpoint)
	}

	deleted, err := client.EvaluationJobs.DeleteTyped(context.Background(), "eval-job-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
	}
}

func TestEvaluatorsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/evaluatorsV2":
			if got := r.URL.RawQuery; got != "" {
				t.Errorf("create query = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("create body should not contain account_id: %#v", body)
			}
			if body["evaluatorId"] != "eval-1" {
				t.Errorf("evaluatorId = %#v", body["evaluatorId"])
			}
			if _, ok := body["evaluator_id"]; ok {
				t.Errorf("unexpected evaluator_id body key: %#v", body)
			}
			evaluator, ok := body["evaluator"].(map[string]any)
			if !ok {
				t.Fatalf("evaluator = %#v", body["evaluator"])
			}
			assertEvaluatorPayloadUsesAliases(t, evaluator)
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/evaluators/eval-1",
				"displayName": "Display Name",
				"state":       "EVALUATOR_STATE_ACTIVE",
			})

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/accounts/acct/evaluators/eval-1":
			query := r.URL.Query()
			if got := query.Get("prepareCodeUpload"); got != "true" {
				t.Errorf("prepareCodeUpload = %q", got)
			}
			if got := query.Get("prepare_code_upload"); got != "" {
				t.Errorf("unexpected prepare_code_upload = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			for _, key := range []string{"account_id", "prepareCodeUpload", "prepare_code_upload"} {
				if _, ok := body[key]; ok {
					t.Errorf("update body should not contain %q: %#v", key, body)
				}
			}
			assertEvaluatorPayloadUsesAliases(t, body)
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/evaluators/eval-1",
				"displayName": "Updated Display Name",
				"state":       "EVALUATOR_STATE_BUILDING",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/evaluators":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "state=active" {
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
				"evaluators": []JSON{
					{"name": "accounts/acct/evaluators/eval-1", "displayName": "Display Name", "state": "EVALUATOR_STATE_ACTIVE"},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/evaluators/eval-1":
			if got := r.URL.Query().Get("readMask"); got != "name,state" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/evaluators/eval-1",
				"displayName": "Display Name",
				"state":       "EVALUATOR_STATE_ACTIVE",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/evaluators/eval-1:getBuildLogEndpoint":
			if got := r.URL.Query().Get("readMask"); got != "buildLogSignedUri" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{"buildLogSignedUri": "gs://logs/build.txt"})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/evaluators/eval-1:getSourceCodeSignedUrl":
			if got := r.URL.Query().Get("readMask"); got != "filenameToSignedUrls" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"filenameToSignedUrls": map[string]string{"main.py": "gs://src/main.py"},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/evaluators/eval-1:getUploadEndpoint":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode get upload body: %v", err)
			}
			for _, key := range []string{"account_id", "filename_to_size", "read_mask"} {
				if _, ok := body[key]; ok {
					t.Errorf("get upload body should not contain %q: %#v", key, body)
				}
			}
			filenameToSize, ok := body["filenameToSize"].(map[string]any)
			if !ok || filenameToSize["main.py"] != "123" {
				t.Errorf("filenameToSize = %#v", body["filenameToSize"])
			}
			if body["readMask"] != "filenameToSignedUrls" {
				t.Errorf("readMask body = %#v", body["readMask"])
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"filenameToSignedUrls": map[string]string{"main.py": "gs://upload/main.py"},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/evaluators/eval-1:validateUpload":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode validate body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("validate body should not contain account_id: %#v", body)
			}
			files, ok := body["files"].([]any)
			if !ok || len(files) != 1 || files[0] != "main.py" {
				t.Errorf("files = %#v", body["files"])
			}
			_ = json.NewEncoder(w).Encode(JSON{"valid": true})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/evaluators/eval-1":
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
	created, err := client.Evaluators.CreateTyped(context.Background(), fwtypes.EvaluatorCreateParams{
		AccountID:   "acct",
		EvaluatorID: "eval-1",
		Evaluator: fwtypes.EvaluatorCreateParamsEvaluator{
			CommitHash:     "abc123",
			DefaultDataset: "dataset-1",
			Description:    "description",
			DisplayName:    "Display Name",
			EntryPoint:     "main:evaluate",
			Requirements:   "fireworks-ai",
			Source: fwtypes.EvaluatorSourceParam{
				GithubRepositoryName: "owner/repo",
				Type:                 "GITHUB",
			},
			Criteria: []fwtypes.EvaluatorCreateParamsEvaluatorCriterion{
				{
					CodeSnippets: fwtypes.EvaluatorCreateParamsEvaluatorCriterionCodeSnippets{
						EntryFile:    "main.py",
						EntryFunc:    "evaluate",
						FileContents: map[string]string{"main.py": "def evaluate(): pass"},
						Language:     "python",
					},
					Description: "quality score",
					Name:        "quality",
					Type:        "TYPE_UNSPECIFIED",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/evaluators/eval-1" {
		t.Fatalf("created = %#v", created)
	}

	updated, err := client.Evaluators.UpdateTyped(context.Background(), "eval-1", fwtypes.EvaluatorUpdateParams{
		AccountID:         "acct",
		PrepareCodeUpload: true,
		CommitHash:        "def456",
		DefaultDataset:    "dataset-2",
		Description:       "updated description",
		DisplayName:       "Updated Display Name",
		EntryPoint:        "main:evaluate",
		Requirements:      "fireworks-ai>=1.0",
		Source: fwtypes.EvaluatorSourceParam{
			GithubRepositoryName: "owner/repo",
			Type:                 "GITHUB",
		},
		Criteria: []fwtypes.EvaluatorUpdateParamsCriterion{
			{
				CodeSnippets: fwtypes.EvaluatorUpdateParamsCriterionCodeSnippets{
					EntryFile:    "main.py",
					EntryFunc:    "evaluate",
					FileContents: map[string]string{"main.py": "def evaluate(): pass"},
					Language:     "python",
				},
				Description: "quality score",
				Name:        "quality",
				Type:        "TYPE_UNSPECIFIED",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName == nil || *updated.DisplayName != "Updated Display Name" {
		t.Fatalf("updated = %#v", updated)
	}

	page, err := client.Evaluators.ListTyped(context.Background(), fwtypes.EvaluatorListParams{
		AccountID: "acct",
		Filter:    "state=active",
		OrderBy:   "create_time desc",
		PageSize:  0,
		PageToken: "cursor-1",
		ReadMask:  "name,state",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.EvaluatorsPage = page
	if len(page.Evaluators) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.Evaluators.GetTyped(context.Background(), "eval-1", fwtypes.EvaluatorGetParams{
		AccountID: "acct",
		ReadMask:  "name,state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/evaluators/eval-1" {
		t.Fatalf("got = %#v", got)
	}

	buildLog, err := client.Evaluators.GetBuildLogEndpointTyped(context.Background(), "eval-1", fwtypes.EvaluatorGetBuildLogEndpointParams{
		AccountID: "acct",
		ReadMask:  "buildLogSignedUri",
	})
	if err != nil {
		t.Fatal(err)
	}
	if buildLog.BuildLogSignedURI == nil || *buildLog.BuildLogSignedURI != "gs://logs/build.txt" {
		t.Fatalf("buildLog = %#v", buildLog)
	}

	sourceCode, err := client.Evaluators.GetSourceCodeEndpointTyped(context.Background(), "eval-1", fwtypes.EvaluatorGetSourceCodeEndpointParams{
		AccountID: "acct",
		ReadMask:  "filenameToSignedUrls",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sourceCode.FilenameToSignedUrls["main.py"] != "gs://src/main.py" {
		t.Fatalf("sourceCode = %#v", sourceCode)
	}

	upload, err := client.Evaluators.GetUploadEndpointTyped(context.Background(), "eval-1", fwtypes.EvaluatorGetUploadEndpointParams{
		AccountID:      "acct",
		FilenameToSize: map[string]string{"main.py": "123"},
		ReadMask:       "filenameToSignedUrls",
	})
	if err != nil {
		t.Fatal(err)
	}
	if upload.FilenameToSignedUrls["main.py"] != "gs://upload/main.py" {
		t.Fatalf("upload = %#v", upload)
	}

	validated, err := client.Evaluators.ValidateUploadTyped(context.Background(), "eval-1", JSON{
		"account_id": "acct",
		"files":      []string{"main.py"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if validated["valid"] != true {
		t.Fatalf("validated = %#v", validated)
	}

	deleted, err := client.Evaluators.DeleteTyped(context.Background(), "eval-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
	}
}

func TestDatasetsTypedAllParamsUsePythonAliases(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/datasets":
			if got := r.URL.RawQuery; got != "" {
				t.Errorf("create query = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("create body should not contain account_id: %#v", body)
			}
			if body["datasetId"] != "dataset-1" || body["filter"] != "format=CHAT" || body["sourceDatasetId"] != "source-dataset-1" {
				t.Errorf("create body aliases = %#v", body)
			}
			for _, key := range []string{"dataset_id", "source_dataset_id"} {
				if _, ok := body[key]; ok {
					t.Errorf("unexpected snake create key %q: %#v", key, body)
				}
			}
			dataset, ok := body["dataset"].(map[string]any)
			if !ok {
				t.Fatalf("dataset = %#v", body["dataset"])
			}
			assertDatasetPayloadUsesAliases(t, dataset)
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/datasets/dataset-1",
				"displayName": "Dataset One",
				"format":      "CHAT",
			})

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/accounts/acct/datasets/dataset-1":
			if got := r.URL.RawQuery; got != "" {
				t.Errorf("update query = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("update body should not contain account_id: %#v", body)
			}
			assertDatasetPayloadUsesAliases(t, body)
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/datasets/dataset-1",
				"displayName": "Dataset One Updated",
				"format":      "CHAT",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/datasets":
			query := r.URL.Query()
			if got := query.Get("filter"); got != "format=CHAT" {
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
			if got := query.Get("readMask"); got != "name,format" {
				t.Errorf("readMask = %q", got)
			}
			if got := query.Get("account_id"); got != "" {
				t.Errorf("account_id should not be query param, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"datasets": []JSON{
					{"name": "accounts/acct/datasets/dataset-1", "displayName": "Dataset One", "format": "CHAT"},
				},
				"nextPageToken": "cursor-2",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/datasets/dataset-1":
			if got := r.URL.Query().Get("readMask"); got != "name,format" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"name":        "accounts/acct/datasets/dataset-1",
				"displayName": "Dataset One",
				"format":      "CHAT",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/accounts/acct/datasets/dataset-1:getDownloadEndpoint":
			query := r.URL.Query()
			if got := query.Get("downloadLineage"); got != "true" {
				t.Errorf("downloadLineage = %q", got)
			}
			if got := query.Get("download_lineage"); got != "" {
				t.Errorf("unexpected download_lineage = %q", got)
			}
			if got := query.Get("readMask"); got != "filenameToSignedUrls" {
				t.Errorf("readMask = %q", got)
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"filenameToSignedUrls": map[string]string{"train.jsonl": "gs://download/train.jsonl"},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/datasets/dataset-1:getUploadEndpoint":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode get upload body: %v", err)
			}
			for _, key := range []string{"account_id", "filename_to_size", "read_mask"} {
				if _, ok := body[key]; ok {
					t.Errorf("get upload body should not contain %q: %#v", key, body)
				}
			}
			filenameToSize, ok := body["filenameToSize"].(map[string]any)
			if !ok || filenameToSize["train.jsonl"] != "123" {
				t.Errorf("filenameToSize = %#v", body["filenameToSize"])
			}
			if body["readMask"] != "filenameToSignedUrls" {
				t.Errorf("readMask body = %#v", body["readMask"])
			}
			_ = json.NewEncoder(w).Encode(JSON{
				"filenameToSignedUrls": map[string]string{"train.jsonl": "gs://upload/train.jsonl"},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/accounts/acct/datasets/dataset-1:validateUpload":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode validate body: %v", err)
			}
			if _, ok := body["account_id"]; ok {
				t.Errorf("validate body should not contain account_id: %#v", body)
			}
			if body["filename"] != "train.jsonl" {
				t.Errorf("filename = %#v", body["filename"])
			}
			_ = json.NewEncoder(w).Encode(JSON{"valid": true})

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/accounts/acct/datasets/dataset-1":
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
	created, err := client.Datasets.CreateTyped(context.Background(), fwtypes.DatasetCreateParams{
		AccountID:       "acct",
		DatasetID:       "dataset-1",
		Filter:          "format=CHAT",
		SourceDatasetID: "source-dataset-1",
		Dataset: fwtypes.DatasetParam{
			DisplayName:      "Dataset One",
			EvalProtocol:     JSON{},
			EvaluationResult: fwtypes.EvaluationResultParam{EvaluationJobID: "evaluation-job-1"},
			ExampleCount:     "100",
			ExternalURL:      "https://example.com/train.jsonl",
			Format:           "CHAT",
			SourceJobName:    "accounts/acct/jobs/job-1",
			Splitted:         fwtypes.SplittedParam{SourceDatasetID: "source-dataset-1"},
			Transformed: fwtypes.TransformedParam{
				SourceDatasetID: "source-dataset-1",
				Filter:          "format=CHAT",
				OriginalFormat:  "COMPLETION",
			},
			UserUploaded: JSON{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == nil || *created.Name != "accounts/acct/datasets/dataset-1" {
		t.Fatalf("created = %#v", created)
	}

	updated, err := client.Datasets.UpdateTyped(context.Background(), "dataset-1", fwtypes.DatasetUpdateParams{
		AccountID:        "acct",
		DisplayName:      "Dataset One Updated",
		EvalProtocol:     JSON{},
		EvaluationResult: fwtypes.EvaluationResultParam{EvaluationJobID: "evaluation-job-2"},
		ExampleCount:     "101",
		ExternalURL:      "https://example.com/train-updated.jsonl",
		Format:           "CHAT",
		SourceJobName:    "accounts/acct/jobs/job-2",
		Splitted:         fwtypes.SplittedParam{SourceDatasetID: "source-dataset-1"},
		Transformed: fwtypes.TransformedParam{
			SourceDatasetID: "source-dataset-1",
			Filter:          "format=CHAT",
			OriginalFormat:  "COMPLETION",
		},
		UserUploaded: JSON{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName == nil || *updated.DisplayName != "Dataset One Updated" {
		t.Fatalf("updated = %#v", updated)
	}

	page, err := client.Datasets.ListTyped(context.Background(), fwtypes.DatasetListParams{
		AccountID: "acct",
		Filter:    "format=CHAT",
		OrderBy:   "create_time desc",
		PageSize:  0,
		PageToken: "cursor-1",
		ReadMask:  "name,format",
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ *fwtypes.DatasetsPage = page
	if len(page.Datasets) != 1 || page.NextPageToken == nil || *page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %#v", page)
	}

	got, err := client.Datasets.GetTyped(context.Background(), "dataset-1", fwtypes.DatasetGetParams{
		AccountID: "acct",
		ReadMask:  "name,format",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || *got.Name != "accounts/acct/datasets/dataset-1" {
		t.Fatalf("got = %#v", got)
	}

	download, err := client.Datasets.GetDownloadEndpointTyped(context.Background(), "dataset-1", fwtypes.DatasetGetDownloadEndpointParams{
		AccountID:       "acct",
		DownloadLineage: true,
		ReadMask:        "filenameToSignedUrls",
	})
	if err != nil {
		t.Fatal(err)
	}
	if download.FilenameToSignedUrls["train.jsonl"] != "gs://download/train.jsonl" {
		t.Fatalf("download = %#v", download)
	}

	upload, err := client.Datasets.GetUploadEndpointTyped(context.Background(), "dataset-1", fwtypes.DatasetGetUploadEndpointParams{
		AccountID:      "acct",
		FilenameToSize: map[string]string{"train.jsonl": "123"},
		ReadMask:       "filenameToSignedUrls",
	})
	if err != nil {
		t.Fatal(err)
	}
	if upload.FilenameToSignedUrls["train.jsonl"] != "gs://upload/train.jsonl" {
		t.Fatalf("upload = %#v", upload)
	}

	validated, err := client.Datasets.ValidateUploadTyped(context.Background(), "dataset-1", JSON{
		"account_id": "acct",
		"filename":   "train.jsonl",
	})
	if err != nil {
		t.Fatal(err)
	}
	if validated["valid"] != true {
		t.Fatalf("validated = %#v", validated)
	}

	deleted, err := client.Datasets.DeleteTyped(context.Background(), "dataset-1", WithAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("deleted = %#v", deleted)
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

func assertDatasetPayloadUsesAliases(t *testing.T, payload map[string]any) {
	t.Helper()

	for _, key := range []string{"displayName", "evalProtocol", "evaluationResult", "exampleCount", "externalUrl", "format", "sourceJobName", "splitted", "transformed", "userUploaded"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("dataset payload missing %q: %#v", key, payload)
		}
	}
	for _, key := range []string{"display_name", "eval_protocol", "evaluation_result", "example_count", "external_url", "source_job_name", "user_uploaded"} {
		if _, ok := payload[key]; ok {
			t.Errorf("unexpected snake dataset key %q: %#v", key, payload)
		}
	}
	if payload["displayName"] == "" || payload["exampleCount"] == "" || payload["externalUrl"] == "" || payload["sourceJobName"] == "" {
		t.Errorf("dataset scalar aliases = %#v", payload)
	}

	evaluationResult, ok := payload["evaluationResult"].(map[string]any)
	if !ok || evaluationResult["evaluationJobId"] == "" {
		t.Errorf("evaluationResult = %#v", payload["evaluationResult"])
	}
	if _, ok := evaluationResult["evaluation_job_id"]; ok {
		t.Errorf("unexpected snake evaluationResult key: %#v", evaluationResult)
	}

	splitted, ok := payload["splitted"].(map[string]any)
	if !ok || splitted["sourceDatasetId"] != "source-dataset-1" {
		t.Errorf("splitted = %#v", payload["splitted"])
	}
	if _, ok := splitted["source_dataset_id"]; ok {
		t.Errorf("unexpected snake splitted key: %#v", splitted)
	}

	transformed, ok := payload["transformed"].(map[string]any)
	if !ok {
		t.Fatalf("transformed = %#v", payload["transformed"])
	}
	if transformed["sourceDatasetId"] != "source-dataset-1" || transformed["filter"] != "format=CHAT" || transformed["originalFormat"] != "COMPLETION" {
		t.Errorf("transformed = %#v", transformed)
	}
	for _, key := range []string{"source_dataset_id", "original_format"} {
		if _, ok := transformed[key]; ok {
			t.Errorf("unexpected snake transformed key %q: %#v", key, transformed)
		}
	}
}

func testModelParam(displayName string) fwtypes.ModelParam {
	return fwtypes.ModelParam{
		BaseModelDetails: fwtypes.BaseModelDetailsParam{
			CheckpointFormat:      "CHECKPOINT_FORMAT_UNSPECIFIED",
			ModelType:             "llm",
			Moe:                   true,
			ParameterCount:        "7B",
			SupportsFireattention: true,
			SupportsMtp:           true,
			Tunable:               true,
			WorldSize:             0,
		},
		ContextLength:          0,
		ConversationConfig:     fwtypes.ConversationConfigParam{Style: "chat", System: "system", Template: "template"},
		DefaultDraftModel:      "accounts/acct/models/draft",
		DefaultDraftTokenCount: 0,
		DeprecationDate:        fwtypes.TypeDateParam{Day: 0, Month: 0, Year: 0},
		Description:            "description",
		DisplayName:            displayName,
		GithubURL:              "https://github.com/fw/model",
		HuggingFaceURL:         "https://huggingface.co/fw/model",
		Kind:                   "HF_BASE_MODEL",
		PeftDetails: fwtypes.PeftDetailsParam{
			BaseModel:           "accounts/acct/models/base",
			R:                   0,
			TargetModules:       []string{"q_proj"},
			MergeAddonModelName: "accounts/acct/models/merged",
		},
		Public:                 true,
		SnapshotType:           "FULL_SNAPSHOT",
		SupportsImageInput:     true,
		SupportsLora:           true,
		SupportsTools:          true,
		TeftDetails:            JSON{},
		TrainingContextLength:  0,
		UseHfApplyChatTemplate: true,
	}
}

func testModelUpdateParams(accountID, displayName string) fwtypes.ModelUpdateParams {
	model := testModelParam(displayName)
	return fwtypes.ModelUpdateParams{
		AccountID:              accountID,
		BaseModelDetails:       model.BaseModelDetails,
		ContextLength:          model.ContextLength,
		ConversationConfig:     model.ConversationConfig,
		DefaultDraftModel:      model.DefaultDraftModel,
		DefaultDraftTokenCount: model.DefaultDraftTokenCount,
		DeprecationDate:        model.DeprecationDate,
		Description:            model.Description,
		DisplayName:            model.DisplayName,
		GithubURL:              model.GithubURL,
		HuggingFaceURL:         model.HuggingFaceURL,
		Kind:                   model.Kind,
		PeftDetails:            model.PeftDetails,
		Public:                 model.Public,
		SnapshotType:           model.SnapshotType,
		SupportsImageInput:     model.SupportsImageInput,
		SupportsLora:           model.SupportsLora,
		SupportsTools:          model.SupportsTools,
		TeftDetails:            model.TeftDetails,
		TrainingContextLength:  model.TrainingContextLength,
		UseHfApplyChatTemplate: model.UseHfApplyChatTemplate,
	}
}

func assertModelPayloadUsesAliases(t *testing.T, payload map[string]any) {
	t.Helper()

	for _, key := range []string{
		"baseModelDetails", "contextLength", "conversationConfig", "defaultDraftModel", "defaultDraftTokenCount",
		"deprecationDate", "description", "displayName", "githubUrl", "huggingFaceUrl", "kind", "peftDetails",
		"public", "snapshotType", "supportsImageInput", "supportsLora", "supportsTools", "teftDetails",
		"trainingContextLength", "useHfApplyChatTemplate",
	} {
		if _, ok := payload[key]; !ok {
			t.Errorf("model payload missing %q: %#v", key, payload)
		}
	}
	for _, key := range []string{
		"base_model_details", "conversation_config", "default_draft_model", "default_draft_token_count",
		"deprecation_date", "display_name", "github_url", "hugging_face_url", "peft_details",
		"snapshot_type", "supports_image_input", "supports_lora", "supports_tools", "training_context_length",
		"use_hf_apply_chat_template",
	} {
		if _, ok := payload[key]; ok {
			t.Errorf("unexpected snake model key %q: %#v", key, payload)
		}
	}
	if payload["contextLength"] != float64(0) || payload["defaultDraftTokenCount"] != float64(0) || payload["trainingContextLength"] != float64(0) {
		t.Errorf("zero-valued model fields = %#v", payload)
	}
	for _, key := range []string{"public", "supportsImageInput", "supportsLora", "supportsTools", "useHfApplyChatTemplate"} {
		if payload[key] != true {
			t.Errorf("%s = %#v", key, payload[key])
		}
	}

	baseModelDetails, ok := payload["baseModelDetails"].(map[string]any)
	if !ok {
		t.Fatalf("baseModelDetails = %#v", payload["baseModelDetails"])
	}
	for _, key := range []string{"checkpointFormat", "modelType", "moe", "parameterCount", "supportsFireattention", "supportsMtp", "tunable", "worldSize"} {
		if _, ok := baseModelDetails[key]; !ok {
			t.Errorf("baseModelDetails missing %q: %#v", key, baseModelDetails)
		}
	}
	for _, key := range []string{"checkpoint_format", "model_type", "parameter_count", "supports_fireattention", "supports_mtp", "world_size"} {
		if _, ok := baseModelDetails[key]; ok {
			t.Errorf("unexpected snake baseModelDetails key %q: %#v", key, baseModelDetails)
		}
	}
	if baseModelDetails["worldSize"] != float64(0) || baseModelDetails["moe"] != true || baseModelDetails["supportsFireattention"] != true {
		t.Errorf("baseModelDetails values = %#v", baseModelDetails)
	}

	conversationConfig, ok := payload["conversationConfig"].(map[string]any)
	if !ok || conversationConfig["style"] != "chat" || conversationConfig["system"] != "system" || conversationConfig["template"] != "template" {
		t.Errorf("conversationConfig = %#v", payload["conversationConfig"])
	}

	deprecationDate, ok := payload["deprecationDate"].(map[string]any)
	if !ok || deprecationDate["day"] != float64(0) || deprecationDate["month"] != float64(0) || deprecationDate["year"] != float64(0) {
		t.Errorf("deprecationDate = %#v", payload["deprecationDate"])
	}

	peftDetails, ok := payload["peftDetails"].(map[string]any)
	if !ok {
		t.Fatalf("peftDetails = %#v", payload["peftDetails"])
	}
	if peftDetails["baseModel"] != "accounts/acct/models/base" || peftDetails["r"] != float64(0) || peftDetails["mergeAddonModelName"] != "accounts/acct/models/merged" {
		t.Errorf("peftDetails values = %#v", peftDetails)
	}
	for _, key := range []string{"base_model", "target_modules", "merge_addon_model_name"} {
		if _, ok := peftDetails[key]; ok {
			t.Errorf("unexpected snake peftDetails key %q: %#v", key, peftDetails)
		}
	}
	targetModules, ok := peftDetails["targetModules"].([]any)
	if !ok || len(targetModules) != 1 || targetModules[0] != "q_proj" {
		t.Errorf("targetModules = %#v", peftDetails["targetModules"])
	}
}

func testDeploymentCreateParams(accountID, displayName string) fwtypes.DeploymentCreateParams {
	return fwtypes.DeploymentCreateParams{
		AccountID:                       accountID,
		BaseModel:                       "accounts/fireworks/models/base",
		DeploymentID:                    "dep-1",
		DisableAutoDeploy:               true,
		DisableSpeculativeDecoding:      true,
		SkipImageTagValidation:          true,
		SkipShapeValidation:             true,
		ValidateOnly:                    true,
		AcceleratorCount:                0,
		AcceleratorType:                 "ACCELERATOR_TYPE_UNSPECIFIED",
		ActiveModelVersion:              "v1",
		AutoscalingPolicy:               testAutoscalingPolicyParam(),
		AutoTune:                        fwtypes.AutoTuneParam{LongPrompt: true},
		DeploymentShape:                 "deployment-shape",
		DeploymentTemplate:              "deployment-template",
		Description:                     "description",
		DirectRouteAPIKeys:              []string{"direct-key"},
		DirectRouteType:                 "DIRECT_ROUTE_TYPE_UNSPECIFIED",
		DisableDeploymentSizeValidation: true,
		DisplayName:                     displayName,
		DraftModel:                      "accounts/fireworks/models/draft",
		DraftTokenCount:                 0,
		EnableAddons:                    true,
		EnableHotLoad:                   true,
		EnableHotReloadLatestAddon:      true,
		EnableMtp:                       true,
		EnableSessionAffinity:           true,
		ExpireTime:                      "2019-12-27T18:11:19.117Z",
		HotLoadBucketType:               "BUCKET_TYPE_UNSPECIFIED",
		HotLoadBucketURL:                "gs://bucket/hotload",
		MaxContextLength:                0,
		MaxReplicaCount:                 0,
		MaxWithRevocableReplicaCount:    0,
		MinReplicaCount:                 0,
		NgramSpeculationLength:          0,
		Placement: fwtypes.PlacementParam{
			MultiRegion: "MULTI_REGION_UNSPECIFIED",
			Region:      "REGION_UNSPECIFIED",
			Regions:     []string{"REGION_UNSPECIFIED"},
		},
		Precision:          "PRECISION_UNSPECIFIED",
		PricingPlanID:      "pricing-plan",
		TargetModelVersion: "target-version",
	}
}

func testDeploymentUpdateParams(accountID, displayName string) fwtypes.DeploymentUpdateParams {
	create := testDeploymentCreateParams(accountID, displayName)
	return fwtypes.DeploymentUpdateParams{
		AccountID:                       accountID,
		BaseModel:                       create.BaseModel,
		SkipShapeValidation:             true,
		AcceleratorCount:                create.AcceleratorCount,
		AcceleratorType:                 create.AcceleratorType,
		ActiveModelVersion:              create.ActiveModelVersion,
		AutoscalingPolicy:               create.AutoscalingPolicy,
		AutoTune:                        create.AutoTune,
		DeploymentShape:                 create.DeploymentShape,
		DeploymentTemplate:              create.DeploymentTemplate,
		Description:                     create.Description,
		DirectRouteAPIKeys:              create.DirectRouteAPIKeys,
		DirectRouteType:                 create.DirectRouteType,
		DisableDeploymentSizeValidation: create.DisableDeploymentSizeValidation,
		DisplayName:                     displayName,
		DraftModel:                      create.DraftModel,
		DraftTokenCount:                 create.DraftTokenCount,
		EnableAddons:                    create.EnableAddons,
		EnableHotLoad:                   create.EnableHotLoad,
		EnableHotReloadLatestAddon:      create.EnableHotReloadLatestAddon,
		EnableMtp:                       create.EnableMtp,
		EnableSessionAffinity:           create.EnableSessionAffinity,
		ExpireTime:                      create.ExpireTime,
		HotLoadBucketType:               create.HotLoadBucketType,
		HotLoadBucketURL:                create.HotLoadBucketURL,
		MaxContextLength:                create.MaxContextLength,
		MaxReplicaCount:                 create.MaxReplicaCount,
		MaxWithRevocableReplicaCount:    create.MaxWithRevocableReplicaCount,
		MinReplicaCount:                 create.MinReplicaCount,
		NgramSpeculationLength:          create.NgramSpeculationLength,
		Placement:                       create.Placement,
		Precision:                       create.Precision,
		PricingPlanID:                   create.PricingPlanID,
		TargetModelVersion:              create.TargetModelVersion,
	}
}

func testAutoscalingPolicyParam() fwtypes.AutoscalingPolicyParam {
	return fwtypes.AutoscalingPolicyParam{
		LoadTargets:       map[string]float64{"requests": 0},
		ScaleDownWindow:   "60s",
		ScaleToZeroWindow: "300s",
		ScaleUpWindow:     "30s",
	}
}

func assertDeploymentPayloadUsesAliases(t *testing.T, payload map[string]any) {
	t.Helper()

	for _, key := range []string{
		"baseModel", "acceleratorCount", "acceleratorType", "activeModelVersion", "autoscalingPolicy", "autoTune",
		"deploymentShape", "deploymentTemplate", "description", "directRouteApiKeys", "directRouteType",
		"disableDeploymentSizeValidation", "displayName", "draftModel", "draftTokenCount", "enableAddons",
		"enableHotLoad", "enableHotReloadLatestAddon", "enableMtp", "enableSessionAffinity", "expireTime",
		"hotLoadBucketType", "hotLoadBucketUrl", "maxContextLength", "maxReplicaCount", "maxWithRevocableReplicaCount",
		"minReplicaCount", "ngramSpeculationLength", "placement", "precision", "pricingPlanId", "targetModelVersion",
	} {
		if _, ok := payload[key]; !ok {
			t.Errorf("deployment payload missing %q: %#v", key, payload)
		}
	}
	for _, key := range []string{
		"base_model", "accelerator_count", "accelerator_type", "active_model_version", "autoscaling_policy",
		"deployment_shape", "deployment_template", "direct_route_api_keys", "direct_route_type",
		"disable_deployment_size_validation", "display_name", "draft_model", "draft_token_count", "enable_addons",
		"enable_hot_load", "enable_hot_reload_latest_addon", "enable_mtp", "enable_session_affinity", "expire_time",
		"hot_load_bucket_type", "hot_load_bucket_url", "max_context_length", "max_replica_count",
		"max_with_revocable_replica_count", "min_replica_count", "ngram_speculation_length", "pricing_plan_id",
		"target_model_version",
	} {
		if _, ok := payload[key]; ok {
			t.Errorf("unexpected snake deployment key %q: %#v", key, payload)
		}
	}
	for _, key := range []string{"acceleratorCount", "draftTokenCount", "maxContextLength", "maxReplicaCount", "maxWithRevocableReplicaCount", "minReplicaCount", "ngramSpeculationLength"} {
		if payload[key] != float64(0) {
			t.Errorf("%s = %#v", key, payload[key])
		}
	}
	for _, key := range []string{"disableDeploymentSizeValidation", "enableAddons", "enableHotLoad", "enableHotReloadLatestAddon", "enableMtp", "enableSessionAffinity"} {
		if payload[key] != true {
			t.Errorf("%s = %#v", key, payload[key])
		}
	}
	if payload["baseModel"] != "accounts/fireworks/models/base" || payload["displayName"] == "" || payload["hotLoadBucketUrl"] != "gs://bucket/hotload" {
		t.Errorf("deployment scalar aliases = %#v", payload)
	}

	autoscalingPolicy, ok := payload["autoscalingPolicy"].(map[string]any)
	if !ok {
		t.Fatalf("autoscalingPolicy = %#v", payload["autoscalingPolicy"])
	}
	for _, key := range []string{"loadTargets", "scaleDownWindow", "scaleToZeroWindow", "scaleUpWindow"} {
		if _, ok := autoscalingPolicy[key]; !ok {
			t.Errorf("autoscalingPolicy missing %q: %#v", key, autoscalingPolicy)
		}
	}
	for _, key := range []string{"load_targets", "scale_down_window", "scale_to_zero_window", "scale_up_window"} {
		if _, ok := autoscalingPolicy[key]; ok {
			t.Errorf("unexpected snake autoscalingPolicy key %q: %#v", key, autoscalingPolicy)
		}
	}
	loadTargets, ok := autoscalingPolicy["loadTargets"].(map[string]any)
	if !ok || loadTargets["requests"] != float64(0) {
		t.Errorf("loadTargets = %#v", autoscalingPolicy["loadTargets"])
	}

	autoTune, ok := payload["autoTune"].(map[string]any)
	if !ok || autoTune["longPrompt"] != true {
		t.Errorf("autoTune = %#v", payload["autoTune"])
	}
	if _, ok := autoTune["long_prompt"]; ok {
		t.Errorf("unexpected snake autoTune key: %#v", autoTune)
	}

	placement, ok := payload["placement"].(map[string]any)
	if !ok {
		t.Fatalf("placement = %#v", payload["placement"])
	}
	if placement["multiRegion"] != "MULTI_REGION_UNSPECIFIED" || placement["region"] != "REGION_UNSPECIFIED" {
		t.Errorf("placement = %#v", placement)
	}
	if _, ok := placement["multi_region"]; ok {
		t.Errorf("unexpected snake placement key: %#v", placement)
	}
	regions, ok := placement["regions"].([]any)
	if !ok || len(regions) != 1 || regions[0] != "REGION_UNSPECIFIED" {
		t.Errorf("regions = %#v", placement["regions"])
	}
}

func testLoraLoadParams(accountID, displayName string) fwtypes.LoraLoadParams {
	return fwtypes.LoraLoadParams{
		AccountID:          accountID,
		ReplaceMergedAddon: true,
		Default:            true,
		Deployment:         "accounts/acct/deployments/dep-1",
		Description:        "description",
		DisplayName:        displayName,
		Model:              "accounts/acct/models/lora",
		Public:             true,
		Serverless:         true,
	}
}

func testLoraUpdateParams(accountID, displayName string) fwtypes.LoraUpdateParams {
	return fwtypes.LoraUpdateParams{
		AccountID:   accountID,
		Default:     true,
		Deployment:  "accounts/acct/deployments/dep-1",
		Description: "description",
		DisplayName: displayName,
		Model:       "accounts/acct/models/lora",
		Public:      true,
		Serverless:  true,
	}
}

func assertLoraPayloadUsesAliases(t *testing.T, payload map[string]any) {
	t.Helper()

	for _, key := range []string{"default", "deployment", "description", "displayName", "model", "public", "serverless"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("lora payload missing %q: %#v", key, payload)
		}
	}
	for _, key := range []string{"account_id", "display_name", "replace_merged_addon"} {
		if _, ok := payload[key]; ok {
			t.Errorf("unexpected snake lora key %q: %#v", key, payload)
		}
	}
	for _, key := range []string{"default", "public", "serverless"} {
		if payload[key] != true {
			t.Errorf("%s = %#v", key, payload[key])
		}
	}
	if payload["deployment"] != "accounts/acct/deployments/dep-1" || payload["displayName"] == "" || payload["model"] != "accounts/acct/models/lora" {
		t.Errorf("lora scalar aliases = %#v", payload)
	}
}

func assertEvaluatorPayloadUsesAliases(t *testing.T, payload map[string]any) {
	t.Helper()

	for _, key := range []string{"commitHash", "criteria", "defaultDataset", "description", "displayName", "entryPoint", "requirements", "source"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("evaluator payload missing %q: %#v", key, payload)
		}
	}
	for _, key := range []string{"commit_hash", "default_dataset", "display_name", "entry_point"} {
		if _, ok := payload[key]; ok {
			t.Errorf("unexpected snake evaluator key %q: %#v", key, payload)
		}
	}
	if payload["commitHash"] == "" || payload["defaultDataset"] == "" || payload["displayName"] == "" || payload["entryPoint"] == "" {
		t.Errorf("evaluator scalar aliases = %#v", payload)
	}

	criteria, ok := payload["criteria"].([]any)
	if !ok || len(criteria) != 1 {
		t.Fatalf("criteria = %#v", payload["criteria"])
	}
	criterion, ok := criteria[0].(map[string]any)
	if !ok {
		t.Fatalf("criterion = %#v", criteria[0])
	}
	for _, key := range []string{"codeSnippets", "description", "name", "type"} {
		if _, ok := criterion[key]; !ok {
			t.Errorf("criterion missing %q: %#v", key, criterion)
		}
	}
	for _, key := range []string{"code_snippets"} {
		if _, ok := criterion[key]; ok {
			t.Errorf("unexpected snake criterion key %q: %#v", key, criterion)
		}
	}
	codeSnippets, ok := criterion["codeSnippets"].(map[string]any)
	if !ok {
		t.Fatalf("codeSnippets = %#v", criterion["codeSnippets"])
	}
	for _, key := range []string{"entryFile", "entryFunc", "fileContents", "language"} {
		if _, ok := codeSnippets[key]; !ok {
			t.Errorf("codeSnippets missing %q: %#v", key, codeSnippets)
		}
	}
	for _, key := range []string{"entry_file", "entry_func", "file_contents"} {
		if _, ok := codeSnippets[key]; ok {
			t.Errorf("unexpected snake codeSnippets key %q: %#v", key, codeSnippets)
		}
	}
	fileContents, ok := codeSnippets["fileContents"].(map[string]any)
	if !ok || fileContents["main.py"] == "" {
		t.Errorf("fileContents = %#v", codeSnippets["fileContents"])
	}

	source, ok := payload["source"].(map[string]any)
	if !ok {
		t.Fatalf("source = %#v", payload["source"])
	}
	if source["githubRepositoryName"] != "owner/repo" || source["type"] != "GITHUB" {
		t.Errorf("source = %#v", source)
	}
	if _, ok := source["github_repository_name"]; ok {
		t.Errorf("unexpected snake source key: %#v", source)
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

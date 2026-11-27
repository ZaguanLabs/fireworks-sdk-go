package sdk

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	trainingClientSourceRE = regexp.MustCompile(`^fireworks-training-skill(?:/[0-9A-Za-z][0-9A-Za-z._+\-]{0,63})?$`)
	trainingSessionIDRE    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type TrainingRestClient struct {
	serverlessSupervised409Retry atomic.Bool
	apiKey                       string
	accountMu                    sync.Mutex
	accountID                    string
	baseURL                      string
	additionalHeaders            map[string]string
	verifySSLOverride            *bool
	baseVerifySSL                bool
	httpClient                   *http.Client
	retryOptions                 *RequestRetryOptions
}

type TrainingRestClientOption func(*TrainingRestClient)

func WithTrainingAdditionalHeaders(headers map[string]string) TrainingRestClientOption {
	return func(c *TrainingRestClient) {
		c.additionalHeaders = cloneStringMap(headers)
	}
}

func WithTrainingVerifySSL(verify bool) TrainingRestClientOption {
	return func(c *TrainingRestClient) {
		c.verifySSLOverride = &verify
	}
}

func WithTrainingHTTPClient(httpClient *http.Client) TrainingRestClientOption {
	return func(c *TrainingRestClient) {
		c.httpClient = httpClient
	}
}

func WithTrainingRetryOptions(opts RequestRetryOptions) TrainingRestClientOption {
	return func(c *TrainingRestClient) {
		c.retryOptions = &opts
	}
}

func NewTrainingRestClient(apiKey, baseURL string, opts ...TrainingRestClientOption) *TrainingRestClient {
	if baseURL == "" {
		baseURL = DefaultFireworksAPIURL
	}
	c := &TrainingRestClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	c.baseVerifySSL = c.VerifyForURL(c.baseURL)
	if c.httpClient == nil {
		c.httpClient = trainingHTTPClient(c.baseVerifySSL, time.Minute)
	}
	return c
}

func ShouldVerifySSL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" {
		return false
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		return false
	}
	return true
}

func (c *TrainingRestClient) APIKey() string {
	return c.apiKey
}

func (c *TrainingRestClient) BaseURL() string {
	return c.baseURL
}

func (c *TrainingRestClient) SetAccountID(accountID string) {
	c.accountMu.Lock()
	defer c.accountMu.Unlock()
	c.accountID = accountID
}

func (c *TrainingRestClient) AccountID(ctx context.Context) (string, error) {
	return c.cachedAccountID(ctx, true)
}
func (c *TrainingRestClient) cachedAccountID(ctx context.Context, retry bool) (string, error) {
	c.accountMu.Lock()
	defer c.accountMu.Unlock()
	if c.accountID != "" {
		return c.accountID, nil
	}
	accountID, err := c.resolveAccountID(ctx, retry)
	if err != nil {
		return "", err
	}
	c.accountID = accountID
	return accountID, nil
}

func (c *TrainingRestClient) ResolveAccountID(ctx context.Context) (string, error) {
	return c.resolveAccountID(ctx, true)
}
func (c *TrainingRestClient) resolveAccountID(ctx context.Context, retry bool) (string, error) {
	resp, err := c.requestWithRetry(ctx, http.MethodGet, "/v1/accounts?pageSize=2", nil, nil, HTTPReadTimeout, retry)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("resolve account ID: HTTP %d: %s", resp.StatusCode, ParseAPIErrorBody(body))
	}

	var payload struct {
		Accounts []struct {
			Name string `json:"name"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if len(payload.Accounts) == 0 {
		return "", fmt.Errorf("API key is not associated with any Fireworks account. Verify your API key is valid at: https://fireworks.ai/account/api-keys")
	}
	if len(payload.Accounts) > 1 {
		ids := make([]string, 0, len(payload.Accounts))
		for _, account := range payload.Accounts {
			ids = append(ids, strings.TrimPrefix(account.Name, "accounts/"))
		}
		return "", fmt.Errorf("API key has access to multiple accounts: %v. This is not supported for firetitan training", ids)
	}
	accountID := strings.TrimPrefix(payload.Accounts[0].Name, "accounts/")
	if accountID == "" {
		return "", fmt.Errorf("Could not parse account ID from API response. Got account name: %q", payload.Accounts[0].Name)
	}
	return accountID, nil
}

func (c *TrainingRestClient) VerifyForURL(rawURL string) bool {
	if c.verifySSLOverride != nil {
		return *c.verifySSLOverride
	}
	return ShouldVerifySSL(rawURL)
}

func (c *TrainingRestClient) Headers(extra map[string]string) http.Header {
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Api-Key", c.apiKey)
	if source := ValidatedTrainingClientSource(os.Getenv("FIREWORKS_CLIENT_SOURCE")); source != "" {
		headers.Set("X-Fireworks-Client-Source", source)
	}
	if sessionID := ValidatedTrainingSessionID(os.Getenv("FIREWORKS_SESSION_ID")); sessionID != "" {
		headers.Set("X-Fireworks-Session-Id", sessionID)
	}
	for key, value := range c.additionalHeaders {
		headers.Set(key, value)
	}
	for key, value := range extra {
		headers.Set(key, value)
	}
	return headers
}

func ValidatedTrainingClientSource(value string) string {
	if len(value) > 128 || !trainingClientSourceRE.MatchString(value) {
		return ""
	}
	return value
}

func ValidatedTrainingSessionID(value string) string {
	if len(value) > 36 || !trainingSessionIDRE.MatchString(value) {
		return ""
	}
	return value
}

func (c *TrainingRestClient) Get(ctx context.Context, path string, headers map[string]string) (*http.Response, error) {
	return c.request(ctx, http.MethodGet, path, nil, headers, HTTPReadTimeout)
}

func (c *TrainingRestClient) Post(ctx context.Context, path string, body any, headers map[string]string) (*http.Response, error) {
	return c.request(ctx, http.MethodPost, path, body, headers, HTTPWriteTimeout)
}

func (c *TrainingRestClient) Delete(ctx context.Context, path string, headers map[string]string) (*http.Response, error) {
	return c.request(ctx, http.MethodDelete, path, nil, headers, HTTPWriteTimeout)
}

func (c *TrainingRestClient) Patch(ctx context.Context, path string, body any, headers map[string]string) (*http.Response, error) {
	return c.request(ctx, http.MethodPatch, path, body, headers, HTTPWriteTimeout)
}

func (c *TrainingRestClient) request(ctx context.Context, method, path string, body any, headers map[string]string, timeout time.Duration) (*http.Response, error) {
	return c.requestWithRetry(ctx, method, path, body, headers, timeout, true)
}

func (c *TrainingRestClient) requestWithRetry(ctx context.Context, method, path string, body any, headers map[string]string, timeout time.Duration, retry bool) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var payload []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = data
	}
	requestURL := c.baseURL + path
	client := c.httpClient
	if c.VerifyForURL(requestURL) != c.baseVerifySSL {
		client = trainingHTTPClient(c.VerifyForURL(requestURL), timeout)
	}

	call := func() (*http.Response, error) {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, requestURL, reader)
		if err != nil {
			return nil, err
		}
		req.Header = c.Headers(headers)
		return client.Do(req)
	}
	if !retry {
		return call()
	}
	var options RequestRetryOptions
	if c.retryOptions != nil {
		options = *c.retryOptions
	}
	if c.serverlessSupervised409Retry.Load() && strings.HasSuffix(requestURL, "/api/v1/retrieve_future") {
		original := options.RetryResponse
		options.RetryResponse = func(resp *http.Response) bool {
			if original != nil && original(resp) {
				return true
			}
			if resp.StatusCode != 409 {
				return false
			}
			var body []byte
			if resp.Body != nil {
				var err error
				body, err = io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				resp.Body = io.NopCloser(bytes.NewReader(body))
				if err != nil {
					return false
				}
			}
			return ShouldRetryServerlessCapacity409(requestURL, body, true)
		}
	}
	return RequestWithRetries(call, options)
}

func trainingHTTPClient(verifySSL bool, timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 256
	transport.MaxIdleConns = 256
	transport.MaxIdleConnsPerHost = 64
	if !verifySSL {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

// EnableServerlessSupervised409Retry opts a supervised SFT/DPO client into
// capacity/recovery retries on serverless future retrieval only.
func (c *TrainingRestClient) EnableServerlessSupervised409Retry() {
	c.serverlessSupervised409Retry.Store(true)
}
func WithServerlessSupervised409Retry() TrainingRestClientOption {
	return func(c *TrainingRestClient) { c.EnableServerlessSupervised409Retry() }
}

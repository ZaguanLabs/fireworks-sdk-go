package fireworks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.fireworks.ai"

	DefaultTimeout                   = time.Minute
	DefaultConnectTimeout            = 5 * time.Second
	DefaultMaxRetries                = 2
	DefaultMaxConnectionsPerHost     = 1000
	DefaultMaxIdleConnectionsPerHost = 20

	defaultTimeout      = DefaultTimeout
	defaultConnectLimit = DefaultConnectTimeout
	defaultMaxRetries   = DefaultMaxRetries
	initialRetryDelay   = 500 * time.Millisecond
	maxRetryDelay       = 8 * time.Second
)

type Client struct {
	apiKey            string
	accountID         string
	baseURL           *url.URL
	baseURLOverridden bool
	httpClient        *http.Client
	defaultHeaders    http.Header
	defaultQuery      url.Values
	maxRetries        int

	Chat                         *ChatResource
	Completions                  *CompletionsResource
	Messages                     *MessagesResource
	BatchInferenceJobs           *BatchInferenceJobsResource
	Deployments                  *DeploymentsResource
	Models                       *ModelsResource
	Lora                         *LoraResource
	DeploymentShapes             *DeploymentShapesResource
	DeploymentShapeVersions      *DeploymentShapeVersionsResource
	Datasets                     *DatasetsResource
	SupervisedFineTuningJobs     *SupervisedFineTuningJobsResource
	ReinforcementFineTuningJobs  *ReinforcementFineTuningJobsResource
	ReinforcementFineTuningSteps *ReinforcementFineTuningStepsResource
	DPOJobs                      *DPOJobsResource
	EvaluationJobs               *EvaluationJobsResource
	Evaluators                   *EvaluatorsResource
	Accounts                     *AccountsResource
	Users                        *UsersResource
	APIKeys                      *APIKeysResource
	Secrets                      *SecretsResource
}

type ClientOption func(*clientConfig)

type clientConfig struct {
	apiKey         string
	accountID      string
	baseURL        string
	baseURLSet     bool
	httpClient     *http.Client
	defaultHeaders http.Header
	defaultQuery   url.Values
	maxRetries     int
}

func WithAPIKey(apiKey string) ClientOption {
	return func(c *clientConfig) {
		c.apiKey = apiKey
	}
}

func WithDefaultAccountID(accountID string) ClientOption {
	return func(c *clientConfig) {
		c.accountID = accountID
	}
}

func WithBaseURL(baseURL string) ClientOption {
	return func(c *clientConfig) {
		c.baseURL = baseURL
		c.baseURLSet = true
	}
}

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *clientConfig) {
		c.httpClient = httpClient
	}
}

func WithDefaultHeader(key, value string) ClientOption {
	return func(c *clientConfig) {
		if c.defaultHeaders == nil {
			c.defaultHeaders = make(http.Header)
		}
		c.defaultHeaders.Set(key, value)
	}
}

func WithDefaultOmitHeader(key string) ClientOption {
	return func(c *clientConfig) {
		if c.defaultHeaders == nil {
			c.defaultHeaders = make(http.Header)
		}
		c.defaultHeaders[key] = nil
	}
}

func WithDefaultHeaders(headers map[string]string) ClientOption {
	return func(c *clientConfig) {
		if c.defaultHeaders == nil {
			c.defaultHeaders = make(http.Header)
		}
		for key, value := range headers {
			c.defaultHeaders.Set(key, value)
		}
	}
}

func WithDefaultQuery(query map[string]any) ClientOption {
	return func(c *clientConfig) {
		if c.defaultQuery == nil {
			c.defaultQuery = make(url.Values)
		}
		for key, value := range query {
			addQueryValue(c.defaultQuery, key, value)
		}
	}
}

func WithMaxRetries(maxRetries int) ClientOption {
	return func(c *clientConfig) {
		c.maxRetries = maxRetries
	}
}

func NewClient(opts ...ClientOption) (*Client, error) {
	cfg := clientConfig{maxRetries: defaultMaxRetries}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	if cfg.apiKey == "" {
		cfg.apiKey = os.Getenv("FIREWORKS_API_KEY")
	}
	if cfg.apiKey == "" {
		return nil, &Error{Message: "the api_key client option must be set either by passing WithAPIKey to NewClient or by setting the FIREWORKS_API_KEY environment variable"}
	}

	if cfg.accountID == "" {
		cfg.accountID = os.Getenv("FIREWORKS_ACCOUNT_ID")
	}

	baseURLOverridden := cfg.baseURLSet && cfg.baseURL != ""
	if cfg.baseURL == "" {
		cfg.baseURL = os.Getenv("FIREWORKS_BASE_URL")
		baseURLOverridden = cfg.baseURL != ""
	}
	if cfg.baseURL == "" {
		cfg.baseURL = defaultBaseURL
	}

	parsedBaseURL, err := url.Parse(cfg.baseURL)
	if err != nil {
		return nil, fmt.Errorf("fireworks: invalid base URL: %w", err)
	}

	headers := make(http.Header)
	for key, values := range parseCustomHeadersEnv(os.Getenv("FIREWORKS_CUSTOM_HEADERS")) {
		for _, value := range values {
			headers.Add(key, value)
		}
	}
	for key, values := range cfg.defaultHeaders {
		headers.Del(key)
		if values == nil {
			headers[key] = nil
			continue
		}
		for _, value := range values {
			headers.Add(key, value)
		}
	}

	httpClient := cfg.httpClient
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}

	c := &Client{
		apiKey:            cfg.apiKey,
		accountID:         cfg.accountID,
		baseURL:           parsedBaseURL,
		baseURLOverridden: baseURLOverridden,
		httpClient:        httpClient,
		defaultHeaders:    headers,
		defaultQuery:      cloneValues(cfg.defaultQuery),
		maxRetries:        cfg.maxRetries,
	}
	c.initResources()
	return c, nil
}

func MustNewClient(opts ...ClientOption) *Client {
	client, err := NewClient(opts...)
	if err != nil {
		panic(err)
	}
	return client
}

func (c *Client) WithOptions(opts ...ClientOption) (*Client, error) {
	cfg := clientConfig{
		apiKey:         c.apiKey,
		accountID:      c.accountID,
		baseURL:        c.baseURL.String(),
		baseURLSet:     c.baseURLOverridden,
		httpClient:     c.httpClient,
		defaultHeaders: c.defaultHeaders.Clone(),
		defaultQuery:   cloneValues(c.defaultQuery),
		maxRetries:     c.maxRetries,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.apiKey == "" {
		return nil, &Error{Message: "the api_key client option must be set either by passing WithAPIKey to NewClient or by setting the FIREWORKS_API_KEY environment variable"}
	}
	if cfg.baseURL == "" {
		cfg.baseURL = defaultBaseURL
		cfg.baseURLSet = false
	}
	if cfg.httpClient == nil {
		cfg.httpClient = defaultHTTPClient()
	}

	parsedBaseURL, err := url.Parse(cfg.baseURL)
	if err != nil {
		return nil, fmt.Errorf("fireworks: invalid base URL: %w", err)
	}

	clone := &Client{
		apiKey:            cfg.apiKey,
		accountID:         cfg.accountID,
		baseURL:           parsedBaseURL,
		baseURLOverridden: cfg.baseURLSet && cfg.baseURL != "",
		httpClient:        cfg.httpClient,
		defaultHeaders:    cfg.defaultHeaders.Clone(),
		defaultQuery:      cloneValues(cfg.defaultQuery),
		maxRetries:        cfg.maxRetries,
	}
	clone.initResources()
	return clone, nil
}

func (c *Client) MustWithOptions(opts ...ClientOption) *Client {
	client, err := c.WithOptions(opts...)
	if err != nil {
		panic(err)
	}
	return client
}

func (c *Client) APIKey() string {
	return c.apiKey
}

func (c *Client) AccountID() string {
	return c.accountID
}

func (c *Client) BaseURL() string {
	return c.baseURL.String()
}

func (c *Client) MaxRetries() int {
	return c.maxRetries
}

func (c *Client) initResources() {
	c.Chat = &ChatResource{client: c}
	c.Chat.Completions = &ChatCompletionsResource{client: c}
	c.Completions = &CompletionsResource{client: c}
	c.Messages = &MessagesResource{client: c}
	c.BatchInferenceJobs = &BatchInferenceJobsResource{client: c}
	c.Deployments = &DeploymentsResource{client: c}
	c.Models = &ModelsResource{client: c}
	c.Lora = &LoraResource{client: c}
	c.DeploymentShapes = &DeploymentShapesResource{client: c}
	c.DeploymentShapeVersions = &DeploymentShapeVersionsResource{client: c}
	c.Datasets = &DatasetsResource{client: c}
	c.SupervisedFineTuningJobs = &SupervisedFineTuningJobsResource{client: c}
	c.ReinforcementFineTuningJobs = &ReinforcementFineTuningJobsResource{client: c}
	c.ReinforcementFineTuningSteps = &ReinforcementFineTuningStepsResource{client: c}
	c.DPOJobs = &DPOJobsResource{client: c}
	c.EvaluationJobs = &EvaluationJobsResource{client: c}
	c.Evaluators = &EvaluatorsResource{client: c}
	c.Accounts = &AccountsResource{client: c}
	c.Users = &UsersResource{client: c}
	c.APIKeys = &APIKeysResource{client: c}
	c.Secrets = &SecretsResource{client: c}
}

func (c *Client) NewRequest(ctx context.Context, method, path string, body any, opts ...RequestOption) (*http.Request, error) {
	reqOpts := applyRequestOptions(opts)
	var reqBody io.Reader
	if len(reqOpts.ExtraBody) > 0 {
		var err error
		body, err = mergeExtraBody(body, reqOpts.ExtraBody)
		if err != nil {
			return nil, err
		}
	}
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("fireworks: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(payload)
	}
	return c.newRequestWithReader(ctx, method, path, reqBody, "application/json", opts...)
}

func (c *Client) newRequestWithReader(ctx context.Context, method, path string, reqBody io.Reader, contentType string, opts ...RequestOption) (*http.Request, error) {
	reqOpts := applyRequestOptions(opts)
	reqURL, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}
	query := reqURL.Query()
	for key, values := range c.defaultQuery {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	for key, values := range reqOpts.Query {
		query.Del(key)
		for _, value := range values {
			query.Add(key, value)
		}
	}
	reqURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reqBody)
	if err != nil {
		return nil, err
	}

	for key, values := range c.platformHeaders() {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("User-Agent", "Fireworks/Go "+Version)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-Stainless-Async", "false")
	req.Header.Set("X-Stainless-Read-Timeout", readTimeoutHeader(reqOpts.Timeout))
	for key, values := range c.defaultHeaders {
		if values == nil {
			omitRequestHeader(req.Header, key)
			continue
		}
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	for key, values := range reqOpts.Headers {
		if values == nil {
			omitRequestHeader(req.Header, key)
			continue
		}
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	return req, nil
}

func (c *Client) MultipartRequest(ctx context.Context, method, path string, fields map[string]any, files map[string]File, out any, opts ...RequestOption) error {
	reqOpts := applyRequestOptions(opts)
	var reqBody bytes.Buffer
	writer := multipart.NewWriter(&reqBody)
	for key, value := range reqOpts.ExtraBody {
		if fields == nil {
			fields = make(map[string]any)
		}
		fields[key] = value
	}
	for key, value := range fields {
		if err := writeMultipartField(writer, key, value); err != nil {
			return err
		}
	}
	for key, file := range files {
		if err := writeMultipartFile(writer, key, file); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}

	ctx, cancel := contextWithRequestTimeout(ctx, opts)
	if cancel != nil {
		defer cancel()
	}
	req, err := c.newRequestWithReader(ctx, method, path, &reqBody, writer.FormDataContentType(), opts...)
	if err != nil {
		return err
	}
	maxRetries := c.maxRetries
	if reqOpts.MaxRetries != nil {
		maxRetries = *reqOpts.MaxRetries
	}
	return c.do(req, out, maxRetries)
}

func (c *Client) Do(req *http.Request, out any) error {
	return c.do(req, out, c.maxRetries)
}

func (c *Client) do(req *http.Request, out any, maxRetries int) error {
	body, err := c.doBytes(req, maxRetries)
	if err != nil {
		return err
	}
	if out == nil || len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("fireworks: decode response: %w", err)
	}
	return nil
}

func (c *Client) DoBytes(req *http.Request) ([]byte, error) {
	return c.doBytes(req, c.maxRetries)
}

func (c *Client) doBytes(req *http.Request, maxRetries int) ([]byte, error) {
	var bodyCopy []byte
	if req.Body != nil {
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		bodyCopy = payload
		req.Body = io.NopCloser(bytes.NewReader(payload))
	}

	retries := maxRetries
	if retries < 0 {
		retries = 0
	}

	var lastErr error
	nextRetryDelay := retryDelay(0)
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(nextRetryDelay)
			select {
			case <-req.Context().Done():
				timer.Stop()
				return nil, req.Context().Err()
			case <-timer.C:
			}
		}

		attemptReq := req.Clone(req.Context())
		attemptReq.Header = req.Header.Clone()
		attemptReq.Header.Set("X-Stainless-Retry-Count", strconv.Itoa(attempt))
		if len(bodyCopy) > 0 {
			attemptReq.Body = io.NopCloser(bytes.NewReader(bodyCopy))
			attemptReq.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(bodyCopy)), nil
			}
		}

		resp, err := c.httpClient.Do(attemptReq)
		if err != nil {
			lastErr = err
			if attempt < retries && shouldRetryError(err) {
				nextRetryDelay = retryDelay(attempt)
				continue
			}
			return nil, requestError(attemptReq, err)
		}

		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt < retries && shouldRetryError(readErr) {
				nextRetryDelay = retryDelay(attempt)
				continue
			}
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, nil
		}

		if attempt < retries && shouldRetryResponse(resp, body) {
			lastErr = statusError(resp, body)
			nextRetryDelay = retryDelayForResponse(resp, attempt)
			continue
		}
		return nil, statusError(resp, body)
	}

	return nil, lastErr
}

func (c *Client) Request(ctx context.Context, method, path string, body any, out any, opts ...RequestOption) error {
	reqOpts := applyRequestOptions(opts)
	ctx, cancel := contextWithRequestTimeout(ctx, opts)
	if cancel != nil {
		defer cancel()
	}
	req, err := c.NewRequest(ctx, method, path, body, opts...)
	if err != nil {
		return err
	}
	maxRetries := c.maxRetries
	if reqOpts.MaxRetries != nil {
		maxRetries = *reqOpts.MaxRetries
	}
	return c.do(req, out, maxRetries)
}

func (c *Client) RequestRaw(ctx context.Context, method, path string, body any, opts ...RequestOption) ([]byte, error) {
	reqOpts := applyRequestOptions(opts)
	ctx, cancel := contextWithRequestTimeout(ctx, opts)
	if cancel != nil {
		defer cancel()
	}
	req, err := c.NewRequest(ctx, method, path, body, opts...)
	if err != nil {
		return nil, err
	}
	maxRetries := c.maxRetries
	if reqOpts.MaxRetries != nil {
		maxRetries = *reqOpts.MaxRetries
	}
	return c.doBytes(req, maxRetries)
}

func (c *Client) RequestText(ctx context.Context, method, path string, body any, opts ...RequestOption) (string, error) {
	bodyBytes, err := c.RequestRaw(ctx, method, path, body, opts...)
	if err != nil {
		return "", err
	}
	return string(bodyBytes), nil
}

func (c *Client) RequestBytes(ctx context.Context, method, path string, content []byte, out any, opts ...RequestOption) error {
	reqOpts := applyRequestOptions(opts)
	ctx, cancel := contextWithRequestTimeout(ctx, opts)
	if cancel != nil {
		defer cancel()
	}
	req, err := c.newRequestWithReader(ctx, method, path, bytes.NewReader(content), "application/octet-stream", opts...)
	if err != nil {
		return err
	}
	maxRetries := c.maxRetries
	if reqOpts.MaxRetries != nil {
		maxRetries = *reqOpts.MaxRetries
	}
	return c.do(req, out, maxRetries)
}

func (c *Client) RequestBytesRaw(ctx context.Context, method, path string, content []byte, opts ...RequestOption) ([]byte, error) {
	reqOpts := applyRequestOptions(opts)
	ctx, cancel := contextWithRequestTimeout(ctx, opts)
	if cancel != nil {
		defer cancel()
	}
	req, err := c.newRequestWithReader(ctx, method, path, bytes.NewReader(content), "application/octet-stream", opts...)
	if err != nil {
		return nil, err
	}
	maxRetries := c.maxRetries
	if reqOpts.MaxRetries != nil {
		maxRetries = *reqOpts.MaxRetries
	}
	return c.doBytes(req, maxRetries)
}

func (c *Client) RequestBytesText(ctx context.Context, method, path string, content []byte, opts ...RequestOption) (string, error) {
	bodyBytes, err := c.RequestBytesRaw(ctx, method, path, content, opts...)
	if err != nil {
		return "", err
	}
	return string(bodyBytes), nil
}

func (c *Client) Get(ctx context.Context, path string, opts ...RequestOption) (Response, error) {
	var out Response
	err := c.Request(ctx, http.MethodGet, path, nil, &out, opts...)
	return out, err
}

func (c *Client) GetRaw(ctx context.Context, path string, opts ...RequestOption) ([]byte, error) {
	return c.RequestRaw(ctx, http.MethodGet, path, nil, opts...)
}

func (c *Client) GetText(ctx context.Context, path string, opts ...RequestOption) (string, error) {
	return c.RequestText(ctx, http.MethodGet, path, nil, opts...)
}

func (c *Client) Post(ctx context.Context, path string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	err := c.Request(ctx, http.MethodPost, path, body, &out, opts...)
	return out, err
}

func (c *Client) PostRaw(ctx context.Context, path string, body any, opts ...RequestOption) ([]byte, error) {
	return c.RequestRaw(ctx, http.MethodPost, path, body, opts...)
}

func (c *Client) PostText(ctx context.Context, path string, body any, opts ...RequestOption) (string, error) {
	return c.RequestText(ctx, http.MethodPost, path, body, opts...)
}

func (c *Client) PostBytes(ctx context.Context, path string, content []byte, opts ...RequestOption) (Response, error) {
	var out Response
	err := c.RequestBytes(ctx, http.MethodPost, path, content, &out, opts...)
	return out, err
}

func (c *Client) PostBytesRaw(ctx context.Context, path string, content []byte, opts ...RequestOption) ([]byte, error) {
	return c.RequestBytesRaw(ctx, http.MethodPost, path, content, opts...)
}

func (c *Client) PostBytesText(ctx context.Context, path string, content []byte, opts ...RequestOption) (string, error) {
	return c.RequestBytesText(ctx, http.MethodPost, path, content, opts...)
}

func (c *Client) Patch(ctx context.Context, path string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	err := c.Request(ctx, http.MethodPatch, path, body, &out, opts...)
	return out, err
}

func (c *Client) PatchRaw(ctx context.Context, path string, body any, opts ...RequestOption) ([]byte, error) {
	return c.RequestRaw(ctx, http.MethodPatch, path, body, opts...)
}

func (c *Client) PatchText(ctx context.Context, path string, body any, opts ...RequestOption) (string, error) {
	return c.RequestText(ctx, http.MethodPatch, path, body, opts...)
}

func (c *Client) PatchBytes(ctx context.Context, path string, content []byte, opts ...RequestOption) (Response, error) {
	var out Response
	err := c.RequestBytes(ctx, http.MethodPatch, path, content, &out, opts...)
	return out, err
}

func (c *Client) PatchBytesRaw(ctx context.Context, path string, content []byte, opts ...RequestOption) ([]byte, error) {
	return c.RequestBytesRaw(ctx, http.MethodPatch, path, content, opts...)
}

func (c *Client) PatchBytesText(ctx context.Context, path string, content []byte, opts ...RequestOption) (string, error) {
	return c.RequestBytesText(ctx, http.MethodPatch, path, content, opts...)
}

func (c *Client) Put(ctx context.Context, path string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	err := c.Request(ctx, http.MethodPut, path, body, &out, opts...)
	return out, err
}

func (c *Client) PutRaw(ctx context.Context, path string, body any, opts ...RequestOption) ([]byte, error) {
	return c.RequestRaw(ctx, http.MethodPut, path, body, opts...)
}

func (c *Client) PutText(ctx context.Context, path string, body any, opts ...RequestOption) (string, error) {
	return c.RequestText(ctx, http.MethodPut, path, body, opts...)
}

func (c *Client) PutBytes(ctx context.Context, path string, content []byte, opts ...RequestOption) (Response, error) {
	var out Response
	err := c.RequestBytes(ctx, http.MethodPut, path, content, &out, opts...)
	return out, err
}

func (c *Client) PutBytesRaw(ctx context.Context, path string, content []byte, opts ...RequestOption) ([]byte, error) {
	return c.RequestBytesRaw(ctx, http.MethodPut, path, content, opts...)
}

func (c *Client) PutBytesText(ctx context.Context, path string, content []byte, opts ...RequestOption) (string, error) {
	return c.RequestBytesText(ctx, http.MethodPut, path, content, opts...)
}

func (c *Client) Delete(ctx context.Context, path string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	err := c.Request(ctx, http.MethodDelete, path, body, &out, opts...)
	return out, err
}

func (c *Client) DeleteRaw(ctx context.Context, path string, body any, opts ...RequestOption) ([]byte, error) {
	return c.RequestRaw(ctx, http.MethodDelete, path, body, opts...)
}

func (c *Client) DeleteText(ctx context.Context, path string, body any, opts ...RequestOption) (string, error) {
	return c.RequestText(ctx, http.MethodDelete, path, body, opts...)
}

func (c *Client) DeleteBytes(ctx context.Context, path string, content []byte, opts ...RequestOption) (Response, error) {
	var out Response
	err := c.RequestBytes(ctx, http.MethodDelete, path, content, &out, opts...)
	return out, err
}

func (c *Client) DeleteBytesRaw(ctx context.Context, path string, content []byte, opts ...RequestOption) ([]byte, error) {
	return c.RequestBytesRaw(ctx, http.MethodDelete, path, content, opts...)
}

func (c *Client) DeleteBytesText(ctx context.Context, path string, content []byte, opts ...RequestOption) (string, error) {
	return c.RequestBytesText(ctx, http.MethodDelete, path, content, opts...)
}

func (c *Client) Raw(ctx context.Context, method, path string, body any, opts ...RequestOption) (*http.Response, error) {
	ctx, cancel := contextWithRequestTimeout(ctx, opts)
	req, err := c.NewRequest(ctx, method, path, body, opts...)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, requestError(req, err)
	}
	if cancel != nil {
		if resp.Body != nil {
			resp.Body = &cancelReadCloser{ReadCloser: resp.Body, cancel: cancel}
		} else {
			cancel()
		}
	}
	return resp, nil
}

func (c *Client) resolveAccountID(opts RequestOptions) (string, error) {
	if opts.AccountID != "" {
		return opts.AccountID, nil
	}
	if c.accountID != "" {
		return c.accountID, nil
	}
	return "", &Error{Message: "missing account_id argument; provide it with WithDefaultAccountID, FIREWORKS_ACCOUNT_ID, or per request with WithAccountID"}
}

func (c *Client) managementPath(path string) string {
	if c.baseURLOverridden {
		return path
	}
	return defaultBaseURL + path
}

func (c *Client) inferencePath(path string) string {
	if c.baseURLOverridden {
		return path
	}
	return defaultBaseURL + "/inference" + path
}

func (c *Client) resolveURL(path string) (*url.URL, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return url.Parse(path)
	}
	base := *c.baseURL
	joined, err := url.JoinPath(base.String(), path)
	if err != nil {
		return nil, err
	}
	return url.Parse(joined)
}

func (c *Client) platformHeaders() http.Header {
	headers := make(http.Header)
	headers.Set("X-Stainless-Lang", "go")
	headers.Set("X-Stainless-Package-Version", Version)
	headers.Set("X-Stainless-OS", runtime.GOOS)
	headers.Set("X-Stainless-Arch", runtime.GOARCH)
	headers.Set("X-Stainless-Runtime", "go")
	headers.Set("X-Stainless-Runtime-Version", runtime.Version())
	return headers
}

func defaultHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = DefaultMaxConnectionsPerHost
	transport.MaxIdleConnsPerHost = DefaultMaxIdleConnectionsPerHost
	transport.DialContext = (&net.Dialer{
		Timeout:   defaultConnectLimit,
		KeepAlive: 30 * time.Second,
	}).DialContext
	return &http.Client{
		Timeout:   defaultTimeout,
		Transport: transport,
	}
}

func parseCustomHeadersEnv(raw string) http.Header {
	headers := make(http.Header)
	for _, line := range strings.Split(raw, "\n") {
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		headers.Set(strings.TrimSpace(line[:colon]), strings.TrimSpace(line[colon+1:]))
	}
	return headers
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values)
	for key, vals := range values {
		cloned[key] = append([]string(nil), vals...)
	}
	return cloned
}

func mergeExtraBody(body any, extra map[string]any) (map[string]any, error) {
	merged := make(map[string]any)
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("fireworks: marshal request body: %w", err)
		}
		if err := json.Unmarshal(payload, &merged); err != nil {
			return nil, fmt.Errorf("fireworks: merge extra body: %w", err)
		}
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged, nil
}

func contextWithRequestTimeout(ctx context.Context, opts []RequestOption) (context.Context, context.CancelFunc) {
	reqOpts := applyRequestOptions(opts)
	if reqOpts.Timeout <= 0 {
		return ctx, nil
	}
	return context.WithTimeout(ctx, reqOpts.Timeout)
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}

func writeMultipartField(writer *multipart.Writer, key string, value any) error {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return writer.WriteField(key, v)
	case []string:
		for _, item := range v {
			if err := writer.WriteField(key, item); err != nil {
				return err
			}
		}
		return nil
	case []byte:
		return writer.WriteField(key, string(v))
	default:
		payload, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("fireworks: marshal multipart field %q: %w", key, err)
		}
		return writer.WriteField(key, string(payload))
	}
}

func writeMultipartFile(writer *multipart.Writer, key string, file File) error {
	if file.Content == nil {
		return &Error{Message: "multipart file content must be set"}
	}
	filename := file.Filename
	if filename == "" {
		filename = key
	}

	var part io.Writer
	var err error
	if file.ContentType == "" {
		part, err = writer.CreateFormFile(key, filename)
	} else {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
			"name":     key,
			"filename": filename,
		}))
		header.Set("Content-Type", file.ContentType)
		part, err = writer.CreatePart(header)
	}
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file.Content); err != nil {
		return err
	}
	return nil
}

func omitRequestHeader(headers http.Header, key string) {
	headers.Del(key)
	if strings.EqualFold(key, "User-Agent") {
		headers.Set(key, "")
	}
}

func readTimeoutHeader(timeout time.Duration) string {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return strconv.FormatFloat(timeout.Seconds(), 'f', -1, 64)
}

func addQueryValue(values url.Values, key string, value any) {
	switch v := value.(type) {
	case nil:
		return
	case string:
		values.Set(key, v)
	case []string:
		values.Set(key, strings.Join(v, ","))
	case []int:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, strconv.Itoa(item))
		}
		values.Set(key, strings.Join(parts, ","))
	case []int64:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, strconv.FormatInt(item, 10))
		}
		values.Set(key, strings.Join(parts, ","))
	case []float64:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, strconv.FormatFloat(item, 'f', -1, 64))
		}
		values.Set(key, strings.Join(parts, ","))
	case []bool:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, strconv.FormatBool(item))
		}
		values.Set(key, strings.Join(parts, ","))
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, fmt.Sprint(item))
		}
		values.Set(key, strings.Join(parts, ","))
	default:
		values.Set(key, fmt.Sprint(v))
	}
}

func retryDelay(attempt int) time.Duration {
	multiplier := math.Pow(2, float64(attempt))
	delay := time.Duration(float64(initialRetryDelay) * multiplier)
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	jitter := 1 - (0.25 * rand.Float64())
	return time.Duration(float64(delay) * jitter)
}

func retryDelayForResponse(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if delay, ok := parseRetryAfter(resp.Header); ok {
			return delay
		}
	}
	return retryDelay(attempt)
}

func parseRetryAfter(headers http.Header) (time.Duration, bool) {
	if headers == nil {
		return 0, false
	}
	if raw := headers.Get("retry-after-ms"); raw != "" {
		if ms, err := strconv.ParseFloat(raw, 64); err == nil {
			delay := time.Duration(ms * float64(time.Millisecond))
			if isReasonableRetryDelay(delay) {
				return delay, true
			}
		}
	}
	if raw := headers.Get("retry-after"); raw != "" {
		if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
			delay := time.Duration(seconds * float64(time.Second))
			if isReasonableRetryDelay(delay) {
				return delay, true
			}
		}
		if retryAt, err := http.ParseTime(raw); err == nil {
			delay := time.Until(retryAt)
			if isReasonableRetryDelay(delay) {
				return delay, true
			}
		}
	}
	return 0, false
}

func isReasonableRetryDelay(delay time.Duration) bool {
	return delay > 0 && delay <= time.Minute
}

func shouldRetryError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func shouldRetryResponse(resp *http.Response, _ []byte) bool {
	if resp == nil {
		return false
	}
	if resp.Header.Get("X-Should-Retry") == "true" {
		return true
	}
	if resp.Header.Get("X-Should-Retry") == "false" {
		return false
	}
	switch resp.StatusCode {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return true
	default:
		return resp.StatusCode >= 500
	}
}

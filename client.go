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
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL      = "https://api.fireworks.ai"
	defaultTimeout      = time.Minute
	defaultConnectLimit = 5 * time.Second
	defaultMaxRetries   = 2
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
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("fireworks: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(payload)
	}

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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Fireworks/Go "+Version)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-Stainless-Async", "false")
	for key, values := range c.defaultHeaders {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	for key, values := range reqOpts.Headers {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	return req, nil
}

func (c *Client) Do(req *http.Request, out any) error {
	var bodyCopy []byte
	if req.Body != nil {
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			return err
		}
		bodyCopy = payload
		req.Body = io.NopCloser(bytes.NewReader(payload))
	}

	retries := c.maxRetries
	if retries < 0 {
		retries = 0
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt - 1)
			timer := time.NewTimer(delay)
			select {
			case <-req.Context().Done():
				timer.Stop()
				return req.Context().Err()
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
				continue
			}
			return err
		}

		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt < retries && shouldRetryError(readErr) {
				continue
			}
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out == nil || len(strings.TrimSpace(string(body))) == 0 {
				return nil
			}
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("fireworks: decode response: %w", err)
			}
			return nil
		}

		if attempt < retries && shouldRetryResponse(resp, body) {
			lastErr = statusError(resp, body)
			continue
		}
		return statusError(resp, body)
	}

	return lastErr
}

func (c *Client) Request(ctx context.Context, method, path string, body any, out any, opts ...RequestOption) error {
	ctx, cancel := contextWithRequestTimeout(ctx, opts)
	if cancel != nil {
		defer cancel()
	}
	req, err := c.NewRequest(ctx, method, path, body, opts...)
	if err != nil {
		return err
	}
	return c.Do(req, out)
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
		return nil, err
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
	transport.MaxConnsPerHost = 1000
	transport.MaxIdleConnsPerHost = 20
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

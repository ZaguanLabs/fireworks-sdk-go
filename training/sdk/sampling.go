package sdk

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ServerMetrics struct {
	NumConcurrentRequests   *int
	PrefillQueueDuration    *float64
	GenerationQueueDuration *float64
	ServerTTFT              *float64
	CachedPromptTokens      *int
	PromptTokens            *int
	ServerProcessingTime    *float64
	ClientTTFT              *float64
}

type SampledCompletion struct {
	Text              string
	FullTokens        []int
	PromptLen         int
	FinishReason      string
	CompletionLen     int
	InferenceLogprobs []float64
	LogprobsEchoed    bool
	RoutingMatrices   []string
}

type DeploymentSamplerTimeoutError struct {
	Message string
	Err     error
}

func (e *DeploymentSamplerTimeoutError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "deployment sampler timeout"
}

func (e *DeploymentSamplerTimeoutError) Unwrap() error {
	return e.Err
}

type FiretitanSampledSequence struct {
	StopReason string
	Tokens     []int
	Logprobs   []float64
}

type FiretitanSampleResponse struct {
	Sequences      []FiretitanSampledSequence
	PromptLogprobs []*float64
}

type FiretitanSamplingParams struct {
	MaxTokens   *int
	Stop        any
	Temperature *float64
	TopP        *float64
	TopK        *int
	Seed        *int
}

type FiretitanSampleOptions struct {
	IncludePromptLogprobs bool
	TopKPromptLogprobs    int
}

type DeploymentTokenizer interface {
	ApplyChatTemplate(messages []map[string]string) ([]int, error)
	Decode(tokenIDs []int) (string, error)
}

type SamplingConcurrencyController interface {
	Acquire(context.Context) error
	Release(*ServerMetrics)
}

type CompletionRequestOptions struct {
	MaxTokens            int
	Temperature          float64
	RawOutput            bool
	Logprobs             bool
	Echo                 bool
	IncludeRoutingMatrix bool
	Stop                 []string
	Extra                map[string]any
}

type CompletionRequester func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error)

type DeploymentSampler struct {
	InferenceURL string
	Model        string
	APIKey       string
	Tokenizer    DeploymentTokenizer

	ConcurrencyController SamplingConcurrencyController
	CompletionRequester   CompletionRequester
	HTTPClient            *http.Client
	Now                   func() time.Time
	Sleep                 func(time.Duration)
	RetryJitter           func() float64
	maxConcurrency        int

	mu            sync.Mutex
	recentMetrics []ServerMetrics
}

type DeploymentSamplerOption func(*DeploymentSampler)

func WithDeploymentSamplerTokenizer(tokenizer DeploymentTokenizer) DeploymentSamplerOption {
	return func(s *DeploymentSampler) {
		s.Tokenizer = tokenizer
	}
}

func WithDeploymentSamplerConcurrencyController(controller SamplingConcurrencyController) DeploymentSamplerOption {
	return func(s *DeploymentSampler) {
		s.ConcurrencyController = controller
	}
}

func WithDeploymentSamplerMaxConcurrency(maxConcurrency int) DeploymentSamplerOption {
	return func(s *DeploymentSampler) {
		s.maxConcurrency = maxConcurrency
	}
}

func WithDeploymentSamplerRequester(requester CompletionRequester) DeploymentSamplerOption {
	return func(s *DeploymentSampler) {
		s.CompletionRequester = requester
	}
}

func WithDeploymentSamplerHTTPClient(client *http.Client) DeploymentSamplerOption {
	return func(s *DeploymentSampler) {
		s.HTTPClient = client
	}
}

func WithDeploymentSamplerClock(now func() time.Time, sleep func(time.Duration)) DeploymentSamplerOption {
	return func(s *DeploymentSampler) {
		if now != nil {
			s.Now = now
		}
		if sleep != nil {
			s.Sleep = sleep
		}
	}
}

func WithDeploymentSamplerRetryJitter(jitter func() float64) DeploymentSamplerOption {
	return func(s *DeploymentSampler) {
		if jitter != nil {
			s.RetryJitter = jitter
		}
	}
}

func NewDeploymentSampler(inferenceURL, model, apiKey string, opts ...DeploymentSamplerOption) *DeploymentSampler {
	sampler := &DeploymentSampler{
		InferenceURL: strings.TrimRight(inferenceURL, "/"),
		Model:        model,
		APIKey:       apiKey,
		HTTPClient:   &http.Client{Timeout: 10 * time.Minute},
		Now:          time.Now,
		Sleep:        time.Sleep,
		RetryJitter:  rand.Float64,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(sampler)
		}
	}
	if sampler.CompletionRequester == nil {
		sampler.CompletionRequester = sampler.defaultCompletionRequest
	}
	if sampler.ConcurrencyController == nil && sampler.maxConcurrency > 0 {
		sampler.ConcurrencyController = NewFixedConcurrencyController(sampler.maxConcurrency)
	}
	return sampler
}

type SampleOptions struct {
	N                        int
	MaxTokens                int
	Temperature              float64
	MaxSeqLen                int
	Logprobs                 bool
	Echo                     bool
	IncludeRoutingMatrix     bool
	Stop                     any
	Extra                    map[string]any
	TimeoutDiagnosticContext any
}

const (
	SamplerHotloadRetryInterval = 5 * time.Second
	SamplerHotloadMaxRetries    = 10
	SamplerRetryMaxAttempts     = 7
	SamplerRetryBaseBackoff     = 2 * time.Second
	SamplerRetryMaxBackoff      = 30 * time.Second
)

var samplerRetryHTTPTransientCodes = map[int]bool{
	http.StatusRequestTimeout:     true,
	http.StatusTooManyRequests:    true,
	http.StatusBadGateway:         true,
	http.StatusServiceUnavailable: true,
	http.StatusGatewayTimeout:     true,
}

type CompletionHTTPStatusError struct {
	StatusCode int
	Body       []byte
}

func (e *CompletionHTTPStatusError) Error() string {
	return fmt.Sprintf("completions: HTTP %d: %s", e.StatusCode, ParseAPIErrorBody(e.Body))
}

func (s *DeploymentSampler) SampleWithTokens(ctx context.Context, messages []map[string]string, opts ...SampleOptions) ([]SampledCompletion, error) {
	if s.Tokenizer == nil {
		return nil, fmt.Errorf("tokenizer is required for SampleWithTokens")
	}
	promptIDs, err := s.Tokenizer.ApplyChatTemplate(messages)
	if err != nil {
		return nil, err
	}
	return s.SampleWithPromptTokens(ctx, promptIDs, opts...)
}

func (s *DeploymentSampler) SampleWithPromptTokens(ctx context.Context, promptTokenIDs []int, opts ...SampleOptions) ([]SampledCompletion, error) {
	opt := sampleOptions(opts...)
	if opt.MaxSeqLen > 0 && len(promptTokenIDs) >= opt.MaxSeqLen {
		return nil, nil
	}
	stop, err := s.resolveStop(opt.Stop)
	if err != nil {
		return nil, err
	}
	if opt.N <= 0 {
		return nil, nil
	}
	if opt.N == 1 {
		return s.doOneCompletion(ctx, promptTokenIDs, opt, stop)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	batches := make([][]SampledCompletion, opt.N)
	errs := make(chan error, opt.N)
	var wg sync.WaitGroup
	for i := 0; i < opt.N; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			batch, err := s.doOneCompletion(ctx, promptTokenIDs, opt, stop)
			if err != nil {
				errs <- err
				cancel()
				return
			}
			batches[index] = batch
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return nil, err
		}
	}
	var out []SampledCompletion
	for _, batch := range batches {
		out = append(out, batch...)
	}
	return out, nil
}

func (s *DeploymentSampler) DrainMetrics() []ServerMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]ServerMetrics(nil), s.recentMetrics...)
	s.recentMetrics = nil
	return out
}

func (s *DeploymentSampler) FiretitanSamplingClient() *FiretitanSamplingClient {
	return NewFiretitanSamplingClient(s)
}

type FiretitanSamplingClient struct {
	DeploymentSampler *DeploymentSampler
	mu                sync.Mutex
	closed            bool
}

func NewFiretitanSamplingClient(sampler *DeploymentSampler) *FiretitanSamplingClient {
	return &FiretitanSamplingClient{DeploymentSampler: sampler}
}

func NewFiretitanSamplingClientForDeployment(inferenceURL, model, apiKey string, opts ...DeploymentSamplerOption) *FiretitanSamplingClient {
	return NewFiretitanSamplingClient(NewDeploymentSampler(inferenceURL, model, apiKey, opts...))
}

func (c *FiretitanSamplingClient) Sample(ctx context.Context, prompt []int, numSamples int, params FiretitanSamplingParams, opts ...FiretitanSampleOptions) (FiretitanSampleResponse, error) {
	if c == nil || c.DeploymentSampler == nil {
		return FiretitanSampleResponse{}, fmt.Errorf("FiretitanSamplingClient requires a DeploymentSampler")
	}
	if err := c.ensureOpen(); err != nil {
		return FiretitanSampleResponse{}, err
	}
	var opt FiretitanSampleOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.TopKPromptLogprobs != 0 {
		return FiretitanSampleResponse{}, fmt.Errorf("FiretitanSamplingClient does not support topk_prompt_logprobs yet")
	}

	maxTokens := 1024
	if params.MaxTokens != nil {
		maxTokens = *params.MaxTokens
	}
	temperature := 1.0
	if params.Temperature != nil {
		temperature = *params.Temperature
	}
	extra := map[string]any{}
	if params.TopP != nil {
		extra["top_p"] = *params.TopP
	}
	if params.TopK != nil && *params.TopK >= 0 {
		extra["top_k"] = *params.TopK
	}
	if params.Seed != nil {
		extra["seed"] = *params.Seed
	}
	completions, err := c.DeploymentSampler.SampleWithPromptTokens(ctx, prompt, SampleOptions{
		N:           numSamples,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stop:        params.Stop,
		Logprobs:    true,
		Echo:        opt.IncludePromptLogprobs,
		Extra:       extra,
	})
	if err != nil {
		return FiretitanSampleResponse{}, err
	}

	response := FiretitanSampleResponse{Sequences: make([]FiretitanSampledSequence, 0, len(completions))}
	for _, completion := range completions {
		completionTokens := append([]int(nil), completion.FullTokens[completion.PromptLen:]...)
		completionLogprobs := append([]float64(nil), completion.InferenceLogprobs...)
		if completion.LogprobsEchoed && completion.InferenceLogprobs != nil {
			if completion.PromptLen == 0 {
				response.PromptLogprobs = []*float64{}
			} else {
				response.PromptLogprobs = make([]*float64, completion.PromptLen)
				for i := 1; i < completion.PromptLen && i <= len(completion.InferenceLogprobs); i++ {
					value := completion.InferenceLogprobs[i-1]
					response.PromptLogprobs[i] = &value
				}
			}
			start := completion.PromptLen - 1
			if start < 0 {
				start = 0
			}
			if start <= len(completion.InferenceLogprobs) {
				completionLogprobs = append([]float64(nil), completion.InferenceLogprobs[start:]...)
			}
		}
		response.Sequences = append(response.Sequences, FiretitanSampledSequence{
			StopReason: firetitanStopReason(completion.FinishReason),
			Tokens:     completionTokens,
			Logprobs:   completionLogprobs,
		})
	}
	if !opt.IncludePromptLogprobs {
		response.PromptLogprobs = nil
	}
	return response, nil
}

func (c *FiretitanSamplingClient) ComputeLogprobs(ctx context.Context, prompt []int) ([]*float64, error) {
	maxTokens := 1
	temperature := 1.0
	topP := 1.0
	response, err := c.Sample(ctx, prompt, 1, FiretitanSamplingParams{
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
		TopP:        &topP,
	}, FiretitanSampleOptions{IncludePromptLogprobs: true})
	if err != nil {
		return nil, err
	}
	return response.PromptLogprobs, nil
}

func (c *FiretitanSamplingClient) GetTokenizer() (DeploymentTokenizer, error) {
	if c == nil || c.DeploymentSampler == nil || c.DeploymentSampler.Tokenizer == nil {
		return nil, fmt.Errorf("DeploymentSampler was created without a tokenizer")
	}
	return c.DeploymentSampler.Tokenizer, nil
}

func (c *FiretitanSamplingClient) GetBaseModel() string {
	if c == nil || c.DeploymentSampler == nil {
		return ""
	}
	return c.DeploymentSampler.Model
}

func (c *FiretitanSamplingClient) GetTelemetry() any {
	return nil
}

func (c *FiretitanSamplingClient) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	alreadyClosed := c.closed
	c.closed = true
	sampler := c.DeploymentSampler
	c.mu.Unlock()
	if alreadyClosed || sampler == nil {
		return
	}
	sampler.Close()
}

func (c *FiretitanSamplingClient) ensureOpen() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("FiretitanSamplingClient is closed")
	}
	return nil
}

func (s *DeploymentSampler) Close() {
	if s == nil || s.HTTPClient == nil || s.HTTPClient.Transport == nil {
		return
	}
	if transport, ok := s.HTTPClient.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

func (s *DeploymentSampler) doOneCompletion(ctx context.Context, promptIDs []int, opt SampleOptions, stop []string) ([]SampledCompletion, error) {
	backoff := SamplerRetryBaseBackoff
	for attempt := 1; attempt <= SamplerRetryMaxAttempts; attempt++ {
		if s.ConcurrencyController != nil {
			if err := s.ConcurrencyController.Acquire(ctx); err != nil {
				return nil, err
			}
		}
		var metrics *ServerMetrics
		result, serverMetrics, err := s.CompletionRequester(ctx, promptIDs, CompletionRequestOptions{
			MaxTokens:            opt.MaxTokens,
			Temperature:          opt.Temperature,
			RawOutput:            true,
			Logprobs:             opt.Logprobs,
			Echo:                 opt.Echo,
			IncludeRoutingMatrix: opt.IncludeRoutingMatrix,
			Stop:                 stop,
			Extra:                cloneAnyMap(opt.Extra),
		})
		if err == nil {
			metrics = &serverMetrics
		}
		if s.ConcurrencyController != nil {
			s.ConcurrencyController.Release(metrics)
		}
		if metrics != nil {
			s.mu.Lock()
			s.recentMetrics = append(s.recentMetrics, *metrics)
			s.mu.Unlock()
		}
		if err == nil {
			return s.ParseCompletionsResult(result, promptIDs, opt.MaxSeqLen, opt.Logprobs, opt.IncludeRoutingMatrix, opt.Echo)
		}
		if !samplerRetryableCompletionError(err) {
			return nil, err
		}
		if attempt == SamplerRetryMaxAttempts {
			if samplerTimeoutLikeError(err) {
				return nil, &DeploymentSamplerTimeoutError{
					Message: s.timeoutDiagnostic(err.Error(), promptIDs, opt, true),
					Err:     err,
				}
			}
			return nil, err
		}
		s.Sleep(s.retryBackoffDelay(backoff))
		backoff *= 2
		if backoff > SamplerRetryMaxBackoff {
			backoff = SamplerRetryMaxBackoff
		}
	}
	return nil, fmt.Errorf("unreachable: sampler retry loop exited")
}

func samplerTimeoutLikeError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var statusErr *CompletionHTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusRequestTimeout || statusErr.StatusCode == http.StatusGatewayTimeout
	}
	return false
}

func (s *DeploymentSampler) timeoutDiagnostic(label string, promptIDs []int, opt SampleOptions, exhausted bool) string {
	httpTimeout := "600s"
	if s.HTTPClient != nil && s.HTTPClient.Timeout > 0 {
		httpTimeout = fmt.Sprintf("%.0fs", s.HTTPClient.Timeout.Seconds())
	}
	summary := "DeploymentSampler request hit a timeout-like transient."
	if exhausted {
		summary = "DeploymentSampler request failed after exhausting retries on a timeout-like error."
	}
	fields := []string{
		summary,
		"raw_error=" + label,
		"model=" + s.Model,
		fmt.Sprintf("prompt_tokens=%d", len(promptIDs)),
		fmt.Sprintf("max_tokens=%d", opt.MaxTokens),
		"http_timeout=" + httpTimeout,
	}
	contextFields, isRLRollout := formatTimeoutDiagnosticContext(opt.TimeoutDiagnosticContext)
	if exhausted {
		if window := concurrencyWindowSize(s.ConcurrencyController); window != "" {
			fields = append(fields, "sampler_concurrency_window="+window)
		}
		fields = append(fields, contextFields...)
		fields = append(fields, s.recentMetricsDiagnostic()...)
		if isRLRollout {
			fields = append(fields, "RL rollout context detected. If recent queue/TTFT metrics are high, rollout sampling may be exceeding sampler capacity.")
			fields = append(fields, "Check recent queue/TTFT metrics. If they are elevated, reduce rollout concurrency and/or max_completion_tokens, or increase sampler capacity; otherwise investigate gateway, network, or client timeout limits.")
		} else {
			fields = append(fields, "Check serving queue/TTFT metrics, gateway timeout limits, network stability, and request shape before changing capacity.")
		}
	} else if isRLRollout {
		for _, field := range contextFields {
			if strings.HasPrefix(field, "max_concurrency_rollout_sample=") {
				fields = append(fields, field)
			}
		}
		fields = append(fields, "If this repeats and serving queue/TTFT metrics are high, reduce rollout concurrency/completion tokens or increase sampler capacity.")
	}
	return strings.Join(fields, " ")
}

func formatTimeoutDiagnosticContext(contextValue any) ([]string, bool) {
	contextMap, ok := contextValue.(map[string]any)
	if !ok {
		return nil, false
	}
	keys := make([]string, 0, len(contextMap))
	for key, value := range contextMap {
		if value != nil {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	fields := make([]string, 0, len(keys))
	isRLRollout := false
	for _, key := range keys {
		value := contextMap[key]
		if key == "workload" && (value == "async_rl_rollout" || value == "rl_rollout") {
			isRLRollout = true
		}
		fields = append(fields, fmt.Sprintf("%s=%v", key, value))
	}
	return fields, isRLRollout
}

func concurrencyWindowSize(controller SamplingConcurrencyController) string {
	if withWindow, ok := controller.(interface{ WindowSize() int }); ok {
		return fmt.Sprint(withWindow.WindowSize())
	}
	return ""
}

func (s *DeploymentSampler) recentMetricsDiagnostic() []string {
	s.mu.Lock()
	recent := append([]ServerMetrics(nil), s.recentMetrics...)
	s.mu.Unlock()
	if len(recent) > 32 {
		recent = recent[len(recent)-32:]
	}
	if len(recent) == 0 {
		return nil
	}
	var prefill, generation, ttft []float64
	var concurrent []int
	for _, metric := range recent {
		if metric.PrefillQueueDuration != nil {
			prefill = append(prefill, *metric.PrefillQueueDuration)
		}
		if metric.GenerationQueueDuration != nil {
			generation = append(generation, *metric.GenerationQueueDuration)
		}
		if metric.ClientTTFT != nil {
			ttft = append(ttft, *metric.ClientTTFT)
		}
		if metric.NumConcurrentRequests != nil {
			concurrent = append(concurrent, *metric.NumConcurrentRequests)
		}
	}
	var fields []string
	if value, ok := p95(prefill); ok {
		fields = append(fields, fmt.Sprintf("recent_prefill_queue_p95=%.1fs", value))
	}
	if value, ok := p95(generation); ok {
		fields = append(fields, fmt.Sprintf("recent_generation_queue_p95=%.1fs", value))
	}
	if value, ok := p95(ttft); ok {
		fields = append(fields, fmt.Sprintf("recent_client_ttft_p95=%.1fs", value))
	}
	if len(concurrent) > 0 {
		maxConcurrent := concurrent[0]
		for _, value := range concurrent[1:] {
			if value > maxConcurrent {
				maxConcurrent = value
			}
		}
		fields = append(fields, fmt.Sprintf("recent_concurrent_requests_max=%d", maxConcurrent))
	}
	return fields
}

func p95(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sort.Float64s(values)
	index := int(math.Ceil(0.95*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index], true
}

func (s *DeploymentSampler) retryBackoffDelay(backoff time.Duration) time.Duration {
	jitter := 0.5
	if s.RetryJitter != nil {
		jitter = s.RetryJitter()
	}
	if jitter < 0 {
		jitter = 0
	} else if jitter > 1 {
		jitter = 1
	}
	return time.Duration(float64(backoff) * (0.5 + jitter))
}

func (s *DeploymentSampler) ParseCompletionsResult(result map[string]any, promptIDs []int, maxSeqLen int, userRequestedLogprobs, routingRequested, echoMode bool) ([]SampledCompletion, error) {
	choices, _ := result["choices"].([]any)
	completions := make([]SampledCompletion, 0, len(choices))
	for _, item := range choices {
		choice, _ := item.(map[string]any)
		if choice == nil {
			continue
		}
		text := stringFromAny(choice["text"])
		finishReason := stringOrDefault(choice["finish_reason"], "unknown")
		raw, _ := choice["raw_output"].(map[string]any)
		if raw == nil {
			raw = map[string]any{}
		}
		completionIDs, ok := intSliceFromAny(raw["completion_token_ids"])
		if !ok {
			return nil, fmt.Errorf("%s", FormatSDKError(
				"Deployment did not return raw_output token IDs",
				fmt.Sprintf("The API response is missing completion_token_ids. Got choice keys: %v", mapKeys(choice)),
				"The sampler requested raw_output=True, which is required for token-in RL rollouts. Use a deployment path that returns raw_output.completion_token_ids for completions.",
				SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: true},
			))
		}

		var tokenLogprobs []float64
		if userRequestedLogprobs {
			tokenLogprobs, _ = ExtractLogprobs(choice)
		}
		var routingMatrices []string
		if routingRequested {
			routingMatrices, _ = ExtractRoutingMatrices(choice)
		}

		promptForFull := append([]int(nil), promptIDs...)
		if expanded, ok := intSliceFromAny(choice["prompt_token_ids"]); ok {
			promptForFull = expanded
		} else if expanded, ok := intSliceFromAny(raw["prompt_token_ids"]); ok {
			promptForFull = expanded
		}

		logprobsEchoed := false
		if echoMode {
			if len(completionIDs) < len(promptForFull) || !intSlicePrefixEqual(completionIDs, promptForFull) {
				return nil, fmt.Errorf("%s", FormatSDKError(
					"Echo response format mismatch",
					"echo=True was requested but completion_token_ids do not include the prompt prefix.",
					"The sampler uses echo=True to align prompt and completion token logprobs. Use a deployment path whose raw_output token IDs include the prompt prefix when echo is enabled.",
					SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: true},
				))
			}
			completionIDs = append([]int(nil), completionIDs[len(promptForFull):]...)
			if tokenLogprobs != nil {
				if len(tokenLogprobs) > 0 {
					tokenLogprobs = append([]float64(nil), tokenLogprobs[1:]...)
				}
				logprobsEchoed = true
			}
			if routingMatrices != nil {
				if len(routingMatrices) > 0 {
					routingMatrices = append([]string(nil), routingMatrices[1:]...)
				}
			}
		}

		fullTokens := append(append([]int(nil), promptForFull...), completionIDs...)
		if maxSeqLen > 0 && len(fullTokens) > maxSeqLen {
			continue
		}
		completions = append(completions, SampledCompletion{
			Text:              text,
			FullTokens:        fullTokens,
			PromptLen:         len(promptForFull),
			FinishReason:      finishReason,
			CompletionLen:     len(completionIDs),
			InferenceLogprobs: tokenLogprobs,
			LogprobsEchoed:    logprobsEchoed,
			RoutingMatrices:   routingMatrices,
		})
	}
	return completions, nil
}

func ExtractLogprobs(choice map[string]any) ([]float64, bool) {
	lpData, _ := choice["logprobs"].(map[string]any)
	if lpData == nil {
		return nil, false
	}
	content, _ := lpData["content"].([]any)
	if len(content) == 0 {
		return nil, false
	}
	out := make([]float64, 0, len(content))
	for _, item := range content {
		token, _ := item.(map[string]any)
		out = append(out, floatFromAny(token["logprob"]))
	}
	return out, true
}

func ExtractRoutingMatrices(choice map[string]any) ([]string, bool) {
	lpData, _ := choice["logprobs"].(map[string]any)
	if lpData == nil {
		return nil, false
	}
	content, _ := lpData["content"].([]any)
	if len(content) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(content))
	anySet := false
	for _, item := range content {
		token, _ := item.(map[string]any)
		matrix := stringFromAny(token["routing_matrix"])
		if matrix != "" {
			anySet = true
		}
		out = append(out, matrix)
	}
	if !anySet {
		return nil, false
	}
	return out, true
}

func (s *DeploymentSampler) defaultCompletionRequest(ctx context.Context, prompt []int, opts CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
	return s.StreamCompletions(ctx, prompt, opts)
}

func (s *DeploymentSampler) StreamCompletions(ctx context.Context, prompt []int, opts CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
	payload := map[string]any{
		"model":                    s.Model,
		"prompt":                   prompt,
		"n":                        1,
		"max_tokens":               opts.MaxTokens,
		"temperature":              opts.Temperature,
		"stream":                   true,
		"perf_metrics_in_response": true,
	}
	if opts.RawOutput {
		payload["raw_output"] = true
	}
	if opts.Logprobs {
		payload["logprobs"] = true
	}
	if opts.Echo {
		payload["echo"] = true
	}
	if opts.IncludeRoutingMatrix {
		payload["include_routing_matrix"] = true
	}
	if len(opts.Stop) > 0 {
		payload["stop"] = opts.Stop
	}
	for key, value := range opts.Extra {
		payload[key] = value
	}
	if _, hasImages := opts.Extra["images"]; hasImages {
		if _, exists := payload["return_token_ids"]; !exists {
			payload["return_token_ids"] = true
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, ServerMetrics{}, err
	}
	var lastStatus int
	for hotloadAttempt := 0; hotloadAttempt <= SamplerHotloadMaxRetries; hotloadAttempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.InferenceURL+"/inference/v1/completions", bytes.NewReader(data))
		if err != nil {
			return nil, ServerMetrics{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", s.APIKey)
		req.Header.Set("Authorization", "Bearer "+s.APIKey)
		resp, err := s.HTTPClient.Do(req)
		if err != nil {
			return nil, ServerMetrics{}, err
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		lastStatus = resp.StatusCode
		if (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusTooEarly) && hotloadAttempt < SamplerHotloadMaxRetries {
			s.Sleep(SamplerHotloadRetryInterval)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, ServerMetricsFromHTTPHeaders(resp.Header), &CompletionHTTPStatusError{StatusCode: resp.StatusCode, Body: body}
		}
		return s.assembleStreamResponse(body, resp.Header)
	}
	return nil, ServerMetrics{}, &CompletionHTTPStatusError{StatusCode: lastStatus}
}

func (s *DeploymentSampler) assembleStreamResponse(body []byte, headers http.Header) (map[string]any, ServerMetrics, error) {
	events, err := NewSSEDecoder().Decode(bytes.NewReader(body))
	if err != nil {
		return nil, ServerMetrics{}, err
	}
	accumulatedText := ""
	var accumulatedLogprobs []any
	finishReason := ""
	var usageInfo any
	var rawOutput map[string]any
	var perfMetrics map[string]string
	hasSeenDone := false
	hasSeenFinishReason := false

	for _, event := range events {
		if strings.HasPrefix(event.Data, "[DONE]") {
			hasSeenDone = true
			break
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			continue
		}
		choices, _ := chunk["choices"].([]any)
		for _, item := range choices {
			choice, _ := item.(map[string]any)
			if choice == nil {
				continue
			}
			if textDelta := stringFromAny(choice["text"]); textDelta != "" {
				accumulatedText += textDelta
			}
			if lp, _ := choice["logprobs"].(map[string]any); lp != nil {
				if content, _ := lp["content"].([]any); len(content) > 0 {
					accumulatedLogprobs = append(accumulatedLogprobs, content...)
				}
			}
			if fr := stringFromAny(choice["finish_reason"]); fr != "" {
				finishReason = fr
				hasSeenFinishReason = true
			}
			if ro, _ := choice["raw_output"].(map[string]any); ro != nil {
				rawOutput = ro
			}
		}
		if usage, ok := chunk["usage"]; ok {
			usageInfo = usage
		}
		if perf, _ := chunk["perf_metrics"].(map[string]any); perf != nil {
			perfMetrics = stringMapFromAny(perf)
		}
	}
	if rawOutput == nil && !hasSeenDone && !hasSeenFinishReason {
		return nil, ServerMetrics{}, &SSETruncationError{Message: "Transient server-side error: the inference deployment closed the SSE stream mid-generation without sending [DONE], finish_reason, or raw_output. The SDK is retrying. If this persists across all retry attempts, contact the Fireworks team."}
	}
	if finishReason == "" {
		finishReason = "stop"
	}
	choice := map[string]any{
		"text":          accumulatedText,
		"finish_reason": finishReason,
	}
	if len(accumulatedLogprobs) > 0 {
		choice["logprobs"] = map[string]any{"content": accumulatedLogprobs}
	}
	if rawOutput != nil {
		choice["raw_output"] = rawOutput
	}
	result := map[string]any{"choices": []any{choice}}
	if usageInfo != nil {
		result["usage"] = usageInfo
	}
	if perfMetrics != nil {
		return result, ServerMetricsFromHeaders(perfMetrics), nil
	}
	return result, ServerMetricsFromHTTPHeaders(headers), nil
}

func ServerMetricsFromHeaders(headers map[string]string, clientTTFT ...float64) ServerMetrics {
	metrics := ServerMetrics{
		NumConcurrentRequests:   parseOptionalInt(headers["num-concurrent-requests"]),
		PrefillQueueDuration:    parseOptionalFloat(headers["prefill-queue-duration"]),
		GenerationQueueDuration: parseOptionalFloat(headers["generation-queue-duration"]),
		ServerTTFT:              parseOptionalFloat(headers["server-time-to-first-token"]),
		CachedPromptTokens:      parseOptionalInt(headers["cached-prompt-tokens"]),
		PromptTokens:            parseOptionalInt(headers["prompt-tokens"]),
		ServerProcessingTime:    parseOptionalFloat(headers["server-processing-time"]),
	}
	if len(clientTTFT) > 0 {
		metrics.ClientTTFT = &clientTTFT[0]
	}
	return metrics
}

func ServerMetricsFromHTTPHeaders(headers http.Header, clientTTFT ...float64) ServerMetrics {
	values := map[string]string{
		"num-concurrent-requests":    headers.Get("num-concurrent-requests"),
		"prefill-queue-duration":     headers.Get("prefill-queue-duration"),
		"generation-queue-duration":  headers.Get("generation-queue-duration"),
		"server-time-to-first-token": headers.Get("server-time-to-first-token"),
		"cached-prompt-tokens":       headers.Get("cached-prompt-tokens"),
		"prompt-tokens":              headers.Get("prompt-tokens"),
		"server-processing-time":     headers.Get("server-processing-time"),
	}
	return ServerMetricsFromHeaders(values, clientTTFT...)
}

func sampleOptions(opts ...SampleOptions) SampleOptions {
	opt := SampleOptions{
		N:           1,
		MaxTokens:   1024,
		Temperature: 1.0,
	}
	if len(opts) > 0 {
		provided := opts[0]
		if provided.N != 0 {
			opt.N = provided.N
		}
		if provided.MaxTokens != 0 {
			opt.MaxTokens = provided.MaxTokens
		}
		if provided.Temperature != 0 {
			opt.Temperature = provided.Temperature
		}
		opt.MaxSeqLen = provided.MaxSeqLen
		opt.Logprobs = provided.Logprobs
		opt.Echo = provided.Echo
		opt.IncludeRoutingMatrix = provided.IncludeRoutingMatrix
		opt.Stop = provided.Stop
		opt.Extra = cloneAnyMap(provided.Extra)
		opt.TimeoutDiagnosticContext = provided.TimeoutDiagnosticContext
	}
	return opt
}

func (s *DeploymentSampler) resolveStop(stop any) ([]string, error) {
	switch values := stop.(type) {
	case nil:
		return nil, nil
	case []string:
		return append([]string(nil), values...), nil
	case []int:
		if s.Tokenizer == nil {
			return nil, fmt.Errorf("tokenizer is required to convert integer stop token IDs to string stop sequences for the completions API")
		}
		out := make([]string, 0, len(values))
		for _, tokenID := range values {
			text, err := s.Tokenizer.Decode([]int{tokenID})
			if err != nil {
				return nil, err
			}
			out = append(out, text)
		}
		return out, nil
	case []any:
		if len(values) == 0 {
			return nil, nil
		}
		allString := true
		allInt := true
		for _, value := range values {
			if _, ok := value.(string); !ok {
				allString = false
			}
			if _, ok := intFromStrictAny(value); !ok {
				allInt = false
			}
		}
		if allString {
			out := make([]string, 0, len(values))
			for _, value := range values {
				out = append(out, value.(string))
			}
			return out, nil
		}
		if allInt {
			ints := make([]int, 0, len(values))
			for _, value := range values {
				got, _ := intFromStrictAny(value)
				ints = append(ints, got)
			}
			return s.resolveStop(ints)
		}
	}
	return nil, fmt.Errorf("stop must be []string or []int")
}

func intSliceFromAny(value any) ([]int, bool) {
	switch values := value.(type) {
	case []int:
		return append([]int(nil), values...), true
	case []int64:
		out := make([]int, len(values))
		for i, value := range values {
			out[i] = int(value)
		}
		return out, true
	case []float64:
		out := make([]int, len(values))
		for i, value := range values {
			out[i] = int(value)
		}
		return out, true
	case []any:
		out := make([]int, 0, len(values))
		for _, value := range values {
			got, ok := intFromStrictAny(value)
			if !ok {
				return nil, false
			}
			out = append(out, got)
		}
		return out, true
	default:
		return nil, false
	}
}

func intFromStrictAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i), true
		}
		f, err := strconv.ParseFloat(string(v), 64)
		return int(f), err == nil
	default:
		return 0, false
	}
}

func floatFromAny(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		out, _ := strconv.ParseFloat(string(v), 64)
		return out
	default:
		return 0
	}
}

func intSlicePrefixEqual(values, prefix []int) bool {
	if len(values) < len(prefix) {
		return false
	}
	for i := range prefix {
		if values[i] != prefix[i] {
			return false
		}
	}
	return true
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func samplerRetryableCompletionError(err error) bool {
	var trunc *SSETruncationError
	if errors.As(err, &trunc) {
		return true
	}
	var statusErr *CompletionHTTPStatusError
	if errors.As(err, &statusErr) {
		return samplerRetryHTTPTransientCodes[statusErr.StatusCode]
	}
	return false
}

func stringMapFromAny(values map[string]any) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		switch v := value.(type) {
		case string:
			out[key] = v
		case fmt.Stringer:
			out[key] = v.String()
		default:
			out[key] = fmt.Sprint(v)
		}
	}
	return out
}

func firetitanStopReason(finishReason string) string {
	if finishReason == "length" {
		return "length"
	}
	return "stop"
}

type FixedConcurrencyController struct {
	maxConcurrency int
	sem            chan struct{}
}

func NewFixedConcurrencyController(maxConcurrency int) *FixedConcurrencyController {
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	sem := make(chan struct{}, maxConcurrency)
	for i := 0; i < maxConcurrency; i++ {
		sem <- struct{}{}
	}
	return &FixedConcurrencyController{
		maxConcurrency: maxConcurrency,
		sem:            sem,
	}
}

func (c *FixedConcurrencyController) WindowSize() int {
	return c.maxConcurrency
}

func (c *FixedConcurrencyController) Acquire(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-c.sem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *FixedConcurrencyController) Release(_ *ServerMetrics) {
	select {
	case c.sem <- struct{}{}:
	default:
	}
}

func (c *FixedConcurrencyController) StepCompleted() map[string]float64 {
	return map[string]float64{"window": float64(c.maxConcurrency)}
}

type AdaptiveConcurrencyOptions struct {
	InitialWindow          int
	MinWindow              int
	MaxWindow              int
	PrefillQueueTarget     float64
	AdditiveIncrease       float64
	MultiplicativeDecrease float64
	EMAAlpha               float64
}

type AdaptiveConcurrencyController struct {
	mu                     sync.Mutex
	window                 float64
	minWindow              int
	maxWindow              int
	prefillQueueTarget     float64
	additiveIncrease       float64
	multiplicativeDecrease float64
	emaAlpha               float64
	emaPrefillQueue        *float64
	sem                    chan struct{}

	completedRequests int
	stepPrefillQueues []float64
	stepMetricsCount  int
	stepCacheHits     int
	stepCacheTotal    int
}

func NewAdaptiveConcurrencyController(opts ...AdaptiveConcurrencyOptions) *AdaptiveConcurrencyController {
	opt := AdaptiveConcurrencyOptions{
		InitialWindow:          16,
		MinWindow:              1,
		MaxWindow:              256,
		PrefillQueueTarget:     0.5,
		AdditiveIncrease:       1.0,
		MultiplicativeDecrease: 0.5,
		EMAAlpha:               0.3,
	}
	if len(opts) > 0 {
		provided := opts[0]
		if provided.InitialWindow != 0 {
			opt.InitialWindow = provided.InitialWindow
		}
		if provided.MinWindow != 0 {
			opt.MinWindow = provided.MinWindow
		}
		if provided.MaxWindow != 0 {
			opt.MaxWindow = provided.MaxWindow
		}
		if provided.PrefillQueueTarget != 0 {
			opt.PrefillQueueTarget = provided.PrefillQueueTarget
		}
		if provided.AdditiveIncrease != 0 {
			opt.AdditiveIncrease = provided.AdditiveIncrease
		}
		if provided.MultiplicativeDecrease != 0 {
			opt.MultiplicativeDecrease = provided.MultiplicativeDecrease
		}
		if provided.EMAAlpha != 0 {
			opt.EMAAlpha = provided.EMAAlpha
		}
	}
	if opt.InitialWindow < opt.MinWindow {
		opt.InitialWindow = opt.MinWindow
	}
	if opt.InitialWindow > opt.MaxWindow {
		opt.InitialWindow = opt.MaxWindow
	}
	sem := make(chan struct{}, opt.MaxWindow)
	for i := 0; i < opt.InitialWindow; i++ {
		sem <- struct{}{}
	}
	return &AdaptiveConcurrencyController{
		window:                 float64(opt.InitialWindow),
		minWindow:              opt.MinWindow,
		maxWindow:              opt.MaxWindow,
		prefillQueueTarget:     opt.PrefillQueueTarget,
		additiveIncrease:       opt.AdditiveIncrease,
		multiplicativeDecrease: opt.MultiplicativeDecrease,
		emaAlpha:               opt.EMAAlpha,
		sem:                    sem,
	}
}

func (c *AdaptiveConcurrencyController) WindowSize() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.windowSizeLocked()
}

func (c *AdaptiveConcurrencyController) EMAPrefillQueue() *float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.emaPrefillQueue == nil {
		return nil
	}
	value := *c.emaPrefillQueue
	return &value
}

func (c *AdaptiveConcurrencyController) Acquire(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-c.sem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *AdaptiveConcurrencyController) Release(metrics *ServerMetrics) {
	select {
	case c.sem <- struct{}{}:
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.completedRequests++
	if metrics == nil {
		return
	}
	if metrics.PrefillQueueDuration != nil {
		c.stepPrefillQueues = append(c.stepPrefillQueues, *metrics.PrefillQueueDuration)
	}
	c.stepMetricsCount++
	if metrics.CachedPromptTokens != nil {
		c.stepCacheHits += *metrics.CachedPromptTokens
	}
	if metrics.PromptTokens != nil {
		c.stepCacheTotal += *metrics.PromptTokens
	}
}

func (c *AdaptiveConcurrencyController) StepCompleted() map[string]float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	summary := map[string]float64{
		"window":   float64(c.windowSizeLocked()),
		"requests": float64(c.stepMetricsCount),
	}
	if len(c.stepPrefillQueues) > 0 {
		avgPQ := averageFloat64(c.stepPrefillQueues)
		summary["avg_pq"] = avgPQ
		c.updateWindowLocked(avgPQ)
		summary["window_after"] = float64(c.windowSizeLocked())
	}
	if c.stepCacheTotal > 0 {
		summary["cache_hit_rate"] = float64(c.stepCacheHits) / float64(c.stepCacheTotal)
	}
	if c.emaPrefillQueue != nil {
		summary["ema_pq"] = *c.emaPrefillQueue
	}

	c.stepPrefillQueues = nil
	c.stepMetricsCount = 0
	c.stepCacheHits = 0
	c.stepCacheTotal = 0
	return summary
}

func (c *AdaptiveConcurrencyController) updateWindowLocked(avgPrefillQueue float64) {
	oldWindow := c.windowSizeLocked()
	if c.emaPrefillQueue == nil {
		value := avgPrefillQueue
		c.emaPrefillQueue = &value
	} else {
		value := c.emaAlpha*avgPrefillQueue + (1-c.emaAlpha)*(*c.emaPrefillQueue)
		c.emaPrefillQueue = &value
	}

	if *c.emaPrefillQueue > c.prefillQueueTarget {
		c.window *= c.multiplicativeDecrease
	} else {
		headroom := c.prefillQueueTarget / maxFloat64(*c.emaPrefillQueue, 0.001)
		increase := c.additiveIncrease * minFloat64(headroom, 4.0)
		c.window += increase
	}
	c.window = maxFloat64(float64(c.minWindow), minFloat64(float64(c.maxWindow), c.window))
	newWindow := c.windowSizeLocked()
	c.resizeSemaphoreLocked(oldWindow, newWindow)
}

func (c *AdaptiveConcurrencyController) windowSizeLocked() int {
	return maxInt(c.minWindow, minInt(c.maxWindow, int(c.window)))
}

func (c *AdaptiveConcurrencyController) resizeSemaphoreLocked(oldSize, newSize int) {
	delta := newSize - oldSize
	if delta > 0 {
		for i := 0; i < delta; i++ {
			select {
			case c.sem <- struct{}{}:
			default:
			}
		}
		return
	}
	for i := 0; i < -delta; i++ {
		select {
		case <-c.sem:
		default:
		}
	}
}

func parseOptionalInt(raw string) *int {
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &value
}

func parseOptionalFloat(raw string) *float64 {
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &value
}

func averageFloat64(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minFloat64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

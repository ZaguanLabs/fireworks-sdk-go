package sdk

import (
	"context"
	"net/http"
	"strconv"
	"sync"
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

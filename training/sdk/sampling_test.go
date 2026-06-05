package sdk

import (
	"context"
	"math"
	"net/http"
	"testing"
	"time"
)

func TestServerMetricsFromHeadersFull(t *testing.T) {
	headers := map[string]string{
		"num-concurrent-requests":    "12",
		"prefill-queue-duration":     "0.250",
		"generation-queue-duration":  "1.100",
		"server-time-to-first-token": "0.350",
		"cached-prompt-tokens":       "128",
		"prompt-tokens":              "256",
		"server-processing-time":     "2.500",
	}
	metrics := ServerMetricsFromHeaders(headers, 0.4)

	assertIntPtr(t, metrics.NumConcurrentRequests, 12)
	assertFloatPtr(t, metrics.PrefillQueueDuration, 0.25)
	assertFloatPtr(t, metrics.GenerationQueueDuration, 1.1)
	assertFloatPtr(t, metrics.ServerTTFT, 0.35)
	assertIntPtr(t, metrics.CachedPromptTokens, 128)
	assertIntPtr(t, metrics.PromptTokens, 256)
	assertFloatPtr(t, metrics.ServerProcessingTime, 2.5)
	assertFloatPtr(t, metrics.ClientTTFT, 0.4)
}

func TestServerMetricsFromHeadersEmpty(t *testing.T) {
	metrics := ServerMetricsFromHeaders(map[string]string{})
	if metrics.NumConcurrentRequests != nil || metrics.PrefillQueueDuration != nil || metrics.ClientTTFT != nil {
		t.Fatalf("metrics = %#v, want nil optional fields", metrics)
	}
}

func TestServerMetricsFromHeadersInvalidValues(t *testing.T) {
	metrics := ServerMetricsFromHeaders(map[string]string{
		"num-concurrent-requests": "bad",
		"prefill-queue-duration":  "bad",
	})
	if metrics.NumConcurrentRequests != nil || metrics.PrefillQueueDuration != nil {
		t.Fatalf("metrics = %#v, want invalid fields ignored", metrics)
	}
}

func TestServerMetricsFromHTTPHeaders(t *testing.T) {
	headers := http.Header{
		"Num-Concurrent-Requests": {"12"},
		"Prefill-Queue-Duration":  {"0.25"},
	}
	metrics := ServerMetricsFromHTTPHeaders(headers)
	assertIntPtr(t, metrics.NumConcurrentRequests, 12)
	assertFloatPtr(t, metrics.PrefillQueueDuration, 0.25)
}

func TestFixedConcurrencyController(t *testing.T) {
	ctrl := NewFixedConcurrencyController(2)
	if ctrl.WindowSize() != 2 {
		t.Fatalf("window = %d, want 2", ctrl.WindowSize())
	}
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if err := ctrl.Acquire(ctx); err == nil {
		t.Fatal("expected acquire timeout")
	}
	ctrl.Release(nil)
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ctrl.StepCompleted()["window"]; got != 2 {
		t.Fatalf("window summary = %f, want 2", got)
	}
}

func TestAdaptiveConcurrencyControllerInitialWindow(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 16})
	if ctrl.WindowSize() != 16 {
		t.Fatalf("window = %d, want 16", ctrl.WindowSize())
	}
}

func TestAdaptiveConcurrencyControllerAcquireReleaseBasic(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 2})
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if err := ctrl.Acquire(ctx); err == nil {
		t.Fatal("expected acquire timeout")
	}
	ctrl.Release(nil)
	ctrl.Release(nil)
	if err := ctrl.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAdaptiveConcurrencyReleaseCollectsButDoesNotAdjust(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 10, EMAAlpha: 1.0})
	pq := 5.0
	ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
	if ctrl.WindowSize() != 10 {
		t.Fatalf("window = %d, want 10", ctrl.WindowSize())
	}
}

func TestAdaptiveConcurrencyStepCompletedDecreaseOnHighPQ(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{
		InitialWindow:          10,
		PrefillQueueTarget:     0.5,
		MultiplicativeDecrease: 0.5,
		EMAAlpha:               1.0,
	})
	pq := 2.0
	for i := 0; i < 8; i++ {
		ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
	}
	summary := ctrl.StepCompleted()
	if ctrl.WindowSize() >= 10 {
		t.Fatalf("window = %d, want less than 10", ctrl.WindowSize())
	}
	assertApprox(t, summary["avg_pq"], 2.0)
}

func TestAdaptiveConcurrencyStepCompletedIncreaseOnLowPQ(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{
		InitialWindow:      4,
		PrefillQueueTarget: 1.0,
		AdditiveIncrease:   2.0,
		EMAAlpha:           1.0,
	})
	pq := 0.1
	for i := 0; i < 8; i++ {
		ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
	}
	ctrl.StepCompleted()
	if ctrl.window <= 4.0 {
		t.Fatalf("raw window = %f, want greater than 4", ctrl.window)
	}
}

func TestAdaptiveConcurrencyNoChangeWithoutMetrics(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 8})
	ctrl.Release(nil)
	ctrl.StepCompleted()
	if ctrl.WindowSize() != 8 {
		t.Fatalf("window = %d, want 8", ctrl.WindowSize())
	}
	if ctrl.EMAPrefillQueue() != nil {
		t.Fatalf("ema = %v, want nil", *ctrl.EMAPrefillQueue())
	}
}

func TestAdaptiveConcurrencyEMASmoothingAcrossSteps(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{
		InitialWindow:      10,
		EMAAlpha:           0.5,
		PrefillQueueTarget: 1.0,
	})
	pq := 0.4
	ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
	ctrl.StepCompleted()
	assertFloatPtr(t, ctrl.EMAPrefillQueue(), 0.4)

	pq = 0.8
	ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
	ctrl.StepCompleted()
	assertFloatPtr(t, ctrl.EMAPrefillQueue(), 0.5*0.8+0.5*0.4)
}

func TestAdaptiveConcurrencyRepeatedCongestionFloorsAtMin(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{
		InitialWindow:          16,
		MinWindow:              2,
		PrefillQueueTarget:     0.1,
		MultiplicativeDecrease: 0.5,
		EMAAlpha:               1.0,
	})
	pq := 5.0
	for i := 0; i < 20; i++ {
		ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
		ctrl.StepCompleted()
	}
	if ctrl.WindowSize() != 2 {
		t.Fatalf("window = %d, want 2", ctrl.WindowSize())
	}
}

func TestAdaptiveConcurrencyRepeatedGoodMetricsCapsAtMax(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{
		InitialWindow:      4,
		MaxWindow:          16,
		PrefillQueueTarget: 1.0,
		AdditiveIncrease:   5.0,
		EMAAlpha:           1.0,
	})
	pq := 0.01
	for i := 0; i < 100; i++ {
		ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq})
		ctrl.StepCompleted()
	}
	if ctrl.WindowSize() != 16 {
		t.Fatalf("window = %d, want 16", ctrl.WindowSize())
	}
}

func TestAdaptiveConcurrencyStepCompletedResetsAccumulators(t *testing.T) {
	ctrl := NewAdaptiveConcurrencyController(AdaptiveConcurrencyOptions{InitialWindow: 8})
	pq1 := 0.3
	cached1 := 10
	prompt1 := 100
	pq2 := 0.5
	cached2 := 20
	prompt2 := 100
	ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq1, CachedPromptTokens: &cached1, PromptTokens: &prompt1})
	ctrl.Release(&ServerMetrics{PrefillQueueDuration: &pq2, CachedPromptTokens: &cached2, PromptTokens: &prompt2})

	summary := ctrl.StepCompleted()
	assertApprox(t, summary["requests"], 2)
	assertApprox(t, summary["avg_pq"], 0.4)
	assertApprox(t, summary["cache_hit_rate"], 30.0/200.0)

	summary = ctrl.StepCompleted()
	assertApprox(t, summary["requests"], 0)
	if _, ok := summary["avg_pq"]; ok {
		t.Fatalf("summary includes avg_pq after reset: %#v", summary)
	}
}

func assertIntPtr(t *testing.T, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("value = %v, want %d", got, want)
	}
}

func assertFloatPtr(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("value = nil, want %f", want)
	}
	assertApprox(t, *got, want)
}

func assertApprox(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("value = %.12f, want %.12f", got, want)
	}
}

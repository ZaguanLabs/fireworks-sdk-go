package sdk

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TargetLogprobSupport validates the native trainer loss configuration before submission.
func TargetLogprobSupport(config map[string]any) (string, error) {
	for _, key := range []string{"replay", "logprob_normalization", "importance_sampling"} {
		if _, ok := config[key]; ok {
			return "", fmt.Errorf("loss_fn_config.%s is unsupported; use target_logprob_support", key)
		}
	}
	value, ok := config["target_logprob_support"]
	if !ok {
		return "full_vocabulary", nil
	}
	support, ok := value.(string)
	if !ok || (support != "full_vocabulary" && support != "sampling_support" && support != "candidates") {
		return "", fmt.Errorf("target_logprob_support must be full_vocabulary, sampling_support or candidates")
	}
	return support, nil
}
func ModelInputFromIntsWithTopSampling(tokens []int, refs *RoutingReferences) (map[string]any, error) {
	input := ModelInputFromInts(tokens)
	if refs != nil {
		if err := refs.Validate(); err != nil {
			return nil, err
		}
		if refs.Length != len(tokens) {
			return nil, fmt.Errorf("top-K references must cover every input position")
		}
		input["top_sampling_references"] = refs.Compact()
	}
	return input, nil
}

type timedServerMetrics struct {
	At      time.Time
	Metrics ServerMetrics
}

func (s *DeploymentSampler) recordAttemptMetrics(metrics *ServerMetrics) {
	if metrics == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.recentMetrics) < 65536 {
		s.recentMetrics = append(s.recentMetrics, *cloneServerMetrics(metrics))
	} else {
		s.recentMetrics[s.recentMetricsNext] = *cloneServerMetrics(metrics)
		s.recentMetricsNext = (s.recentMetricsNext + 1) % 65536
	}
	if metrics.PrefillQueueDuration != nil || metrics.GenerationQueueDuration != nil || metrics.ClientTTFT != nil || metrics.NumConcurrentRequests != nil {
		s.diagnosticMetrics = append(s.diagnosticMetrics, timedServerMetrics{At: s.Now(), Metrics: *cloneServerMetrics(metrics)})
		if len(s.diagnosticMetrics) > 32 {
			s.diagnosticMetrics = append([]timedServerMetrics(nil), s.diagnosticMetrics[len(s.diagnosticMetrics)-32:]...)
		}
	}
}

// ShouldRetryServerlessCapacity409 classifies an opted-in retrieve-path conflict.
// Explicit identity conflicts and unrecognized lifecycle failures remain terminal.
func ShouldRetryServerlessCapacity409(requestURL string, body []byte, supervised bool) bool {
	parsed, err := url.Parse(requestURL)
	if err != nil || !supervised || !(parsed.Path == "/training/v1/serverless" || strings.HasPrefix(parsed.Path, "/training/v1/serverless/")) {
		return false
	}
	haystack := strings.ToLower(string(body))
	for _, marker := range []string{"already exists", "idempotency", "different request payload", "conflicts with durable model state", "model lifecycle conflicts with durable state"} {
		if strings.Contains(haystack, marker) {
			return false
		}
	}
	for _, marker := range []string{"capacity_exhausted", "recovery_required", "out of capacity", "at capacity", "capacity is temporarily", "training capacity", "paused_capacity", "recovery is required", "fenced recovery", "lifecycle is closing", "lifecycle is releasing", "resource_exhausted"} {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	if strings.TrimSpace(string(body)) == "" || strings.TrimSpace(string(body)) == "null" {
		return true
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return false
	}
	switch v := value.(type) {
	case map[string]any:
		return len(v) == 0
	case string:
		return strings.TrimSpace(v) == ""
	}
	return false
}

// TopSamplingReferencesWireBytes includes compact sampling metadata in transport size estimates.
func TopSamplingReferencesWireBytes(modelInput map[string]any) int {
	total := 0
	for _, key := range []string{"top_sampling_references", "sampling_mask_references"} {
		if value := modelInput[key]; value != nil {
			data, err := json.Marshal(map[string]any{key: value})
			if err == nil {
				total += len(data)
			}
		}
	}
	return total
}

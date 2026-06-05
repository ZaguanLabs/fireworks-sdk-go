package sdk

import (
	"testing"
	"time"
)

func TestTrainingSDKConstantsMatchPythonSDK(t *testing.T) {
	tests := map[string]time.Duration{
		"DefaultTrainerTimeout":  DefaultTrainerTimeout,
		"TrainerReadyTimeout":    TrainerReadyTimeout,
		"DeploymentReadyTimeout": DeploymentReadyTimeout,
		"ReattachSettleTimeout":  ReattachSettleTimeout,
		"ReconnectTimeout":       ReconnectTimeout,
		"ResumableWaitTimeout":   ResumableWaitTimeout,
		"HotloadTimeout":         HotloadTimeout,
		"HotloadReadyTimeout":    HotloadReadyTimeout,
		"PollInterval":           PollInterval,
		"SlowPollInterval":       SlowPollInterval,
		"PollLogHeartbeat":       PollLogHeartbeat,
		"HTTPReadTimeout":        HTTPReadTimeout,
		"HTTPWriteTimeout":       HTTPWriteTimeout,
		"HTTPLongWriteTimeout":   HTTPLongWriteTimeout,
		"WarmupProbeTimeout":     WarmupProbeTimeout,
	}
	want := map[string]time.Duration{
		"DefaultTrainerTimeout":  3600 * time.Second,
		"TrainerReadyTimeout":    15 * time.Minute,
		"DeploymentReadyTimeout": 5400 * time.Second,
		"ReattachSettleTimeout":  600 * time.Second,
		"ReconnectTimeout":       600 * time.Second,
		"ResumableWaitTimeout":   120 * time.Second,
		"HotloadTimeout":         600 * time.Second,
		"HotloadReadyTimeout":    300 * time.Second,
		"PollInterval":           5 * time.Second,
		"SlowPollInterval":       10 * time.Second,
		"PollLogHeartbeat":       60 * time.Second,
		"HTTPReadTimeout":        30 * time.Second,
		"HTTPWriteTimeout":       60 * time.Second,
		"HTTPLongWriteTimeout":   300 * time.Second,
		"WarmupProbeTimeout":     10 * time.Second,
	}
	for name, got := range tests {
		if got != want[name] {
			t.Fatalf("%s = %s, want %s", name, got, want[name])
		}
	}
}

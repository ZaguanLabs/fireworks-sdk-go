package sdk

import "time"

const (
	DefaultTrainerTimeout  = 3600 * time.Second
	TrainerReadyTimeout    = 15 * time.Minute
	DeploymentReadyTimeout = 5400 * time.Second
	ReattachSettleTimeout  = 600 * time.Second
	ReconnectTimeout       = 600 * time.Second
	ResumableWaitTimeout   = 120 * time.Second

	HotloadTimeout      = 600 * time.Second
	HotloadReadyTimeout = 300 * time.Second

	PollInterval         = 5 * time.Second
	SlowPollInterval     = 10 * time.Second
	PollLogHeartbeat     = 60 * time.Second
	HTTPReadTimeout      = 30 * time.Second
	HTTPWriteTimeout     = 60 * time.Second
	HTTPLongWriteTimeout = 300 * time.Second
	WarmupProbeTimeout   = 10 * time.Second
)

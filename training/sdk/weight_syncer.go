package sdk

import (
	"context"
	"fmt"
	"time"
)

type SaveSamplerResult struct {
	Path         string
	SnapshotName string
}

type SaveWeightsForSamplerOptions struct {
	CheckpointType string
	TTL            time.Duration
}

type SamplerWeightSaver interface {
	SaveWeightsForSamplerExt(context.Context, string, SaveWeightsForSamplerOptions) (SaveSamplerResult, error)
}

type WeightSyncerConfig struct {
	PolicyClient        SamplerWeightSaver
	DeployMgr           *DeploymentManager
	DeploymentID        string
	BaseModel           string
	HotloadTimeout      time.Duration
	FirstCheckpointType string
	CompressionFormat   string
	WarmupAfterHotload  *bool
	WarmupMaxRetries    int
	WarmupRetryInterval time.Duration
	ResetPromptCache    *bool
	LoraRank            int
	Now                 func() time.Time
	Sleep               func(time.Duration)
}

type WeightSyncer struct {
	PolicyClient        SamplerWeightSaver
	DeployMgr           *DeploymentManager
	DeploymentID        string
	BaseModel           string
	HotloadTimeout      time.Duration
	FirstCheckpointType string
	CompressionFormat   string
	WarmupAfterHotload  bool
	WarmupMaxRetries    int
	WarmupRetryInterval time.Duration
	ResetPromptCache    bool
	LoraRank            int

	BaseSaved         bool
	BaseIdentity      string
	DeploymentChecked bool
	LastTiming        map[string]time.Duration
	Now               func() time.Time
	Sleep             func(time.Duration)

	CheckStatus    func(context.Context, string, string) (map[string]any, error)
	HotloadAndWait func(context.Context, string, string, string, ...HotloadAndWaitOptions) (bool, error)
	Warmup         func(context.Context, string, WarmupOptions) bool
}

func NewWeightSyncer(config WeightSyncerConfig) *WeightSyncer {
	hotloadTimeout := config.HotloadTimeout
	if hotloadTimeout == 0 {
		hotloadTimeout = HotloadTimeout
	}
	firstCheckpointType := config.FirstCheckpointType
	if firstCheckpointType == "" {
		firstCheckpointType = string(SamplerCheckpointTypeBase)
	}
	compressionFormat := config.CompressionFormat
	if compressionFormat == "" {
		compressionFormat = DefaultDeltaCompression
	}
	warmupAfterHotload := true
	if config.WarmupAfterHotload != nil {
		warmupAfterHotload = *config.WarmupAfterHotload
	}
	warmupMaxRetries := config.WarmupMaxRetries
	if warmupMaxRetries == 0 {
		warmupMaxRetries = 10
	}
	warmupRetryInterval := config.WarmupRetryInterval
	if warmupRetryInterval == 0 {
		warmupRetryInterval = 10 * time.Second
	}
	resetPromptCache := true
	if config.ResetPromptCache != nil {
		resetPromptCache = *config.ResetPromptCache
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	sleep := config.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	return &WeightSyncer{
		PolicyClient:        config.PolicyClient,
		DeployMgr:           config.DeployMgr,
		DeploymentID:        config.DeploymentID,
		BaseModel:           config.BaseModel,
		HotloadTimeout:      hotloadTimeout,
		FirstCheckpointType: firstCheckpointType,
		CompressionFormat:   compressionFormat,
		WarmupAfterHotload:  warmupAfterHotload,
		WarmupMaxRetries:    warmupMaxRetries,
		WarmupRetryInterval: warmupRetryInterval,
		ResetPromptCache:    resetPromptCache,
		LoraRank:            config.LoraRank,
		LastTiming:          map[string]time.Duration{},
		Now:                 now,
		Sleep:               sleep,
	}
}

func (s *WeightSyncer) HotloadEnabled() bool {
	return s != nil && s.DeploymentID != "" && s.DeployMgr != nil
}

func (s *WeightSyncer) DeploymentModel(ctx context.Context) (string, error) {
	if !s.HotloadEnabled() {
		return "", fmt.Errorf("inference warmup requires both DeployMgr and DeploymentID")
	}
	accountID, err := s.DeployMgr.AccountID(ctx)
	if err != nil {
		return "", err
	}
	return "accounts/" + accountID + "/deployments/" + s.DeploymentID, nil
}

func (s *WeightSyncer) CheckDeploymentState(ctx context.Context) string {
	if !s.HotloadEnabled() {
		return ""
	}
	status, err := s.checkStatus(ctx)
	if err != nil {
		return ""
	}
	replicas, _ := status["replicas"].([]any)
	if len(replicas) == 0 {
		return ""
	}
	replica, _ := replicas[0].(map[string]any)
	if replica == nil {
		return ""
	}
	current := stringFromAny(replica["current_snapshot_identity"])
	if current == "" {
		current = stringFromAny(replica["identity"])
	}
	return current
}

func (s *WeightSyncer) ResetDeltaChain() {
	s.BaseSaved = false
	s.BaseIdentity = ""
}

func (s *WeightSyncer) WaitForHotloadReady(ctx context.Context, timeout, pollInterval time.Duration) error {
	if !s.HotloadEnabled() {
		return nil
	}
	if timeout == 0 {
		timeout = HotloadReadyTimeout
	}
	if pollInterval == 0 {
		pollInterval = PollInterval
	}
	start := s.now()()
	for s.now()().Sub(start) < timeout {
		status, err := s.checkStatus(ctx)
		if err == nil {
			replicas, _ := status["replicas"].([]any)
			if len(replicas) > 0 {
				return nil
			}
		}
		s.sleep()(pollInterval)
	}
	return fmt.Errorf("deployment hotload manager not ready after %.0fs", timeout.Seconds())
}

func (s *WeightSyncer) EnsureDeploymentChecked(ctx context.Context) error {
	if s.DeploymentChecked {
		return nil
	}
	s.DeploymentChecked = true
	if err := s.WaitForHotloadReady(ctx, 0, 0); err != nil {
		return err
	}
	if current := s.CheckDeploymentState(ctx); current != "" {
		s.BaseIdentity = ""
	}
	return nil
}

func (s *WeightSyncer) NextCheckpointType(explicit ...string) (SamplerCheckpointType, error) {
	return ResolveNextCheckpointType(s.LoraRank, s.BaseSaved, s.FirstCheckpointType, explicit...)
}

func (s *WeightSyncer) BuildIncrementalMetadata(checkpointType string) map[string]any {
	compressionFormat := s.CompressionFormat
	if compressionFormat == "" {
		compressionFormat = DefaultDeltaCompression
	}
	return BuildIncrementalMetadata(s.LoraRank, checkpointType, s.BaseIdentity, compressionFormat)
}

func (s *WeightSyncer) MarkFirstSaveDone() {
	s.BaseSaved = true
}

func (s *WeightSyncer) SaveOnly(ctx context.Context, name string, checkpointType ...string) (string, error) {
	var opts SaveWeightsForSamplerOptions
	if len(checkpointType) > 0 {
		opts.CheckpointType = checkpointType[0]
	}
	saveResult, err := s.SaveOnlyExt(ctx, name, opts)
	if err != nil {
		return "", err
	}
	return saveResult.SnapshotName, nil
}

func (s *WeightSyncer) SaveOnlyExt(ctx context.Context, name string, opts SaveWeightsForSamplerOptions) (SaveSamplerResult, error) {
	s.LastTiming = map[string]time.Duration{}
	if s.PolicyClient == nil {
		return SaveSamplerResult{}, fmt.Errorf("WeightSyncer requires PolicyClient")
	}
	ckptType, err := s.NextCheckpointType(opts.CheckpointType)
	if err != nil {
		return SaveSamplerResult{}, err
	}
	opts.CheckpointType = string(ckptType)
	start := s.now()()
	saveResult, err := s.PolicyClient.SaveWeightsForSamplerExt(ctx, name, opts)
	s.LastTiming["save_time_s"] = s.now()().Sub(start)
	if err != nil {
		return SaveSamplerResult{}, err
	}
	if saveResult.SnapshotName == "" {
		saveResult.SnapshotName = saveResult.Path
	}
	if saveResult.Path == "" {
		saveResult.Path = saveResult.SnapshotName
	}
	s.MarkFirstSaveDone()
	return saveResult, nil
}

func (s *WeightSyncer) Hotload(ctx context.Context, snapshotName, checkpointType string) (bool, error) {
	s.LastTiming = map[string]time.Duration{}
	if !s.HotloadEnabled() {
		return false, nil
	}
	if err := s.doHotload(ctx, snapshotName, checkpointType); err != nil {
		return false, err
	}
	return true, nil
}

func (s *WeightSyncer) SaveAndHotload(ctx context.Context, name string, checkpointType ...string) (string, error) {
	s.LastTiming = map[string]time.Duration{}
	if s.PolicyClient == nil {
		return "", fmt.Errorf("WeightSyncer requires PolicyClient")
	}
	totalStart := s.now()()
	ckptType, err := s.NextCheckpointType(checkpointType...)
	if err != nil {
		return "", err
	}
	saveStart := s.now()()
	saveResult, err := s.PolicyClient.SaveWeightsForSamplerExt(ctx, name, SaveWeightsForSamplerOptions{CheckpointType: string(ckptType)})
	s.LastTiming["save_time_s"] = s.now()().Sub(saveStart)
	if err != nil {
		s.LastTiming["total_time_s"] = s.now()().Sub(totalStart)
		return "", err
	}
	s.MarkFirstSaveDone()
	if s.HotloadEnabled() {
		if err := s.doHotload(ctx, saveResult.SnapshotName, string(ckptType)); err != nil {
			s.LastTiming["total_time_s"] = s.now()().Sub(totalStart)
			return "", err
		}
	}
	s.LastTiming["total_time_s"] = s.now()().Sub(totalStart)
	return saveResult.SnapshotName, nil
}

func (s *WeightSyncer) GetDeploymentSampler(ctx context.Context, tokenizer DeploymentTokenizer) (*DeploymentSampler, error) {
	if !s.HotloadEnabled() {
		return nil, fmt.Errorf("WeightSyncer requires DeployMgr and DeploymentID")
	}
	model, err := s.DeploymentModel(ctx)
	if err != nil {
		return nil, err
	}
	return NewDeploymentSampler(
		s.DeployMgr.InferenceURL,
		model,
		s.DeployMgr.APIKey(),
		WithDeploymentSamplerTokenizer(tokenizer),
	), nil
}

func (s *WeightSyncer) GetSamplingClient(ctx context.Context, tokenizer DeploymentTokenizer) (*FiretitanSamplingClient, error) {
	sampler, err := s.GetDeploymentSampler(ctx, tokenizer)
	if err != nil {
		return nil, err
	}
	return NewFiretitanSamplingClient(sampler), nil
}

func (s *WeightSyncer) doHotload(ctx context.Context, snapshotName, checkpointType string) error {
	if err := s.EnsureDeploymentChecked(ctx); err != nil {
		return err
	}
	incremental := s.BuildIncrementalMetadata(checkpointType)
	resetPromptCache := s.ResetPromptCache
	hotloadTimeout := s.HotloadTimeout
	if hotloadTimeout == 0 {
		hotloadTimeout = HotloadTimeout
	}
	start := s.now()()
	ok, err := s.hotloadAndWait(ctx, s.DeploymentID, s.BaseModel, snapshotName, HotloadAndWaitOptions{
		IncrementalSnapshotMetadata: incremental,
		ResetPromptCache:            &resetPromptCache,
		RequestTimeout:              hotloadTimeout,
		Path:                        "",
		Wait: HotloadWaitOptions{
			Timeout: hotloadTimeout,
		},
	})
	s.LastTiming["hotload_time_s"] = s.now()().Sub(start)
	if err != nil {
		return err
	}
	if !ok {
		if s.DeployMgr != nil && s.DeployMgr.LastHotloadErrorMessage != "" {
			return fmt.Errorf("%s", s.DeployMgr.LastHotloadErrorMessage)
		}
		return fmt.Errorf("%s", FormatSDKError(
			"Hotload failed for '"+snapshotName+"'",
			"hotload_and_wait returned false without a detailed status error.",
			"Use the Fireworks training cookbook skill's hotload recovery self-check. Verify the requested snapshot identity against deployment status, then use the documented PER_TRAINER or PER_DEPLOYMENT flow.",
			SDKErrorFormatOptions{DocsURL: DocsSDK},
		))
	}
	s.BaseIdentity = snapshotName
	warmupStart := s.now()()
	s.warmupAfterHotload(ctx)
	s.LastTiming["warmup_time_s"] = s.now()().Sub(warmupStart)
	return nil
}

func (s *WeightSyncer) warmupAfterHotload(ctx context.Context) {
	if !s.WarmupAfterHotload || s.DeployMgr == nil {
		return
	}
	model, err := s.DeploymentModel(ctx)
	if err != nil {
		return
	}
	s.warmup(ctx, model, WarmupOptions{
		MaxRetries:    s.WarmupMaxRetries,
		RetryInterval: s.WarmupRetryInterval,
	})
}

func (s *WeightSyncer) checkStatus(ctx context.Context) (map[string]any, error) {
	if s.CheckStatus != nil {
		return s.CheckStatus(ctx, s.DeploymentID, s.BaseModel)
	}
	return s.DeployMgr.HotloadCheckStatus(ctx, s.DeploymentID, s.BaseModel)
}

func (s *WeightSyncer) hotloadAndWait(ctx context.Context, deploymentID, baseModel, snapshotName string, opts ...HotloadAndWaitOptions) (bool, error) {
	if s.HotloadAndWait != nil {
		return s.HotloadAndWait(ctx, deploymentID, baseModel, snapshotName, opts...)
	}
	return s.DeployMgr.HotloadAndWait(ctx, deploymentID, baseModel, snapshotName, opts...)
}

func (s *WeightSyncer) warmup(ctx context.Context, model string, opts WarmupOptions) bool {
	if s.Warmup != nil {
		return s.Warmup(ctx, model, opts)
	}
	return s.DeployMgr.Warmup(ctx, model, opts)
}

func (s *WeightSyncer) now() func() time.Time {
	if s.Now != nil {
		return s.Now
	}
	return time.Now
}

func (s *WeightSyncer) sleep() func(time.Duration) {
	if s.Sleep != nil {
		return s.Sleep
	}
	return time.Sleep
}

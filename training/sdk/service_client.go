package sdk

import (
	"context"
	"fmt"
	"time"
)

type FiretitanServiceClientOptions struct {
	APIKey              string
	BaseURL             string
	Config              FiretitanProvisioningConfig
	DefaultUserMetadata map[string]string
	DefaultProjectID    string
	FireworksClient     *FireworksClient
	Registry            *TrainingClientConfigRegistry
	Now                 func() time.Time
}

type FiretitanServiceClient struct {
	Config              FiretitanProvisioningConfig
	DefaultUserMetadata map[string]string
	DefaultProjectID    string
	Fireworks           *FireworksClient
	Registry            *TrainingClientConfigRegistry
	ManagedHandle       *ManagedHandleMetadata
	SamplerBackend      *TinkerSamplerBackend
	SyncState           ManagedSamplerSyncState
	Now                 func() time.Time

	ListCheckpointsFunc   func(context.Context, string, int) ([]map[string]any, error)
	PromoteCheckpointFunc func(context.Context, PromoteCheckpointOptions) (map[string]any, error)
}

func NewFiretitanServiceClient(opts FiretitanServiceClientOptions) (*FiretitanServiceClient, error) {
	config, err := opts.Config.Normalize()
	if err != nil {
		return nil, err
	}
	fireworks := opts.FireworksClient
	if fireworks == nil {
		fireworks = NewFireworksClient(FireworksAPIKey(opts.APIKey), FireworksBaseURL(opts.BaseURL))
	}
	registry := opts.Registry
	if registry == nil {
		registry = NewTrainingClientConfigRegistry()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	client := &FiretitanServiceClient{
		Config:              config,
		DefaultUserMetadata: cloneStringMap(opts.DefaultUserMetadata),
		DefaultProjectID:    opts.DefaultProjectID,
		Fireworks:           fireworks,
		Registry:            registry,
		Now:                 now,
	}
	client.ListCheckpointsFunc = fireworks.ListCheckpoints
	client.PromoteCheckpointFunc = fireworks.PromoteCheckpoint
	return client, nil
}

func (c *FiretitanServiceClient) ResolvedMetadata() ManagedResolvedMetadata {
	var handle ManagedHandleMetadata
	if c != nil && c.ManagedHandle != nil {
		handle = *c.ManagedHandle
	}
	if c == nil {
		return ResolveManagedMetadata(nil, handle)
	}
	return ResolveManagedMetadata(&c.Config, handle)
}

func (c *FiretitanServiceClient) ManagedTrainerJobID() (string, error) {
	return RequireManagedString(c.ResolvedMetadata().TrainerJobID, "trainer job id")
}

func (c *FiretitanServiceClient) ManagedDeploymentID() (string, error) {
	return RequireManagedString(c.ResolvedMetadata().DeploymentID, "deployment id")
}

func (c *FiretitanServiceClient) ManagedMaxContextLength() (int, error) {
	return RequireManagedInt(c.ResolvedMetadata().MaxContextLength, "max context length")
}

type CreateFiretitanTrainingClientOptions struct {
	BaseModel                  string
	LoraRank                   *int
	UserMetadata               map[string]string
	HandleMetadata             *ManagedHandleMetadata
	SamplerBackend             *TinkerSamplerBackend
	WeightSyncer               *WeightSyncer
	RequiresInitialSamplerSync bool
}

type FiretitanTrainingClient struct {
	Service        *FiretitanServiceClient
	Config         FiretitanProvisioningConfig
	UserMetadata   map[string]string
	Warnings       []string
	HandleMetadata ManagedHandleMetadata
	SamplerBackend *TinkerSamplerBackend
	WeightSyncer   *WeightSyncer
	SyncState      ManagedSamplerSyncState
}

func (c *FiretitanServiceClient) CreateTrainingClient(_ context.Context, opts ...CreateFiretitanTrainingClientOptions) (*FiretitanTrainingClient, error) {
	if c == nil {
		return nil, fmt.Errorf("FiretitanServiceClient is nil")
	}
	var opt CreateFiretitanTrainingClientOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if c.Registry == nil {
		c.Registry = NewTrainingClientConfigRegistry()
	}
	key := ManagedTrainingClientKey(c.Config)
	if err := c.Registry.Add(key); err != nil {
		return nil, err
	}
	var warnings []string
	if opt.BaseModel != "" {
		if warning := DeprecatedManagedOverrideMessage("create_training_client", "base_model", opt.BaseModel, c.Config.BaseModel); warning != "" {
			warnings = append(warnings, warning)
		}
	}
	if opt.LoraRank != nil {
		if warning := DeprecatedManagedOverrideMessage("create_training_client", "lora_rank", *opt.LoraRank, c.Config.LoraRank); warning != "" {
			warnings = append(warnings, warning)
		}
	}

	handle := ManagedHandleMetadata{}
	if c.ManagedHandle != nil {
		handle = *c.ManagedHandle
	}
	if opt.HandleMetadata != nil {
		handle = *opt.HandleMetadata
	}
	backend := c.SamplerBackend
	if opt.SamplerBackend != nil {
		backend = opt.SamplerBackend
	}
	state := ManagedSamplerSyncState{RequiresInitialSamplerSync: opt.RequiresInitialSamplerSync}
	if !state.RequiresInitialSamplerSync {
		state = c.SyncState
	}
	return &FiretitanTrainingClient{
		Service:        c,
		Config:         c.Config,
		UserMetadata:   ResolveUserMetadata(c.DefaultUserMetadata, opt.UserMetadata),
		Warnings:       warnings,
		HandleMetadata: handle,
		SamplerBackend: backend,
		WeightSyncer:   opt.WeightSyncer,
		SyncState:      state,
	}, nil
}

func (c *FiretitanTrainingClient) ResolvedMetadata() ManagedResolvedMetadata {
	if c == nil {
		return ResolveManagedMetadata(nil, ManagedHandleMetadata{})
	}
	return ResolveManagedMetadata(&c.Config, c.HandleMetadata)
}

func (c *FiretitanTrainingClient) TrainerJobID() (string, error) {
	return RequireManagedString(c.ResolvedMetadata().TrainerJobID, "trainer job id")
}

func (c *FiretitanTrainingClient) DeploymentID() (string, error) {
	return RequireManagedString(c.ResolvedMetadata().DeploymentID, "deployment id")
}

func (c *FiretitanTrainingClient) ListCheckpoints(ctx context.Context, pageSize int) ([]map[string]any, error) {
	jobID, err := c.TrainerJobID()
	if err != nil {
		return nil, ControlPlaneCheckpointClientError()
	}
	if c.Service == nil || c.Service.ListCheckpointsFunc == nil {
		return nil, fmt.Errorf("FiretitanTrainingClient requires a service checkpoint client")
	}
	return c.Service.ListCheckpointsFunc(ctx, jobID, pageSize)
}

func (c *FiretitanTrainingClient) PromoteCheckpoint(ctx context.Context, opts PromoteCheckpointOptions) (map[string]any, error) {
	jobID, err := c.TrainerJobID()
	if err != nil {
		return nil, ControlPlaneCheckpointClientError()
	}
	if opts.Name == "" && opts.JobID == "" {
		opts.JobID = jobID
	}
	if opts.BaseModel == "" {
		opts.BaseModel = c.Config.BaseModel
	}
	if c.Service == nil || c.Service.PromoteCheckpointFunc == nil {
		return nil, fmt.Errorf("FiretitanTrainingClient requires a service checkpoint client")
	}
	return c.Service.PromoteCheckpointFunc(ctx, opts)
}

func (c *FiretitanTrainingClient) AttachSamplerBackend(backend *TinkerSamplerBackend) *FiretitanTrainingClient {
	if c != nil {
		c.SamplerBackend = backend
	}
	return c
}

func (c *FiretitanTrainingClient) AttachWeightSyncer(syncer *WeightSyncer) *FiretitanTrainingClient {
	if c != nil {
		c.WeightSyncer = syncer
	}
	return c
}

func (c *FiretitanTrainingClient) NextSamplerCheckpointType(checkpointType ...string) (SamplerCheckpointType, error) {
	if c != nil && c.WeightSyncer != nil {
		return c.WeightSyncer.NextCheckpointType(checkpointType...)
	}
	if c == nil {
		return ResolveNextCheckpointType(0, false, string(SamplerCheckpointTypeBase), checkpointType...)
	}
	return ResolveNextCheckpointType(c.Config.LoraRank, false, string(SamplerCheckpointTypeBase), checkpointType...)
}

func (c *FiretitanTrainingClient) SaveWeightsForSampler(ctx context.Context, name string, checkpointType ...string) (SaveSamplerResult, error) {
	if c == nil || c.WeightSyncer == nil {
		return SaveSamplerResult{}, fmt.Errorf("FiretitanTrainingClient requires a WeightSyncer to save weights for sampler")
	}
	snapshotName, err := c.WeightSyncer.SaveOnly(ctx, name, checkpointType...)
	if err != nil {
		return SaveSamplerResult{}, err
	}
	return SaveSamplerResult{Path: snapshotName, SnapshotName: snapshotName}, nil
}

func (c *FiretitanTrainingClient) SaveWeightsAndHotload(ctx context.Context, name string, checkpointType ...string) (SaveSamplerResult, error) {
	if c == nil || c.WeightSyncer == nil {
		return SaveSamplerResult{}, fmt.Errorf("FiretitanTrainingClient requires a WeightSyncer to save and hotload weights for sampler")
	}
	snapshotName, err := c.WeightSyncer.SaveAndHotload(ctx, name, checkpointType...)
	if err != nil {
		return SaveSamplerResult{}, err
	}
	c.SyncState.MarkSamplerHotloaded()
	return SaveSamplerResult{Path: snapshotName, SnapshotName: snapshotName}, nil
}

func (c *FiretitanTrainingClient) SaveWeightsAndGetSamplingClient(context.Context, string, DeploymentTokenizer, ...string) (*FiretitanSamplingClient, error) {
	return nil, fmt.Errorf("%s", SamplingClientFromTrainerMessage)
}

func (c *FiretitanTrainingClient) RequiresInitialSamplerSync() bool {
	return c != nil && c.SyncState.RequiresInitialSync()
}

func (c *FiretitanTrainingClient) HotloadSamplerSnapshot(ctx context.Context, modelPath string) error {
	if c == nil || c.SamplerBackend == nil {
		return CreateSamplingClientUnsupportedError()
	}
	ok, err := c.SamplerBackend.HotloadSavedSnapshot(ctx, modelPath)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("hotload sampler snapshot %q did not complete", modelPath)
	}
	c.SyncState.MarkSamplerHotloaded()
	return nil
}

func (c *FiretitanTrainingClient) CreateSamplingClient(ctx context.Context, modelPath string, tokenizer DeploymentTokenizer, controller SamplingConcurrencyController) (*FiretitanSamplingClient, error) {
	if c == nil || c.SamplerBackend == nil {
		return nil, CreateSamplingClientUnsupportedError()
	}
	if modelPath != "" {
		if err := c.HotloadSamplerSnapshot(ctx, modelPath); err != nil {
			return nil, err
		}
	}
	return c.SamplerBackend.GetSamplingClient(ctx, tokenizer, controller)
}

func (c *FiretitanTrainingClient) GetTokenizerModel() (string, error) {
	if c == nil {
		_, err := ResolveTokenizerModel("")
		return "", err
	}
	return ResolveTokenizerModel(c.Config.TokenizerModel)
}

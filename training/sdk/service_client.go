package sdk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type FiretitanServiceClientOptions struct {
	APIKey              string
	BaseURL             string
	TrainingSessionID   string
	Config              FiretitanProvisioningConfig
	DefaultUserMetadata map[string]string
	DefaultProjectID    string
	FireworksClient     *FireworksClient
	Registry            *TrainingClientConfigRegistry
	Now                 func() time.Time
}

type FiretitanServiceClient struct {
	Config                    FiretitanProvisioningConfig
	ServerlessSessionID       string
	AccountIDCache            string
	DefaultUserMetadata       map[string]string
	DefaultProjectID          string
	Fireworks                 *FireworksClient
	Registry                  *TrainingClientConfigRegistry
	ManagedHandle             *ManagedHandleMetadata
	ProvisionedHandle         *ManagedProvisionedHandle
	ReferenceHandle           *ManagedProvisionedHandle
	OwnedInferenceDeployments []OwnedInferenceDeployment
	SamplerBackend            *TinkerSamplerBackend
	SyncState                 ManagedSamplerSyncState
	Now                       func() time.Time

	ListCheckpointsFunc                func(context.Context, string, int) ([]map[string]any, error)
	PromoteCheckpointFunc              func(context.Context, PromoteCheckpointOptions) (map[string]any, error)
	ListTrainingSessionCheckpointsFunc func(context.Context, string, int) ([]map[string]any, error)
	PromoteSessionCheckpointFunc       func(context.Context, PromoteSessionCheckpointOptions) (map[string]any, error)
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
		ServerlessSessionID: strings.TrimSpace(opts.TrainingSessionID),
		DefaultUserMetadata: cloneStringMap(opts.DefaultUserMetadata),
		DefaultProjectID:    opts.DefaultProjectID,
		Fireworks:           fireworks,
		Registry:            registry,
		Now:                 now,
	}
	client.ListCheckpointsFunc = fireworks.ListCheckpoints
	client.PromoteCheckpointFunc = fireworks.PromoteCheckpoint
	client.ListTrainingSessionCheckpointsFunc = fireworks.ListTrainingSessionCheckpoints
	client.PromoteSessionCheckpointFunc = fireworks.PromoteSessionCheckpoint
	return client, nil
}

func (c *FiretitanServiceClient) CurrentSessionID() string {
	if c == nil {
		return ""
	}
	return c.ServerlessSessionID
}

func (c *FiretitanServiceClient) TrainingSessionID() string {
	sessionID := c.CurrentSessionID()
	if IsServerlessSessionID(sessionID) {
		return sessionID
	}
	return ""
}

func (c *FiretitanServiceClient) ResolvedAccountID(ctx context.Context) string {
	if c == nil {
		return ""
	}
	if c.AccountIDCache != "" {
		return c.AccountIDCache
	}
	if c.Fireworks == nil {
		return ""
	}
	accountID, err := c.Fireworks.AccountID(ctx)
	if err != nil {
		return ""
	}
	c.AccountIDCache = accountID
	return accountID
}

func (c *FiretitanServiceClient) TrainingSessionName(ctx context.Context) string {
	sessionID := c.TrainingSessionID()
	if sessionID == "" {
		return ""
	}
	accountID := c.ResolvedAccountID(ctx)
	if accountID == "" {
		return ""
	}
	return "accounts/" + accountID + "/trainingSessions/" + sessionID
}

func (c *FiretitanServiceClient) ServerlessRunName(ctx context.Context, modelID any) string {
	runID := RunIDFromModelID(modelID)
	if runID == "" {
		return ""
	}
	accountID := c.ResolvedAccountID(ctx)
	if accountID == "" {
		return ""
	}
	return "accounts/" + accountID + "/trainingRuns/" + runID
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

func (c *FiretitanServiceClient) ManagedTrainerJobID() string {
	return c.ResolvedMetadata().TrainerJobID
}

func (c *FiretitanServiceClient) ManagedDeploymentID() string {
	return c.ResolvedMetadata().DeploymentID
}

func (c *FiretitanServiceClient) ManagedTrainingProfile() *TrainingShapeProfile {
	return c.ResolvedMetadata().TrainingProfile
}

func (c *FiretitanServiceClient) ManagedAcceleratorType() string {
	return c.ResolvedMetadata().AcceleratorType
}

func (c *FiretitanServiceClient) ManagedAcceleratorCount() *int {
	return cloneIntPointer(c.ResolvedMetadata().AcceleratorCount)
}

func (c *FiretitanServiceClient) ManagedMaxContextLength() *int {
	return cloneIntPointer(c.ResolvedMetadata().MaxContextLength)
}

func (c *FiretitanServiceClient) ManagedDeploymentShape() string {
	return c.ResolvedMetadata().DeploymentShape
}

func (c *FiretitanServiceClient) TrainerJobID() (string, error) {
	return RequireManagedString(c.ManagedTrainerJobID(), "trainer job id")
}

func (c *FiretitanServiceClient) DeploymentID() (string, error) {
	return RequireManagedString(c.ManagedDeploymentID(), "deployment id")
}

func (c *FiretitanServiceClient) MaxContextLength() (int, error) {
	return RequireManagedInt(c.ManagedMaxContextLength(), "max context length")
}

func (c *FiretitanServiceClient) DeploymentShape() (string, error) {
	return RequireManagedString(c.ManagedDeploymentShape(), "deployment shape")
}

func (c *FiretitanServiceClient) TrainingProfile() *TrainingShapeProfile {
	return c.ManagedTrainingProfile()
}

func (c *FiretitanServiceClient) AcceleratorType() string {
	return c.ManagedAcceleratorType()
}

func (c *FiretitanServiceClient) AcceleratorCount() *int {
	return c.ManagedAcceleratorCount()
}

func (c *FiretitanServiceClient) ReferenceJobID() string {
	if c == nil || c.ReferenceHandle == nil {
		return ""
	}
	return c.ReferenceHandle.TrainerEndpoint.JobID
}

func (c *FiretitanServiceClient) ReferenceTrainerJobID() string {
	return c.ReferenceJobID()
}

func (c *FiretitanServiceClient) ReferenceClientJobID() (string, error) {
	if referenceJobID := c.ReferenceJobID(); referenceJobID != "" {
		return referenceJobID, nil
	}
	return c.TrainerJobID()
}

func (c *FiretitanServiceClient) ReleaseReferences(ctx context.Context) error {
	if c == nil || c.ReferenceHandle == nil {
		return nil
	}
	handle := c.ReferenceHandle
	c.ReferenceHandle = nil
	return handle.Close(ctx)
}

type OwnedInferenceDeployment struct {
	DeploymentManager ManagedDeploymentCleanup
	DeploymentID      string
	CleanupOnClose    string
}

func (c *FiretitanServiceClient) TrackInferenceDeploymentCleanup(manager ManagedDeploymentCleanup, deploymentID string, cleanupOnClose string) {
	if c == nil || manager == nil || strings.TrimSpace(deploymentID) == "" || strings.TrimSpace(cleanupOnClose) == "" {
		return
	}
	c.OwnedInferenceDeployments = append(c.OwnedInferenceDeployments, OwnedInferenceDeployment{
		DeploymentManager: manager,
		DeploymentID:      deploymentID,
		CleanupOnClose:    cleanupOnClose,
	})
}

func (c *FiretitanServiceClient) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	var closeErr error
	if err := c.ReleaseReferences(ctx); err != nil {
		closeErr = errors.Join(closeErr, err)
	}
	for _, owned := range c.OwnedInferenceDeployments {
		switch owned.CleanupOnClose {
		case string(CleanupDeploymentOnCloseScaleToZero):
			closeErr = errors.Join(closeErr, owned.DeploymentManager.ScaleToZero(ctx, owned.DeploymentID))
		case string(CleanupDeploymentOnCloseDelete):
			closeErr = errors.Join(closeErr, owned.DeploymentManager.DeleteInfo(ctx, owned.DeploymentID))
		default:
			closeErr = errors.Join(closeErr, fmt.Errorf("cleanup_on_close must be one of: %s, %s", CleanupDeploymentOnCloseDelete, CleanupDeploymentOnCloseScaleToZero))
		}
	}
	c.OwnedInferenceDeployments = nil
	if c.ProvisionedHandle != nil {
		closeErr = errors.Join(closeErr, c.ProvisionedHandle.Close(ctx))
	}
	return closeErr
}

func (c *FiretitanServiceClient) GetTelemetry() any {
	return nil
}

func (c *FiretitanServiceClient) CreateRestClient() *FireworksClient {
	if c == nil {
		return nil
	}
	return c.Fireworks
}

func (c *FiretitanServiceClient) GetServerCapabilities() ServerCapabilitiesResponse {
	if c == nil {
		return LazyManagedServerCapabilities(nil)
	}
	return LazyManagedServerCapabilities(&c.Config)
}

func (c *FiretitanServiceClient) ListCheckpoints(ctx context.Context, jobID string, pageSize int) ([]map[string]any, error) {
	if c == nil || c.ListCheckpointsFunc == nil {
		return nil, ControlPlaneCheckpointClientError()
	}
	if strings.TrimSpace(jobID) == "" {
		var err error
		jobID, err = c.TrainerJobID()
		if err != nil {
			return nil, ControlPlaneCheckpointClientError()
		}
	}
	return c.ListCheckpointsFunc(ctx, jobID, pageSize)
}

func (c *FiretitanServiceClient) PromoteCheckpoint(ctx context.Context, opts PromoteCheckpointOptions) (map[string]any, error) {
	if c == nil || c.PromoteCheckpointFunc == nil {
		return nil, ControlPlaneCheckpointClientError()
	}
	if opts.Name == "" && opts.JobID == "" {
		jobID, err := c.TrainerJobID()
		if err != nil {
			return nil, ControlPlaneCheckpointClientError()
		}
		opts.JobID = jobID
	}
	if opts.BaseModel == "" {
		opts.BaseModel = c.Config.BaseModel
	}
	return c.PromoteCheckpointFunc(ctx, opts)
}

func (c *FiretitanServiceClient) ListTrainingSessionCheckpoints(ctx context.Context, name string, pageSize int) ([]map[string]any, error) {
	if c == nil || c.ListTrainingSessionCheckpointsFunc == nil {
		return nil, ControlPlaneCheckpointClientError()
	}
	return c.ListTrainingSessionCheckpointsFunc(ctx, name, pageSize)
}

func (c *FiretitanServiceClient) PromoteSessionCheckpoint(ctx context.Context, opts PromoteSessionCheckpointOptions) (map[string]any, error) {
	if c == nil || c.PromoteSessionCheckpointFunc == nil {
		return nil, ControlPlaneCheckpointClientError()
	}
	if opts.BaseModel == "" {
		opts.BaseModel = c.Config.BaseModel
	}
	return c.PromoteSessionCheckpointFunc(ctx, opts)
}

func (c *FiretitanServiceClient) AttachSamplerBackend(backend *TinkerSamplerBackend) *FiretitanServiceClient {
	if c != nil {
		c.SamplerBackend = backend
	}
	return c
}

func (c *FiretitanServiceClient) RequiresInitialSamplerSync() bool {
	if c == nil {
		return false
	}
	if c.SyncState.RequiresInitialSync() {
		return true
	}
	return c.ProvisionedHandle != nil && c.ProvisionedHandle.RequiresInitialSamplerSync
}

func (c *FiretitanServiceClient) requireSamplerBackend() (*TinkerSamplerBackend, error) {
	if c == nil {
		return nil, CreateSamplingClientUnsupportedError()
	}
	if c.SamplerBackend != nil {
		return c.SamplerBackend, nil
	}
	if c.ProvisionedHandle != nil && c.ProvisionedHandle.SamplerBackend != nil {
		c.SamplerBackend = c.ProvisionedHandle.SamplerBackend
		return c.SamplerBackend, nil
	}
	return nil, CreateSamplingClientUnsupportedError()
}

func (c *FiretitanServiceClient) HotloadSamplerSnapshot(ctx context.Context, modelPath string) error {
	backend, err := c.requireSamplerBackend()
	if err != nil {
		return err
	}
	ok, err := backend.HotloadSavedSnapshot(ctx, modelPath)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("hotload sampler snapshot %q did not complete", modelPath)
	}
	c.SyncState.MarkSamplerHotloaded()
	if c.ProvisionedHandle != nil {
		c.ProvisionedHandle.RequiresInitialSamplerSync = false
	}
	return nil
}

func (c *FiretitanServiceClient) CreateSamplingClient(ctx context.Context, modelPath string, tokenizer DeploymentTokenizer, controller SamplingConcurrencyController, deploymentSampler ...*DeploymentSampler) (*FiretitanSamplingClient, error) {
	if len(deploymentSampler) > 0 && deploymentSampler[0] != nil {
		sampler := deploymentSampler[0]
		if tokenizer != nil {
			sampler.Tokenizer = tokenizer
		}
		if controller != nil {
			sampler.ConcurrencyController = controller
		}
		return NewFiretitanSamplingClient(sampler), nil
	}
	backend, err := c.requireSamplerBackend()
	if err != nil {
		return nil, err
	}
	if modelPath != "" {
		if err := c.HotloadSamplerSnapshot(ctx, modelPath); err != nil {
			return nil, err
		}
	}
	return backend.GetSamplingClient(ctx, tokenizer, controller)
}

func (c *FiretitanServiceClient) CreateDeploymentSampler(ctx context.Context, modelPath string, tokenizer DeploymentTokenizer, controller SamplingConcurrencyController) (*DeploymentSampler, error) {
	client, err := c.CreateSamplingClient(ctx, modelPath, tokenizer, controller)
	if err != nil {
		return nil, err
	}
	return client.DeploymentSampler, nil
}

type CreateInferenceDeploymentSamplerOptions struct {
	Timeout               time.Duration
	CleanupOnClose        string
	Tokenizer             DeploymentTokenizer
	ConcurrencyController SamplingConcurrencyController
	Deployment            ManagedDeploymentController
}

func (c *FiretitanServiceClient) CreateDeploymentSamplerForModel(_ context.Context, model string, tokenizer DeploymentTokenizer, controller SamplingConcurrencyController, inferenceURL ...string) (*DeploymentSampler, error) {
	if c == nil {
		return nil, fmt.Errorf("FiretitanServiceClient is nil")
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("model must be a non-empty string")
	}
	baseURL := ""
	if len(inferenceURL) > 0 {
		baseURL = inferenceURL[0]
	}
	if baseURL == "" && c.ProvisionedHandle != nil && c.ProvisionedHandle.ConcreteDeploymentManager != nil {
		baseURL = c.ProvisionedHandle.ConcreteDeploymentManager.InferenceURL
	}
	if baseURL == "" && c.Fireworks != nil {
		baseURL = c.Fireworks.BaseURL()
	}
	if baseURL == "" {
		baseURL = DefaultFireworksAPIURL
	}
	apiKey := ""
	if c.Fireworks != nil {
		apiKey = c.Fireworks.APIKey()
	}
	var samplerOpts []DeploymentSamplerOption
	if tokenizer != nil {
		samplerOpts = append(samplerOpts, WithDeploymentSamplerTokenizer(tokenizer))
	}
	if controller != nil {
		samplerOpts = append(samplerOpts, WithDeploymentSamplerConcurrencyController(controller))
	}
	return NewDeploymentSampler(baseURL, model, apiKey, samplerOpts...), nil
}

func (c *FiretitanServiceClient) CreateInferenceDeploymentSampler(ctx context.Context, config DeploymentConfig, opts ...CreateInferenceDeploymentSamplerOptions) (*DeploymentSampler, error) {
	if c == nil {
		return nil, fmt.Errorf("FiretitanServiceClient is nil")
	}
	var opt CreateInferenceDeploymentSamplerOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if config.MaxReplicaCount != nil && *config.MaxReplicaCount <= 0 {
		return nil, fmt.Errorf("DeploymentConfig.max_replica_count must be positive for inference sampling")
	}
	deployment := opt.Deployment
	var concrete *DeploymentManager
	if deployment == nil && c.ProvisionedHandle != nil {
		deployment = c.ProvisionedHandle.DeploymentManager
		concrete = c.ProvisionedHandle.ConcreteDeploymentManager
	}
	if deployment == nil && c.Fireworks != nil {
		concrete = NewDeploymentManager(c.Fireworks.APIKey(), c.Fireworks.BaseURL())
		deployment = concrete
	}
	if deployment == nil {
		return nil, fmt.Errorf("inference deployment sampler requires a deployment manager")
	}
	managedRegion := c.Config.Region
	if config.Region != "" && managedRegion != "" && config.Region != managedRegion {
		return nil, fmt.Errorf("inference deployment region conflicts with managed trainer region: region=%q, managed region=%q", config.Region, managedRegion)
	}
	if config.Region == "" {
		config.Region = managedRegion
	}
	if config.DeploymentShape == "" && config.BaseModel == c.Config.BaseModel {
		config.DeploymentShape = c.ManagedDeploymentShape()
	}
	info, found, err := deployment.GetInfo(ctx, config.DeploymentID)
	if err != nil {
		return nil, err
	}
	if found {
		if DeploymentTerminalStates[info.State] {
			return nil, fmt.Errorf("inference deployment %q is in terminal state %q. Use a different deployment_id or restore/delete the old resource", config.DeploymentID, info.State)
		}
	} else {
		info, err = deployment.CreateOrGet(ctx, config, false)
		if err != nil {
			return nil, err
		}
		if cleanup, ok := deployment.(ManagedDeploymentCleanup); ok {
			c.TrackInferenceDeploymentCleanup(cleanup, config.DeploymentID, opt.CleanupOnClose)
		}
	}
	timeout := opt.Timeout
	if timeout == 0 {
		timeout = DeploymentReadyTimeout
	}
	ready, err := deployment.WaitForReady(ctx, config.DeploymentID, DeploymentWaitOptions{Timeout: timeout})
	if err != nil {
		return nil, err
	}
	model := ready.InferenceModel
	if model == "" {
		if concrete != nil {
			accountID, _ := concrete.AccountID(ctx)
			if accountID != "" {
				model = "accounts/" + accountID + "/deployments/" + config.DeploymentID
			}
		}
		if model == "" {
			model = info.InferenceModel
		}
		if model == "" {
			model = "deployments/" + config.DeploymentID
		}
	}
	inferenceURL := ""
	if concrete != nil {
		inferenceURL = concrete.InferenceURL
	}
	return c.CreateDeploymentSamplerForModel(ctx, model, opt.Tokenizer, opt.ConcurrencyController, inferenceURL)
}

type CreateFiretitanTrainingClientOptions struct {
	ConfigOverride             *FiretitanProvisioningConfig
	BaseModel                  string
	LoraRank                   *int
	LoraAlpha                  *int
	UserMetadata               map[string]string
	HandleMetadata             *ManagedHandleMetadata
	SamplerBackend             *TinkerSamplerBackend
	WeightSyncer               *WeightSyncer
	StateBackend               TrainingStateBackend
	AdapterLoader              TrainingAdapterLoader
	ComputeBackend             TrainingComputeBackend
	ModelID                    string
	RunName                    string
	TrainerBaseURL             string
	RequiresInitialSamplerSync bool
	SkipRegistry               bool
}

type SaveStateOptions struct {
	TTLSeconds *int
	Overwrite  bool
}

type SaveStateResult struct {
	Name string
	Path string
}

type TrainingStateBackend interface {
	SaveState(context.Context, string, SaveStateOptions) (SaveStateResult, error)
	LoadState(context.Context, string) error
	LoadStateWithOptimizer(context.Context, string) error
}

type TrainingAdapterLoader interface {
	LoadAdapter(context.Context, LoadAdapterOptions) (LoadAdapterResponse, error)
}

type OptimStepOptions struct {
	ModelID                       string
	SeqID                         int
	AdamParams                    any
	GradAccumulationNormalization string
	EmitGradNormMetrics           any
}

type ForwardBackwardOptions struct {
	ModelID      string
	SeqID        int
	Data         []TrainingDatum
	LossFn       LossFn
	LossFnConfig map[string]any
}

type ForwardBackwardContrastiveOptions struct {
	NumQueries        int
	Temperature       float64
	Pooling           EmbeddingPooling
	NumExtraNegatives int
}

type TrainingComputeBackend interface {
	OptimStep(context.Context, OptimStepOptions) (map[string]any, error)
	ForwardBackward(context.Context, ForwardBackwardOptions) (ForwardBackwardOutput, error)
}

type WeightsInfoProvider func(context.Context, string) (WeightsInfo, error)

type CreateTrainingClientFromStateOptions struct {
	UserMetadata        map[string]string
	WeightsAccessToken  *string
	WeightsInfo         *WeightsInfo
	WeightsInfoProvider WeightsInfoProvider
	StateBackend        TrainingStateBackend
	AdapterLoader       TrainingAdapterLoader
	ModelID             string
	TrainerBaseURL      string
}

type CreateReferenceClientOptions struct {
	LoraRank     int
	UserMetadata map[string]string
}

type CreateLoraTrainingClientOptions struct {
	Rank         int
	Alpha        *int
	Seed         *int
	TrainMLP     *bool
	TrainAttn    *bool
	TrainUnembed *bool
	UserMetadata map[string]string
}

type FiretitanTrainingClient struct {
	Service         *FiretitanServiceClient
	Config          FiretitanProvisioningConfig
	UserMetadata    map[string]string
	Warnings        []string
	HandleMetadata  ManagedHandleMetadata
	SamplerBackend  *TinkerSamplerBackend
	WeightSyncer    *WeightSyncer
	StateBackend    TrainingStateBackend
	AdapterLoader   TrainingAdapterLoader
	ComputeBackend  TrainingComputeBackend
	ModelID         string
	RunName         string
	TrainerBaseURL  string
	RequestSeqID    int
	SavedStateNames map[string]bool
	SyncState       ManagedSamplerSyncState
}

func (c *FiretitanServiceClient) CreateTrainingClient(ctx context.Context, opts ...CreateFiretitanTrainingClientOptions) (*FiretitanTrainingClient, error) {
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
	config := c.Config
	if opt.ConfigOverride != nil {
		normalized, err := opt.ConfigOverride.Normalize()
		if err != nil {
			return nil, err
		}
		config = normalized
	}
	if config.LoraRank > 0 && config.LoraAlpha == nil {
		config.LoraAlpha = intPointer(DefaultLoraAlpha)
	}
	if config.LoraRank == 0 {
		config.LoraAlpha = nil
	}
	if !opt.SkipRegistry {
		key := ManagedTrainingClientKey(config)
		if err := c.Registry.Add(key); err != nil {
			return nil, err
		}
	}
	var warnings []string
	if opt.BaseModel != "" {
		if warning := DeprecatedManagedOverrideMessage("create_training_client", "base_model", opt.BaseModel, config.BaseModel); warning != "" {
			warnings = append(warnings, warning)
		}
	}
	if opt.LoraRank != nil {
		if warning := DeprecatedManagedOverrideMessage("create_training_client", "lora_rank", *opt.LoraRank, config.LoraRank); warning != "" {
			warnings = append(warnings, warning)
		}
	}
	if opt.LoraAlpha != nil {
		var configuredAlpha any
		if config.LoraAlpha != nil {
			configuredAlpha = *config.LoraAlpha
		}
		if warning := DeprecatedManagedOverrideMessage("create_training_client", "lora_alpha", *opt.LoraAlpha, configuredAlpha); warning != "" {
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
	runName := opt.RunName
	if runName == "" {
		runName = c.ServerlessRunName(ctx, opt.ModelID)
	}
	return &FiretitanTrainingClient{
		Service:         c,
		Config:          config,
		UserMetadata:    ResolveUserMetadata(c.DefaultUserMetadata, opt.UserMetadata),
		Warnings:        warnings,
		HandleMetadata:  handle,
		SamplerBackend:  backend,
		WeightSyncer:    opt.WeightSyncer,
		StateBackend:    opt.StateBackend,
		AdapterLoader:   opt.AdapterLoader,
		ComputeBackend:  opt.ComputeBackend,
		ModelID:         opt.ModelID,
		RunName:         runName,
		TrainerBaseURL:  opt.TrainerBaseURL,
		SavedStateNames: map[string]bool{},
		SyncState:       state,
	}, nil
}

func (c *FiretitanServiceClient) CreateLoraTrainingClient(ctx context.Context, baseModel string, opts ...CreateLoraTrainingClientOptions) (*FiretitanTrainingClient, error) {
	if c == nil {
		return nil, fmt.Errorf("FiretitanServiceClient is nil")
	}
	var opt CreateLoraTrainingClientOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	config := c.Config
	config.BaseModel = strings.TrimSpace(baseModel)
	if config.BaseModel == "" {
		return nil, fmt.Errorf("base_model must be a non-empty string")
	}
	rank := opt.Rank
	if rank == 0 {
		rank = 32
	}
	config.LoraRank = rank
	config.LoraAlpha = intPointer(DefaultLoraAlpha)
	if opt.Alpha != nil {
		config.LoraAlpha = cloneIntPointer(opt.Alpha)
	}
	config.Seed = cloneIntPointer(opt.Seed)
	config.TrainMLP = boolPointer(true)
	config.TrainAttn = boolPointer(true)
	config.TrainUnembed = boolPointer(true)
	if opt.TrainMLP != nil {
		config.TrainMLP = boolPointer(*opt.TrainMLP)
	}
	if opt.TrainAttn != nil {
		config.TrainAttn = boolPointer(*opt.TrainAttn)
	}
	if opt.TrainUnembed != nil {
		config.TrainUnembed = boolPointer(*opt.TrainUnembed)
	}
	return c.CreateTrainingClient(ctx, CreateFiretitanTrainingClientOptions{
		ConfigOverride: &config,
		BaseModel:      baseModel,
		LoraRank:       &rank,
		LoraAlpha:      config.LoraAlpha,
		UserMetadata:   opt.UserMetadata,
	})
}

func (c *FiretitanServiceClient) CreateBaseTrainingClient(ctx context.Context, baseModel string, userMetadata map[string]string) (*FiretitanTrainingClient, error) {
	if c == nil {
		return nil, fmt.Errorf("FiretitanServiceClient is nil")
	}
	config := c.Config
	config.BaseModel = strings.TrimSpace(baseModel)
	config.LoraRank = 0
	config.ForwardOnly = true
	config.CreateDeployment = boolPointer(false)
	if config.BaseModel == "" {
		return nil, fmt.Errorf("base_model must be a non-empty string")
	}
	return c.CreateTrainingClient(ctx, CreateFiretitanTrainingClientOptions{
		ConfigOverride: &config,
		BaseModel:      baseModel,
		UserMetadata:   userMetadata,
		SkipRegistry:   true,
	})
}

func (c *FiretitanServiceClient) CreateReferenceClient(ctx context.Context, baseModel string, opts ...CreateReferenceClientOptions) (*FiretitanTrainingClient, error) {
	if c == nil {
		return nil, fmt.Errorf("FiretitanServiceClient is nil")
	}
	var opt CreateReferenceClientOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	policyLoraRank := opt.LoraRank
	if policyLoraRank == 0 {
		policyLoraRank = c.Config.LoraRank
	}
	if UseSharedBaseReference(c.Config, policyLoraRank) {
		return c.CreateBaseTrainingClient(ctx, baseModel, opt.UserMetadata)
	}
	if c.ReferenceHandle == nil {
		return nil, fmt.Errorf("create_reference_client requires a provisioned separate reference trainer or a shared LoRA base reference")
	}
	config := c.ReferenceHandle.Config
	if strings.TrimSpace(baseModel) != "" {
		config.BaseModel = strings.TrimSpace(baseModel)
	}
	if config.TrainerJobID == "" {
		config.TrainerJobID = c.ReferenceHandle.TrainerEndpoint.JobID
	}
	return c.CreateTrainingClient(ctx, CreateFiretitanTrainingClientOptions{
		ConfigOverride: &config,
		BaseModel:      baseModel,
		UserMetadata:   opt.UserMetadata,
		HandleMetadata: &c.ReferenceHandle.Metadata,
		SamplerBackend: c.ReferenceHandle.SamplerBackend,
		WeightSyncer:   c.ReferenceHandle.WeightSyncer,
		SkipRegistry:   true,
	})
}

func (c *FiretitanServiceClient) CreateTrainingClientFromWeightsInfo(ctx context.Context, info WeightsInfo, opts ...CreateTrainingClientFromStateOptions) (*FiretitanTrainingClient, error) {
	plan, err := TrainingClientPlanFromWeightsInfo(info)
	if err != nil {
		return nil, err
	}
	config := c.Config
	config.BaseModel = plan.BaseModel
	config.LoraRank = plan.LoraRank
	config.TrainUnembed = boolPointer(plan.TrainUnembed)
	config.TrainMLP = boolPointer(plan.TrainMLP)
	config.TrainAttn = boolPointer(plan.TrainAttn)

	var opt CreateTrainingClientFromStateOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	return c.CreateTrainingClient(ctx, CreateFiretitanTrainingClientOptions{
		ConfigOverride: &config,
		UserMetadata:   opt.UserMetadata,
		StateBackend:   opt.StateBackend,
		AdapterLoader:  opt.AdapterLoader,
		ModelID:        opt.ModelID,
		TrainerBaseURL: opt.TrainerBaseURL,
	})
}

func (c *FiretitanServiceClient) CreateTrainingClientFromState(ctx context.Context, path string, opts ...CreateTrainingClientFromStateOptions) (*FiretitanTrainingClient, error) {
	return c.createTrainingClientFromState(ctx, path, false, opts...)
}

func (c *FiretitanServiceClient) CreateTrainingClientFromStateWithOptimizer(ctx context.Context, path string, opts ...CreateTrainingClientFromStateOptions) (*FiretitanTrainingClient, error) {
	return c.createTrainingClientFromState(ctx, path, true, opts...)
}

func (c *FiretitanServiceClient) createTrainingClientFromState(ctx context.Context, path string, withOptimizer bool, opts ...CreateTrainingClientFromStateOptions) (*FiretitanTrainingClient, error) {
	if c == nil {
		return nil, fmt.Errorf("FiretitanServiceClient is nil")
	}
	var opt CreateTrainingClientFromStateOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	method := "create_training_client_from_state"
	if withOptimizer {
		method = "create_training_client_from_state_with_optimizer"
	}
	if err := RejectServiceWeightsAccessToken(method, opt.WeightsAccessToken); err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("path must be a non-empty string")
	}
	stateBackend := opt.StateBackend
	if stateBackend == nil {
		return nil, fmt.Errorf("CreateTrainingClientFromState requires a StateBackend")
	}

	var info WeightsInfo
	if opt.WeightsInfo != nil {
		info = *opt.WeightsInfo
	} else if opt.WeightsInfoProvider != nil {
		var err error
		info, err = opt.WeightsInfoProvider(ctx, path)
		if err != nil {
			return nil, err
		}
	} else {
		info = WeightsInfoFromManagedConfig(c.Config)
	}

	client, err := c.CreateTrainingClientFromWeightsInfo(ctx, info, CreateTrainingClientFromStateOptions{
		UserMetadata:   opt.UserMetadata,
		StateBackend:   stateBackend,
		AdapterLoader:  opt.AdapterLoader,
		ModelID:        opt.ModelID,
		TrainerBaseURL: opt.TrainerBaseURL,
	})
	if err != nil {
		return nil, err
	}
	if withOptimizer {
		err = client.LoadStateWithOptimizer(ctx, path, nil)
	} else {
		err = client.LoadState(ctx, path, nil)
	}
	if err != nil {
		return nil, err
	}
	return client, nil
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

func (c *FiretitanTrainingClient) CreateBaseTrainingClient(ctx context.Context, baseModel string, userMetadata map[string]string) (*FiretitanTrainingClient, error) {
	if c == nil || c.Service == nil {
		return nil, fmt.Errorf("FiretitanTrainingClient requires a service to create a base training client")
	}
	return c.Service.CreateBaseTrainingClient(ctx, baseModel, userMetadata)
}

func (c *FiretitanTrainingClient) AttachComputeBackend(backend TrainingComputeBackend) *FiretitanTrainingClient {
	if c != nil {
		c.ComputeBackend = backend
	}
	return c
}

func (c *FiretitanTrainingClient) OptimStep(ctx context.Context, adamParams any, gradAccumulationNormalization any) (map[string]any, error) {
	return c.OptimStepExt(ctx, adamParams, OptimStepExtOptions{GradAccumulationNormalization: gradAccumulationNormalization})
}

type OptimStepExtOptions struct {
	GradAccumulationNormalization any
	EmitGradNormMetrics           any
}

func (c *FiretitanTrainingClient) OptimStepExt(ctx context.Context, adamParams any, opts OptimStepExtOptions) (map[string]any, error) {
	if c == nil || c.ComputeBackend == nil {
		return nil, fmt.Errorf("FiretitanTrainingClient requires a ComputeBackend to optim_step")
	}
	if strings.TrimSpace(c.ModelID) == "" {
		return nil, fmt.Errorf("model_id must be a non-empty string")
	}
	normalization, err := NormalizeGradAccNormalization(opts.GradAccumulationNormalization)
	if err != nil {
		return nil, err
	}
	gradNormMode, err := NormalizeGradNormMetricsMode(opts.EmitGradNormMetrics)
	if err != nil {
		return nil, err
	}
	c.RequestSeqID++
	return c.ComputeBackend.OptimStep(ctx, OptimStepOptions{
		ModelID:                       c.ModelID,
		SeqID:                         c.RequestSeqID,
		AdamParams:                    adamParams,
		GradAccumulationNormalization: normalization,
		EmitGradNormMetrics:           gradNormMode,
	})
}

func (c *FiretitanTrainingClient) RunID() string {
	if c == nil {
		return ""
	}
	return RunIDFromModelID(c.ModelID)
}

func (c *FiretitanTrainingClient) ForwardBackward(ctx context.Context, data []TrainingDatum, lossFn string, lossFnConfig map[string]any) (ForwardBackwardOutput, error) {
	if c == nil || c.ComputeBackend == nil {
		return ForwardBackwardOutput{}, fmt.Errorf("FiretitanTrainingClient requires a ComputeBackend to forward_backward")
	}
	if strings.TrimSpace(c.ModelID) == "" {
		return ForwardBackwardOutput{}, fmt.Errorf("model_id must be a non-empty string")
	}
	normalizedLossFn, err := NormalizeLossFn(lossFn)
	if err != nil {
		return ForwardBackwardOutput{}, err
	}
	c.RequestSeqID++
	output, err := c.ComputeBackend.ForwardBackward(ctx, ForwardBackwardOptions{
		ModelID:      c.ModelID,
		SeqID:        c.RequestSeqID,
		Data:         append([]TrainingDatum(nil), data...),
		LossFn:       normalizedLossFn,
		LossFnConfig: cloneAnyMap(lossFnConfig),
	})
	if err != nil {
		return ForwardBackwardOutput{}, err
	}
	if normalizedLossFn == LossFnCrossEntropy {
		output = AddCrossEntropyResponseTokens(output, data)
	}
	return output, nil
}

func (c *FiretitanTrainingClient) ForwardBackwardContrastive(ctx context.Context, data []TrainingDatum, opts ForwardBackwardContrastiveOptions) (ForwardBackwardOutput, error) {
	expectedLen := 2*opts.NumQueries + opts.NumExtraNegatives
	if len(data) != expectedLen {
		return ForwardBackwardOutput{}, fmt.Errorf(
			"forward_backward_contrastive expects len(data) == 2*num_queries + num_extra_negatives = 2*%d + %d = %d; got len(data)=%d",
			opts.NumQueries,
			opts.NumExtraNegatives,
			expectedLen,
			len(data),
		)
	}
	if opts.NumExtraNegatives < 0 {
		return ForwardBackwardOutput{}, fmt.Errorf("num_extra_negatives must be >= 0, got %d", opts.NumExtraNegatives)
	}
	pooling := opts.Pooling
	if pooling == "" {
		pooling = EmbeddingPoolingLast
	}
	return c.ForwardBackward(ctx, data, string(LossFnCrossEntropy), map[string]any{
		"output":              "contrastive_loss",
		"num_queries":         opts.NumQueries,
		"temperature":         opts.Temperature,
		"pooling":             string(pooling),
		"num_extra_negatives": opts.NumExtraNegatives,
	})
}

func (c *FiretitanTrainingClient) ListCheckpoints(ctx context.Context, pageSize int) ([]map[string]any, error) {
	jobID, err := c.TrainerJobID()
	if err != nil {
		return nil, ControlPlaneCheckpointClientError()
	}
	if c.Service == nil {
		return nil, ControlPlaneCheckpointClientError()
	}
	return c.Service.ListCheckpoints(ctx, jobID, pageSize)
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
	if c.Service == nil {
		return nil, ControlPlaneCheckpointClientError()
	}
	return c.Service.PromoteCheckpoint(ctx, opts)
}

func (c *FiretitanTrainingClient) ResolveCheckpointPath(checkpointName string, sourceJobID ...string) (string, error) {
	return ResolveCheckpointPath(checkpointName, sourceJobID...)
}

func (c *FiretitanTrainingClient) SaveState(ctx context.Context, name string, opts ...SaveStateOptions) (SaveStateResult, error) {
	if c == nil {
		return SaveStateResult{}, fmt.Errorf("FiretitanTrainingClient is nil")
	}
	var opt SaveStateOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if err := ValidateSaveStateOptions(opt.Overwrite); err != nil {
		return SaveStateResult{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return SaveStateResult{}, fmt.Errorf("name must be a non-empty string")
	}
	if c.SavedStateNames == nil {
		c.SavedStateNames = map[string]bool{}
	}
	if c.SavedStateNames[name] {
		c.Warnings = append(c.Warnings, fmt.Sprintf("DCP checkpoint name %q already used in this session; overwriting", name))
	}
	c.SavedStateNames[name] = true
	if c.StateBackend == nil {
		return SaveStateResult{}, fmt.Errorf("FiretitanTrainingClient requires a StateBackend to save_state")
	}
	return c.StateBackend.SaveState(ctx, name, opt)
}

func (c *FiretitanTrainingClient) LoadState(ctx context.Context, path string, weightsAccessToken *string) error {
	if err := RejectTrainingLoadWeightsAccessToken("load_state", weightsAccessToken); err != nil {
		return err
	}
	if c == nil || c.StateBackend == nil {
		return fmt.Errorf("FiretitanTrainingClient requires a StateBackend to load_state")
	}
	return c.StateBackend.LoadState(ctx, path)
}

func (c *FiretitanTrainingClient) LoadStateWithOptimizer(ctx context.Context, path string, weightsAccessToken *string) error {
	if err := RejectTrainingLoadWeightsAccessToken("load_state_with_optimizer", weightsAccessToken); err != nil {
		return err
	}
	if c == nil || c.StateBackend == nil {
		return fmt.Errorf("FiretitanTrainingClient requires a StateBackend to load_state_with_optimizer")
	}
	return c.StateBackend.LoadStateWithOptimizer(ctx, path)
}

func (c *FiretitanTrainingClient) LoadAdapter(ctx context.Context, adapterPath string) (LoadAdapterResponse, error) {
	adapterPath = strings.TrimSpace(adapterPath)
	if adapterPath == "" {
		return LoadAdapterResponse{}, fmt.Errorf("adapter_path must be a non-empty string")
	}
	if c == nil || c.AdapterLoader == nil {
		return LoadAdapterResponse{}, fmt.Errorf("FiretitanTrainingClient requires an AdapterLoader to load_adapter")
	}
	if strings.TrimSpace(c.ModelID) == "" {
		return LoadAdapterResponse{}, fmt.Errorf("model_id must be a non-empty string")
	}
	trainerBaseURL := strings.TrimSpace(c.TrainerBaseURL)
	if trainerBaseURL == "" {
		if c.Service != nil && c.Service.ProvisionedHandle != nil {
			trainerBaseURL = c.Service.ProvisionedHandle.TrainerEndpoint.BaseURL
		}
	}
	if trainerBaseURL == "" {
		return LoadAdapterResponse{}, fmt.Errorf("trainer_base_url must be a non-empty string")
	}
	c.RequestSeqID++
	return c.AdapterLoader.LoadAdapter(ctx, LoadAdapterOptions{
		TrainerBaseURL: trainerBaseURL,
		ModelID:        c.ModelID,
		AdapterPath:    adapterPath,
		SeqID:          c.RequestSeqID,
	})
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
	var opts SaveWeightsForSamplerOptions
	if len(checkpointType) > 0 {
		opts.CheckpointType = checkpointType[0]
	}
	return c.SaveWeightsForSamplerExt(ctx, name, opts)
}

func (c *FiretitanTrainingClient) SaveWeightsForSamplerExt(ctx context.Context, name string, opts SaveWeightsForSamplerOptions) (SaveSamplerResult, error) {
	if c == nil || c.WeightSyncer == nil {
		return SaveSamplerResult{}, fmt.Errorf("FiretitanTrainingClient requires a WeightSyncer to save weights for sampler")
	}
	ckptType, err := c.NextSamplerCheckpointType(opts.CheckpointType)
	if err != nil {
		return SaveSamplerResult{}, err
	}
	opts.CheckpointType = string(ckptType)
	result, err := c.WeightSyncer.SaveOnlyExt(ctx, name, opts)
	if err != nil {
		return SaveSamplerResult{}, err
	}
	result.Path = result.SnapshotName
	if c.SamplerBackend != nil {
		c.SamplerBackend.RememberSavedSnapshot(result.SnapshotName, opts.CheckpointType)
	}
	return result, nil
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

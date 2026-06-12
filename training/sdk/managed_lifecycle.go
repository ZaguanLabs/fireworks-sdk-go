package sdk

import (
	"context"
	"fmt"
	"time"
)

type ManagedTrainingProfileResolver interface {
	ResolveTrainingProfile(context.Context, string) (TrainingShapeProfile, error)
}

type ManagedTrainerController interface {
	AccountID(context.Context) (string, error)
	Create(context.Context, TrainerJobConfig) (CreatedTrainerJob, error)
	WaitForReady(context.Context, string, string, ...TrainerPollOptions) (TrainerServiceEndpoint, error)
	ReconnectAndWait(context.Context, string, ...TrainerReconnectOptions) (TrainerServiceEndpoint, error)
}

type ManagedTrainerTryGetter interface {
	TryGetJob(context.Context, string) (map[string]any, bool, error)
}

type startedManagedTrainer struct {
	Endpoint TrainerServiceEndpoint
	Created  bool
}

type ManagedDeploymentController interface {
	GetInfo(context.Context, string) (DeploymentInfo, bool, error)
	CreateOrGet(context.Context, DeploymentConfig, bool) (DeploymentInfo, error)
	WaitForReady(context.Context, string, ...DeploymentWaitOptions) (DeploymentInfo, error)
	ReattachTrainer(context.Context, DeploymentInfo, string, string, ...ReattachTrainerOptions) (DeploymentInfo, error)
}

type managedDeploymentAttachment struct {
	Info               DeploymentInfo
	ResetSnapshotChain bool
	Created            bool
}

type ManagedTrainerCleanup interface {
	DeleteJob(context.Context, string) error
}

type ManagedDeploymentCleanup interface {
	DeleteInfo(context.Context, string) error
	ScaleToZero(context.Context, string) error
}

type ManagedProvisionOptions struct {
	Config               FiretitanProvisioningConfig
	UserMetadata         map[string]string
	ProfileResolver      ManagedTrainingProfileResolver
	Trainer              ManagedTrainerController
	Deployment           ManagedDeploymentController
	DeploymentManager    *DeploymentManager
	TrainerPollOptions   TrainerPollOptions
	ReconnectOptions     TrainerReconnectOptions
	DeploymentWait       DeploymentWaitOptions
	ReattachOptions      ReattachTrainerOptions
	ReconnectExistingJob bool
	Now                  func() time.Time
}

type ManagedProvisionedHandle struct {
	Config                     FiretitanProvisioningConfig
	UserMetadata               map[string]string
	TrainerEndpoint            TrainerServiceEndpoint
	TrainingProfile            *TrainingShapeProfile
	MaxContextLength           *int
	DeploymentShape            string
	Deployment                 *DeploymentInfo
	RequiresInitialSamplerSync bool
	SamplerBackend             *TinkerSamplerBackend
	WeightSyncer               *WeightSyncer
	ReferenceHandle            *ManagedProvisionedHandle
	Metadata                   ManagedHandleMetadata
	CleanupPlan                []ManagedCleanupStep
	TrainerManager             ManagedTrainerController
	DeploymentManager          ManagedDeploymentController
	ConcreteDeploymentManager  *DeploymentManager
	CleanupTrainerOnClose      bool
	CleanupDeploymentOnClose   string
	closed                     bool
}

func (h *ManagedProvisionedHandle) Close(ctx context.Context) error {
	if h == nil || h.closed {
		return nil
	}
	h.closed = true
	if h.ReferenceHandle != nil {
		if err := h.ReferenceHandle.Close(ctx); err != nil {
			return err
		}
	}
	for _, step := range h.CleanupPlan {
		switch step.Operation {
		case ManagedCleanupDeleteDeployment:
			cleanup, ok := h.DeploymentManager.(ManagedDeploymentCleanup)
			if !ok {
				return fmt.Errorf("managed deployment cleanup requires DeleteInfo")
			}
			if err := cleanup.DeleteInfo(ctx, step.DeploymentID); err != nil {
				return err
			}
		case ManagedCleanupScaleDeploymentZero:
			cleanup, ok := h.DeploymentManager.(ManagedDeploymentCleanup)
			if !ok {
				return fmt.Errorf("managed deployment cleanup requires ScaleToZero")
			}
			if err := cleanup.ScaleToZero(ctx, step.DeploymentID); err != nil {
				return err
			}
		case ManagedCleanupDeleteTrainer:
			cleanup, ok := h.TrainerManager.(ManagedTrainerCleanup)
			if !ok {
				return fmt.Errorf("managed trainer cleanup requires DeleteJob")
			}
			if err := cleanup.DeleteJob(ctx, step.TrainerJobID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *FiretitanServiceClient) ProvisionManagedHandle(ctx context.Context, opts ...ManagedProvisionOptions) (*ManagedProvisionedHandle, error) {
	if c == nil {
		return nil, fmt.Errorf("FiretitanServiceClient is nil")
	}
	var opt ManagedProvisionOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Config.BaseModel == "" {
		opt.Config = c.Config
	}
	if opt.UserMetadata == nil {
		opt.UserMetadata = c.DefaultUserMetadata
	}
	if opt.ProfileResolver == nil {
		opt.ProfileResolver = c.Fireworks
	}
	if opt.Trainer == nil {
		opt.Trainer = NewTrainerJobManager(c.Fireworks.APIKey(), c.Fireworks.BaseURL())
	}
	if opt.DeploymentManager != nil && opt.Deployment == nil {
		opt.Deployment = opt.DeploymentManager
	}
	if opt.Deployment == nil {
		opt.DeploymentManager = NewDeploymentManager(c.Fireworks.APIKey(), c.Fireworks.BaseURL())
		opt.Deployment = opt.DeploymentManager
	}
	if opt.Now == nil {
		opt.Now = c.Now
	}
	handle, err := ProvisionManagedHandle(ctx, opt)
	if err != nil {
		return nil, err
	}
	c.ProvisionedHandle = handle
	c.ManagedHandle = &handle.Metadata
	c.ReferenceHandle = handle.ReferenceHandle
	c.SamplerBackend = handle.SamplerBackend
	c.SyncState = ManagedSamplerSyncState{RequiresInitialSamplerSync: handle.RequiresInitialSamplerSync}
	return handle, nil
}

func (c *FiretitanServiceClient) CreateManagedTrainingClient(ctx context.Context, opts ...ManagedProvisionOptions) (*FiretitanTrainingClient, error) {
	handle, err := c.ProvisionManagedHandle(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return c.CreateTrainingClient(ctx, CreateFiretitanTrainingClientOptions{
		UserMetadata:               handle.UserMetadata,
		HandleMetadata:             &handle.Metadata,
		SamplerBackend:             handle.SamplerBackend,
		WeightSyncer:               handle.WeightSyncer,
		RequiresInitialSamplerSync: handle.RequiresInitialSamplerSync,
	})
}

func ProvisionManagedHandle(ctx context.Context, opts ManagedProvisionOptions) (*ManagedProvisionedHandle, error) {
	config, err := opts.Config.Normalize()
	if err != nil {
		return nil, err
	}
	explicitTrainerJobID := config.TrainerJobID != ""
	if !explicitTrainerJobID {
		config.TrainerJobID = newTrainerJobID()
	}
	explicitDeploymentID := config.DeploymentID != ""
	if opts.DeploymentManager != nil && opts.Deployment == nil {
		opts.Deployment = opts.DeploymentManager
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	var profile *TrainingShapeProfile
	if config.TrainingShapeID != "" {
		if opts.ProfileResolver == nil {
			return nil, fmt.Errorf("managed provisioning requires ProfileResolver for training_shape_id")
		}
		resolved, err := opts.ProfileResolver.ResolveTrainingProfile(ctx, config.TrainingShapeID)
		if err != nil {
			return nil, err
		}
		profile = &resolved
	}

	maxContextLength := cloneIntPointer(config.MaxContextLength)
	if maxContextLength == nil && profile != nil && profile.MaxSupportedContextLength != 0 {
		maxContextLength = intPointer(profile.MaxSupportedContextLength)
	}
	deploymentShape := config.DeploymentShape
	if deploymentShape == "" && profile != nil {
		deploymentShape = profile.DeploymentShape()
	}

	var referenceConfig *FiretitanProvisioningConfig
	if ShouldProvisionReference(config) {
		derived, err := ReferenceManagedConfig(config, config.LoraRank)
		if err != nil {
			return nil, err
		}
		referenceConfig = &derived
		if derived.TrainingShapeID != "" {
			if opts.ProfileResolver == nil {
				return nil, fmt.Errorf("managed reference validation requires ProfileResolver")
			}
			referenceProfile, err := opts.ProfileResolver.ResolveTrainingProfile(ctx, derived.TrainingShapeID)
			if err != nil {
				return nil, err
			}
			if err := ValidateReferenceTrainingShape(derived, referenceProfile); err != nil {
				return nil, err
			}
		}
	}

	startedTrainer, err := provisionManagedTrainer(ctx, opts.Trainer, config, maxContextLength, profile, explicitTrainerJobID, opts)
	if err != nil {
		return nil, err
	}
	trainerEndpoint := startedTrainer.Endpoint

	var deployment *DeploymentInfo
	var samplerBackend *TinkerSamplerBackend
	requiresInitialSync := false
	deploymentCreated := false
	createDeployment := true
	if config.CreateDeployment != nil {
		createDeployment = *config.CreateDeployment
	}
	if createDeployment {
		attached, err := attachManagedDeployment(ctx, opts.Deployment, config, trainerEndpoint.JobName, deploymentShape, now, explicitDeploymentID, opts)
		if err != nil {
			return nil, err
		}
		deployment = &attached.Info
		config.DeploymentID = attached.Info.DeploymentID
		requiresInitialSync = attached.ResetSnapshotChain
		deploymentCreated = attached.Created
		if opts.DeploymentManager != nil {
			samplerBackend = &TinkerSamplerBackend{
				DeployMgr:         opts.DeploymentManager,
				DeploymentID:      attached.Info.DeploymentID,
				BaseModel:         config.BaseModel,
				HotloadTimeout:    config.HotloadTimeout,
				LoraRank:          config.LoraRank,
				CompressionFormat: DefaultDeltaCompression,
			}
			if attached.ResetSnapshotChain {
				samplerBackend.ResetSnapshotChain()
			}
		}
	}

	var weightSyncer *WeightSyncer
	if opts.DeploymentManager != nil && deployment != nil {
		weightSyncer = NewWeightSyncer(WeightSyncerConfig{
			DeployMgr:      opts.DeploymentManager,
			DeploymentID:   deployment.DeploymentID,
			BaseModel:      config.BaseModel,
			HotloadTimeout: config.HotloadTimeout,
			LoraRank:       config.LoraRank,
		})
	}

	var referenceHandle *ManagedProvisionedHandle
	if referenceConfig != nil {
		referenceOpts := opts
		referenceOpts.Config = *referenceConfig
		referenceHandle, err = ProvisionManagedHandle(ctx, referenceOpts)
		if err != nil {
			return nil, err
		}
	}

	metadata := ManagedHandleMetadata{
		TrainerJobID:     trainerEndpoint.JobID,
		MaxContextLength: cloneIntPointer(maxContextLength),
		DeploymentShape:  deploymentShape,
		TrainingProfile:  profile,
	}
	if deployment != nil {
		metadata.DeploymentID = deployment.DeploymentID
	}
	cleanupPlan, err := PlanManagedHandleCleanup(ManagedHandleCleanupConfig{
		TrainerJobID:             trainerEndpoint.JobID,
		Deployment:               deployment,
		CleanupTrainerOnClose:    config.CleanupTrainerOnClose && startedTrainer.Created,
		CleanupDeploymentOnClose: cleanupDeploymentModeForCreated(config.CleanupDeploymentOnClose, deploymentCreated),
	})
	if err != nil {
		return nil, err
	}
	return &ManagedProvisionedHandle{
		Config:                     config,
		UserMetadata:               ResolveUserMetadata(nil, opts.UserMetadata),
		TrainerEndpoint:            trainerEndpoint,
		TrainingProfile:            profile,
		MaxContextLength:           cloneIntPointer(maxContextLength),
		DeploymentShape:            deploymentShape,
		Deployment:                 deployment,
		RequiresInitialSamplerSync: requiresInitialSync,
		SamplerBackend:             samplerBackend,
		WeightSyncer:               weightSyncer,
		ReferenceHandle:            referenceHandle,
		Metadata:                   metadata,
		CleanupPlan:                cleanupPlan,
		TrainerManager:             opts.Trainer,
		DeploymentManager:          opts.Deployment,
		ConcreteDeploymentManager:  opts.DeploymentManager,
		CleanupTrainerOnClose:      config.CleanupTrainerOnClose && startedTrainer.Created,
		CleanupDeploymentOnClose:   cleanupDeploymentModeForCreated(config.CleanupDeploymentOnClose, deploymentCreated),
	}, nil
}

func BuildManagedTrainerJobConfig(config FiretitanProvisioningConfig, maxContextLength *int, profile *TrainingShapeProfile) TrainerJobConfig {
	trainingShapeRef := ""
	if profile != nil {
		trainingShapeRef = profile.TrainingShapeVersion
	}
	return TrainerJobConfig{
		BaseModel:                 config.BaseModel,
		LoraRank:                  config.LoraRank,
		MaxContextLength:          cloneIntPointer(maxContextLength),
		LearningRate:              config.LearningRate,
		GradientAccumulationSteps: intPointer(config.GradientAccumulationSteps),
		NodeCount:                 cloneIntPointer(config.NodeCount),
		TrainerReplicaCount:       cloneIntPointer(config.TrainerReplicaCount),
		DisplayName:               config.DisplayName,
		Region:                    config.Region,
		CustomImageTag:            config.CustomImageTag,
		ExtraArgs:                 append([]string(nil), config.ExtraArgs...),
		AcceleratorType:           config.AcceleratorType,
		AcceleratorCount:          cloneIntPointer(config.AcceleratorCount),
		TrainingShapeRef:          trainingShapeRef,
		ForwardOnly:               config.ForwardOnly,
		SkipValidations:           config.SkipValidations,
		Purpose:                   config.Purpose,
		ManagedBy:                 config.ManagedBy,
	}
}

func provisionManagedTrainer(ctx context.Context, trainer ManagedTrainerController, config FiretitanProvisioningConfig, maxContextLength *int, profile *TrainingShapeProfile, explicitTrainerJobID bool, opts ManagedProvisionOptions) (startedManagedTrainer, error) {
	if trainer == nil {
		return startedManagedTrainer{}, fmt.Errorf("managed provisioning requires Trainer")
	}
	if explicitTrainerJobID {
		if opts.ReconnectExistingJob {
			endpoint, err := trainer.ReconnectAndWait(ctx, config.TrainerJobID, opts.ReconnectOptions)
			return startedManagedTrainer{Endpoint: endpoint, Created: false}, err
		}
		accountID, err := trainer.AccountID(ctx)
		if err != nil {
			return startedManagedTrainer{}, err
		}
		if tryGetter, ok := trainer.(ManagedTrainerTryGetter); ok {
			job, found, err := tryGetter.TryGetJob(ctx, config.TrainerJobID)
			if err != nil {
				return startedManagedTrainer{}, err
			}
			if !found {
				trainerConfig := BuildManagedTrainerJobConfig(config, maxContextLength, profile)
				trainerConfig.RequestedJobID = config.TrainerJobID
				created, err := trainer.Create(ctx, trainerConfig)
				if err != nil {
					return startedManagedTrainer{}, err
				}
				endpoint, err := waitForStartedTrainer(ctx, trainer, created, config, opts)
				return startedManagedTrainer{Endpoint: endpoint, Created: true}, err
			}
			if resumableTrainerStates[stringFromAny(job["state"])] {
				endpoint, err := trainer.ReconnectAndWait(ctx, config.TrainerJobID, opts.ReconnectOptions)
				return startedManagedTrainer{Endpoint: endpoint, Created: false}, err
			}
		}
		jobName := "accounts/" + accountID + "/rlorTrainerJobs/" + config.TrainerJobID
		endpoint, err := trainer.WaitForReady(ctx, config.TrainerJobID, jobName, opts.TrainerPollOptions)
		return startedManagedTrainer{Endpoint: endpoint, Created: false}, err
	}
	trainerConfig := BuildManagedTrainerJobConfig(config, maxContextLength, profile)
	trainerConfig.RequestedJobID = config.TrainerJobID
	created, err := trainer.Create(ctx, trainerConfig)
	if err != nil {
		return startedManagedTrainer{}, err
	}
	endpoint, err := waitForStartedTrainer(ctx, trainer, created, config, opts)
	return startedManagedTrainer{Endpoint: endpoint, Created: true}, err
}

func attachManagedDeployment(ctx context.Context, deployment ManagedDeploymentController, config FiretitanProvisioningConfig, trainerJobName, deploymentShape string, now func() time.Time, explicitDeploymentID bool, opts ManagedProvisionOptions) (managedDeploymentAttachment, error) {
	if deployment == nil {
		return managedDeploymentAttachment{}, fmt.Errorf("managed deployment provisioning requires Deployment")
	}
	var existing *DeploymentInfo
	if explicitDeploymentID {
		info, ok, err := deployment.GetInfo(ctx, config.DeploymentID)
		if err != nil {
			return managedDeploymentAttachment{}, err
		}
		if ok {
			existing = &info
		}
	}
	unixSeconds := now().Unix()
	plan, err := PlanManagedDeploymentAttachment(config, trainerJobName, deploymentShape, existing, unixSeconds)
	if err != nil {
		return managedDeploymentAttachment{}, err
	}
	var info DeploymentInfo
	switch plan.Action {
	case ManagedDeploymentActionReuse:
		info = *existing
	case ManagedDeploymentActionReattach:
		info, err = deployment.ReattachTrainer(ctx, *existing, config.BaseModel, trainerJobName, opts.ReattachOptions)
	case ManagedDeploymentActionCreate:
		info, err = deployment.CreateOrGet(ctx, *plan.CreateConfig, false)
	default:
		err = fmt.Errorf("unsupported managed deployment action %q", plan.Action)
	}
	if err != nil {
		return managedDeploymentAttachment{}, err
	}
	if plan.WaitForReady && !DeploymentServingStates[info.State] {
		info, err = deployment.WaitForReady(ctx, plan.DeploymentID, opts.DeploymentWait)
		if err != nil {
			return managedDeploymentAttachment{}, err
		}
	}
	return managedDeploymentAttachment{Info: info, ResetSnapshotChain: plan.ResetSnapshotChain, Created: plan.Created}, nil
}

func cleanupDeploymentModeForCreated(mode string, created bool) string {
	if !created {
		return ""
	}
	return mode
}

var resumableTrainerStates = map[string]bool{
	"JOB_STATE_FAILED":    true,
	"JOB_STATE_CANCELLED": true,
	"JOB_STATE_PAUSED":    true,
	"JOB_STATE_COMPLETED": true,
}

func waitForStartedTrainer(ctx context.Context, trainer ManagedTrainerController, created CreatedTrainerJob, config FiretitanProvisioningConfig, opts ManagedProvisionOptions) (TrainerServiceEndpoint, error) {
	if tryGetter, ok := trainer.(ManagedTrainerTryGetter); ok {
		job, found, err := tryGetter.TryGetJob(ctx, created.JobID)
		if err != nil {
			return TrainerServiceEndpoint{}, err
		}
		if found && resumableTrainerStates[stringFromAny(job["state"])] {
			return trainer.ReconnectAndWait(ctx, created.JobID, opts.ReconnectOptions)
		}
	}
	return trainer.WaitForReady(ctx, created.JobID, created.JobName, opts.TrainerPollOptions)
}

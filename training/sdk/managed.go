package sdk

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	DeploymentStateFailed   = "FAILED"
	DeploymentStateDeleted  = "DELETED"
	DeploymentStateDeleting = "DELETING"
	DeploymentStateReady    = "READY"
	DeploymentStateUpdating = "UPDATING"

	PolicyTrainerMode   = "POLICY_TRAINER"
	ForwardOnlyMode     = "FORWARD_ONLY"
	LoraTrainerMode     = "LORA_TRAINER"
	DefaultLearningRate = 1e-5
	DefaultLoraAlpha    = 32
)

var DeploymentTerminalStates = map[string]bool{
	DeploymentStateFailed:   true,
	DeploymentStateDeleted:  true,
	DeploymentStateDeleting: true,
}

var DeploymentServingStates = map[string]bool{
	DeploymentStateReady:    true,
	DeploymentStateUpdating: true,
}

type FiretitanProvisioningConfig struct {
	BaseModel       string
	TokenizerModel  string
	LoraRank        int
	LoraAlpha       *int
	TrainingShapeID string

	ReferenceTrainingShapeID       string
	ReferenceTrainerJobID          string
	CleanupReferenceTrainerOnClose *bool
	ReferenceRequired              bool

	DeploymentShape  string
	TrainerJobID     string
	DeploymentID     string
	CreateDeployment *bool
	ForwardOnly      bool

	Region                    string
	DeploymentRegion          string
	MaxContextLength          *int
	LearningRate              float64
	GradientAccumulationSteps int
	Seed                      *int
	TrainMLP                  *bool
	TrainAttn                 *bool
	TrainUnembed              *bool
	NodeCount                 *int
	AcceleratorType           string
	AcceleratorCount          *int
	CustomImageTag            string
	ExtraArgs                 []string
	DeploymentExtraArgs       []string
	DeploymentExtraValues     map[string]string
	TrainerReplicaCount       *int
	ReplicaCount              *int

	TrainerTimeout        time.Duration
	DeploymentTimeout     time.Duration
	HotloadTimeout        time.Duration
	ReattachSettleTimeout time.Duration
	ReattachPollInterval  time.Duration

	CleanupTrainerOnClose      bool
	CleanupDeploymentOnClose   string
	DisplayName                string
	Purpose                    string
	ManagedBy                  string
	SkipValidations            bool
	DisableSpeculativeDecoding bool
}

type ManagedDeploymentShapeResolver interface {
	GetDeploymentShapeVersion(context.Context, string) (map[string]any, error)
}

func (c FiretitanProvisioningConfig) Normalize() (FiretitanProvisioningConfig, error) {
	out := c
	if out.CreateDeployment == nil {
		out.CreateDeployment = boolPointer(true)
	}
	out.DeploymentRegion = ""

	if out.CleanupReferenceTrainerOnClose == nil {
		out.CleanupReferenceTrainerOnClose = boolPointer(true)
	}
	if out.TrainMLP == nil {
		out.TrainMLP = boolPointer(true)
	}
	if out.TrainAttn == nil {
		out.TrainAttn = boolPointer(true)
	}
	if out.TrainUnembed == nil {
		out.TrainUnembed = boolPointer(true)
	}
	if out.ReplicaCount == nil || *out.ReplicaCount == 0 {
		out.ReplicaCount = intPointer(1)
	}
	if out.HotloadTimeout == 0 {
		out.HotloadTimeout = HotloadTimeout
	}
	if out.TrainerTimeout == 0 {
		out.TrainerTimeout = DefaultTrainerTimeout
	}
	if out.DeploymentTimeout == 0 {
		out.DeploymentTimeout = DeploymentReadyTimeout
	}
	if out.ReattachSettleTimeout == 0 {
		out.ReattachSettleTimeout = ReattachSettleTimeout
	}
	if out.ReattachPollInterval == 0 {
		out.ReattachPollInterval = PollInterval
	}
	if out.LearningRate == 0 {
		out.LearningRate = DefaultLearningRate
	}
	if out.GradientAccumulationSteps == 0 {
		out.GradientAccumulationSteps = 1
	}

	out.AcceleratorType = ""
	out.AcceleratorCount = nil
	out.ExtraArgs = append([]string(nil), out.ExtraArgs...)
	out.DeploymentExtraArgs = append([]string(nil), out.DeploymentExtraArgs...)
	out.DeploymentExtraValues = cloneStringMap(out.DeploymentExtraValues)
	return out, nil
}

func UseSharedBaseReference(config FiretitanProvisioningConfig, policyLoraRank int) bool {
	return config.ReferenceTrainingShapeID == "" && config.ReferenceTrainerJobID == "" && policyLoraRank > 0
}

func ShouldProvisionReference(config FiretitanProvisioningConfig) bool {
	return config.ReferenceRequired && !config.ForwardOnly && !UseSharedBaseReference(config, config.LoraRank)
}

func ReferenceManagedConfig(config FiretitanProvisioningConfig, policyLoraRank int) (FiretitanProvisioningConfig, error) {
	if config.ReferenceTrainingShapeID == "" && config.ReferenceTrainerJobID == "" {
		return FiretitanProvisioningConfig{}, fmt.Errorf(
			"cannot provision a separate reference trainer without reference_training_shape_id or reference_trainer_job_id",
		)
	}
	out := config
	out.TrainingShapeID = config.ReferenceTrainingShapeID
	if config.ReferenceTrainingShapeID != "" && policyLoraRank > 0 {
		out.LoraRank = policyLoraRank
	} else {
		out.LoraRank = 0
	}
	out.TrainerJobID = config.ReferenceTrainerJobID
	out.DeploymentID = ""
	out.CreateDeployment = boolPointer(false)
	out.ForwardOnly = true
	out.ReferenceRequired = false
	out.TrainerReplicaCount = nil
	cleanupReference := true
	if config.CleanupReferenceTrainerOnClose != nil {
		cleanupReference = *config.CleanupReferenceTrainerOnClose
	}
	out.CleanupTrainerOnClose = cleanupReference
	return out, nil
}

func ExpectedReferenceTrainerMode(config FiretitanProvisioningConfig) string {
	return LoraTrainerMode
}

func ValidateReferenceTrainingShape(config FiretitanProvisioningConfig, profile TrainingShapeProfile) error {
	if config.TrainingShapeID == "" {
		return nil
	}
	actual := profile.TrainerMode
	if actual == "" {
		actual = PolicyTrainerMode
	}
	allowed := AllowedReferenceTrainerModes(config.LoraRank)
	if allowed[actual] {
		return nil
	}
	allowedLabels := make([]string, 0, len(allowed))
	for mode := range allowed {
		allowedLabels = append(allowedLabels, mode)
	}
	sort.Strings(allowedLabels)
	return fmt.Errorf(
		"reference_training_shape_id='%s' resolves to trainer_mode='%s', but this run requires trainer_mode in {%s} (preferred '%s'; lora_rank=%d, forward_only=%t). Use a training shape validated for the reference trainer mode",
		config.TrainingShapeID,
		actual,
		strings.Join(allowedLabels, ", "),
		ExpectedReferenceTrainerMode(config),
		config.LoraRank,
		config.ForwardOnly,
	)
}

func AllowedReferenceTrainerModes(loraRank int) map[string]bool {
	if loraRank > 0 {
		return map[string]bool{LoraTrainerMode: true}
	}
	return map[string]bool{
		LoraTrainerMode: true,
		ForwardOnlyMode: true,
	}
}

func DeploymentShapeConflict(requested, existingVersion string) bool {
	if requested == "" || existingVersion == "" {
		return false
	}
	return stripDeploymentShapeVersion(requested) != stripDeploymentShapeVersion(existingVersion)
}

func DefaultDeploymentID(baseModel string) string {
	return DefaultDeploymentIDAt(baseModel, time.Now().Unix())
}

func DefaultDeploymentIDAt(baseModel string, unixSeconds int64) string {
	short := strings.ToLower(strings.TrimRight(baseModel, "/"))
	if i := strings.LastIndex(short, "/"); i >= 0 {
		short = short[i+1:]
	}
	var b strings.Builder
	for _, r := range short {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	safe := strings.Trim(b.String(), "-")
	if safe == "" {
		safe = "model"
	}
	return fmt.Sprintf("%s-%d", safe, unixSeconds)
}

type TinkerSamplerBackend struct {
	DeployMgr         *DeploymentManager
	DeploymentID      string
	BaseModel         string
	HotloadTimeout    time.Duration
	ResetPromptCache  *bool
	LoraRank          int
	CompressionFormat string

	HotloadAndWait func(context.Context, string, string, string, ...HotloadAndWaitOptions) (bool, error)

	snapshotTypes map[string]string
	baseIdentity  string
}

func (b *TinkerSamplerBackend) RememberSavedSnapshot(modelPath, checkpointType string) {
	if checkpointType == "" {
		return
	}
	if b.snapshotTypes == nil {
		b.snapshotTypes = map[string]string{}
	}
	b.snapshotTypes[modelPath] = strings.ToLower(checkpointType)
}

func (b *TinkerSamplerBackend) ResetSnapshotChain() {
	b.snapshotTypes = map[string]string{}
	b.baseIdentity = ""
}

func (b *TinkerSamplerBackend) HotloadSavedSnapshot(ctx context.Context, modelPath string) (bool, error) {
	if b.CompressionFormat == "" {
		b.CompressionFormat = DefaultDeltaCompression
	}
	timeout := b.HotloadTimeout
	if timeout == 0 {
		timeout = HotloadTimeout
	}
	resetPromptCache := true
	if b.ResetPromptCache != nil {
		resetPromptCache = *b.ResetPromptCache
	}
	checkpointType := ""
	if b.snapshotTypes != nil {
		checkpointType = b.snapshotTypes[modelPath]
	}
	incremental := BuildIncrementalMetadata(b.LoraRank, checkpointType, b.baseIdentity, b.CompressionFormat)
	hotload := b.HotloadAndWait
	if hotload == nil {
		if b.DeployMgr == nil {
			return false, fmt.Errorf("TinkerSamplerBackend requires DeployMgr or HotloadAndWait")
		}
		hotload = b.DeployMgr.HotloadAndWait
	}
	ok, err := hotload(ctx, b.DeploymentID, b.BaseModel, modelPath, HotloadAndWaitOptions{
		IncrementalSnapshotMetadata: incremental,
		ResetPromptCache:            &resetPromptCache,
		RequestTimeout:              timeout,
		Path:                        "",
		Wait: HotloadWaitOptions{
			Timeout: timeout,
		},
	})
	if err != nil {
		return false, err
	}
	if ok {
		b.baseIdentity = modelPath
	}
	return ok, nil
}

func (b *TinkerSamplerBackend) DeploymentModel(ctx context.Context) (string, error) {
	if b.DeployMgr == nil {
		return "", fmt.Errorf("TinkerSamplerBackend requires DeployMgr")
	}
	accountID, err := b.DeployMgr.AccountID(ctx)
	if err != nil {
		return "", err
	}
	return "accounts/" + accountID + "/deployments/" + b.DeploymentID, nil
}

func (b *TinkerSamplerBackend) GetSamplingClient(ctx context.Context, tokenizer DeploymentTokenizer, controller SamplingConcurrencyController) (*FiretitanSamplingClient, error) {
	if b.DeployMgr == nil {
		return nil, fmt.Errorf("TinkerSamplerBackend requires DeployMgr")
	}
	model, err := b.DeploymentModel(ctx)
	if err != nil {
		return nil, err
	}
	return NewFiretitanSamplingClient(NewDeploymentSampler(
		b.DeployMgr.InferenceURL,
		model,
		b.DeployMgr.APIKey(),
		WithDeploymentSamplerTokenizer(tokenizer),
		WithDeploymentSamplerConcurrencyController(controller),
	)), nil
}

func stripDeploymentShapeVersion(shape string) string {
	if idx := strings.Index(shape, "/versions/"); idx >= 0 {
		return shape[:idx]
	}
	return shape
}

func boolPointer(v bool) *bool {
	return &v
}

func intPointer(v int) *int {
	return &v
}

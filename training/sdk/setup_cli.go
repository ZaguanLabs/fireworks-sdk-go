package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	DefaultSetupBaseModel        = "accounts/fireworks/models/kimi-k2p5"
	DefaultSetupRegion           = "US_OHIO_1"
	DefaultSetupTrainerOutput    = "trainer_info.json"
	DefaultSetupDeploymentOutput = "deployment_info.json"
)

var DefaultSetupTrainerExtraArgs = []string{"--forward-only"}

type SetupTrainerManager interface {
	CreateAndWait(context.Context, TrainerJobConfig, ...TrainerPollOptions) (TrainerServiceEndpoint, error)
}

type SetupDeploymentManager interface {
	CreateOrGet(context.Context, DeploymentConfig, bool) (DeploymentInfo, error)
	WaitForReady(context.Context, string, ...DeploymentWaitOptions) (DeploymentInfo, error)
}

type SetupTrainerOptions struct {
	DisplayName       string
	BaseModel         string
	CustomImageTag    string
	Region            string
	NodeCount         int
	ExtraArgs         []string
	Timeout           time.Duration
	PollInterval      time.Duration
	LoraRank          int
	MaxSeqLen         int
	AcceleratorType   string
	AcceleratorCount  *int
	FireworksAPIKey   string
	FireworksBaseURL  string
	AdditionalHeaders map[string]string
	OutputFile        string
	Manager           SetupTrainerManager
	Now               func() time.Time
}

type SetupTrainerOutput struct {
	JobID       string   `json:"job_id"`
	JobName     string   `json:"job_name"`
	BaseURL     string   `json:"base_url"`
	DisplayName string   `json:"display_name"`
	ExtraArgs   []string `json:"extra_args"`
	ElapsedS    float64  `json:"elapsed_s"`
}

type SetupDeploymentOptions struct {
	DeploymentID        string
	DeploymentShape     string
	BaseModel           string
	Region              string
	Timeout             time.Duration
	FireworksAPIKey     string
	FireworksBaseURL    string
	AdditionalHeaders   map[string]string
	HotloadAPIURL       string
	OutputFile          string
	SkipShapeValidation bool
	AcceleratorType     string
	Manager             SetupDeploymentManager
	Now                 func() time.Time
}

type SetupDeploymentOutput struct {
	DeploymentID   string  `json:"deployment_id"`
	State          string  `json:"state"`
	InferenceModel string  `json:"inference_model"`
	ElapsedS       float64 `json:"elapsed_s"`
}

func ParseAdditionalHeadersJSON(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return nil, err
	}
	return headers, nil
}

func SplitSetupExtraArgs(raw string) []string {
	return strings.Fields(raw)
}

func SetupTrainer(ctx context.Context, opts SetupTrainerOptions) (SetupTrainerOutput, error) {
	opts = normalizeSetupTrainerOptions(opts)
	if opts.DisplayName == "" {
		return SetupTrainerOutput{}, fmt.Errorf("display name is required")
	}
	manager := opts.Manager
	if manager == nil {
		if opts.FireworksAPIKey == "" {
			return SetupTrainerOutput{}, fmt.Errorf("Set FIREWORKS_API_KEY")
		}
		manager = NewTrainerJobManager(
			opts.FireworksAPIKey,
			opts.FireworksBaseURL,
			WithTrainingAdditionalHeaders(opts.AdditionalHeaders),
		)
	}
	gradientAccumulationSteps := 1
	nodeCount := opts.NodeCount
	maxSeqLen := opts.MaxSeqLen
	config := TrainerJobConfig{
		BaseModel:                 opts.BaseModel,
		LoraRank:                  opts.LoraRank,
		MaxContextLength:          &maxSeqLen,
		LearningRate:              DefaultLearningRate,
		GradientAccumulationSteps: &gradientAccumulationSteps,
		NodeCount:                 &nodeCount,
		DisplayName:               opts.DisplayName,
		Region:                    opts.Region,
		CustomImageTag:            opts.CustomImageTag,
		ExtraArgs:                 append([]string(nil), opts.ExtraArgs...),
		AcceleratorType:           opts.AcceleratorType,
		AcceleratorCount:          cloneIntPointer(opts.AcceleratorCount),
	}
	start := opts.Now()
	endpoint, err := manager.CreateAndWait(ctx, config, TrainerPollOptions{
		PollInterval: opts.PollInterval,
		Timeout:      opts.Timeout,
	})
	if err != nil {
		return SetupTrainerOutput{}, err
	}
	output := SetupTrainerOutput{
		JobID:       endpoint.JobID,
		JobName:     endpoint.JobName,
		BaseURL:     endpoint.BaseURL,
		DisplayName: opts.DisplayName,
		ExtraArgs:   append([]string(nil), opts.ExtraArgs...),
		ElapsedS:    opts.Now().Sub(start).Seconds(),
	}
	if opts.OutputFile != "" {
		if err := writeIndentedJSONFile(opts.OutputFile, output); err != nil {
			return SetupTrainerOutput{}, err
		}
	}
	return output, nil
}

func SetupDeployment(ctx context.Context, opts SetupDeploymentOptions) (SetupDeploymentOutput, error) {
	opts = normalizeSetupDeploymentOptions(opts)
	if opts.DeploymentID == "" {
		return SetupDeploymentOutput{}, fmt.Errorf("deployment id is required")
	}
	if opts.DeploymentShape == "" {
		return SetupDeploymentOutput{}, fmt.Errorf("deployment shape is required")
	}
	manager := opts.Manager
	if manager == nil {
		if opts.FireworksAPIKey == "" {
			return SetupDeploymentOutput{}, fmt.Errorf("Set FIREWORKS_API_KEY")
		}
		manager = NewDeploymentManager(
			opts.FireworksAPIKey,
			opts.FireworksBaseURL,
			WithDeploymentHotloadAPIURL(opts.HotloadAPIURL),
			WithDeploymentAdditionalHeaders(opts.AdditionalHeaders),
		)
	}
	config := DeploymentConfig{
		DeploymentID:        opts.DeploymentID,
		BaseModel:           opts.BaseModel,
		DeploymentShape:     opts.DeploymentShape,
		Region:              opts.Region,
		MinReplicaCount:     1,
		AcceleratorType:     opts.AcceleratorType,
		SkipShapeValidation: opts.SkipShapeValidation,
	}
	start := opts.Now()
	info, err := manager.CreateOrGet(ctx, config, false)
	if err != nil {
		return SetupDeploymentOutput{}, err
	}
	if info.State != DeploymentStateReady {
		info, err = manager.WaitForReady(ctx, opts.DeploymentID, DeploymentWaitOptions{Timeout: opts.Timeout})
		if err != nil {
			return SetupDeploymentOutput{}, err
		}
	}
	output := SetupDeploymentOutput{
		DeploymentID:   opts.DeploymentID,
		State:          info.State,
		InferenceModel: info.InferenceModel,
		ElapsedS:       opts.Now().Sub(start).Seconds(),
	}
	if opts.OutputFile != "" {
		if err := writeIndentedJSONFile(opts.OutputFile, output); err != nil {
			return SetupDeploymentOutput{}, err
		}
	}
	return output, nil
}

func normalizeSetupTrainerOptions(opts SetupTrainerOptions) SetupTrainerOptions {
	if opts.BaseModel == "" {
		opts.BaseModel = DefaultSetupBaseModel
	}
	if opts.Region == "" {
		opts.Region = DefaultSetupRegion
	}
	if opts.NodeCount == 0 {
		opts.NodeCount = 2
	}
	if opts.Timeout == 0 {
		opts.Timeout = 1200 * time.Second
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 5 * time.Second
	}
	if opts.MaxSeqLen == 0 {
		opts.MaxSeqLen = 4096
	}
	if opts.FireworksAPIKey == "" {
		opts.FireworksAPIKey = os.Getenv("FIREWORKS_API_KEY")
	}
	if opts.FireworksBaseURL == "" {
		opts.FireworksBaseURL = os.Getenv("FIREWORKS_BASE_URL")
	}
	if opts.OutputFile == "" {
		opts.OutputFile = DefaultSetupTrainerOutput
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ExtraArgs == nil {
		opts.ExtraArgs = append([]string(nil), DefaultSetupTrainerExtraArgs...)
	}
	opts.ExtraArgs = append([]string(nil), opts.ExtraArgs...)
	return opts
}

func normalizeSetupDeploymentOptions(opts SetupDeploymentOptions) SetupDeploymentOptions {
	if opts.BaseModel == "" {
		opts.BaseModel = DefaultSetupBaseModel
	}
	if opts.Region == "" {
		opts.Region = DefaultSetupRegion
	}
	if opts.Timeout == 0 {
		opts.Timeout = 1800 * time.Second
	}
	if opts.FireworksAPIKey == "" {
		opts.FireworksAPIKey = os.Getenv("FIREWORKS_API_KEY")
	}
	if opts.FireworksBaseURL == "" {
		opts.FireworksBaseURL = os.Getenv("FIREWORKS_BASE_URL")
	}
	if opts.HotloadAPIURL == "" {
		opts.HotloadAPIURL = DefaultFireworksAPIURL
	}
	if opts.OutputFile == "" {
		opts.OutputFile = DefaultSetupDeploymentOutput
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return opts
}

func writeIndentedJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

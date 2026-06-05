package sdk

import (
	"fmt"
	"strings"
)

type WeightsInfo struct {
	BaseModel    string
	IsLora       bool
	LoraRank     *int
	TrainUnembed *bool
	TrainMLP     *bool
	TrainAttn    *bool
}

type TrainingClientResumePlan struct {
	BaseModel    string
	LoraRank     int
	IsLora       bool
	TrainUnembed bool
	TrainMLP     bool
	TrainAttn    bool
}

func TrainingClientPlanFromWeightsInfo(info WeightsInfo) (TrainingClientResumePlan, error) {
	baseModel := strings.TrimSpace(info.BaseModel)
	if baseModel == "" {
		return TrainingClientResumePlan{}, fmt.Errorf("weights_info.base_model is required")
	}

	plan := TrainingClientResumePlan{
		BaseModel:    baseModel,
		TrainUnembed: true,
		TrainMLP:     true,
		TrainAttn:    true,
	}
	if !info.IsLora {
		return plan, nil
	}

	if info.LoraRank == nil {
		return TrainingClientResumePlan{}, fmt.Errorf("weights_info.lora_rank is required for LoRA checkpoints")
	}
	plan.IsLora = true
	plan.LoraRank = *info.LoraRank
	if info.TrainUnembed != nil {
		plan.TrainUnembed = *info.TrainUnembed
	}
	if info.TrainMLP != nil {
		plan.TrainMLP = *info.TrainMLP
	}
	if info.TrainAttn != nil {
		plan.TrainAttn = *info.TrainAttn
	}
	return plan, nil
}

func RejectServiceWeightsAccessToken(method string, weightsAccessToken *string) error {
	if weightsAccessToken == nil {
		return nil
	}
	return fmt.Errorf(
		"FiretitanServiceClient.%s(weights_access_token=...) is not supported. Load checkpoints that are accessible to the current Fireworks API key.",
		method,
	)
}

func RejectTrainingLoadWeightsAccessToken(method string, weightsAccessToken *string) error {
	if weightsAccessToken == nil {
		return nil
	}
	return fmt.Errorf(
		"FiretitanTrainingClient.%s(weights_access_token=...) is not supported. Load checkpoints that are accessible to the current Fireworks API key.",
		method,
	)
}

func ValidateSaveStateOptions(overwrite bool) error {
	if !overwrite {
		return nil
	}
	return fmt.Errorf("FiretitanTrainingClient.save_state(overwrite=True) is not supported. Use a new checkpoint name instead.")
}

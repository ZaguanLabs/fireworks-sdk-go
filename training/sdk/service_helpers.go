package sdk

import (
	"fmt"
	"strings"
	"time"
)

const LazyManagedTrainingRunID = "firetitan-managed"

const CreateSamplingClientRequiresManagedSamplerStateMessage = "create_sampling_client requires SDK-managed sampler state or deployment_sampler=.... Base-model/serverless sampling is not supported in this path."

type TrainingRunMetadata struct {
	TrainingRunID string
	BaseModel     string
	ModelOwner    string
	IsLora        bool
	LoraRank      *int
	LastRequest   time.Time
	UserMetadata  map[string]string
	TrainerJobID  string
	TrainUnembed  *bool
	TrainMLP      *bool
	TrainAttn     *bool
}

type Cursor struct {
	Offset     int
	Limit      int
	TotalCount int
}

type ManagedSamplerSyncState struct {
	RequiresInitialSamplerSync bool
}

func ResolveUserMetadata(defaultUserMetadata, userMetadata map[string]string) map[string]string {
	if userMetadata != nil {
		return cloneStringMap(userMetadata)
	}
	return cloneStringMap(defaultUserMetadata)
}

func ModelOwnerFromBaseModel(baseModel string) string {
	parts := strings.Split(baseModel, "/")
	if len(parts) >= 2 && parts[0] == "accounts" && parts[1] != "" {
		return parts[1]
	}
	return "fireworks"
}

func TrainingRunIDFromManagedConfig(config FiretitanProvisioningConfig) string {
	if config.TrainerJobID != "" {
		return config.TrainerJobID
	}
	return LazyManagedTrainingRunID
}

func TrainingRunMetadataFromManagedConfig(config FiretitanProvisioningConfig, userMetadata map[string]string, now time.Time) TrainingRunMetadata {
	isLora := config.LoraRank > 0
	var loraRank *int
	if isLora {
		loraRank = intPointer(config.LoraRank)
	}
	return TrainingRunMetadata{
		TrainingRunID: TrainingRunIDFromManagedConfig(config),
		BaseModel:     config.BaseModel,
		ModelOwner:    ModelOwnerFromBaseModel(config.BaseModel),
		IsLora:        isLora,
		LoraRank:      loraRank,
		LastRequest:   now,
		UserMetadata:  cloneStringMap(userMetadata),
		TrainerJobID:  config.TrainerJobID,
		TrainUnembed:  cloneBoolPointer(config.TrainUnembed),
		TrainMLP:      cloneBoolPointer(config.TrainMLP),
		TrainAttn:     cloneBoolPointer(config.TrainAttn),
	}
}

func WeightsInfoFromManagedConfig(config FiretitanProvisioningConfig) WeightsInfo {
	info := WeightsInfo{
		BaseModel:    config.BaseModel,
		IsLora:       config.LoraRank > 0,
		TrainUnembed: cloneBoolPointer(config.TrainUnembed),
		TrainMLP:     cloneBoolPointer(config.TrainMLP),
		TrainAttn:    cloneBoolPointer(config.TrainAttn),
	}
	if info.IsLora {
		info.LoraRank = intPointer(config.LoraRank)
	}
	return info
}

func EmptyCursor(limit, offset int) Cursor {
	return Cursor{Offset: offset, Limit: limit, TotalCount: 0}
}

func ControlPlaneCheckpointClientError() error {
	return fmt.Errorf(
		"Control-plane checkpoint operations require a provisioned trainer. Call create_training_client() before listing or promoting checkpoints.",
	)
}

func ReferenceClientJobID(referenceJobID, trainerJobID string) string {
	if referenceJobID != "" {
		return referenceJobID
	}
	return trainerJobID
}

func LazyManagedRestUnsupportedError(method string) error {
	return fmt.Errorf(
		"FireTitan lazy managed REST client does not support %s. Create a trainer-backed service client or use Fireworks checkpoint APIs for this operation.",
		method,
	)
}

func (s *ManagedSamplerSyncState) RequiresInitialSync() bool {
	return s != nil && s.RequiresInitialSamplerSync
}

func (s *ManagedSamplerSyncState) MarkSamplerHotloaded() {
	if s != nil {
		s.RequiresInitialSamplerSync = false
	}
}

func CreateSamplingClientUnsupportedError() error {
	return fmt.Errorf("%s", CreateSamplingClientRequiresManagedSamplerStateMessage)
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	return boolPointer(*value)
}

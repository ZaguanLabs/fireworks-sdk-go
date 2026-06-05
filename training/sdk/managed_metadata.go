package sdk

import "fmt"

type ManagedHandleMetadata struct {
	TrainerJobID     string
	DeploymentID     string
	MaxContextLength *int
	DeploymentShape  string
	TrainingProfile  *TrainingShapeProfile
}

type ManagedResolvedMetadata struct {
	TrainerJobID     string
	DeploymentID     string
	MaxContextLength *int
	DeploymentShape  string
	TrainingProfile  *TrainingShapeProfile
	AcceleratorType  string
	AcceleratorCount *int
}

func ResolveManagedMetadata(config *FiretitanProvisioningConfig, handle ManagedHandleMetadata) ManagedResolvedMetadata {
	out := ManagedResolvedMetadata{
		TrainerJobID:     handle.TrainerJobID,
		DeploymentID:     handle.DeploymentID,
		MaxContextLength: cloneIntPointer(handle.MaxContextLength),
		DeploymentShape:  handle.DeploymentShape,
		TrainingProfile:  handle.TrainingProfile,
	}
	if config != nil {
		if out.TrainerJobID == "" {
			out.TrainerJobID = config.TrainerJobID
		}
		if out.DeploymentID == "" {
			out.DeploymentID = config.DeploymentID
		}
		if out.MaxContextLength == nil {
			out.MaxContextLength = cloneIntPointer(config.MaxContextLength)
		}
		if out.DeploymentShape == "" {
			out.DeploymentShape = config.DeploymentShape
		}
		if config.AcceleratorType != "" {
			out.AcceleratorType = config.AcceleratorType
		}
		if config.AcceleratorCount != nil {
			out.AcceleratorCount = cloneIntPointer(config.AcceleratorCount)
		}
	}
	if out.TrainingProfile != nil {
		if out.AcceleratorType == "" {
			out.AcceleratorType = out.TrainingProfile.AcceleratorType
		}
		if out.AcceleratorCount == nil && out.TrainingProfile.AcceleratorCount != 0 {
			out.AcceleratorCount = intPointer(out.TrainingProfile.AcceleratorCount)
		}
	}
	return out
}

func RequireManagedString(value, label string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("managed %s is unavailable; create a managed training client before reading resolved metadata", label)
	}
	return value, nil
}

func RequireManagedInt(value *int, label string) (int, error) {
	if value == nil {
		return 0, fmt.Errorf("managed %s is unavailable; create a managed training client before reading resolved metadata", label)
	}
	return *value, nil
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	return intPointer(*value)
}

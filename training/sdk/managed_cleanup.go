package sdk

import "fmt"

type ManagedCleanupOperation string

const (
	ManagedCleanupFlushTelemetry      ManagedCleanupOperation = "flush_telemetry"
	ManagedCleanupDrainTelemetry      ManagedCleanupOperation = "drain_telemetry"
	ManagedCleanupStopTelemetry       ManagedCleanupOperation = "stop_telemetry"
	ManagedCleanupStopTrainerHolder   ManagedCleanupOperation = "stop_trainer_holder"
	ManagedCleanupScaleDeploymentZero ManagedCleanupOperation = "scale_deployment_to_zero"
	ManagedCleanupDeleteDeployment    ManagedCleanupOperation = "delete_deployment"
	ManagedCleanupDeleteTrainer       ManagedCleanupOperation = "delete_trainer"
)

const (
	CleanupDeploymentDelete      = string(CleanupDeploymentOnCloseDelete)
	CleanupDeploymentScaleToZero = string(CleanupDeploymentOnCloseScaleToZero)
)

type ManagedHandleCleanupConfig struct {
	TrainerJobID             string
	Deployment               *DeploymentInfo
	HasTrainerHolder         bool
	CleanupTrainerOnClose    bool
	CleanupDeploymentOnClose string
}

type ManagedCleanupStep struct {
	Operation    ManagedCleanupOperation
	TrainerJobID string
	DeploymentID string
}

func PlanManagedHandleCleanup(config ManagedHandleCleanupConfig) ([]ManagedCleanupStep, error) {
	var steps []ManagedCleanupStep
	if config.HasTrainerHolder {
		steps = append(steps,
			ManagedCleanupStep{Operation: ManagedCleanupFlushTelemetry, TrainerJobID: config.TrainerJobID},
			ManagedCleanupStep{Operation: ManagedCleanupDrainTelemetry, TrainerJobID: config.TrainerJobID},
			ManagedCleanupStep{Operation: ManagedCleanupStopTelemetry, TrainerJobID: config.TrainerJobID},
			ManagedCleanupStep{Operation: ManagedCleanupStopTrainerHolder, TrainerJobID: config.TrainerJobID},
		)
	}

	if config.CleanupDeploymentOnClose != "" && config.Deployment != nil {
		switch config.CleanupDeploymentOnClose {
		case CleanupDeploymentScaleToZero:
			steps = append(steps, ManagedCleanupStep{
				Operation:    ManagedCleanupScaleDeploymentZero,
				DeploymentID: config.Deployment.DeploymentID,
			})
		case CleanupDeploymentDelete:
			steps = append(steps, ManagedCleanupStep{
				Operation:    ManagedCleanupDeleteDeployment,
				DeploymentID: config.Deployment.DeploymentID,
			})
		default:
			return nil, fmt.Errorf("cleanup_deployment_on_close must be empty, %q, or %q", CleanupDeploymentDelete, CleanupDeploymentScaleToZero)
		}
	}

	if config.CleanupTrainerOnClose && config.TrainerJobID != "" {
		steps = append(steps, ManagedCleanupStep{
			Operation:    ManagedCleanupDeleteTrainer,
			TrainerJobID: config.TrainerJobID,
		})
	}
	return steps, nil
}

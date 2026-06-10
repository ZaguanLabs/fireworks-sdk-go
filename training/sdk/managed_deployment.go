package sdk

import "fmt"

type ManagedDeploymentAction string

const (
	ManagedDeploymentActionCreate   ManagedDeploymentAction = "create"
	ManagedDeploymentActionReattach ManagedDeploymentAction = "reattach"
	ManagedDeploymentActionReuse    ManagedDeploymentAction = "reuse"
)

type ManagedDeploymentAttachPlan struct {
	DeploymentID       string
	Action             ManagedDeploymentAction
	Reattached         bool
	Created            bool
	ResetSnapshotChain bool
	WaitForReady       bool
	CreateConfig       *DeploymentConfig
}

func ShouldLookupManagedDeployment(config FiretitanProvisioningConfig) bool {
	return config.DeploymentID != ""
}

func ManagedDeploymentIDAt(config FiretitanProvisioningConfig, unixSeconds int64) string {
	if config.DeploymentID != "" {
		return config.DeploymentID
	}
	return DefaultDeploymentIDAt(config.BaseModel, unixSeconds)
}

func PlanManagedDeploymentAttachment(config FiretitanProvisioningConfig, trainerJobName, deploymentShape string, existing *DeploymentInfo, unixSeconds int64) (ManagedDeploymentAttachPlan, error) {
	deploymentID := ManagedDeploymentIDAt(config, unixSeconds)
	plan := ManagedDeploymentAttachPlan{DeploymentID: deploymentID}

	if ShouldLookupManagedDeployment(config) && existing != nil && !DeploymentTerminalStates[existing.State] {
		if DeploymentShapeConflict(deploymentShape, existing.DeploymentShapeVersion) {
			return ManagedDeploymentAttachPlan{}, fmt.Errorf(
				"reattach target deployment %q serves shape %q, which does not match the requested deployment_shape %q. Delete the existing deployment or request its shape; a reattach must not silently serve a different shape",
				deploymentID,
				existing.DeploymentShapeVersion,
				deploymentShape,
			)
		}
		plan.WaitForReady = !DeploymentServingStates[existing.State]
		if DeploymentHotLoadTrainerJob(*existing) == trainerJobName {
			plan.Action = ManagedDeploymentActionReuse
			return plan, nil
		}
		plan.Action = ManagedDeploymentActionReattach
		plan.Reattached = true
		plan.ResetSnapshotChain = true
		return plan, nil
	}

	createConfig, err := ManagedDeploymentCreateConfig(config, trainerJobName, deploymentShape, deploymentID)
	if err != nil {
		return ManagedDeploymentAttachPlan{}, err
	}
	plan.Action = ManagedDeploymentActionCreate
	plan.Created = true
	plan.CreateConfig = &createConfig
	plan.WaitForReady = true
	return plan, nil
}

func ManagedDeploymentCreateConfig(config FiretitanProvisioningConfig, trainerJobName, deploymentShape, deploymentID string) (DeploymentConfig, error) {
	if deploymentID == "" {
		deploymentID = config.DeploymentID
	}
	if deploymentID == "" {
		return DeploymentConfig{}, fmt.Errorf("managed deployment requires a deployment ID")
	}
	if deploymentShape == "" && config.AcceleratorType == "" {
		return DeploymentConfig{}, fmt.Errorf(
			"cannot create a managed deployment without a deployment shape: the deployment accelerator is owned by the deployment shape. Provide a deployment_shape or a training_shape_id whose shape references one",
		)
	}
	replicaCount := 1
	if config.ReplicaCount != nil {
		replicaCount = *config.ReplicaCount
	}
	if replicaCount < 0 {
		replicaCount = 0
	}
	maxReplicaCount := replicaCount
	return DeploymentConfig{
		DeploymentID:               deploymentID,
		BaseModel:                  config.BaseModel,
		DeploymentShape:            deploymentShape,
		Region:                     config.Region,
		MinReplicaCount:            replicaCount,
		MaxReplicaCount:            &maxReplicaCount,
		AcceleratorType:            config.AcceleratorType,
		HotLoadTrainerJob:          trainerJobName,
		ForTraining:                true,
		SkipShapeValidation:        false,
		DisableSpeculativeDecoding: config.DisableSpeculativeDecoding,
		ExtraArgs:                  append([]string(nil), config.DeploymentExtraArgs...),
		ExtraValues:                cloneStringMap(config.DeploymentExtraValues),
		Annotations:                map[string]string{SDKManagedRolloutDeploymentAnnotation: "true"},
	}, nil
}

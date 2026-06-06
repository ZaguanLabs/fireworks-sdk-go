package sdk

import (
	"strings"
	"testing"
)

func TestShouldLookupManagedDeploymentOnlyWithExplicitID(t *testing.T) {
	if ShouldLookupManagedDeployment(FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"}) {
		t.Fatal("generated deployment IDs should not trigger an existing deployment lookup")
	}
	if !ShouldLookupManagedDeployment(FiretitanProvisioningConfig{DeploymentID: "dep-1"}) {
		t.Fatal("explicit deployment ID should trigger an existing deployment lookup")
	}
}

func TestManagedDeploymentIDAt(t *testing.T) {
	config := FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/Qwen3-1.7B"}
	if got := ManagedDeploymentIDAt(config, 123); got != "qwen3-1-7b-123" {
		t.Fatalf("generated id = %q", got)
	}
	config.DeploymentID = "dep-1"
	if got := ManagedDeploymentIDAt(config, 123); got != "dep-1" {
		t.Fatalf("explicit id = %q", got)
	}
}

func TestPlanManagedDeploymentAttachmentReattachesMovedDeployment(t *testing.T) {
	existing := &DeploymentInfo{
		DeploymentID:           "dep-1",
		State:                  DeploymentStateReady,
		HotLoadTrainerJob:      "accounts/acct/rlorTrainerJobs/old-job",
		DeploymentShapeVersion: "accounts/acct/deploymentShapes/rft/versions/1",
	}
	plan, err := PlanManagedDeploymentAttachment(
		FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base", DeploymentID: "dep-1"},
		"accounts/acct/rlorTrainerJobs/new-job",
		"accounts/acct/deploymentShapes/rft/versions/2",
		existing,
		123,
	)
	if err != nil {
		t.Fatalf("PlanManagedDeploymentAttachment() error = %v", err)
	}
	if plan.Action != ManagedDeploymentActionReattach || !plan.Reattached || !plan.ResetSnapshotChain {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.WaitForReady {
		t.Fatalf("ready deployment should not require wait: %#v", plan)
	}
}

func TestPlanManagedDeploymentAttachmentDoesNotReattachGeneratedID(t *testing.T) {
	existing := &DeploymentInfo{
		DeploymentID:      "qwen3-1-7b-123",
		State:             DeploymentStateReady,
		HotLoadTrainerJob: "accounts/acct/rlorTrainerJobs/old-job",
	}
	plan, err := PlanManagedDeploymentAttachment(
		FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/Qwen3-1.7B"},
		"accounts/acct/rlorTrainerJobs/new-job",
		"accounts/acct/deploymentShapes/rft/versions/1",
		existing,
		123,
	)
	if err != nil {
		t.Fatalf("PlanManagedDeploymentAttachment() error = %v", err)
	}
	if plan.Action != ManagedDeploymentActionCreate || plan.DeploymentID != "qwen3-1-7b-123" || plan.Reattached {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanManagedDeploymentAttachmentReusesAlreadyAttachedDeployment(t *testing.T) {
	existing := &DeploymentInfo{
		DeploymentID:      "dep-1",
		State:             DeploymentStateUpdating,
		HotLoadTrainerJob: "accounts/acct/rlorTrainerJobs/job-1",
	}
	plan, err := PlanManagedDeploymentAttachment(
		FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base", DeploymentID: "dep-1"},
		"accounts/acct/rlorTrainerJobs/job-1",
		"",
		existing,
		123,
	)
	if err != nil {
		t.Fatalf("PlanManagedDeploymentAttachment() error = %v", err)
	}
	if plan.Action != ManagedDeploymentActionReuse || plan.Reattached || plan.ResetSnapshotChain || plan.WaitForReady {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanManagedDeploymentAttachmentWaitsForNonServingReattach(t *testing.T) {
	existing := &DeploymentInfo{
		DeploymentID:      "dep-1",
		State:             "PROVISIONING",
		HotLoadTrainerJob: "accounts/acct/rlorTrainerJobs/old-job",
	}
	plan, err := PlanManagedDeploymentAttachment(
		FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base", DeploymentID: "dep-1"},
		"accounts/acct/rlorTrainerJobs/job-1",
		"",
		existing,
		123,
	)
	if err != nil {
		t.Fatalf("PlanManagedDeploymentAttachment() error = %v", err)
	}
	if plan.Action != ManagedDeploymentActionReattach || !plan.WaitForReady {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanManagedDeploymentAttachmentRejectsShapeMismatchBeforeMutation(t *testing.T) {
	existing := &DeploymentInfo{
		DeploymentID:           "dep-1",
		State:                  DeploymentStateReady,
		DeploymentShapeVersion: "accounts/acct/deploymentShapes/rft/versions/1",
	}
	_, err := PlanManagedDeploymentAttachment(
		FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base", DeploymentID: "dep-1"},
		"accounts/acct/rlorTrainerJobs/job-1",
		"accounts/acct/deploymentShapes/other/versions/1",
		existing,
		123,
	)
	if err == nil || !strings.Contains(err.Error(), "does not match the requested deployment_shape") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanManagedDeploymentAttachmentCreatesForTerminalDeployment(t *testing.T) {
	replicas := 2
	existing := &DeploymentInfo{DeploymentID: "dep-1", State: DeploymentStateFailed}
	plan, err := PlanManagedDeploymentAttachment(
		FiretitanProvisioningConfig{
			BaseModel:                "accounts/acct/models/base",
			DeploymentID:             "dep-1",
			Region:                   "US_OHIO_1",
			ReplicaCount:             &replicas,
			DeploymentExtraArgs:      []string{"--tp 2"},
			DeploymentExtraValues:    map[string]string{"k": "v"},
			SkipValidations:          true,
			AcceleratorType:          "NVIDIA_H200_141GB",
			DeploymentShape:          "ignored-by-explicit-arg",
			TrainerReplicaCount:      intPointer(4),
			CleanupTrainerOnClose:    true,
			CleanupDeploymentOnClose: "delete",
		},
		"accounts/acct/rlorTrainerJobs/job-1",
		"accounts/acct/deploymentShapes/rft/versions/1",
		existing,
		123,
	)
	if err != nil {
		t.Fatalf("PlanManagedDeploymentAttachment() error = %v", err)
	}
	if plan.Action != ManagedDeploymentActionCreate || plan.CreateConfig == nil || !plan.WaitForReady {
		t.Fatalf("plan = %#v", plan)
	}
	create := plan.CreateConfig
	if create.DeploymentID != "dep-1" || create.DeploymentShape != "accounts/acct/deploymentShapes/rft/versions/1" || create.Region != "US_OHIO_1" {
		t.Fatalf("create config = %#v", create)
	}
	if create.MinReplicaCount != 2 || create.MaxReplicaCount == nil || *create.MaxReplicaCount != 2 {
		t.Fatalf("replicas = %#v", create)
	}
	if create.SkipShapeValidation {
		t.Fatalf("managed deployment must not inherit trainer skip validations: %#v", create)
	}
	if len(create.ExtraArgs) != 1 || create.ExtraArgs[0] != "--tp 2" || create.ExtraValues["k"] != "v" {
		t.Fatalf("extra config = %#v", create)
	}
}

func TestPlanManagedDeploymentAttachmentClampsNegativeReplicas(t *testing.T) {
	replicas := -3
	plan, err := PlanManagedDeploymentAttachment(
		FiretitanProvisioningConfig{
			BaseModel:    "accounts/acct/models/base",
			ReplicaCount: &replicas,
		},
		"accounts/acct/rlorTrainerJobs/job-1",
		"accounts/acct/deploymentShapes/rft/versions/1",
		nil,
		123,
	)
	if err != nil {
		t.Fatalf("PlanManagedDeploymentAttachment() error = %v", err)
	}
	if plan.CreateConfig.MinReplicaCount != 0 || *plan.CreateConfig.MaxReplicaCount != 0 {
		t.Fatalf("replicas = %#v", plan.CreateConfig)
	}
}

func TestManagedDeploymentCreateConfigRequiresShapeOrAccelerator(t *testing.T) {
	_, err := ManagedDeploymentCreateConfig(
		FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"},
		"accounts/acct/rlorTrainerJobs/job-1",
		"",
		"dep-1",
	)
	if err == nil || !strings.Contains(err.Error(), "without a deployment shape") {
		t.Fatalf("err = %v", err)
	}
}

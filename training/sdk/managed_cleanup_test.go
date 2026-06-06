package sdk

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlanManagedHandleCleanupOrdersHolderBeforeTrainerDelete(t *testing.T) {
	steps, err := PlanManagedHandleCleanup(ManagedHandleCleanupConfig{
		TrainerJobID:          "ref-job",
		HasTrainerHolder:      true,
		CleanupTrainerOnClose: true,
	})
	if err != nil {
		t.Fatalf("PlanManagedHandleCleanup() error = %v", err)
	}
	got := cleanupOperations(steps)
	want := []ManagedCleanupOperation{
		ManagedCleanupFlushTelemetry,
		ManagedCleanupDrainTelemetry,
		ManagedCleanupStopTelemetry,
		ManagedCleanupStopTrainerHolder,
		ManagedCleanupDeleteTrainer,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	if steps[len(steps)-1].TrainerJobID != "ref-job" {
		t.Fatalf("delete trainer step = %#v", steps[len(steps)-1])
	}
}

func TestPlanManagedHandleCleanupScalesDeploymentBeforeTrainerDelete(t *testing.T) {
	steps, err := PlanManagedHandleCleanup(ManagedHandleCleanupConfig{
		TrainerJobID:             "job-1",
		Deployment:               &DeploymentInfo{DeploymentID: "dep-1"},
		CleanupTrainerOnClose:    true,
		CleanupDeploymentOnClose: CleanupDeploymentScaleToZero,
	})
	if err != nil {
		t.Fatalf("PlanManagedHandleCleanup() error = %v", err)
	}
	got := cleanupOperations(steps)
	want := []ManagedCleanupOperation{
		ManagedCleanupScaleDeploymentZero,
		ManagedCleanupDeleteTrainer,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	if steps[0].DeploymentID != "dep-1" {
		t.Fatalf("deployment step = %#v", steps[0])
	}
}

func TestPlanManagedHandleCleanupDeletesDeployment(t *testing.T) {
	steps, err := PlanManagedHandleCleanup(ManagedHandleCleanupConfig{
		Deployment:               &DeploymentInfo{DeploymentID: "dep-1"},
		CleanupDeploymentOnClose: CleanupDeploymentDelete,
	})
	if err != nil {
		t.Fatalf("PlanManagedHandleCleanup() error = %v", err)
	}
	if len(steps) != 1 || steps[0].Operation != ManagedCleanupDeleteDeployment || steps[0].DeploymentID != "dep-1" {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestPlanManagedHandleCleanupIgnoresDeploymentModeWithoutDeployment(t *testing.T) {
	steps, err := PlanManagedHandleCleanup(ManagedHandleCleanupConfig{
		CleanupDeploymentOnClose: "not-a-mode",
	})
	if err != nil {
		t.Fatalf("PlanManagedHandleCleanup() error = %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestPlanManagedHandleCleanupRejectsInvalidDeploymentMode(t *testing.T) {
	_, err := PlanManagedHandleCleanup(ManagedHandleCleanupConfig{
		Deployment:               &DeploymentInfo{DeploymentID: "dep-1"},
		CleanupDeploymentOnClose: "not-a-mode",
	})
	if err == nil || !strings.Contains(err.Error(), "cleanup_deployment_on_close") {
		t.Fatalf("err = %v", err)
	}
}

func cleanupOperations(steps []ManagedCleanupStep) []ManagedCleanupOperation {
	ops := make([]ManagedCleanupOperation, len(steps))
	for i, step := range steps {
		ops[i] = step.Operation
	}
	return ops
}

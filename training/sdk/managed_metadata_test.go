package sdk

import (
	"strings"
	"testing"
)

func TestResolveManagedMetadataPrefersHandleValues(t *testing.T) {
	configMax := 4096
	handleMax := 8192
	configCount := 4
	config := FiretitanProvisioningConfig{
		TrainerJobID:     "config-trainer",
		DeploymentID:     "config-deployment",
		MaxContextLength: &configMax,
		DeploymentShape:  "config-shape",
		AcceleratorType:  "CONFIG_ACCEL",
		AcceleratorCount: &configCount,
	}
	handle := ManagedHandleMetadata{
		TrainerJobID:     "trainer-1",
		DeploymentID:     "deployment-1",
		MaxContextLength: &handleMax,
		DeploymentShape:  "accounts/acct/deploymentShapes/ds/versions/v1",
		TrainingProfile: &TrainingShapeProfile{
			AcceleratorType:  "NVIDIA_H100_80GB",
			AcceleratorCount: 8,
		},
	}
	got := ResolveManagedMetadata(&config, handle)
	if got.TrainerJobID != "trainer-1" || got.DeploymentID != "deployment-1" {
		t.Fatalf("metadata = %#v", got)
	}
	if got.MaxContextLength == nil || *got.MaxContextLength != 8192 {
		t.Fatalf("max context = %#v", got.MaxContextLength)
	}
	if got.DeploymentShape != "accounts/acct/deploymentShapes/ds/versions/v1" {
		t.Fatalf("deployment shape = %q", got.DeploymentShape)
	}
	if got.AcceleratorType != "CONFIG_ACCEL" || got.AcceleratorCount == nil || *got.AcceleratorCount != 4 {
		t.Fatalf("deprecated config accelerators should override profile: %#v", got)
	}
}

func TestResolveManagedMetadataUsesProfileAcceleratorFallback(t *testing.T) {
	maxContext := 32768
	got := ResolveManagedMetadata(&FiretitanProvisioningConfig{}, ManagedHandleMetadata{
		MaxContextLength: &maxContext,
		TrainingProfile: &TrainingShapeProfile{
			AcceleratorType:  "NVIDIA_H100_80GB",
			AcceleratorCount: 8,
		},
	})
	if got.AcceleratorType != "NVIDIA_H100_80GB" || got.AcceleratorCount == nil || *got.AcceleratorCount != 8 {
		t.Fatalf("metadata = %#v", got)
	}
}

func TestResolveManagedMetadataFallsBackToConfig(t *testing.T) {
	maxContext := 2048
	got := ResolveManagedMetadata(&FiretitanProvisioningConfig{
		TrainerJobID:     "trainer-config",
		DeploymentID:     "deployment-config",
		MaxContextLength: &maxContext,
		DeploymentShape:  "shape-config",
	}, ManagedHandleMetadata{})
	if got.TrainerJobID != "trainer-config" || got.DeploymentID != "deployment-config" || got.DeploymentShape != "shape-config" {
		t.Fatalf("metadata = %#v", got)
	}
	if got.MaxContextLength == nil || *got.MaxContextLength != 2048 {
		t.Fatalf("max context = %#v", got.MaxContextLength)
	}
}

func TestRequireManagedString(t *testing.T) {
	got, err := RequireManagedString("trainer-1", "trainer job id")
	if err != nil {
		t.Fatal(err)
	}
	if got != "trainer-1" {
		t.Fatalf("value = %q", got)
	}
	_, err = RequireManagedString("", "trainer job id")
	if err == nil || !strings.Contains(err.Error(), "trainer job id") {
		t.Fatalf("error = %v", err)
	}
}

func TestRequireManagedInt(t *testing.T) {
	value := 8192
	got, err := RequireManagedInt(&value, "max context length")
	if err != nil {
		t.Fatal(err)
	}
	if got != 8192 {
		t.Fatalf("value = %d", got)
	}
	_, err = RequireManagedInt(nil, "max context length")
	if err == nil || !strings.Contains(err.Error(), "max context length") {
		t.Fatalf("error = %v", err)
	}
}

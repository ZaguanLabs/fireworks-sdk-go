package sdk

import (
	"strings"
	"testing"
	"time"
)

func TestResolveUserMetadataUsesCallMetadataWhenProvided(t *testing.T) {
	defaults := map[string]string{"owner": "default"}
	call := map[string]string{"owner": "call"}
	got := ResolveUserMetadata(defaults, call)
	if got["owner"] != "call" {
		t.Fatalf("metadata = %#v", got)
	}
	got["owner"] = "mutated"
	if call["owner"] != "call" {
		t.Fatalf("metadata was not cloned: %#v", call)
	}
}

func TestResolveUserMetadataFallsBackToDefault(t *testing.T) {
	defaults := map[string]string{"owner": "default"}
	got := ResolveUserMetadata(defaults, nil)
	if got["owner"] != "default" {
		t.Fatalf("metadata = %#v", got)
	}
	got["owner"] = "mutated"
	if defaults["owner"] != "default" {
		t.Fatalf("default metadata was not cloned: %#v", defaults)
	}
	if ResolveUserMetadata(nil, nil) != nil {
		t.Fatal("nil metadata should stay nil")
	}
}

func TestModelOwnerFromBaseModel(t *testing.T) {
	if got := ModelOwnerFromBaseModel("accounts/acct/models/base"); got != "acct" {
		t.Fatalf("owner = %q", got)
	}
	if got := ModelOwnerFromBaseModel("Qwen/Qwen3-1.7B"); got != "fireworks" {
		t.Fatalf("owner = %q", got)
	}
}

func TestTrainingRunMetadataFromManagedConfig(t *testing.T) {
	now := time.Unix(123, 0)
	config := FiretitanProvisioningConfig{
		BaseModel:    "accounts/acct/models/base",
		TrainerJobID: "trainer-1",
		LoraRank:     8,
		TrainUnembed: boolPointer(false),
		TrainMLP:     boolPointer(true),
		TrainAttn:    boolPointer(false),
	}
	got := TrainingRunMetadataFromManagedConfig(config, map[string]string{"recipe": "sdft"}, now)
	if got.TrainingRunID != "trainer-1" || got.BaseModel != config.BaseModel || got.ModelOwner != "acct" {
		t.Fatalf("metadata = %#v", got)
	}
	if !got.IsLora || got.LoraRank == nil || *got.LoraRank != 8 {
		t.Fatalf("lora metadata = %#v", got)
	}
	if got.LastRequest != now || got.UserMetadata["recipe"] != "sdft" {
		t.Fatalf("metadata = %#v", got)
	}
	if got.TrainUnembed == nil || *got.TrainUnembed || got.TrainMLP == nil || !*got.TrainMLP || got.TrainAttn == nil || *got.TrainAttn {
		t.Fatalf("train flags = %#v %#v %#v", got.TrainUnembed, got.TrainMLP, got.TrainAttn)
	}
}

func TestTrainingRunIDFromManagedConfigFallback(t *testing.T) {
	if got := TrainingRunIDFromManagedConfig(FiretitanProvisioningConfig{}); got != LazyManagedTrainingRunID {
		t.Fatalf("run ID = %q", got)
	}
}

func TestWeightsInfoFromManagedConfigFullParam(t *testing.T) {
	info := WeightsInfoFromManagedConfig(FiretitanProvisioningConfig{
		BaseModel: "accounts/acct/models/base",
		LoraRank:  0,
	})
	if info.BaseModel != "accounts/acct/models/base" || info.IsLora || info.LoraRank != nil {
		t.Fatalf("weights info = %#v", info)
	}
}

func TestWeightsInfoFromManagedConfigLora(t *testing.T) {
	info := WeightsInfoFromManagedConfig(FiretitanProvisioningConfig{
		BaseModel:    "accounts/acct/models/base",
		LoraRank:     16,
		TrainUnembed: boolPointer(true),
		TrainMLP:     boolPointer(false),
		TrainAttn:    boolPointer(true),
	})
	if !info.IsLora || info.LoraRank == nil || *info.LoraRank != 16 {
		t.Fatalf("weights info = %#v", info)
	}
	if info.TrainMLP == nil || *info.TrainMLP {
		t.Fatalf("train MLP = %#v", info.TrainMLP)
	}
}

func TestEmptyCursor(t *testing.T) {
	if got := EmptyCursor(20, 5); got.Limit != 20 || got.Offset != 5 || got.TotalCount != 0 {
		t.Fatalf("cursor = %#v", got)
	}
}

func TestControlPlaneCheckpointClientError(t *testing.T) {
	err := ControlPlaneCheckpointClientError()
	if err == nil || !strings.Contains(err.Error(), "provisioned trainer") || !strings.Contains(err.Error(), "create_training_client") {
		t.Fatalf("error = %v", err)
	}
}

func TestReferenceClientJobID(t *testing.T) {
	if got := ReferenceClientJobID("ref-trainer", "policy-trainer"); got != "ref-trainer" {
		t.Fatalf("job ID = %q", got)
	}
	if got := ReferenceClientJobID("", "policy-trainer"); got != "policy-trainer" {
		t.Fatalf("job ID = %q", got)
	}
}

func TestLazyManagedRestUnsupportedError(t *testing.T) {
	err := LazyManagedRestUnsupportedError("get_audit_log_async")
	if err == nil || !strings.Contains(err.Error(), "get_audit_log_async") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "trainer-backed service client") || !strings.Contains(err.Error(), "Fireworks checkpoint APIs") {
		t.Fatalf("error = %v", err)
	}
}

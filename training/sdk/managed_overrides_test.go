package sdk

import (
	"strings"
	"testing"
)

func TestDeprecatedManagedOverrideMessage(t *testing.T) {
	msg := DeprecatedManagedOverrideMessage(
		"create_training_client",
		"base_model",
		"accounts/acct/models/OTHER",
		"accounts/acct/models/base",
	)
	if !strings.Contains(msg, "base_model") || !strings.Contains(msg, "deprecated and ignored") {
		t.Fatalf("message = %q", msg)
	}
	if !strings.Contains(msg, "service config is authoritative") {
		t.Fatalf("message = %q", msg)
	}
}

func TestDeprecatedManagedOverrideMessageEmptyWhenMatchingOrNil(t *testing.T) {
	if got := DeprecatedManagedOverrideMessage("create_training_client", "base_model", "m", "m"); got != "" {
		t.Fatalf("message = %q", got)
	}
	if got := DeprecatedManagedOverrideMessage("create_training_client", "metadata", map[string]string{"a": "b"}, map[string]string{"a": "b"}); got != "" {
		t.Fatalf("message = %q", got)
	}
	if got := DeprecatedManagedOverrideMessage("create_training_client", "base_model", nil, "m"); got != "" {
		t.Fatalf("message = %q", got)
	}
	if got := DeprecatedManagedOverrideMessage("create_training_client", "base_model", "m", nil); got != "" {
		t.Fatalf("message = %q", got)
	}
}

func TestManagedTrainingClientKeyUsesImmutableConfig(t *testing.T) {
	config := FiretitanProvisioningConfig{
		BaseModel:    "accounts/acct/models/base",
		LoraRank:     8,
		Seed:         intPointer(123),
		TrainMLP:     boolPointer(false),
		TrainAttn:    boolPointer(true),
		TrainUnembed: boolPointer(false),
	}
	key := ManagedTrainingClientKey(config)
	if key.BaseModel != "accounts/acct/models/base" || key.LoraRank != 8 {
		t.Fatalf("key = %#v", key)
	}
	if !key.HasSeed || key.Seed != 123 {
		t.Fatalf("seed key = %#v", key)
	}
	if key.TrainMLP || !key.TrainAttn || key.TrainUnembed {
		t.Fatalf("train flags = %#v", key)
	}
}

func TestManagedTrainingClientKeyDefaultsTrainFlags(t *testing.T) {
	key := ManagedTrainingClientKey(FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"})
	if !key.TrainMLP || !key.TrainAttn || !key.TrainUnembed {
		t.Fatalf("train flags should default true: %#v", key)
	}
}

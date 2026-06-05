package sdk

import (
	"strings"
	"testing"
)

func TestTrainingClientConfigRegistryRejectsDuplicateConfig(t *testing.T) {
	key := NewTrainingClientKey("model-a", 0, nil, true, true, true)
	registry := NewTrainingClientConfigRegistry(key)

	err := registry.Add(key)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "'model-a' (lora_rank=0)") {
		t.Fatalf("error = %v", err)
	}
}

func TestTrainingClientConfigRegistryAllowsDifferentLoraRank(t *testing.T) {
	registry := NewTrainingClientConfigRegistry(
		NewTrainingClientKey("model-a", 0, nil, true, true, true),
	)
	if err := registry.Add(NewTrainingClientKey("model-a", 32, nil, true, true, true)); err != nil {
		t.Fatal(err)
	}
}

func TestTrainingClientConfigRegistryDistinguishesSeed(t *testing.T) {
	registry := NewTrainingClientConfigRegistry(
		NewTrainingClientKey("model-a", 0, nil, true, true, true),
	)
	if err := registry.Add(NewTrainingClientKey("model-a", 0, intPointer(0), true, true, true)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(NewTrainingClientKey("model-a", 0, intPointer(1), true, true, true)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(NewTrainingClientKey("model-a", 0, intPointer(1), true, true, true)); err == nil {
		t.Fatal("expected duplicate seed to be rejected")
	}
}

func TestTrainingClientConfigRegistryDistinguishesTrainFlags(t *testing.T) {
	registry := NewTrainingClientConfigRegistry(
		NewTrainingClientKey("model-a", 0, nil, true, true, true),
	)
	if err := registry.Add(NewTrainingClientKey("model-a", 0, nil, false, true, true)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(NewTrainingClientKey("model-a", 0, nil, true, false, true)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(NewTrainingClientKey("model-a", 0, nil, true, true, false)); err != nil {
		t.Fatal(err)
	}
}

func TestTrainingClientConfigRegistryNilHasNoDuplicates(t *testing.T) {
	var registry *TrainingClientConfigRegistry
	if registry.Has(NewTrainingClientKey("model-a", 0, nil, true, true, true)) {
		t.Fatal("nil registry should not contain keys")
	}
	if err := registry.CheckDuplicate(NewTrainingClientKey("model-a", 0, nil, true, true, true)); err != nil {
		t.Fatal(err)
	}
}

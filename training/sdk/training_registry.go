package sdk

import (
	"fmt"
	"sync"
)

type TrainingClientKey struct {
	BaseModel    string
	LoraRank     int
	Seed         int
	HasSeed      bool
	TrainMLP     bool
	TrainAttn    bool
	TrainUnembed bool
	LoraAlpha    int
	HasLoraAlpha bool
}

func NewTrainingClientKey(baseModel string, loraRank int, seed *int, trainMLP, trainAttn, trainUnembed bool, loraAlpha ...*int) TrainingClientKey {
	key := TrainingClientKey{
		BaseModel:    baseModel,
		LoraRank:     loraRank,
		TrainMLP:     trainMLP,
		TrainAttn:    trainAttn,
		TrainUnembed: trainUnembed,
	}
	if seed != nil {
		key.Seed = *seed
		key.HasSeed = true
	}
	if len(loraAlpha) > 0 && loraAlpha[0] != nil {
		key.LoraAlpha = *loraAlpha[0]
		key.HasLoraAlpha = true
	}
	return key
}

type TrainingClientConfigRegistry struct {
	mu      sync.RWMutex
	created map[TrainingClientKey]bool
}

func NewTrainingClientConfigRegistry(keys ...TrainingClientKey) *TrainingClientConfigRegistry {
	registry := &TrainingClientConfigRegistry{}
	for _, key := range keys {
		registry.Record(key)
	}
	return registry
}

func (r *TrainingClientConfigRegistry) Record(key TrainingClientKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked(key)
}

func (r *TrainingClientConfigRegistry) recordLocked(key TrainingClientKey) {
	if r.created == nil {
		r.created = map[TrainingClientKey]bool{}
	}
	r.created[key] = true
}

func (r *TrainingClientConfigRegistry) Has(key TrainingClientKey) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.created[key]
}

func (r *TrainingClientConfigRegistry) CheckDuplicate(key TrainingClientKey) error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.created[key] {
		return DuplicateTrainingClientError(key)
	}
	return nil
}

func (r *TrainingClientConfigRegistry) Add(key TrainingClientKey) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.created[key] {
		return DuplicateTrainingClientError(key)
	}
	r.recordLocked(key)
	return nil
}

func DuplicateTrainingClientError(key TrainingClientKey) error {
	return fmt.Errorf(
		"A training client for '%s' (lora_rank=%d) already exists on this service. Create a new FiretitanServiceClient for a separate trainer.",
		key.BaseModel,
		key.LoraRank,
	)
}

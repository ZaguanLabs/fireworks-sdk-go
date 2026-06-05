package sdk

import "fmt"

type TrainingClientKey struct {
	BaseModel    string
	LoraRank     int
	Seed         int
	HasSeed      bool
	TrainMLP     bool
	TrainAttn    bool
	TrainUnembed bool
}

func NewTrainingClientKey(baseModel string, loraRank int, seed *int, trainMLP, trainAttn, trainUnembed bool) TrainingClientKey {
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
	return key
}

type TrainingClientConfigRegistry struct {
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
	if r.created == nil {
		r.created = map[TrainingClientKey]bool{}
	}
	r.created[key] = true
}

func (r *TrainingClientConfigRegistry) Has(key TrainingClientKey) bool {
	if r == nil || r.created == nil {
		return false
	}
	return r.created[key]
}

func (r *TrainingClientConfigRegistry) CheckDuplicate(key TrainingClientKey) error {
	if r.Has(key) {
		return DuplicateTrainingClientError(key)
	}
	return nil
}

func (r *TrainingClientConfigRegistry) Add(key TrainingClientKey) error {
	if err := r.CheckDuplicate(key); err != nil {
		return err
	}
	r.Record(key)
	return nil
}

func DuplicateTrainingClientError(key TrainingClientKey) error {
	return fmt.Errorf(
		"A training client for '%s' (lora_rank=%d) already exists on this service. Create a new FiretitanServiceClient for a separate trainer.",
		key.BaseModel,
		key.LoraRank,
	)
}

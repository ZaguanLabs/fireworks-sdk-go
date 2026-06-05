package sdk

import (
	"fmt"
	"reflect"
)

func DeprecatedManagedOverrideMessage(method, field string, passed, configured any) string {
	if passed == nil || configured == nil || reflect.DeepEqual(passed, configured) {
		return ""
	}
	return fmt.Sprintf(
		"%s=%#v passed to %s differs from the service-configured %s=%#v; this override is deprecated and ignored - the service config is authoritative. Create a separate FiretitanServiceClient for a different training configuration.",
		field,
		passed,
		method,
		field,
		configured,
	)
}

func ManagedTrainingClientKey(config FiretitanProvisioningConfig) TrainingClientKey {
	trainMLP := true
	if config.TrainMLP != nil {
		trainMLP = *config.TrainMLP
	}
	trainAttn := true
	if config.TrainAttn != nil {
		trainAttn = *config.TrainAttn
	}
	trainUnembed := true
	if config.TrainUnembed != nil {
		trainUnembed = *config.TrainUnembed
	}
	return NewTrainingClientKey(
		config.BaseModel,
		config.LoraRank,
		config.Seed,
		trainMLP,
		trainAttn,
		trainUnembed,
	)
}

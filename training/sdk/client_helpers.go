package sdk

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"
)

const (
	DefaultFireworksAPIURL           = "https://api.fireworks.ai"
	CrossJobCheckpointRefPrefix      = "cross_job://"
	SamplingClientFromTrainerMessage = "FiretitanTrainingClient does not support save_weights_and_get_sampling_client(). Fireworks serves sampling from a separate hot-load inference deployment, not from an in-service ephemeral sampling session as Tinker's managed service does. Save a sampler snapshot and open a sampling client against the deployment instead:\n    saved = training_client.save_weights_for_sampler(name).result()\n    sampler = service.create_sampling_client(model_path=saved.path)\nThe SDK resolves base vs. delta hot-load automatically from the snapshot chain."
)

var FiretitanTinkerClientConfig = map[string]bool{
	"parallel_fwdbwd_chunks": false,
	"proto_write_fwdbwd":     false,
	"proto_compress_fwdbwd":  false,
	"sample_no_retries":      false,
	"use_pyqwest_transport":  true,
}

type GradAccNormalization string

const (
	GradAccNormalizationNumLossTokens GradAccNormalization = "num_loss_tokens"
	GradAccNormalizationNumSequences  GradAccNormalization = "num_sequences"
	GradAccNormalizationNone          GradAccNormalization = "none"
)

func NormalizeGradAccNormalization(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	var raw string
	switch v := value.(type) {
	case GradAccNormalization:
		raw = string(v)
	case string:
		raw = v
	default:
		raw = fmt.Sprint(v)
	}
	normalized := strings.ToLower(raw)
	switch GradAccNormalization(normalized) {
	case GradAccNormalizationNumLossTokens, GradAccNormalizationNumSequences, GradAccNormalizationNone:
		return normalized, nil
	default:
		return "", fmt.Errorf("unknown grad_accumulation_normalization %q; expected one of: num_loss_tokens, num_sequences, none", raw)
	}
}

func CloneFiretitanTinkerClientConfig() map[string]bool {
	out := make(map[string]bool, len(FiretitanTinkerClientConfig))
	for key, value := range FiretitanTinkerClientConfig {
		out[key] = value
	}
	return out
}

func MakeCrossJobCheckpointRef(sourceJobID, checkpointName string) (string, error) {
	sourceJobID = strings.TrimSpace(sourceJobID)
	checkpointName = strings.TrimSpace(checkpointName)
	if sourceJobID == "" {
		return "", fmt.Errorf("source_job_id cannot be empty")
	}
	if checkpointName == "" {
		return "", fmt.Errorf("checkpoint_name cannot be empty")
	}
	if strings.HasPrefix(checkpointName, "gs://") || strings.HasPrefix(checkpointName, "/") {
		return "", fmt.Errorf("checkpoint_name must be a logical checkpoint name, not a full path")
	}
	return CrossJobCheckpointRefPrefix + sourceJobID + "/" + checkpointName, nil
}

func GenerateSessionID() (string, error) {
	var data [4]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func QualifySnapshotName(sessionID, name string) string {
	return name + "-" + sessionID
}

func ResolveCheckpointPath(checkpointName string, sourceJobID ...string) (string, error) {
	if strings.HasPrefix(checkpointName, "gs://") || strings.HasPrefix(checkpointName, "/") {
		return checkpointName, nil
	}
	if len(sourceJobID) > 0 && sourceJobID[0] != "" {
		return MakeCrossJobCheckpointRef(sourceJobID[0], checkpointName)
	}
	return checkpointName, nil
}

func FireworksAPIKey(apiKey string) string {
	if apiKey != "" {
		return apiKey
	}
	return os.Getenv("FIREWORKS_API_KEY")
}

func FireworksBaseURL(baseURL string) string {
	if baseURL != "" {
		return baseURL
	}
	if env := os.Getenv("FIREWORKS_BASE_URL"); env != "" {
		return env
	}
	return DefaultFireworksAPIURL
}

func PopAlias(values map[string]any, canonical string, aliases ...string) error {
	var presentAliases []string
	for _, alias := range aliases {
		if _, ok := values[alias]; ok {
			presentAliases = append(presentAliases, alias)
		}
	}
	if _, canonicalPresent := values[canonical]; canonicalPresent && len(presentAliases) > 0 {
		return fmt.Errorf("pass either %q or alias %s, not both", canonical, strings.Join(presentAliases, ", "))
	}
	if len(presentAliases) > 1 {
		return fmt.Errorf("pass only one alias for %q; got %s", canonical, strings.Join(presentAliases, ", "))
	}
	if len(presentAliases) == 1 {
		values[canonical] = values[presentAliases[0]]
		delete(values, presentAliases[0])
	}
	return nil
}

func ManagedConfigFromMap(values map[string]any) (FiretitanProvisioningConfig, []string, error) {
	kwargs := cloneAnyMap(values)
	if err := PopAlias(kwargs, "base_model", "model_name"); err != nil {
		return FiretitanProvisioningConfig{}, nil, err
	}
	if err := PopAlias(kwargs, "training_shape_id", "training_shape", "training_shape_ref"); err != nil {
		return FiretitanProvisioningConfig{}, nil, err
	}
	if err := PopAlias(kwargs, "trainer_job_id", "trainer_id"); err != nil {
		return FiretitanProvisioningConfig{}, nil, err
	}
	if err := PopAlias(kwargs, "replica_count", "deployment_replica_count"); err != nil {
		return FiretitanProvisioningConfig{}, nil, err
	}
	if err := PopAlias(kwargs, "lora_alpha", "loraAlpha"); err != nil {
		return FiretitanProvisioningConfig{}, nil, err
	}

	var deprecated []string
	for _, field := range []string{"accelerator_type", "accelerator_count"} {
		if value, ok := kwargs[field]; ok {
			if value != nil {
				deprecated = append(deprecated, field)
			}
			delete(kwargs, field)
		}
	}

	config, err := firetitanProvisioningConfigFromCanonicalMap(kwargs)
	if err != nil {
		return FiretitanProvisioningConfig{}, deprecated, err
	}
	normalized, err := config.Normalize()
	if err != nil {
		return FiretitanProvisioningConfig{}, deprecated, err
	}
	return normalized, deprecated, nil
}

func firetitanProvisioningConfigFromCanonicalMap(values map[string]any) (FiretitanProvisioningConfig, error) {
	var config FiretitanProvisioningConfig
	for key, value := range values {
		var err error
		switch key {
		case "base_model":
			config.BaseModel, err = asString(value, key)
		case "tokenizer_model":
			config.TokenizerModel, err = asString(value, key)
		case "lora_rank":
			config.LoraRank, err = asInt(value, key)
		case "lora_alpha":
			config.LoraAlpha, err = asIntPointer(value, key)
		case "training_shape_id":
			config.TrainingShapeID, err = asString(value, key)
		case "reference_training_shape_id":
			config.ReferenceTrainingShapeID, err = asString(value, key)
		case "reference_trainer_job_id":
			config.ReferenceTrainerJobID, err = asString(value, key)
		case "cleanup_reference_trainer_on_close":
			config.CleanupReferenceTrainerOnClose, err = asBoolPointer(value, key)
		case "reference_required":
			config.ReferenceRequired, err = asBool(value, key)
		case "deployment_shape":
			config.DeploymentShape, err = asString(value, key)
		case "trainer_job_id":
			config.TrainerJobID, err = asString(value, key)
		case "deployment_id":
			config.DeploymentID, err = asString(value, key)
		case "create_deployment":
			config.CreateDeployment, err = asBoolPointer(value, key)
		case "forward_only":
			config.ForwardOnly, err = asBool(value, key)
		case "region":
			config.Region, err = asString(value, key)
		case "deployment_region":
			config.DeploymentRegion, err = asString(value, key)
		case "max_context_length":
			config.MaxContextLength, err = asIntPointer(value, key)
		case "learning_rate":
			config.LearningRate, err = asFloat(value, key)
		case "gradient_accumulation_steps":
			config.GradientAccumulationSteps, err = asInt(value, key)
		case "seed":
			config.Seed, err = asIntPointer(value, key)
		case "train_mlp":
			config.TrainMLP, err = asBoolPointer(value, key)
		case "train_attn":
			config.TrainAttn, err = asBoolPointer(value, key)
		case "train_unembed":
			config.TrainUnembed, err = asBoolPointer(value, key)
		case "node_count":
			config.NodeCount, err = asIntPointer(value, key)
		case "custom_image_tag":
			config.CustomImageTag, err = asString(value, key)
		case "extra_args":
			config.ExtraArgs, err = asStringSlice(value, key)
		case "deployment_extra_args":
			config.DeploymentExtraArgs, err = asStringSlice(value, key)
		case "deployment_extra_values":
			config.DeploymentExtraValues, err = asStringMap(value, key)
		case "trainer_replica_count":
			config.TrainerReplicaCount, err = asIntPointer(value, key)
		case "replica_count":
			config.ReplicaCount, err = asIntPointer(value, key)
		case "trainer_timeout", "trainer_timeout_s":
			config.TrainerTimeout, err = asDuration(value, key)
		case "deployment_timeout", "deployment_timeout_s":
			config.DeploymentTimeout, err = asDuration(value, key)
		case "hotload_timeout", "hotload_timeout_s":
			config.HotloadTimeout, err = asDuration(value, key)
		case "reattach_settle_timeout", "reattach_settle_timeout_s":
			config.ReattachSettleTimeout, err = asDuration(value, key)
		case "reattach_poll_interval", "reattach_poll_interval_s":
			config.ReattachPollInterval, err = asDuration(value, key)
		case "cleanup_trainer_on_close":
			config.CleanupTrainerOnClose, err = asBool(value, key)
		case "cleanup_deployment_on_close":
			config.CleanupDeploymentOnClose, err = asString(value, key)
		case "display_name":
			config.DisplayName, err = asString(value, key)
		case "purpose":
			config.Purpose, err = asString(value, key)
		case "managed_by":
			config.ManagedBy, err = asString(value, key)
		case "skip_validations":
			config.SkipValidations, err = asBool(value, key)
		case "disable_speculative_decoding":
			config.DisableSpeculativeDecoding, err = asBool(value, key)
		default:
			return FiretitanProvisioningConfig{}, fmt.Errorf("unknown FiretitanProvisioningConfig field %q", key)
		}
		if err != nil {
			return FiretitanProvisioningConfig{}, err
		}
	}
	return config, nil
}

func asString(value any, field string) (string, error) {
	if value == nil {
		return "", nil
	}
	v, ok := value.(string)
	if !ok {
		return "", typeError(field, "string", value)
	}
	return v, nil
}

func asBool(value any, field string) (bool, error) {
	if value == nil {
		return false, nil
	}
	v, ok := value.(bool)
	if !ok {
		return false, typeError(field, "bool", value)
	}
	return v, nil
}

func asBoolPointer(value any, field string) (*bool, error) {
	if value == nil {
		return nil, nil
	}
	v, err := asBool(value, field)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func asInt(value any, field string) (int, error) {
	if value == nil {
		return 0, nil
	}
	switch v := value.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		return int(v), nil
	case float64:
		if v == float64(int(v)) {
			return int(v), nil
		}
	case float32:
		if v == float32(int(v)) {
			return int(v), nil
		}
	}
	return 0, typeError(field, "integer", value)
}

func asIntPointer(value any, field string) (*int, error) {
	if value == nil {
		return nil, nil
	}
	v, err := asInt(value, field)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func asFloat(value any, field string) (float64, error) {
	if value == nil {
		return 0, nil
	}
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	}
	return 0, typeError(field, "number", value)
}

func asDuration(value any, field string) (time.Duration, error) {
	if value == nil {
		return 0, nil
	}
	if v, ok := value.(time.Duration); ok {
		return v, nil
	}
	seconds, err := asFloat(value, field)
	if err != nil {
		return 0, err
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func asStringSlice(value any, field string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	if v, ok := value.([]string); ok {
		return append([]string(nil), v...), nil
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, typeError(field, "[]string", value)
	}
	out := make([]string, 0, len(raw))
	for i, item := range raw {
		v, ok := item.(string)
		if !ok {
			return nil, typeError(fmt.Sprintf("%s[%d]", field, i), "string", item)
		}
		out = append(out, v)
	}
	return out, nil
}

func asStringMap(value any, field string) (map[string]string, error) {
	if value == nil {
		return nil, nil
	}
	if v, ok := value.(map[string]string); ok {
		return cloneStringMap(v), nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, typeError(field, "map[string]string", value)
	}
	out := make(map[string]string, len(raw))
	for key, item := range raw {
		v, ok := item.(string)
		if !ok {
			return nil, typeError(field+"."+key, "string", item)
		}
		out[key] = v
	}
	return out, nil
}

func typeError(field, want string, got any) error {
	return fmt.Errorf("%s must be %s, got %s", field, want, reflect.TypeOf(got))
}

package sdk

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFiretitanTinkerClientConfigDefaults(t *testing.T) {
	cfg := CloneFiretitanTinkerClientConfig()
	want := map[string]bool{
		"parallel_fwdbwd_chunks": false,
		"proto_write_fwdbwd":     false,
		"proto_compress_fwdbwd":  false,
		"sample_no_retries":      false,
		"use_pyqwest_transport":  true,
	}
	for key, wantValue := range want {
		if cfg[key] != wantValue {
			t.Fatalf("%s = %t, want %t", key, cfg[key], wantValue)
		}
	}
	cfg["use_pyqwest_transport"] = false
	if !FiretitanTinkerClientConfig["use_pyqwest_transport"] {
		t.Fatal("CloneFiretitanTinkerClientConfig returned shared map")
	}
}

func TestMakeCrossJobCheckpointRef(t *testing.T) {
	ref, err := MakeCrossJobCheckpointRef(" old-job ", " step-2 ")
	if err != nil {
		t.Fatal(err)
	}
	if ref != "cross_job://old-job/step-2" {
		t.Fatalf("ref = %q", ref)
	}
}

func TestMakeCrossJobCheckpointRefRejectsEmptySourceJobID(t *testing.T) {
	_, err := MakeCrossJobCheckpointRef(" ", "step-2")
	if err == nil || !strings.Contains(err.Error(), "source_job_id") {
		t.Fatalf("error = %v", err)
	}
}

func TestMakeCrossJobCheckpointRefRejectsEmptyCheckpointName(t *testing.T) {
	_, err := MakeCrossJobCheckpointRef("old-job", " ")
	if err == nil || !strings.Contains(err.Error(), "checkpoint_name") {
		t.Fatalf("error = %v", err)
	}
}

func TestMakeCrossJobCheckpointRefRejectsFullPaths(t *testing.T) {
	for _, checkpointName := range []string{"gs://bucket/path", "/tmp/checkpoint"} {
		_, err := MakeCrossJobCheckpointRef("old-job", checkpointName)
		if err == nil || !strings.Contains(err.Error(), "logical checkpoint name") {
			t.Fatalf("%q error = %v", checkpointName, err)
		}
	}
}

func TestGenerateSessionID(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		sessionID, err := GenerateSessionID()
		if err != nil {
			t.Fatal(err)
		}
		if len(sessionID) != 8 {
			t.Fatalf("len(%q) = %d, want 8", sessionID, len(sessionID))
		}
		if _, err := strconv.ParseUint(sessionID, 16, 32); err != nil {
			t.Fatalf("session ID %q is not hex: %v", sessionID, err)
		}
		ids[sessionID] = true
	}
	if len(ids) != 100 {
		t.Fatalf("unique session IDs = %d, want 100", len(ids))
	}
}

func TestQualifySnapshotName(t *testing.T) {
	if got := QualifySnapshotName("a1b2c3d4", "step-0-base"); got != "step-0-base-a1b2c3d4" {
		t.Fatalf("qualified name = %q", got)
	}
	if got := QualifySnapshotName("deadbeef", "ckpt"); strings.Contains(got, "/") || got != "ckpt-deadbeef" {
		t.Fatalf("qualified name = %q", got)
	}
}

func TestResolveCheckpointPath(t *testing.T) {
	tests := []struct {
		name        string
		sourceJobID string
		want        string
	}{
		{name: "gs://bucket/path", want: "gs://bucket/path"},
		{name: "/tmp/checkpoint", want: "/tmp/checkpoint"},
		{name: "step-2", want: "step-2"},
		{name: "step-2", sourceJobID: "old-job", want: "cross_job://old-job/step-2"},
	}
	for _, test := range tests {
		var (
			got string
			err error
		)
		if test.sourceJobID == "" {
			got, err = ResolveCheckpointPath(test.name)
		} else {
			got, err = ResolveCheckpointPath(test.name, test.sourceJobID)
		}
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("ResolveCheckpointPath(%q, %q) = %q, want %q", test.name, test.sourceJobID, got, test.want)
		}
	}
}

func TestSamplingClientFromTrainerMessage(t *testing.T) {
	if !strings.Contains(SamplingClientFromTrainerMessage, "does not support save_weights_and_get_sampling_client") {
		t.Fatalf("message = %q", SamplingClientFromTrainerMessage)
	}
	if !strings.Contains(SamplingClientFromTrainerMessage, "separate hot-load inference deployment") {
		t.Fatalf("message = %q", SamplingClientFromTrainerMessage)
	}
}

func TestNormalizeGradAccNormalization(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "nil", value: nil, want: ""},
		{name: "enum", value: GradAccNormalizationNumLossTokens, want: "num_loss_tokens"},
		{name: "case insensitive string", value: "NUM_SEQUENCES", want: "num_sequences"},
		{name: "none", value: "none", want: "none"},
	}
	for _, test := range tests {
		got, err := NormalizeGradAccNormalization(test.value)
		if err != nil {
			t.Fatalf("%s error = %v", test.name, err)
		}
		if got != test.want {
			t.Fatalf("%s got %q, want %q", test.name, got, test.want)
		}
	}
	_, err := NormalizeGradAccNormalization("tokens")
	if err == nil || !strings.Contains(err.Error(), "num_loss_tokens") {
		t.Fatalf("error = %v", err)
	}
}

func TestFireworksAPIKeyAndBaseURL(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "env-key")
	t.Setenv("FIREWORKS_BASE_URL", "https://env.example.com")
	if got := FireworksAPIKey("explicit"); got != "explicit" {
		t.Fatalf("api key = %q", got)
	}
	if got := FireworksAPIKey(""); got != "env-key" {
		t.Fatalf("env api key = %q", got)
	}
	if got := FireworksBaseURL("https://explicit.example.com"); got != "https://explicit.example.com" {
		t.Fatalf("base url = %q", got)
	}
	if got := FireworksBaseURL(""); got != "https://env.example.com" {
		t.Fatalf("env base url = %q", got)
	}
	t.Setenv("FIREWORKS_BASE_URL", "")
	if got := FireworksBaseURL(""); got != DefaultFireworksAPIURL {
		t.Fatalf("default base url = %q", got)
	}
}

func TestPopAlias(t *testing.T) {
	values := map[string]any{"model_name": "accounts/acct/models/base"}
	if err := PopAlias(values, "base_model", "model_name"); err != nil {
		t.Fatal(err)
	}
	if values["base_model"] != "accounts/acct/models/base" {
		t.Fatalf("values = %#v", values)
	}

	err := PopAlias(map[string]any{"base_model": "a", "model_name": "b"}, "base_model", "model_name")
	if err == nil || !strings.Contains(err.Error(), "either") {
		t.Fatalf("canonical conflict error = %v", err)
	}
	err = PopAlias(map[string]any{"training_shape": "a", "training_shape_ref": "b"}, "training_shape_id", "training_shape", "training_shape_ref")
	if err == nil || !strings.Contains(err.Error(), "only one alias") {
		t.Fatalf("alias conflict error = %v", err)
	}
}

func TestManagedConfigFromMapAcceptsAliases(t *testing.T) {
	config, deprecated, err := ManagedConfigFromMap(map[string]any{
		"model_name":               "accounts/acct/models/base",
		"training_shape":           "accounts/acct/trainingShapes/shape",
		"trainer_id":               "trainer-1",
		"deployment_replica_count": 2,
		"trainer_timeout_s":        30,
		"deployment_extra_args":    []any{"--a", "--b"},
		"deployment_extra_values":  map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(deprecated) != 0 {
		t.Fatalf("deprecated = %v", deprecated)
	}
	if config.BaseModel != "accounts/acct/models/base" || config.TrainingShapeID != "accounts/acct/trainingShapes/shape" || config.TrainerJobID != "trainer-1" {
		t.Fatalf("config = %#v", config)
	}
	if config.ReplicaCount == nil || *config.ReplicaCount != 2 {
		t.Fatalf("replica count = %#v", config.ReplicaCount)
	}
	if config.TrainerTimeout != 30*time.Second {
		t.Fatalf("trainer timeout = %v", config.TrainerTimeout)
	}
	if len(config.DeploymentExtraArgs) != 2 || config.DeploymentExtraValues["k"] != "v" {
		t.Fatalf("extra config = %#v", config)
	}
}

func TestManagedConfigFromMapNormalizesBlankOptionalReferenceFields(t *testing.T) {
	config, _, err := ManagedConfigFromMap(map[string]any{
		"base_model":                  "accounts/acct/models/base",
		"training_shape_id":           "accounts/acct/trainingShapes/shape",
		"reference_training_shape_id": "",
		"reference_trainer_job_id":    "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.ReferenceTrainingShapeID != "" || config.ReferenceTrainerJobID != "" {
		t.Fatalf("config = %#v", config)
	}
}

func TestManagedConfigFromMapDefaultsSpeculativeDecodingEnabled(t *testing.T) {
	config, _, err := ManagedConfigFromMap(map[string]any{
		"base_model":        "accounts/acct/models/base",
		"training_shape_id": "accounts/acct/trainingShapes/shape",
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.DisableSpeculativeDecoding {
		t.Fatalf("config = %#v", config)
	}
}

func TestManagedConfigFromMapRejectsConflictingAliases(t *testing.T) {
	_, _, err := ManagedConfigFromMap(map[string]any{
		"model_name":         "accounts/acct/models/base",
		"training_shape":     "shape-a",
		"training_shape_ref": "shape-b",
	})
	if err == nil || !strings.Contains(err.Error(), "only one alias") {
		t.Fatalf("error = %v", err)
	}
}

func TestManagedConfigFromMapDropsDeprecatedAccelerators(t *testing.T) {
	config, deprecated, err := ManagedConfigFromMap(map[string]any{
		"base_model":            "accounts/acct/models/base",
		"training_shape_id":     "accounts/acct/trainingShapes/shape",
		"accelerator_type":      "NVIDIA_H200",
		"accelerator_count":     8,
		"replica_count":         2,
		"trainer_replica_count": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(deprecated, ",") != "accelerator_type,accelerator_count" {
		t.Fatalf("deprecated = %v", deprecated)
	}
	if config.AcceleratorType != "" || config.AcceleratorCount != nil {
		t.Fatalf("accelerator fields = %#v", config)
	}
	if config.ReplicaCount == nil || *config.ReplicaCount != 2 || config.TrainerReplicaCount == nil || *config.TrainerReplicaCount != 3 {
		t.Fatalf("scaling fields = %#v", config)
	}
}

func TestManagedConfigFromMapRejectsUnknownAndWrongTypes(t *testing.T) {
	_, _, err := ManagedConfigFromMap(map[string]any{"unknown": true})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown error = %v", err)
	}
	_, _, err = ManagedConfigFromMap(map[string]any{"replica_count": "two"})
	if err == nil || !strings.Contains(err.Error(), "integer") {
		t.Fatalf("type error = %v", err)
	}
}

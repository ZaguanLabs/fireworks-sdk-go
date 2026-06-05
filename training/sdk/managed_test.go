package sdk

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFiretitanProvisioningConfigNormalizeDeploymentRegionConflict(t *testing.T) {
	_, err := (FiretitanProvisioningConfig{
		BaseModel:        "accounts/acct/models/base",
		Region:           "US_OHIO_1",
		DeploymentRegion: "US_VIRGINIA_1",
	}).Normalize()
	if err == nil || !strings.Contains(err.Error(), "deployment_region") {
		t.Fatalf("err = %v", err)
	}
}

func TestFiretitanProvisioningConfigNormalizeInheritsDeploymentRegion(t *testing.T) {
	got, err := (FiretitanProvisioningConfig{
		BaseModel:        "accounts/acct/models/base",
		DeploymentRegion: "US_OHIO_1",
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Region != "US_OHIO_1" || got.DeploymentRegion != "" {
		t.Fatalf("got = %#v", got)
	}
}

func TestFiretitanProvisioningConfigNormalizeIgnoresInactiveDeploymentRegion(t *testing.T) {
	createDeployment := false
	got, err := (FiretitanProvisioningConfig{
		BaseModel:         "accounts/acct/models/base",
		CreateDeployment:  &createDeployment,
		Region:            "US_OHIO_1",
		DeploymentRegion:  "US_VIRGINIA_1",
		AcceleratorType:   "NVIDIA_H200_141GB",
		AcceleratorCount:  intPointer(8),
		HotloadTimeout:    77 * time.Second,
		DeploymentTimeout: 88 * time.Second,
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.Region != "US_OHIO_1" || got.DeploymentRegion != "" {
		t.Fatalf("regions = %#v", got)
	}
	if got.AcceleratorType != "" || got.AcceleratorCount != nil {
		t.Fatalf("deprecated accelerators were not cleared: %#v", got)
	}
	if got.HotloadTimeout != 77*time.Second || got.DeploymentTimeout != 88*time.Second {
		t.Fatalf("timeouts = %#v", got)
	}
}

func TestFiretitanProvisioningConfigNormalizeDefaults(t *testing.T) {
	zeroReplicas := 0
	got, err := (FiretitanProvisioningConfig{
		BaseModel:    "accounts/acct/models/base",
		ReplicaCount: &zeroReplicas,
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.CreateDeployment == nil || !*got.CreateDeployment {
		t.Fatalf("create deployment default = %#v", got.CreateDeployment)
	}
	if got.CleanupReferenceTrainerOnClose == nil || !*got.CleanupReferenceTrainerOnClose {
		t.Fatalf("cleanup reference default = %#v", got.CleanupReferenceTrainerOnClose)
	}
	if got.ReplicaCount == nil || *got.ReplicaCount != 1 {
		t.Fatalf("replica count = %#v", got.ReplicaCount)
	}
	if got.HotloadTimeout != HotloadTimeout || got.TrainerTimeout != DefaultTrainerTimeout || got.DeploymentTimeout != DeploymentReadyTimeout {
		t.Fatalf("timeouts = %#v", got)
	}
	if got.LearningRate != DefaultLearningRate || got.GradientAccumulationSteps != 1 {
		t.Fatalf("optimizer defaults = %#v", got)
	}
	if got.TrainMLP == nil || !*got.TrainMLP || got.TrainAttn == nil || !*got.TrainAttn || got.TrainUnembed == nil || !*got.TrainUnembed {
		t.Fatalf("train defaults = %#v", got)
	}
}

func TestUseSharedBaseReference(t *testing.T) {
	config := FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"}
	if !UseSharedBaseReference(config, 16) {
		t.Fatal("LoRA without explicit reference should share base reference")
	}
	if UseSharedBaseReference(config, 0) {
		t.Fatal("full-parameter policy cannot share base reference")
	}
	config.ReferenceTrainingShapeID = "ts-ref"
	if UseSharedBaseReference(config, 16) {
		t.Fatal("explicit reference shape should disable sharing")
	}
	config.ReferenceTrainingShapeID = ""
	config.ReferenceTrainerJobID = "ref-job"
	if UseSharedBaseReference(config, 16) {
		t.Fatal("explicit reference job should disable sharing")
	}
}

func TestShouldProvisionReference(t *testing.T) {
	if ShouldProvisionReference(FiretitanProvisioningConfig{ReferenceRequired: true, LoraRank: 16}) {
		t.Fatal("LoRA with shared base should not provision separate reference")
	}
	if !ShouldProvisionReference(FiretitanProvisioningConfig{ReferenceRequired: true, LoraRank: 0, ReferenceTrainingShapeID: "ts-ref"}) {
		t.Fatal("full-parameter explicit reference should provision separate reference")
	}
	if ShouldProvisionReference(FiretitanProvisioningConfig{ReferenceRequired: true, ForwardOnly: true, ReferenceTrainingShapeID: "ts-ref"}) {
		t.Fatal("forward-only reference config should not recursively provision reference")
	}
}

func TestReferenceManagedConfigRequiresReference(t *testing.T) {
	_, err := ReferenceManagedConfig(FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"}, 0)
	if err == nil || !strings.Contains(err.Error(), "reference_training_shape_id") {
		t.Fatalf("err = %v", err)
	}
}

func TestReferenceManagedConfigFreshForwardOnly(t *testing.T) {
	config := FiretitanProvisioningConfig{
		BaseModel:                  "accounts/acct/models/base",
		TrainingShapeID:            "ts-policy",
		ReferenceTrainingShapeID:   "ts-ref",
		TrainerJobID:               "policy-job",
		DeploymentID:               "dep-1",
		ReferenceRequired:          true,
		Region:                     "US_OHIO_1",
		TrainerReplicaCount:        intPointer(2),
		CleanupTrainerOnClose:      false,
		CleanupDeploymentOnClose:   "delete",
		DisableSpeculativeDecoding: true,
	}
	got, err := ReferenceManagedConfig(config, 0)
	if err != nil {
		t.Fatalf("ReferenceManagedConfig() error = %v", err)
	}
	if got.TrainingShapeID != "ts-ref" || got.TrainerJobID != "" || got.DeploymentID != "" {
		t.Fatalf("resource IDs = %#v", got)
	}
	if got.CreateDeployment == nil || *got.CreateDeployment {
		t.Fatalf("CreateDeployment = %#v", got.CreateDeployment)
	}
	if !got.ForwardOnly || got.ReferenceRequired || got.TrainerReplicaCount != nil {
		t.Fatalf("reference flags = %#v", got)
	}
	if !got.CleanupTrainerOnClose {
		t.Fatalf("fresh SDK reference should clean up by default: %#v", got)
	}
	if got.Region != "US_OHIO_1" {
		t.Fatalf("region = %q", got.Region)
	}
}

func TestReferenceManagedConfigExistingJobNoCleanup(t *testing.T) {
	got, err := ReferenceManagedConfig(FiretitanProvisioningConfig{
		BaseModel:                "accounts/acct/models/base",
		ReferenceTrainerJobID:    "ref-job",
		TrainingShapeID:          "ts-policy",
		TrainerJobID:             "policy-job",
		CleanupTrainerOnClose:    true,
		ReferenceTrainingShapeID: "",
	}, 0)
	if err != nil {
		t.Fatalf("ReferenceManagedConfig() error = %v", err)
	}
	if got.TrainingShapeID != "" || got.TrainerJobID != "ref-job" || got.CleanupTrainerOnClose {
		t.Fatalf("got = %#v", got)
	}
}

func TestReferenceManagedConfigLoraReferenceShape(t *testing.T) {
	got, err := ReferenceManagedConfig(FiretitanProvisioningConfig{
		BaseModel:                "accounts/acct/models/base",
		ReferenceTrainingShapeID: "ts-ref",
	}, 16)
	if err != nil {
		t.Fatalf("ReferenceManagedConfig() error = %v", err)
	}
	if got.LoraRank != 16 || ExpectedReferenceTrainerMode(got) != LoraTrainerMode {
		t.Fatalf("got = %#v", got)
	}
	got.LoraRank = 0
	if ExpectedReferenceTrainerMode(got) != ForwardOnlyMode {
		t.Fatalf("mode = %q", ExpectedReferenceTrainerMode(got))
	}
}

func TestReferenceManagedConfigCanKeepFreshReference(t *testing.T) {
	cleanup := false
	got, err := ReferenceManagedConfig(FiretitanProvisioningConfig{
		BaseModel:                      "accounts/acct/models/base",
		ReferenceTrainingShapeID:       "ts-ref",
		CleanupReferenceTrainerOnClose: &cleanup,
	}, 0)
	if err != nil {
		t.Fatalf("ReferenceManagedConfig() error = %v", err)
	}
	if got.CleanupTrainerOnClose {
		t.Fatalf("got = %#v", got)
	}
}

func TestDeploymentShapeConflictIgnoresVersionDrift(t *testing.T) {
	if DeploymentShapeConflict("accounts/a/deploymentShapes/s/versions/v1", "accounts/a/deploymentShapes/s/versions/v2") {
		t.Fatal("same shape with different versions should not conflict")
	}
	if DeploymentShapeConflict("", "accounts/a/deploymentShapes/s/versions/v2") {
		t.Fatal("empty requested shape should not conflict")
	}
	if !DeploymentShapeConflict("accounts/a/deploymentShapes/s1/versions/v1", "accounts/a/deploymentShapes/s2/versions/v1") {
		t.Fatal("different shapes should conflict")
	}
}

func TestDefaultDeploymentIDAt(t *testing.T) {
	if got := DefaultDeploymentIDAt("accounts/acct/models/Qwen3-1.7B/", 123); got != "qwen3-1-7b-123" {
		t.Fatalf("id = %q", got)
	}
	if got := DefaultDeploymentIDAt("accounts/acct/models/a--b", 123); got != "a--b-123" {
		t.Fatalf("repeated separator id = %q", got)
	}
	if got := DefaultDeploymentIDAt("///", 123); got != "model-123" {
		t.Fatalf("fallback id = %q", got)
	}
}

func TestInferRegionFromAccelerator(t *testing.T) {
	cases := map[string]string{
		"NVIDIA_H200_141GB": "US_VIRGINIA_1",
		"NVIDIA_B200":       "US_OHIO_1",
		"NVIDIA_B300_X":     "NA_BRITISHCOLUMBIA_1",
		"NVIDIA_A100":       "",
	}
	for accelerator, want := range cases {
		if got := InferRegionFromAccelerator(accelerator); got != want {
			t.Fatalf("InferRegionFromAccelerator(%q) = %q, want %q", accelerator, got, want)
		}
	}
	if got := InferRegionFromDeploymentShapeSnapshot(map[string]any{"acceleratorType": "NVIDIA_B200_180GB"}); got != "US_OHIO_1" {
		t.Fatalf("InferRegionFromDeploymentShapeSnapshot() = %q", got)
	}
}

func TestTinkerSamplerBackendHotloadBaseThenDelta(t *testing.T) {
	ctx := context.Background()
	var calls []HotloadAndWaitOptions
	var identities []string
	backend := &TinkerSamplerBackend{
		DeploymentID: "dep-1",
		BaseModel:    "accounts/acct/models/base",
		LoraRank:     0,
		HotloadAndWait: func(_ context.Context, deploymentID, baseModel, snapshotIdentity string, opts ...HotloadAndWaitOptions) (bool, error) {
			if deploymentID != "dep-1" || baseModel != "accounts/acct/models/base" {
				t.Fatalf("hotload args = %q %q", deploymentID, baseModel)
			}
			identities = append(identities, snapshotIdentity)
			calls = append(calls, opts[0])
			return true, nil
		},
	}

	backend.RememberSavedSnapshot("snap-base", "base")
	ok, err := backend.HotloadSavedSnapshot(ctx, "snap-base")
	if err != nil || !ok {
		t.Fatalf("base hotload ok=%v err=%v", ok, err)
	}
	if calls[0].IncrementalSnapshotMetadata != nil {
		t.Fatalf("base metadata = %#v", calls[0].IncrementalSnapshotMetadata)
	}

	backend.RememberSavedSnapshot("snap-delta", "delta")
	ok, err = backend.HotloadSavedSnapshot(ctx, "snap-delta")
	if err != nil || !ok {
		t.Fatalf("delta hotload ok=%v err=%v", ok, err)
	}
	meta := calls[1].IncrementalSnapshotMetadata
	if meta["previous_snapshot_identity"] != "snap-base" || meta["compression_format"] != DefaultDeltaCompression || meta["checksum_format"] != DefaultChecksumFormat {
		t.Fatalf("delta metadata = %#v", meta)
	}
	if calls[1].ResetPromptCache == nil || !*calls[1].ResetPromptCache {
		t.Fatalf("reset prompt cache = %#v", calls[1].ResetPromptCache)
	}
	if calls[1].RequestTimeout != HotloadTimeout || calls[1].Wait.Timeout != HotloadTimeout {
		t.Fatalf("timeouts = %#v", calls[1])
	}
	if strings.Join(identities, ",") != "snap-base,snap-delta" {
		t.Fatalf("identities = %v", identities)
	}
}

func TestTinkerSamplerBackendResetSnapshotChain(t *testing.T) {
	ctx := context.Background()
	var lastMetadata map[string]any
	backend := &TinkerSamplerBackend{
		DeploymentID: "dep-1",
		BaseModel:    "accounts/acct/models/base",
		HotloadAndWait: func(_ context.Context, _, _, _ string, opts ...HotloadAndWaitOptions) (bool, error) {
			lastMetadata = opts[0].IncrementalSnapshotMetadata
			return true, nil
		},
	}
	backend.RememberSavedSnapshot("snap-base", "base")
	if _, err := backend.HotloadSavedSnapshot(ctx, "snap-base"); err != nil {
		t.Fatal(err)
	}
	backend.RememberSavedSnapshot("snap-delta", "delta")
	if _, err := backend.HotloadSavedSnapshot(ctx, "snap-delta"); err != nil {
		t.Fatal(err)
	}
	if lastMetadata == nil {
		t.Fatal("expected incremental metadata before reset")
	}
	backend.ResetSnapshotChain()
	backend.RememberSavedSnapshot("snap-after-reattach", "delta")
	if _, err := backend.HotloadSavedSnapshot(ctx, "snap-after-reattach"); err != nil {
		t.Fatal(err)
	}
	if lastMetadata != nil {
		t.Fatalf("metadata after reset = %#v", lastMetadata)
	}
}

func TestTinkerSamplerBackendLoraNeverIncremental(t *testing.T) {
	ctx := context.Background()
	var lastMetadata map[string]any
	backend := &TinkerSamplerBackend{
		DeploymentID: "dep-1",
		BaseModel:    "accounts/acct/models/base",
		LoraRank:     8,
		HotloadAndWait: func(_ context.Context, _, _, _ string, opts ...HotloadAndWaitOptions) (bool, error) {
			lastMetadata = opts[0].IncrementalSnapshotMetadata
			return true, nil
		},
	}
	backend.RememberSavedSnapshot("snap-a", "base")
	if _, err := backend.HotloadSavedSnapshot(ctx, "snap-a"); err != nil {
		t.Fatal(err)
	}
	backend.RememberSavedSnapshot("snap-b", "delta")
	if _, err := backend.HotloadSavedSnapshot(ctx, "snap-b"); err != nil {
		t.Fatal(err)
	}
	if lastMetadata != nil {
		t.Fatalf("LoRA metadata = %#v", lastMetadata)
	}
}

func TestTinkerSamplerBackendGetSamplingClient(t *testing.T) {
	mgr := NewDeploymentManager("fw-key", "https://api.example.com", WithDeploymentInferenceURL("https://inference.example.com"))
	mgr.SetAccountID("acct")
	backend := &TinkerSamplerBackend{
		DeployMgr:    mgr,
		DeploymentID: "dep-1",
		BaseModel:    "accounts/acct/models/base",
	}
	client, err := backend.GetSamplingClient(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("GetSamplingClient() error = %v", err)
	}
	if client.DeploymentSampler.Model != "accounts/acct/deployments/dep-1" {
		t.Fatalf("model = %q", client.DeploymentSampler.Model)
	}
	if client.DeploymentSampler.InferenceURL != "https://inference.example.com" || client.DeploymentSampler.APIKey != "fw-key" {
		t.Fatalf("sampler = %#v", client.DeploymentSampler)
	}
}

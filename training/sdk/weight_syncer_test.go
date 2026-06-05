package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeSamplerSaver struct {
	results []SaveSamplerResult
	err     error
	calls   []SaveWeightsForSamplerOptions
	names   []string
}

func (s *fakeSamplerSaver) SaveWeightsForSamplerExt(_ context.Context, name string, opts SaveWeightsForSamplerOptions) (SaveSamplerResult, error) {
	s.names = append(s.names, name)
	s.calls = append(s.calls, opts)
	if s.err != nil {
		return SaveSamplerResult{}, s.err
	}
	if len(s.results) == 0 {
		return SaveSamplerResult{Path: name, SnapshotName: name}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

func newWeightSyncerForTest(saver *fakeSamplerSaver) *WeightSyncer {
	mgr := NewDeploymentManager("test-key", "https://api.example.com", WithDeploymentInferenceURL("https://inference.example.com"))
	mgr.SetAccountID("test-acct")
	syncer := NewWeightSyncer(WeightSyncerConfig{
		PolicyClient: saver,
		DeployMgr:    mgr,
		DeploymentID: "dep-1",
		BaseModel:    "accounts/test/models/m",
	})
	syncer.CheckStatus = func(context.Context, string, string) (map[string]any, error) {
		return map[string]any{
			"replicas": []any{
				map[string]any{
					"current_snapshot_identity": nil,
					"readiness":                 true,
					"loading_state":             map[string]any{"stage": "ready"},
				},
			},
		}, nil
	}
	return syncer
}

func TestWeightSyncerBuildIncrementalMetadata(t *testing.T) {
	syncer := NewWeightSyncer(WeightSyncerConfig{})
	if got := syncer.BuildIncrementalMetadata("base"); got != nil {
		t.Fatalf("base metadata = %#v", got)
	}
	syncer.BaseIdentity = "snap-prev"
	meta := syncer.BuildIncrementalMetadata("delta")
	if meta["previous_snapshot_identity"] != "snap-prev" || meta["compression_format"] != DefaultDeltaCompression || meta["checksum_format"] != DefaultChecksumFormat {
		t.Fatalf("delta metadata = %#v", meta)
	}
	syncer.BaseIdentity = ""
	if got := syncer.BuildIncrementalMetadata("delta"); got != nil {
		t.Fatalf("delta without base metadata = %#v", got)
	}
}

func TestWeightSyncerMarkFirstSaveDone(t *testing.T) {
	syncer := NewWeightSyncer(WeightSyncerConfig{})
	syncer.MarkFirstSaveDone()
	syncer.MarkFirstSaveDone()
	if !syncer.BaseSaved || syncer.BaseIdentity != "" {
		t.Fatalf("syncer = %#v", syncer)
	}
}

func TestWeightSyncerEnsureDeploymentChecked(t *testing.T) {
	syncer := newWeightSyncerForTest(&fakeSamplerSaver{})
	calls := 0
	syncer.BaseIdentity = "old-snap"
	syncer.CheckStatus = func(context.Context, string, string) (map[string]any, error) {
		calls++
		return map[string]any{
			"replicas": []any{
				map[string]any{
					"current_snapshot_identity": "stale-snap",
					"readiness":                 true,
					"loading_state":             map[string]any{"stage": "done"},
				},
			},
		}, nil
	}

	if err := syncer.EnsureDeploymentChecked(context.Background()); err != nil {
		t.Fatalf("EnsureDeploymentChecked() error = %v", err)
	}
	if !syncer.DeploymentChecked || syncer.BaseIdentity != "" {
		t.Fatalf("syncer = %#v", syncer)
	}
	if err := syncer.EnsureDeploymentChecked(context.Background()); err != nil {
		t.Fatalf("EnsureDeploymentChecked() second error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("check status calls = %d", calls)
	}
}

func TestWeightSyncerWaitForHotloadReadyTimeout(t *testing.T) {
	syncer := newWeightSyncerForTest(&fakeSamplerSaver{})
	now := time.Unix(0, 0)
	syncer.Now = func() time.Time { return now }
	syncer.Sleep = func(d time.Duration) { now = now.Add(d) }
	syncer.CheckStatus = func(context.Context, string, string) (map[string]any, error) {
		return map[string]any{"replicas": []any{}}, nil
	}
	err := syncer.WaitForHotloadReady(context.Background(), 15*time.Second, 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("err = %v", err)
	}
}

func TestWeightSyncerGetSamplingClient(t *testing.T) {
	syncer := newWeightSyncerForTest(&fakeSamplerSaver{})
	client, err := syncer.GetSamplingClient(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetSamplingClient() error = %v", err)
	}
	if client.DeploymentSampler.Model != "accounts/test-acct/deployments/dep-1" {
		t.Fatalf("model = %q", client.DeploymentSampler.Model)
	}
	if client.DeploymentSampler.InferenceURL != "https://inference.example.com" || client.DeploymentSampler.APIKey != "test-key" {
		t.Fatalf("sampler = %#v", client.DeploymentSampler)
	}
}

func TestWeightSyncerSaveAndHotloadDeltaChain(t *testing.T) {
	saver := &fakeSamplerSaver{
		results: []SaveSamplerResult{
			{Path: "p1", SnapshotName: "step-0-base-sess"},
			{Path: "p2", SnapshotName: "step-1-sess"},
			{Path: "p3", SnapshotName: "step-2-sess"},
		},
	}
	syncer := newWeightSyncerForTest(saver)
	syncer.DeploymentChecked = true
	var calls []HotloadAndWaitOptions
	var identities []string
	syncer.HotloadAndWait = func(_ context.Context, deploymentID, baseModel, snapshotIdentity string, opts ...HotloadAndWaitOptions) (bool, error) {
		if deploymentID != "dep-1" || baseModel != "accounts/test/models/m" {
			t.Fatalf("hotload args = %q %q", deploymentID, baseModel)
		}
		identities = append(identities, snapshotIdentity)
		calls = append(calls, opts[0])
		return true, nil
	}
	var warmups []string
	syncer.Warmup = func(_ context.Context, model string, opts WarmupOptions) bool {
		warmups = append(warmups, model)
		if opts.MaxRetries != 10 || opts.RetryInterval != 10*time.Second {
			t.Fatalf("warmup opts = %#v", opts)
		}
		return true
	}

	snapshot, err := syncer.SaveAndHotload(context.Background(), "step-0-base")
	if err != nil || snapshot != "step-0-base-sess" {
		t.Fatalf("first save snapshot=%q err=%v", snapshot, err)
	}
	if !syncer.BaseSaved || syncer.BaseIdentity != "step-0-base-sess" {
		t.Fatalf("syncer after base = %#v", syncer)
	}
	if saver.calls[0].CheckpointType != "base" || calls[0].IncrementalSnapshotMetadata != nil {
		t.Fatalf("base save/hotload calls = %#v %#v", saver.calls[0], calls[0])
	}

	snapshot, err = syncer.SaveAndHotload(context.Background(), "step-1")
	if err != nil || snapshot != "step-1-sess" {
		t.Fatalf("second save snapshot=%q err=%v", snapshot, err)
	}
	meta := calls[1].IncrementalSnapshotMetadata
	if saver.calls[1].CheckpointType != "delta" || meta["previous_snapshot_identity"] != "step-0-base-sess" {
		t.Fatalf("delta calls = %#v %#v", saver.calls[1], meta)
	}
	if syncer.BaseIdentity != "step-1-sess" {
		t.Fatalf("base identity = %q", syncer.BaseIdentity)
	}

	snapshot, err = syncer.SaveAndHotload(context.Background(), "step-2")
	if err != nil || snapshot != "step-2-sess" {
		t.Fatalf("third save snapshot=%q err=%v", snapshot, err)
	}
	meta = calls[2].IncrementalSnapshotMetadata
	if meta["previous_snapshot_identity"] != "step-1-sess" {
		t.Fatalf("third metadata = %#v", meta)
	}
	if strings.Join(identities, ",") != "step-0-base-sess,step-1-sess,step-2-sess" {
		t.Fatalf("identities = %v", identities)
	}
	if len(warmups) != 3 || warmups[0] != "accounts/test-acct/deployments/dep-1" {
		t.Fatalf("warmups = %v", warmups)
	}
	for _, key := range []string{"save_time_s", "hotload_time_s", "warmup_time_s", "total_time_s"} {
		if _, ok := syncer.LastTiming[key]; !ok {
			t.Fatalf("timing missing %q: %#v", key, syncer.LastTiming)
		}
	}
}

func TestWeightSyncerResetDeltaChainForcesBase(t *testing.T) {
	saver := &fakeSamplerSaver{
		results: []SaveSamplerResult{
			{Path: "p1", SnapshotName: "t1-base-sess"},
			{Path: "p2", SnapshotName: "t2-base-sess"},
		},
	}
	syncer := newWeightSyncerForTest(saver)
	syncer.DeploymentChecked = true
	var calls []HotloadAndWaitOptions
	syncer.HotloadAndWait = func(_ context.Context, _, _, _ string, opts ...HotloadAndWaitOptions) (bool, error) {
		calls = append(calls, opts[0])
		return true, nil
	}
	syncer.WarmupAfterHotload = false

	if _, err := syncer.SaveAndHotload(context.Background(), "t1-base"); err != nil {
		t.Fatal(err)
	}
	syncer.ResetDeltaChain()
	if _, err := syncer.SaveAndHotload(context.Background(), "t2-base"); err != nil {
		t.Fatal(err)
	}
	if saver.calls[1].CheckpointType != "base" || calls[1].IncrementalSnapshotMetadata != nil {
		t.Fatalf("after reset calls = %#v %#v", saver.calls[1], calls[1])
	}
}

func TestWeightSyncerSaveOnlyThenHotloadUsesSnapshotIdentity(t *testing.T) {
	saver := &fakeSamplerSaver{
		results: []SaveSamplerResult{{Path: "storage-path", SnapshotName: "step-0-base-sess"}},
	}
	syncer := newWeightSyncerForTest(saver)
	syncer.DeploymentChecked = true
	var called bool
	syncer.HotloadAndWait = func(_ context.Context, deploymentID, baseModel, snapshotIdentity string, opts ...HotloadAndWaitOptions) (bool, error) {
		called = true
		if snapshotIdentity != "step-0-base-sess" {
			t.Fatalf("snapshot identity = %q", snapshotIdentity)
		}
		if opts[0].Path != "" {
			t.Fatalf("path = %q", opts[0].Path)
		}
		return true, nil
	}
	syncer.WarmupAfterHotload = false

	snapshot, err := syncer.SaveOnly(context.Background(), "step-0-base", "base")
	if err != nil || snapshot != "step-0-base-sess" {
		t.Fatalf("SaveOnly() snapshot=%q err=%v", snapshot, err)
	}
	if called {
		t.Fatal("hotload called during SaveOnly")
	}
	ok, err := syncer.Hotload(context.Background(), snapshot, "base")
	if err != nil || !ok {
		t.Fatalf("Hotload() ok=%v err=%v", ok, err)
	}
	if !called {
		t.Fatal("hotload was not called")
	}
}

func TestWeightSyncerLoraAlwaysBase(t *testing.T) {
	saver := &fakeSamplerSaver{
		results: []SaveSamplerResult{
			{Path: "p1", SnapshotName: "snap-a"},
			{Path: "p2", SnapshotName: "snap-b"},
		},
	}
	syncer := newWeightSyncerForTest(saver)
	syncer.DeploymentChecked = true
	syncer.LoraRank = 8
	var lastMetadata map[string]any
	syncer.HotloadAndWait = func(_ context.Context, _, _, _ string, opts ...HotloadAndWaitOptions) (bool, error) {
		lastMetadata = opts[0].IncrementalSnapshotMetadata
		return true, nil
	}
	syncer.WarmupAfterHotload = false

	if _, err := syncer.SaveAndHotload(context.Background(), "snap-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := syncer.SaveAndHotload(context.Background(), "snap-b"); err != nil {
		t.Fatal(err)
	}
	if saver.calls[0].CheckpointType != "base" || saver.calls[1].CheckpointType != "base" || lastMetadata != nil {
		t.Fatalf("lora calls = %#v metadata=%#v", saver.calls, lastMetadata)
	}
}

func TestWeightSyncerHotloadFalseUsesDetailedError(t *testing.T) {
	syncer := newWeightSyncerForTest(&fakeSamplerSaver{})
	syncer.DeploymentChecked = true
	syncer.DeployMgr.LastHotloadErrorMessage = "detailed status"
	syncer.HotloadAndWait = func(context.Context, string, string, string, ...HotloadAndWaitOptions) (bool, error) {
		return false, nil
	}
	_, err := syncer.Hotload(context.Background(), "snap", "base")
	if err == nil || !strings.Contains(err.Error(), "detailed status") {
		t.Fatalf("err = %v", err)
	}
}

func TestWeightSyncerSaveError(t *testing.T) {
	syncer := newWeightSyncerForTest(&fakeSamplerSaver{err: errors.New("save failed")})
	_, err := syncer.SaveAndHotload(context.Background(), "step")
	if err == nil || !strings.Contains(err.Error(), "save failed") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := syncer.LastTiming["total_time_s"]; !ok {
		t.Fatalf("timing missing total_time_s: %#v", syncer.LastTiming)
	}
}

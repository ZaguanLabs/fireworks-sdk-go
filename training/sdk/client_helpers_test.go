package sdk

import (
	"strconv"
	"strings"
	"testing"
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

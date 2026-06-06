package sdk

import (
	"testing"
	"time"
)

func TestLazyTrainingRunsResponse(t *testing.T) {
	run := TrainingRunMetadata{
		TrainingRunID: "firetitan-managed",
		BaseModel:     "accounts/acct/models/base",
		LastRequest:   time.Unix(1, 0),
	}
	got := LazyTrainingRunsResponse(run, 20, 5)
	if len(got.TrainingRuns) != 1 || got.TrainingRuns[0].TrainingRunID != "firetitan-managed" {
		t.Fatalf("response = %#v", got)
	}
	if got.Cursor.Limit != 20 || got.Cursor.Offset != 5 || got.Cursor.TotalCount != 0 {
		t.Fatalf("cursor = %#v", got.Cursor)
	}
}

func TestLazyManagedServerCapabilities(t *testing.T) {
	got := LazyManagedServerCapabilities(&FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"})
	if len(got.SupportedModels) != 1 || got.SupportedModels[0].ModelName != "accounts/acct/models/base" {
		t.Fatalf("capabilities = %#v", got)
	}
	empty := LazyManagedServerCapabilities(nil)
	if len(empty.SupportedModels) != 0 {
		t.Fatalf("empty capabilities = %#v", empty)
	}
}

func TestLazyCheckpointsListResponse(t *testing.T) {
	got := LazyCheckpointsListResponse(100, 0)
	if len(got.Checkpoints) != 0 {
		t.Fatalf("checkpoints = %#v", got.Checkpoints)
	}
	if got.Cursor.Limit != 100 || got.Cursor.Offset != 0 {
		t.Fatalf("cursor = %#v", got.Cursor)
	}
}

func TestLazySessionResponses(t *testing.T) {
	session := LazyGetSessionResponse()
	if len(session.TrainingRunIDs) != 0 || len(session.SamplerIDs) != 0 {
		t.Fatalf("session = %#v", session)
	}
	sessions := LazyListSessionsResponse()
	if len(sessions.Sessions) != 0 {
		t.Fatalf("sessions = %#v", sessions)
	}
}

func TestLazyGetSamplerResponse(t *testing.T) {
	got := LazyGetSamplerResponse("sampler-1", FiretitanProvisioningConfig{BaseModel: "accounts/acct/models/base"})
	if got.SamplerID != "sampler-1" || got.BaseModel != "accounts/acct/models/base" {
		t.Fatalf("sampler = %#v", got)
	}
	if got.ModelPath != nil {
		t.Fatalf("model path = %#v", got.ModelPath)
	}
}

func TestLazyManagedDeleteCheckpointNoop(t *testing.T) {
	LazyManagedDeleteCheckpoint()
}

package sdk

type TrainingRunsResponse struct {
	TrainingRuns []TrainingRunMetadata
	Cursor       Cursor
}

type CheckpointsListResponse struct {
	Checkpoints []map[string]any
	Cursor      Cursor
}

type GetSessionResponse struct {
	TrainingRunIDs []string
	SamplerIDs     []string
}

type ListSessionsResponse struct {
	Sessions []map[string]any
}

type GetSamplerResponse struct {
	SamplerID string
	BaseModel string
	ModelPath *string
}

func LazyTrainingRunsResponse(run TrainingRunMetadata, limit, offset int) TrainingRunsResponse {
	return TrainingRunsResponse{
		TrainingRuns: []TrainingRunMetadata{run},
		Cursor:       EmptyCursor(limit, offset),
	}
}

func LazyCheckpointsListResponse(limit, offset int) CheckpointsListResponse {
	return CheckpointsListResponse{
		Checkpoints: []map[string]any{},
		Cursor:      EmptyCursor(limit, offset),
	}
}

func LazyGetSessionResponse() GetSessionResponse {
	return GetSessionResponse{
		TrainingRunIDs: []string{},
		SamplerIDs:     []string{},
	}
}

func LazyListSessionsResponse() ListSessionsResponse {
	return ListSessionsResponse{Sessions: []map[string]any{}}
}

func LazyGetSamplerResponse(samplerID string, config FiretitanProvisioningConfig) GetSamplerResponse {
	return GetSamplerResponse{
		SamplerID: samplerID,
		BaseModel: config.BaseModel,
		ModelPath: nil,
	}
}

func LazyManagedDeleteCheckpoint() {}

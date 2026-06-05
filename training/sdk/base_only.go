package sdk

type BaseOnlyCreateModelRequest struct {
	SessionID    string            `json:"session_id"`
	ModelSeqID   int               `json:"model_seq_id"`
	BaseModel    string            `json:"base_model"`
	BaseOnly     bool              `json:"base_only"`
	UserMetadata map[string]string `json:"user_metadata,omitempty"`
}

func NewBaseOnlyCreateModelRequest(sessionID string, modelSeqID int, baseModel string, userMetadata map[string]string) BaseOnlyCreateModelRequest {
	return BaseOnlyCreateModelRequest{
		SessionID:    sessionID,
		ModelSeqID:   modelSeqID,
		BaseModel:    baseModel,
		BaseOnly:     true,
		UserMetadata: cloneStringMap(userMetadata),
	}
}

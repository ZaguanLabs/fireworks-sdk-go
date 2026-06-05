package sdk

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
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

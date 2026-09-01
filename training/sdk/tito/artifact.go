package tito

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
)

var artifactMagic = []byte{'T', 'I', 'T', 'O', 'A', 'R', 'T', 1}

// Pack encodes an artifact into the deterministic v1 TITO wire format.
func (a TrajectoryArtifact) Pack() ([]byte, error) {
	a.SchemaVersion = 1
	plain, err := jsonRoundTrip(a)
	if err != nil {
		return nil, err
	}
	encoded, err := canonicalJSON(plain)
	if err != nil {
		return nil, err
	}
	var result bytes.Buffer
	result.Write(artifactMagic)
	writer, err := zlib.NewWriterLevel(&result, 6)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(encoded); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

// UnpackTrajectoryArtifact decodes and validates a v1 artifact.
func UnpackTrajectoryArtifact(payload []byte) (TrajectoryArtifact, error) {
	if !bytes.HasPrefix(payload, artifactMagic) {
		return TrajectoryArtifact{}, &Error{Code: "tito_artifact_invalid", Status: 400, Message: "invalid TITO trajectory artifact: missing TITO artifact magic/version"}
	}
	reader, err := zlib.NewReader(bytes.NewReader(payload[len(artifactMagic):]))
	if err != nil {
		return TrajectoryArtifact{}, artifactError(err)
	}
	decoded, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return TrajectoryArtifact{}, artifactError(readErr)
	}
	if closeErr != nil {
		return TrajectoryArtifact{}, artifactError(closeErr)
	}
	var artifact TrajectoryArtifact
	if err := json.Unmarshal(decoded, &artifact); err != nil {
		return TrajectoryArtifact{}, artifactError(err)
	}
	if artifact.SchemaVersion != 1 {
		return TrajectoryArtifact{}, artifactError(fmt.Errorf("unsupported TITO artifact schema: %d", artifact.SchemaVersion))
	}
	return artifact, nil
}

func jsonRoundTrip(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func artifactError(err error) error {
	return &Error{Code: "tito_artifact_invalid", Status: 400, Message: "invalid TITO trajectory artifact: " + err.Error()}
}

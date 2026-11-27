package tito

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
)

var artifactMagic = []byte{'T', 'I', 'T', 'O', 'A', 'R', 'T', 1}

// Pack writes the schema-2 tree artifact. Historical inline replay samples
// must be recaptured before they can be written in this format.
func (a TrajectoryArtifact) Pack() ([]byte, error) {
	if a.LineageMode != "" && a.LineageMode != "tree" {
		return nil, artifactError(fmt.Errorf("unsupported lineage %q", a.LineageMode))
	}
	if a.SchemaVersion == 1 {
		return nil, artifactError(fmt.Errorf("schema 1 lacks tree lineage; recapture this rollout"))
	}
	a.SchemaVersion, a.LineageMode = 2, "tree"
	if err := validateTreeEvidence(a); err != nil {
		return nil, err
	}
	if a.Segments == nil {
		a.Segments = []SegmentResult{}
	}
	if a.Calls == nil {
		a.Calls = []CallRecord{}
	}
	if a.ResponseAttempts == nil {
		a.ResponseAttempts = []ResponseAttempt{}
	}
	if a.Metrics.Counters == nil {
		a.Metrics.Counters = map[string]int{}
	}
	if a.Metrics.Distributions == nil {
		a.Metrics.Distributions = map[string]Distribution{}
	}
	plain, err := jsonRoundTrip(a)
	if err != nil {
		return nil, err
	}
	var result bytes.Buffer
	result.Write(artifactMagic)
	writer, err := zlib.NewWriterLevel(&result, 6)
	if err != nil {
		return nil, err
	}
	stream := bufio.NewWriter(writer)
	if err := writeCanonicalJSON(stream, plain); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := stream.Flush(); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

func validateTreeEvidence(a TrajectoryArtifact) error {
	for _, segment := range a.Segments {
		for _, turn := range segment.Turns {
			if turn.RoutingMatrices != nil || turn.PromptRoutingMatrices != nil || turn.InferenceTopKTokenIDs != nil || turn.InferenceTopKLogprobs != nil {
				return artifactError(fmt.Errorf("inline replay data is unsupported; recapture using trainer-bound storage"))
			}
		}
	}
	return nil
}

// UnpackTrajectoryArtifact reads tree artifacts and existing Go schema-1 archives.
func UnpackTrajectoryArtifact(payload []byte) (TrajectoryArtifact, error) {
	return UnpackTrajectoryStream(bytes.NewReader(payload), int64(len(payload)), "", math.MaxInt64-1)
}

// UnpackTrajectoryStream verifies compressed length, framing, an optional
// lowercase SHA-256, and the caller's decompression limit before returning.
func UnpackTrajectoryStream(src io.Reader, expectedBytes int64, expectedSHA256 string, maxExpandedBytes int64) (TrajectoryArtifact, error) {
	fail := func(err error) (TrajectoryArtifact, error) { return TrajectoryArtifact{}, artifactError(err) }
	if src == nil || expectedBytes <= int64(len(artifactMagic)) || expectedBytes == math.MaxInt64 || maxExpandedBytes < 1 || maxExpandedBytes == math.MaxInt64 {
		return fail(fmt.Errorf("invalid artifact size"))
	}
	if expectedSHA256 != "" {
		if _, err := hex.DecodeString(expectedSHA256); err != nil || len(expectedSHA256) != 64 || strings.ToLower(expectedSHA256) != expectedSHA256 {
			return fail(fmt.Errorf("invalid SHA-256 checksum"))
		}
	}
	meter := &io.LimitedReader{R: src, N: expectedBytes + 1}
	var compressed io.Reader = meter
	h := sha256.New()
	if expectedSHA256 != "" {
		compressed = io.TeeReader(meter, h)
	}
	buffer := bufio.NewReader(compressed)
	var header [8]byte
	if _, err := io.ReadFull(buffer, header[:]); err != nil || !bytes.Equal(header[:], artifactMagic) {
		return fail(fmt.Errorf("missing TITO artifact magic/version"))
	}
	zr, err := zlib.NewReader(buffer)
	if err != nil {
		return fail(err)
	}
	defer zr.Close()
	expanded := &io.LimitedReader{R: zr, N: maxExpandedBytes + 1}
	validator := &utf8Validator{}
	decoder := json.NewDecoder(io.TeeReader(expanded, validator))
	a, err := readArtifact(decoder)
	if err != nil {
		return fail(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fail(fmt.Errorf("invalid trailing JSON or compression checksum"))
	}
	if validator.remaining != 0 {
		return fail(fmt.Errorf("truncated UTF-8 in TITO artifact"))
	}
	if expanded.N == 0 {
		return fail(fmt.Errorf("expanded artifact exceeds limit"))
	}
	if _, err := buffer.ReadByte(); err != io.EOF || meter.N != 1 {
		return fail(fmt.Errorf("compressed size mismatch or trailing bytes"))
	}
	if expectedSHA256 != "" && hex.EncodeToString(h.Sum(nil)) != expectedSHA256 {
		return fail(fmt.Errorf("artifact checksum mismatch"))
	}

	switch {
	case a.SchemaVersion == 1 && a.LineageMode == "": // legacy Go artifact
	case a.SchemaVersion == 2 && a.LineageMode == "tree":
		if err := validateTreeEvidence(a); err != nil {
			return TrajectoryArtifact{}, err
		}
	default:
		return fail(fmt.Errorf("unsupported TITO schema %d or lineage %q", a.SchemaVersion, a.LineageMode))
	}
	return a, nil
}

func jsonRoundTrip(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func artifactError(err error) error {
	return &Error{Code: "tito_artifact_invalid", Status: 400, Message: "invalid TITO trajectory artifact: " + err.Error()}
}

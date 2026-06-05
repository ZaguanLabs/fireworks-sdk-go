package sdk

import (
	"fmt"
	"strings"
)

const (
	DefaultDeltaCompression = "arc_v2"
	DefaultChecksumFormat   = "alder32"
)

type SamplerCheckpointType string

const (
	SamplerCheckpointTypeBase  SamplerCheckpointType = "base"
	SamplerCheckpointTypeDelta SamplerCheckpointType = "delta"
)

func NormalizeCheckpointType(checkpointType string) (SamplerCheckpointType, error) {
	if checkpointType == "" {
		return "", nil
	}
	normalized := SamplerCheckpointType(strings.ToLower(checkpointType))
	switch normalized {
	case SamplerCheckpointTypeBase, SamplerCheckpointTypeDelta:
		return normalized, nil
	default:
		return "", fmt.Errorf("checkpoint_type must be either 'base' or 'delta'")
	}
}

func ResolveNextCheckpointType(loraRank int, baseSaved bool, firstCheckpointType string, explicit ...string) (SamplerCheckpointType, error) {
	overrideValue := ""
	if len(explicit) > 0 {
		overrideValue = explicit[0]
	}
	override, err := NormalizeCheckpointType(overrideValue)
	if err != nil {
		return "", err
	}
	if override != "" {
		return override, nil
	}
	if loraRank > 0 {
		return SamplerCheckpointTypeBase, nil
	}
	if baseSaved {
		return SamplerCheckpointTypeDelta, nil
	}
	first, err := NormalizeCheckpointType(firstCheckpointType)
	if err != nil {
		return "", err
	}
	if first == "" {
		return SamplerCheckpointTypeBase, nil
	}
	return first, nil
}

func BuildIncrementalMetadata(loraRank int, checkpointType string, baseIdentity string, compressionFormat string) map[string]any {
	if loraRank > 0 {
		return nil
	}
	if checkpointType == string(SamplerCheckpointTypeDelta) && baseIdentity != "" {
		return map[string]any{
			"previous_snapshot_identity": baseIdentity,
			"compression_format":         compressionFormat,
			"checksum_format":            DefaultChecksumFormat,
		}
	}
	return nil
}

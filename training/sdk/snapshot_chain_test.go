package sdk

import "testing"

func TestNormalizeCheckpointTypeEmptyPassthrough(t *testing.T) {
	typ, err := NormalizeCheckpointType("")
	if err != nil {
		t.Fatal(err)
	}
	if typ != "" {
		t.Fatalf("checkpoint type = %q, want empty", typ)
	}
}

func TestNormalizeCheckpointTypeLowercases(t *testing.T) {
	tests := map[string]SamplerCheckpointType{
		"BASE":  SamplerCheckpointTypeBase,
		"Delta": SamplerCheckpointTypeDelta,
	}
	for input, want := range tests {
		got, err := NormalizeCheckpointType(input)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("NormalizeCheckpointType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeCheckpointTypeRejectsUnknown(t *testing.T) {
	if _, err := NormalizeCheckpointType("full"); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveNextCheckpointTypeFullParamBaseThenDelta(t *testing.T) {
	got, err := ResolveNextCheckpointType(0, false, "base")
	if err != nil {
		t.Fatal(err)
	}
	if got != SamplerCheckpointTypeBase {
		t.Fatalf("checkpoint type = %q, want base", got)
	}

	got, err = ResolveNextCheckpointType(0, true, "base")
	if err != nil {
		t.Fatal(err)
	}
	if got != SamplerCheckpointTypeDelta {
		t.Fatalf("checkpoint type = %q, want delta", got)
	}
}

func TestResolveNextCheckpointTypeLoraAlwaysBase(t *testing.T) {
	got, err := ResolveNextCheckpointType(8, true, "base")
	if err != nil {
		t.Fatal(err)
	}
	if got != SamplerCheckpointTypeBase {
		t.Fatalf("checkpoint type = %q, want base", got)
	}
}

func TestResolveNextCheckpointTypeExplicitOverrideWins(t *testing.T) {
	got, err := ResolveNextCheckpointType(0, true, "base", "base")
	if err != nil {
		t.Fatal(err)
	}
	if got != SamplerCheckpointTypeBase {
		t.Fatalf("checkpoint type = %q, want base", got)
	}
}

func TestBuildIncrementalMetadataFullParamDeltaPinsPrevious(t *testing.T) {
	got := BuildIncrementalMetadata(0, "delta", "snap-1", DefaultDeltaCompression)
	if got == nil {
		t.Fatal("expected metadata")
	}
	want := map[string]any{
		"previous_snapshot_identity": "snap-1",
		"compression_format":         DefaultDeltaCompression,
		"checksum_format":            DefaultChecksumFormat,
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("metadata[%q] = %v, want %v", key, got[key], wantValue)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("metadata = %#v, want %#v", got, want)
	}
}

func TestBuildIncrementalMetadataBaseHasNoMetadata(t *testing.T) {
	if got := BuildIncrementalMetadata(0, "base", "snap-1", DefaultDeltaCompression); got != nil {
		t.Fatalf("metadata = %#v, want nil", got)
	}
}

func TestBuildIncrementalMetadataDeltaWithoutBaseIdentityHasNoMetadata(t *testing.T) {
	if got := BuildIncrementalMetadata(0, "delta", "", DefaultDeltaCompression); got != nil {
		t.Fatalf("metadata = %#v, want nil", got)
	}
}

func TestBuildIncrementalMetadataLoraNeverSendsMetadata(t *testing.T) {
	if got := BuildIncrementalMetadata(8, "delta", "snap-1", DefaultDeltaCompression); got != nil {
		t.Fatalf("metadata = %#v, want nil", got)
	}
}

func TestPythonCompatibleCheckpointConstants(t *testing.T) {
	if DEFAULT_CHECKSUM_FORMAT != DefaultChecksumFormat {
		t.Fatalf("DEFAULT_CHECKSUM_FORMAT = %q, want %q", DEFAULT_CHECKSUM_FORMAT, DefaultChecksumFormat)
	}
	if DEFAULT_DELTA_COMPRESSION != DefaultDeltaCompression {
		t.Fatalf("DEFAULT_DELTA_COMPRESSION = %q, want %q", DEFAULT_DELTA_COMPRESSION, DefaultDeltaCompression)
	}
}

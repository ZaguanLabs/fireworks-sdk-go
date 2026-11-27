package tito

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"testing"
)

func TestInlineTurnRoutingOnlyReadableInGoV1Archives(t *testing.T) {
	const turn = `{"routing_matrices":["legacy"],"prompt_routing_matrices":["prefix"]}`
	if err := json.Unmarshal([]byte(turn), new(Turn)); err == nil {
		t.Fatal("accepted inline routing outside a v1 archive")
	}
	if _, err := json.Marshal(Turn{PromptRoutingMatrices: []string{"legacy"}}); err == nil {
		t.Fatal("wrote inline routing to the current format")
	}
	for _, test := range []struct {
		schema string
		legacy bool
	}{
		{`"schema_version":1`, true},
		{`"schema_version":2,"lineage_mode":"tree"`, false},
	} {
		var archive bytes.Buffer
		archive.Write(artifactMagic)
		w := zlib.NewWriter(&archive)
		if _, err := w.Write([]byte(`{"segments":[{"turns":[` + turn + `]}],` + test.schema + `}`)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		decoded, err := UnpackTrajectoryArtifact(archive.Bytes())
		if !test.legacy {
			if err == nil {
				t.Fatal("read inline rows from a schema-2 archive")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		got := decoded.Segments[0].Turns[0]
		if len(got.RoutingMatrices) != 1 || got.RoutingMatrices[0] != "legacy" || len(got.PromptRoutingMatrices) != 1 || got.PromptRoutingMatrices[0] != "prefix" {
			t.Fatal("lost historical rows during archive decode")
		}
	}
}

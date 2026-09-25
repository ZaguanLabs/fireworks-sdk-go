package tito

import (
	"context"
	"encoding/json"
	sdk "github.com/ZaguanLabs/fireworks-sdk-go/training/sdk"
	"reflect"
	"testing"
)

func TestArtifactRetainsCompactRoutingAndTopK(t *testing.T) {
	index, row := 0, 5
	refs := &sdk.RoutingReferences{Length: 2, Files: []sdk.RoutingFile{{"format": "parquet_v1", "store_id": "store", "row_count": 10}}, Spans: []sdk.RoutingSpan{{InputTokenStart: 0, Count: 2, FileIndex: &index, FileRowStart: &row}}}
	artifact := TrajectoryArtifact{Segments: []SegmentResult{{Turns: []Turn{{RoutingReferences: refs, PromptRoutingReferences: refs, PromptRoutingStart: &row, InferenceTopKTokenIDs: [][]int{{1, 2}, {3}}, InferenceTopKLogprobs: [][]float64{{-1, -2}, {-0.5}}}}}}}
	payload, err := artifact.Pack()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnpackTrajectoryArtifact(payload)
	if err != nil {
		t.Fatal(err)
	}
	turn := decoded.Segments[0].Turns[0]
	if turn.RoutingReferences == nil || turn.RoutingReferences.Length != 2 || turn.PromptRoutingReferences == nil || *turn.PromptRoutingStart != 5 || !reflect.DeepEqual(turn.InferenceTopKTokenIDs, [][]int{{1, 2}, {3}}) {
		t.Fatalf("turn %+v", turn)
	}
	var fields map[string]any
	data, _ := json.Marshal(turn)
	json.Unmarshal(data, &fields)
	if _, ok := fields["routing_matrices"].(map[string]any); !ok {
		t.Fatal(string(data))
	}
}
func TestAdmissionNormalizesEmptyRefusalWithoutMutatingWire(t *testing.T) {
	payload := map[string]any{"messages": []any{map[string]any{"role": "assistant", "content": "hi", "provider_specific_fields": map[string]any{"refusal": nil}}}}
	req, err := NewChatRequestFromOpenAI(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := req.Messages[0]["provider_specific_fields"]; ok {
		t.Fatal("marker retained")
	}
	if len(req.NormalizationSteps) != 1 {
		t.Fatal(req.NormalizationSteps)
	}
	if _, ok := payload["messages"].([]any)[0].(map[string]any)["provider_specific_fields"]; !ok {
		t.Fatal("caller mutated")
	}
	if _, ok := req.WireRequest["messages"].([]any)[0].(map[string]any)["provider_specific_fields"]; !ok {
		t.Fatal("wire lost")
	}
}

type incrementalRoutingSampler struct{}

func (incrementalRoutingSampler) SampleWithPromptTokensResult(_ context.Context, prompt []int, opts ...sdk.SampleOptions) (sdk.SampledRequestResult, error) {
	n := opts[0].Extra["echo_last"].(int)
	if n == len(prompt) {
		n--
	}
	routes := make([]string, n+1)
	lp := make([]float64, n+1)
	ids := make([][]int, n+1)
	tops := make([][]float64, n+1)
	for i := range routes {
		routes[i] = "route"
		lp[i] = -0.1
		ids[i] = []int{91}
		tops[i] = []float64{-0.1}
	}
	return sdk.SampledRequestResult{Completions: []sdk.SampledCompletion{{FullTokens: append(append([]int{}, prompt...), 91), PromptLen: len(prompt), CompletionLen: 1, Text: "answer", FinishReason: "stop", RoutingMatrices: routes, InferenceLogprobs: lp, InferenceTopKTokenIDs: ids, InferenceTopKLogprobs: tops, EchoedPromptLogprobCount: n}}}, nil
}
func TestIncrementalPromptRoutingSavedSeparately(t *testing.T) {
	sidecar, err := NewSidecar(incrementalRoutingSampler{}, fakeRenderer{}, SidecarOptions{MaxContextTokens: 32, MaxOutputTokens: 4, IncrementalPromptRouting: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := sidecar.Start(); err != nil {
		t.Fatal(err)
	}
	defer sidecar.Close(context.Background())
	if _, err := sidecar.CreateTrajectory("id", "affinity", nil); err != nil {
		t.Fatal(err)
	}
	state, err := sidecar.trajectoryFor("id")
	if err != nil {
		t.Fatal(err)
	}
	req, err := NewChatRequestFromOpenAI(map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sidecar.complete(context.Background(), state, req, ""); err != nil {
		t.Fatal(err)
	}
	artifact, err := sidecar.FinishTrajectory("id")
	if err != nil {
		t.Fatal(err)
	}
	turn := artifact.Segments[0].Turns[0]
	if len(turn.RoutingMatrices) != 1 || len(turn.PromptRoutingMatrices) != 1 || turn.PromptRoutingStart == nil || *turn.PromptRoutingStart != 0 || len(turn.InferenceTopKTokenIDs) != 1 {
		t.Fatalf("turn %+v", turn)
	}
}

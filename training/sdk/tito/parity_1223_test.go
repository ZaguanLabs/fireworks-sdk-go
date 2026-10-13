package tito

import (
	"context"
	"encoding/json"
	sdk "github.com/ZaguanLabs/fireworks-sdk-go/training/sdk"
	"strings"
	"testing"
)

func Test1223SidecarBinding(t *testing.T) {
	for _, ip := range []string{"0.0.0.0", "::", "224.0.0.1", "localhost"} {
		if _, err := NewSidecar(&fakeSampler{}, fakeRenderer{}, SidecarOptions{BindAddress: ip, MaxContextTokens: 32, MaxOutputTokens: 4}); err == nil {
			t.Fatal(ip)
		}
	}
	s, err := NewSidecar(&fakeSampler{}, fakeRenderer{}, SidecarOptions{BindAddress: "127.0.0.2", MaxContextTokens: 32, MaxOutputTokens: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	if !strings.HasPrefix(s.baseURL, "http://127.0.0.2:") {
		t.Fatal(s.baseURL)
	}
}
func Test1223ResponseArgumentsAndStreamIndices(t *testing.T) {
	canonical := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "c", "type": "function", "function": map[string]any{"name": "test", "arguments": `{"a":1,"b":2}`}}}}
	response, _ := cloneJSONMap(canonical)
	response["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"] = `{ "b": 2, "a": 1 }`
	turn := Turn{Assistant: ParsedAssistant{Message: canonical, ResponseMessage: response}, ResponseID: "response"}
	wire := openAIResponse(turn, "", "model")
	chunks := openAIStreamChunks(wire, nil)
	calls := chunks[0]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)
	if calls[0].(map[string]any)["index"] != 0 {
		t.Fatal(chunks)
	}
	origCalls := wire["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["tool_calls"].([]any)
	if _, ok := origCalls[0].(map[string]any)["index"]; ok {
		t.Fatal("stream mutated stored response")
	}
	if origCalls[0].(map[string]any)["function"].(map[string]any)["arguments"] != `{ "b": 2, "a": 1 }` {
		t.Fatal(wire)
	}
	payload := map[string]any{"messages": []any{chunks[0]["choices"].([]any)[0].(map[string]any)["delta"]}}
	request, err := NewChatRequestFromOpenAI(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := request.Messages[0]["tool_calls"].([]any)[0].(map[string]any)["index"]; ok {
		t.Fatal(request)
	}
	encoded, err := json.Marshal(turn.Assistant)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ParsedAssistant
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	response["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"] = `{"wrong":3}`
	encoded, _ = json.Marshal(ParsedAssistant{Message: canonical, ResponseMessage: response})
	if err := json.Unmarshal(encoded, &decoded); err == nil {
		t.Fatal("accepted divergent artifact assistant")
	}
}
func Test1223ArtifactTopSampling(t *testing.T) {
	refs := &sdk.RoutingReferences{Length: 0, Files: []sdk.RoutingFile{}, Spans: []sdk.RoutingSpan{}}
	artifact := TrajectoryArtifact{Segments: []SegmentResult{{Turns: []Turn{{TopSamplingReferences: refs}}}}}
	data, err := artifact.Pack()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnpackTrajectoryArtifact(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Segments[0].Turns[0].TopSamplingReferences == nil {
		t.Fatal(decoded)
	}
}

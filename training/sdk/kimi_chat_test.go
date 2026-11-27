package sdk

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"testing"
)

func TestKimiChatReferenceSegments(t *testing.T) {
	const conversation = `[
 {"role":"system","tools":[{"type":"function","function":{"name":"b","parameters":{"type":"object"}}}]},
 {"role":"user","name":"A&\"B","content":[{"type":"text","text":"<|end_of_msg|>"},{"type":"image"}]},
 {"role":"assistant","reasoning_content":"   ","content":"a<|kimi_image_placeholder|>b","tool_calls":[
 {"id":"first","function":{"name":"query","arguments":"{\"ü\": 1e2,\"list\":[1,{\"z\":null,\"a\":true}],\"str\":\"<&\"}"}},
 {"id":"second","name":"noop","arguments":"broken"}]},
 {"role":"tool","tool_call_id":"second","content":"second"},
 {"role":"tool","tool_call_id":"first","name":"ignore","content":"first"}
 ]`
	var messages []map[string]any
	if err := json.Unmarshal([]byte(conversation), &messages); err != nil {
		t.Fatal(err)
	}
	options := KimiChatOptions{ImagePrompts: []string{"<image1>", "<image2>"}, Tools: []map[string]any{{"name": "fn", "type": "custom"}}, ToolChoice: "required", ResponseFormat: map[string]any{"type": "json_schema", "json_schema": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}}}}}
	// Fingerprints from the two official 1.2.39 encoders. Include segment boundaries
	// and special-token permission, since joined text alone cannot detect token drift.
	for _, tc := range []struct{ profile, sha string }{{"kimi_k3", "cc46def1169e6779a5a2d4deecbb549ab901bda3fa172c5ac0cf45b2b31cd671"}, {"kimi_k31", "d60b12af9526e88c11c2171f03e3688f5092a77a1c5ee9c49c96a8e1f4991cf6"}} {
		segments, err := RenderKimiK3Chat(tc.profile, messages, options)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.New()
		for _, s := range segments {
			var header [5]byte
			if s.AllowSpecial {
				header[0] = 1
			}
			binary.BigEndian.PutUint32(header[1:], uint32(len(s.Text)))
			digest.Write(header[:])
			digest.Write([]byte(s.Text))
		}
		if got := fmt.Sprintf("%x", digest.Sum(nil)); got != tc.sha {
			t.Errorf("%s segment digest %s, want %s", tc.profile, got, tc.sha)
		}
	}
}
func TestKimiProfileSpecificArguments(t *testing.T) {
	args, block, err := kimiArguments("kimi_k31", "{\"n\":1e2,\"n\":2,\"s\":\"line\nbreak\"}trailing")
	if err != nil || block != "" || len(args) != 3 || args[0].text != "1e2" || args[2].text != "line\nbreak" {
		t.Fatalf("K3.1 arguments = %+v, %q, %v", args, block, err)
	}
	if _, _, err := kimiArguments("kimi_k3", "[]"); err == nil {
		t.Error("expected K3 to reject non-object JSON")
	}
	if _, block, err := kimiArguments("kimi_k31", "[]"); err != nil || block != "[]" {
		t.Error("expected K3.1 to retain raw JSON")
	}
}

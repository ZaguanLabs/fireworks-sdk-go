package sdk

import (
	"encoding/json"
	"testing"
)

func TestNewBaseOnlyCreateModelRequest(t *testing.T) {
	metadata := map[string]string{"purpose": "reference"}
	request := NewBaseOnlyCreateModelRequest("session-1", 7, "accounts/acct/models/base", metadata)
	if request.SessionID != "session-1" || request.ModelSeqID != 7 || request.BaseModel != "accounts/acct/models/base" {
		t.Fatalf("request = %#v", request)
	}
	if !request.BaseOnly {
		t.Fatalf("base_only = %t, want true", request.BaseOnly)
	}
	request.UserMetadata["purpose"] = "mutated"
	if metadata["purpose"] != "reference" {
		t.Fatalf("metadata was not cloned: %#v", metadata)
	}
}

func TestBaseOnlyCreateModelRequestSerializesBaseOnly(t *testing.T) {
	request := NewBaseOnlyCreateModelRequest("session-1", 1, "accounts/acct/models/base", nil)
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["base_only"] != true {
		t.Fatalf("decoded = %#v", decoded)
	}
	if _, ok := decoded["user_metadata"]; ok {
		t.Fatalf("empty metadata should be omitted: %#v", decoded)
	}
}

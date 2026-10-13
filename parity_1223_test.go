package fireworks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func Test1223CancellationEndpoints(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test")
	seen := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatal(r.Method)
		}
		seen = append(seen, r.URL.Path)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["reason"] != "stop" {
			t.Fatal(body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	c, err := NewClient(WithBaseURL(server.URL), WithDefaultAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	calls := []func(context.Context, string, any, ...RequestOption) (Response, error){c.DPOJobs.Cancel, c.SupervisedFineTuningJobs.CancelTyped, c.ReinforcementFineTuningSteps.Cancel}
	paths := []string{"dpoJobs", "supervisedFineTuningJobs", "reinforcementFineTuningSteps"}
	for i, call := range calls {
		if _, err := call(context.Background(), "id", map[string]any{"reason": "stop"}); err != nil {
			t.Fatal(err)
		}
		if seen[i] != "/v1/accounts/acct/"+paths[i]+"/id:cancel" {
			t.Fatal(seen)
		}
		if _, err := call(context.Background(), "", nil); err == nil {
			t.Fatal("accepted empty job ID")
		}
	}
}

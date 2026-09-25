package fireworks

import (
	"context"
	"encoding/json"
	fwtypes "github.com/ZaguanLabs/fireworks-sdk-go/types"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeploymentShapeMatchAndShapelessQuery(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/v1/accounts/acct/deploymentShapeVersions:match":
			if r.Method != "POST" {
				t.Error(r.Method)
			}
			req, ok := body["createDeploymentRequest"].(map[string]any)
			if !ok {
				t.Errorf("body: %v", body)
				return
			}
			deployment, _ := req["deployment"].(map[string]any)
			if req["parent"] != "accounts/acct" || deployment["baseModel"] != "accounts/models/base" || deployment["enableAddons"] != true {
				t.Error(body)
			}
			json.NewEncoder(w).Encode(map[string]any{"deploymentShapeVersions": []any{}})
		case "/v1/accounts/acct/deployments":
			if r.URL.Query().Get("acceptShapelessRisk") != "true" || body["acceptShapelessRisk"] != nil {
				t.Errorf("query %v body %v", r.URL.Query(), body)
			}
			json.NewEncoder(w).Encode(map[string]any{"name": "dep"})
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(WithBaseURL(server.URL), WithDefaultAccountID("acct"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.DeploymentShapeVersions.MatchForModel(context.Background(), "accounts/models/base", true); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DeploymentShapeVersions.MatchForModelTyped(context.Background(), "accounts/models/base", true); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Deployments.CreateTyped(context.Background(), fwtypes.DeploymentCreateParams{AcceptShapelessRisk: true}); err != nil {
		t.Fatal(err)
	}
}

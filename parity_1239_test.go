package fireworks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	fwtypes "github.com/ZaguanLabs/fireworks-sdk-go/types"
)

func TestWatermarkDetection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gateway/v1/watermark/detect" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		var params fwtypes.WatermarkDetectionCreateParams
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil || params.Text != "sample" {
			t.Errorf("text = %q, err = %v", params.Text, err)
		}
		json.NewEncoder(w).Encode(fwtypes.WatermarkDetection{Object: "watermark.detection", Result: "not_detected"})
	}))
	defer server.Close()
	client, err := NewClient(WithAPIKey("test"), WithBaseURL(server.URL+"/gateway"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.WatermarkDetections.CreateTyped(context.Background(), fwtypes.WatermarkDetectionCreateParams{Text: "sample"})
	if err != nil || got.Result != "not_detected" || got.Object != "watermark.detection" {
		t.Fatalf("detection = %+v, err = %v", got, err)
	}
}

func TestNumericTokenLogprobsPreserveNulls(t *testing.T) {
	var scores fwtypes.SharedChoiceTokenIdLogProbs
	if err := json.Unmarshal([]byte(`{"format":"token_ids","token_ids":[12,34],"token_logprobs":[null,0],"top_token_ids":[[],[]],"top_logprobs":[[],[]]}`), &scores); err != nil {
		t.Fatal(err)
	}
	if len(scores.TokenLogprobs) != 2 || scores.TokenLogprobs[0] != nil || scores.TokenLogprobs[1] == nil || *scores.TokenLogprobs[1] != 0 {
		t.Fatalf("scores = %+v", scores)
	}
}

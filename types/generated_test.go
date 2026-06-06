package types

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestGeneratedCatalogIncludesCoreTypes(t *testing.T) {
	_ = Account{}
	_ = Model{}
	_ = ChatCompletionCreateResponse{}
	_ = CompletionCreateParamsCompletionCreateParamsBase{}
	_ = MessageCreateParams{}
	_ = EvaluatorGetBuildLogEndpointResponse{}
	_ = SharedStatus{}
	_ = SharedParamsDeployedModelRef{}
	_ = DpoJob{}
	_ = ModelsPage{}
}

func TestDPOAliasesUsePublicNames(t *testing.T) {
	job := DPOJob{Name: strPtr("accounts/acct/dpoJobs/job-1")}
	var _ DpoJob = job

	create := DPOJobCreateParams{Dataset: "accounts/acct/datasets/ds-1"}
	var _ DpoJobCreateParams = create

	get := DPOJobGetParams{ReadMask: "name"}
	var _ DpoJobGetParams = get

	list := DPOJobListParams{Filter: "state=RUNNING"}
	var _ DpoJobListParams = list

	resume := DPOJobResumeParams{}
	var _ DpoJobResumeParams = resume

	endpoint := DPOJobGetMetricsFileEndpointResponse{SignedURL: strPtr("gs://metrics")}
	var _ DpoJobGetMetricsFileEndpointResponse = endpoint
}

func TestPageInfoHelpers(t *testing.T) {
	token := "cursor-2"
	page := ModelsPage{NextPageToken: &token}
	if !page.HasNextPage() {
		t.Fatal("expected next page")
	}
	info := page.NextPageInfo()
	if info == nil {
		t.Fatal("expected page info")
	}
	if got := info.Params["pageToken"]; got != "cursor-2" {
		t.Fatalf("pageToken = %q", got)
	}

	emptyToken := ""
	empty := ModelsPage{NextPageToken: &emptyToken}
	if empty.HasNextPage() {
		t.Fatal("empty token should not have next page")
	}
	if info := empty.NextPageInfo(); info != nil {
		t.Fatalf("empty token page info = %#v", info)
	}

	none := ModelsPage{}
	if none.HasNextPage() {
		t.Fatal("nil token should not have next page")
	}
	if info := none.NextPageInfo(); info != nil {
		t.Fatalf("nil token page info = %#v", info)
	}
}

func TestGeneratedJSONAliases(t *testing.T) {
	modelField, ok := reflect.TypeOf(Model{}).FieldByName("ContextLength")
	if !ok {
		t.Fatal("Model.ContextLength missing")
	}
	if got, want := modelField.Tag.Get("json"), "contextLength,omitempty"; got != want {
		t.Fatalf("Model.ContextLength json tag = %q, want %q", got, want)
	}

	responseField, ok := reflect.TypeOf(EvaluatorGetBuildLogEndpointResponse{}).FieldByName("BuildLogSignedURI")
	if !ok {
		t.Fatal("EvaluatorGetBuildLogEndpointResponse.BuildLogSignedURI missing")
	}
	if got, want := responseField.Tag.Get("json"), "buildLogSignedUri,omitempty"; got != want {
		t.Fatalf("BuildLogSignedURI json tag = %q, want %q", got, want)
	}

	paramsField, ok := reflect.TypeOf(AccountListParams{}).FieldByName("PageToken")
	if !ok {
		t.Fatal("AccountListParams.PageToken missing")
	}
	if got, want := paramsField.Tag.Get("json"), "page_token,omitempty"; got != want {
		t.Fatalf("AccountListParams.PageToken json tag = %q, want %q", got, want)
	}
}

func TestGeneratedOptionalParamsOmitEmpty(t *testing.T) {
	payload, err := json.Marshal(AccountListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "{}" {
		t.Fatalf("empty AccountListParams marshaled to %s", payload)
	}
}

func TestGeneratedOptionalPrimitiveParamsPreserveZeroValues(t *testing.T) {
	messagePayload := marshalObject(t, MessageCreateParams{
		Messages: []MessageCreateParamsMessage{
			{Role: "user", Content: "hello"},
		},
		Model:       "accounts/fireworks/models/test",
		MaxTokens:   0,
		Stream:      false,
		Temperature: 0.0,
		TopK:        0,
		TopP:        0.0,
	})
	assertJSONField(t, messagePayload, "max_tokens", float64(0))
	assertJSONField(t, messagePayload, "stream", false)
	assertJSONField(t, messagePayload, "temperature", float64(0))
	assertJSONField(t, messagePayload, "top_k", float64(0))
	assertJSONField(t, messagePayload, "top_p", float64(0))

	loraPayload := marshalObject(t, LoraLoadParams{
		Default:    false,
		Public:     false,
		Serverless: false,
	})
	assertJSONField(t, loraPayload, "default", false)
	assertJSONField(t, loraPayload, "public", false)
	assertJSONField(t, loraPayload, "serverless", false)

	deletePayload := marshalObject(t, DeploymentDeleteParams{Hard: false})
	assertJSONField(t, deletePayload, "hard", false)
}

func strPtr(value string) *string {
	return &value
}

func marshalObject(t *testing.T, value any) map[string]any {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertJSONField(t *testing.T, payload map[string]any, key string, want any) {
	t.Helper()

	got, ok := payload[key]
	if !ok {
		t.Fatalf("%q missing from payload %#v", key, payload)
	}
	if got != want {
		t.Fatalf("%q = %#v, want %#v", key, got, want)
	}
}

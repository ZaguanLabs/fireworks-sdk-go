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

func strPtr(value string) *string {
	return &value
}

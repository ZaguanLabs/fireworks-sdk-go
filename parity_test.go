package fireworks

import (
	"reflect"
	"strings"
	"testing"
)

func TestTypedResourceSurfaceMatchesGeneric(t *testing.T) {
	resourceTypes := []reflect.Type{
		reflect.TypeOf((*AccountsResource)(nil)),
		reflect.TypeOf((*UsersResource)(nil)),
		reflect.TypeOf((*APIKeysResource)(nil)),
		reflect.TypeOf((*BatchInferenceJobsResource)(nil)),
		reflect.TypeOf((*DeploymentsResource)(nil)),
		reflect.TypeOf((*DeploymentShapesResource)(nil)),
		reflect.TypeOf((*DeploymentShapeVersionsResource)(nil)),
		reflect.TypeOf((*ModelsResource)(nil)),
		reflect.TypeOf((*LoraResource)(nil)),
		reflect.TypeOf((*DatasetsResource)(nil)),
		reflect.TypeOf((*SupervisedFineTuningJobsResource)(nil)),
		reflect.TypeOf((*ReinforcementFineTuningJobsResource)(nil)),
		reflect.TypeOf((*ReinforcementFineTuningStepsResource)(nil)),
		reflect.TypeOf((*DPOJobsResource)(nil)),
		reflect.TypeOf((*EvaluationJobsResource)(nil)),
		reflect.TypeOf((*EvaluatorsResource)(nil)),
		reflect.TypeOf((*SecretsResource)(nil)),
		reflect.TypeOf((*ChatCompletionsResource)(nil)),
		reflect.TypeOf((*CompletionsResource)(nil)),
		reflect.TypeOf((*MessagesResource)(nil)),
	}
	for _, typ := range resourceTypes {
		methods := methodSet(typ)
		for method := range methods {
			if !requiresTypedSibling(method) {
				continue
			}
			typed := method + "Typed"
			if !methods[typed] {
				t.Fatalf("%s.%s is missing %s", typ.Elem().Name(), method, typed)
			}
		}
	}
}

func methodSet(typ reflect.Type) map[string]bool {
	methods := make(map[string]bool)
	for i := 0; i < typ.NumMethod(); i++ {
		methods[typ.Method(i).Name] = true
	}
	return methods
}

func requiresTypedSibling(method string) bool {
	if strings.HasSuffix(method, "Typed") || strings.HasSuffix(method, "Stream") {
		return false
	}
	return method != "UploadFile"
}

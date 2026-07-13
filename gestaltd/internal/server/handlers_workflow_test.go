package server

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

func TestWorkflowValueRequestPreservesExplicitEmptyCollections(t *testing.T) {
	t.Parallel()

	var req workflowValueRequest
	if err := json.Unmarshal([]byte(`{"object":{"explicitObject":{"object":{}},"implicitObject":{},"explicitArray":{"array":[]},"implicitArray":[]}}`), &req); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	value := workflowValueFromRequest(req)
	if value.Object == nil {
		t.Fatal("root object is nil, want explicit object")
	}
	if got := value.Object["explicitObject"].Object; got == nil || len(got) != 0 {
		t.Fatalf("explicit object = %#v, want empty object", got)
	}
	if got := value.Object["implicitObject"].Object; got == nil || len(got) != 0 {
		t.Fatalf("implicit object = %#v, want empty object", got)
	}
	if got := value.Object["explicitArray"].Array; got == nil || len(got) != 0 {
		t.Fatalf("explicit array = %#v, want empty array", got)
	}
	if got := value.Object["implicitArray"].Array; got == nil || len(got) != 0 {
		t.Fatalf("implicit array = %#v, want empty array", got)
	}
	var nullReq workflowValueRequest
	if err := json.Unmarshal([]byte(`null`), &nullReq); err != nil {
		t.Fatalf("UnmarshalJSON null: %v", err)
	}
	if value := workflowValueFromRequest(nullReq); !value.LiteralSet || value.Literal != nil {
		t.Fatalf("null value = %#v, want literal null", value)
	}

	info, ok := workflowValueInfoFromCore(value).(map[string]any)
	if !ok {
		t.Fatalf("workflowValueInfoFromCore = %#v, want map", workflowValueInfoFromCore(value))
	}
	object, ok := info["object"].(map[string]any)
	if !ok {
		t.Fatalf("info object = %#v, want map", info["object"])
	}
	if nested, ok := object["explicitObject"].(map[string]any); !ok {
		t.Fatalf("info explicit object = %#v, want map", object["explicitObject"])
	} else if inner, ok := nested["object"].(map[string]any); !ok || len(inner) != 0 {
		t.Fatalf("info explicit object payload = %#v, want empty object", nested["object"])
	}
	if nested, ok := object["explicitArray"].(map[string]any); !ok {
		t.Fatalf("info explicit array = %#v, want map", object["explicitArray"])
	} else if inner, ok := nested["array"].([]any); !ok || len(inner) != 0 {
		t.Fatalf("info explicit array payload = %#v, want empty array", nested["array"])
	}
}

func TestWorkflowRunInfoOmitsCreator(t *testing.T) {
	t.Parallel()

	info := workflowRunInfoFromCore(&coreworkflow.Run{
		ID:        "run-1",
		CreatedBy: "user:legacy",
	}, "temporal")
	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if strings.Contains(string(payload), `"createdBy"`) {
		t.Fatalf("workflow run response contains removed creator field: %s", payload)
	}
}

func TestWorkflowStepWhenInfoPreservesNullEquals(t *testing.T) {
	t.Parallel()

	info := workflowStepWhenInfoFromCore(&coreworkflow.StepWhen{
		Value:     coreworkflow.Value{Literal: true, LiteralSet: true},
		Equals:    nil,
		EqualsSet: true,
	})
	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if !strings.Contains(string(payload), `"equals":null`) {
		t.Fatalf("payload = %s, want explicit equals null", payload)
	}
}

func TestWorkflowValueInfoFromCoreUsesPublicTemplateWrapper(t *testing.T) {
	t.Parallel()

	got := workflowValueInfoFromCore(coreworkflow.Value{
		Template: &coreworkflow.Text{Template: "hello {{.name}}"},
	})
	want := map[string]any{
		"template": &workflowTextInfo{Template: "hello {{.name}}"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("workflowValueInfoFromCore() = %#v, want %#v", got, want)
	}
}

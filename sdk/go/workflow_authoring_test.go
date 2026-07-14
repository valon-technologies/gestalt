package gestalt

import (
	"encoding/json"
	"testing"
)

func TestDefineWorkflowRequiresRunAs(t *testing.T) {
	if _, err := DefineWorkflow(DefineWorkflowOptions{ID: "demo"}); err == nil {
		t.Fatal("expected RunAs validation error")
	}
}

func TestTypedWorkflowBuilderMatchesExtractRowExample(t *testing.T) {
	builder, err := DefineWorkflow(DefineWorkflowOptions{
		ID:    "extractRow",
		RunAs: "service_account:deal-hub-extraction",
	})
	if err != nil {
		t.Fatalf("DefineWorkflow: %v", err)
	}
	spec := builder.
		On(Event("deal_hub.analyses.extract.requested", func(scope WorkflowEventScope) map[string]WorkflowValue {
			return map[string]WorkflowValue{
				"analysisId": scope.Data("analysisId"),
			}
		}, WorkflowEventActivationOptions{})).
		Step("extract", WorkflowStepConfig{
			App: &WorkflowStepAppConfig{
				Name:      "dealHub",
				Operation: "analyses.extractRowWorkflow",
				Input: func(scope WorkflowStepScope) map[string]WorkflowValue {
					return map[string]WorkflowValue{
						"analysisId": scope.Input("analysisId"),
					}
				},
			},
		}).
		ToSpec()

	contract, err := LoadWorkflowLoweringContract()
	if err != nil {
		t.Fatalf("LoadWorkflowLoweringContract: %v", err)
	}
	var expected map[string]any
	for _, caseData := range contract.Cases {
		if caseData.Name == "extract_row" {
			expected = caseData.ExpectedSpec
			break
		}
	}
	if expected == nil {
		t.Fatal("missing extract_row fixture")
	}
	got, err := CanonicalWorkflowDefinitionSpec(spec)
	if err != nil {
		t.Fatalf("CanonicalWorkflowDefinitionSpec: %v", err)
	}
	assertJSONEqual(t, expected, got)
}

func TestWorkflowAuthoringGoldenFixtures(t *testing.T) {
	contract, err := LoadWorkflowLoweringContract()
	if err != nil {
		t.Fatalf("LoadWorkflowLoweringContract: %v", err)
	}
	for _, caseData := range contract.Cases {
		t.Run(caseData.Name, func(t *testing.T) {
			builder, err := BuildWorkflowFromLoweringCase(caseData)
			if err != nil {
				t.Fatalf("BuildWorkflowFromLoweringCase: %v", err)
			}
			got, err := CanonicalWorkflowDefinitionSpec(builder.ToSpec())
			if err != nil {
				t.Fatalf("CanonicalWorkflowDefinitionSpec: %v", err)
			}
			assertJSONEqual(t, caseData.ExpectedSpec, got)
		})
	}
}

func TestResolveWorkflowDefinitionSpecAcceptsBuilder(t *testing.T) {
	builder, err := DefineWorkflow(DefineWorkflowOptions{
		ID:    "extractRow",
		RunAs: "service_account:deal-hub-extraction",
	})
	if err != nil {
		t.Fatalf("DefineWorkflow: %v", err)
	}
	builder.On(Schedule("0 2 * * *", func(scope WorkflowActivationScope) map[string]WorkflowValue {
		return map[string]WorkflowValue{"reason": scope.Input("reason")}
	}, WorkflowScheduleActivationOptions{}))

	fromBuilder, err := ResolveWorkflowDefinitionSpec(builder)
	if err != nil {
		t.Fatalf("ResolveWorkflowDefinitionSpec(builder): %v", err)
	}
	fromSpec, err := ResolveWorkflowDefinitionSpec(fromBuilder)
	if err != nil {
		t.Fatalf("ResolveWorkflowDefinitionSpec(spec): %v", err)
	}
	if len(fromBuilder.Activations) != 1 || fromBuilder.Activations[0].Schedule == nil {
		t.Fatalf("activations = %#v", fromBuilder.Activations)
	}
	if fromSpec.ID != "extractRow" {
		t.Fatalf("id = %q, want extractRow", fromSpec.ID)
	}
}

func assertJSONEqual(t *testing.T, expected, actual map[string]any) {
	t.Helper()
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("marshal expected: %v", err)
	}
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatalf("marshal actual: %v", err)
	}
	if string(expectedJSON) != string(actualJSON) {
		t.Fatalf("spec mismatch\nexpected: %s\nactual:   %s", expectedJSON, actualJSON)
	}
}

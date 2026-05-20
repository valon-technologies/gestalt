package workflow

import "testing"

func TestCloneValueDeepClonesLiteralReferenceTypes(t *testing.T) {
	original := Value{
		Literal: map[string]any{
			"items": []any{
				map[string]any{"name": "first"},
			},
		},
	}

	clone := CloneValue(original)
	cloneMap := clone.Literal.(map[string]any)
	cloneItems := cloneMap["items"].([]any)
	cloneItems[0].(map[string]any)["name"] = "changed"

	originalMap := original.Literal.(map[string]any)
	originalItems := originalMap["items"].([]any)
	if got := originalItems[0].(map[string]any)["name"]; got != "first" {
		t.Fatalf("original literal changed to %q, want first", got)
	}
}

func TestStepActionIDFormatsAreCanonical(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		suffix string
		want   string
	}{
		{name: "plugin", suffix: WorkflowStepPluginActionSuffix, want: "step/diagnose_1/plugin"},
		{name: "agent", suffix: WorkflowStepAgentActionSuffix, want: "step/diagnose_1/agent-turn"},
		{name: "delivery", suffix: WorkflowStepDeliveryActionSuffix, want: "step/diagnose_1/delivery"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := StepActionID("diagnose_1", tt.suffix)
			if !ok {
				t.Fatalf("StepActionID ok = false")
			}
			if got != tt.want {
				t.Fatalf("StepActionID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStepActionIDRejectsAmbiguousStepIDs(t *testing.T) {
	t.Parallel()

	for _, stepID := range []string{"", " ", "a/b", "a b", "a:b"} {
		if got, ok := StepPluginActionID(stepID); ok {
			t.Fatalf("StepPluginActionID(%q) = %q, true; want false", stepID, got)
		}
	}
}

func TestTargetsEqualNormalizesEmptyStepValues(t *testing.T) {
	t.Parallel()

	left := Target{Steps: []Step{{
		ID: "notify",
		Inputs: map[string]Value{
			"empty_array":  {Array: []Value{}},
			"empty_object": {Object: map[string]Value{}},
			"nested": {
				Object: map[string]Value{
					"items": {Array: []Value{}},
				},
			},
		},
		Plugin: &PluginCall{
			Name:      "slack",
			Operation: "reply",
			Input: Value{Object: map[string]Value{
				"payload": {Object: map[string]Value{}},
				"tags":    {Array: []Value{}},
			}},
		},
		When: &StepWhen{
			Value:     Value{Object: map[string]Value{}},
			EqualsSet: true,
		},
		OutputDelivery: &StepDelivery{Plugin: &PluginCall{
			Name:      "slack",
			Operation: "notify",
			Input: Value{Object: map[string]Value{
				"body": {Object: map[string]Value{}},
			}},
		}},
	}}}
	right := Target{Steps: []Step{{
		ID: "notify",
		Inputs: map[string]Value{
			"empty_array":  {},
			"empty_object": {},
			"nested": {
				Object: map[string]Value{
					"items": {},
				},
			},
		},
		Plugin: &PluginCall{
			Name:      "slack",
			Operation: "reply",
			Input: Value{Object: map[string]Value{
				"payload": {},
				"tags":    {},
			}},
		},
		When: &StepWhen{
			Value:     Value{},
			EqualsSet: true,
		},
		OutputDelivery: &StepDelivery{Plugin: &PluginCall{
			Name:      "slack",
			Operation: "notify",
			Input: Value{Object: map[string]Value{
				"body": {},
			}},
		}},
	}}}

	if !TargetsEqual(left, right) {
		t.Fatal("TargetsEqual returned false for equivalent empty step values")
	}
	leftFingerprint, err := TargetFingerprint(left)
	if err != nil {
		t.Fatalf("TargetFingerprint(left): %v", err)
	}
	rightFingerprint, err := TargetFingerprint(right)
	if err != nil {
		t.Fatalf("TargetFingerprint(right): %v", err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("TargetFingerprint mismatch: %q != %q", leftFingerprint, rightFingerprint)
	}
}

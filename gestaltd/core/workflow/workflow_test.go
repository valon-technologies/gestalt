package workflow

import "testing"

func TestCloneValueDeepClonesLiteralReferenceTypes(t *testing.T) {
	t.Parallel()

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

func TestTargetsEqualNormalizesNestedEmptyWorkflowValues(t *testing.T) {
	t.Parallel()

	left := Target{Steps: []Step{{
		ID: "first",
		Inputs: map[string]Value{
			"payload": {
				Object: map[string]Value{
					"items": {Array: []Value{{Object: map[string]Value{}}}},
				},
			},
		},
		When: &StepWhen{
			Value: Value{Object: map[string]Value{"flag": {Array: []Value{}}}},
		},
		Plugin: &PluginCall{
			Name:      "plugin",
			Operation: "operation",
			Input: Value{Object: map[string]Value{
				"empty": {Object: map[string]Value{}},
			}},
		},
	}}}
	right := Target{Steps: []Step{{
		ID: "first",
		Inputs: map[string]Value{
			"payload": {
				Object: map[string]Value{
					"items": {Array: []Value{{}}},
				},
			},
		},
		When: &StepWhen{
			Value: Value{Object: map[string]Value{"flag": {}}},
		},
		Plugin: &PluginCall{
			Name:      "plugin",
			Operation: "operation",
			Input: Value{Object: map[string]Value{
				"empty": {},
			}},
		},
	}}}

	if !TargetsEqual(left, right) {
		t.Fatal("TargetsEqual = false, want true for nested empty workflow values")
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
		t.Fatalf("fingerprints differ: left %s right %s", leftFingerprint, rightFingerprint)
	}
}

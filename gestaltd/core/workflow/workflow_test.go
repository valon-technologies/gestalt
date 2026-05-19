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

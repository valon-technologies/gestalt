package workflow

import "testing"

func TestPathValueSupportsSingleQuotedBracketKeys(t *testing.T) {
	t.Parallel()

	root := map[string]any{
		"body.with.dot": map[string]any{
			"items": []any{
				map[string]any{
					"quoted'key": "value",
				},
			},
		},
	}

	got, ok, err := PathValue(root, `['body.with.dot'].items[0]['quoted\'key']`)
	if err != nil {
		t.Fatalf("PathValue() error = %v", err)
	}
	if !ok {
		t.Fatal("PathValue() did not resolve")
	}
	if got != "value" {
		t.Fatalf("PathValue() = %#v, want value", got)
	}
}

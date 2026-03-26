package invocation

import (
	"testing"

	"github.com/valon-technologies/gestalt/core"
)

func TestMissingRequiredParams(t *testing.T) {
	t.Parallel()
	params := []core.Parameter{
		{Name: "userId", Required: true},
		{Name: "format", Required: false},
		{Name: "id", Required: true},
	}

	missing := missingRequiredParams(params, map[string]any{"userId": "me"})
	if len(missing) != 1 || missing[0] != "id" {
		t.Errorf("expected [id], got %v", missing)
	}

	missing = missingRequiredParams(params, map[string]any{"userId": "me", "id": "123"})
	if len(missing) != 0 {
		t.Errorf("expected no missing params, got %v", missing)
	}

	missing = missingRequiredParams(params, map[string]any{})
	if len(missing) != 2 {
		t.Errorf("expected 2 missing params, got %v", missing)
	}
}

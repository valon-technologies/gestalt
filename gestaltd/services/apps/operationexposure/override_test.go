package operationexposure

import (
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestOverridesFromManifestEmptyMap(t *testing.T) {
	t.Parallel()

	got := OverridesFromManifest(map[string]*providermanifestv1.ManifestOperationOverride{})
	if got == nil {
		t.Fatal("expected non-nil empty map for explicit empty allowedOperations")
	}
	if len(got) != 0 {
		t.Fatalf("got len %d, want 0", len(got))
	}

	_, err := New(got)
	if err == nil {
		t.Fatal("expected error for empty allowed_operations")
	}
}

func TestOverridesFromManifestNil(t *testing.T) {
	t.Parallel()

	if got := OverridesFromManifest(nil); got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}

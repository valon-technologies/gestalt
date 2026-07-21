package python

import (
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func TestPythonPublicFieldTupleSingleElement(t *testing.T) {
	t.Parallel()
	got := pythonPublicFieldTuple([]publicsurface.PublicField{
		{Name: "app", JSONName: "app"},
	})
	want := `(PublicField(name="app", json_name="app"),)`
	if got != want {
		t.Fatalf("pythonPublicFieldTuple() = %q, want %q", got, want)
	}
}

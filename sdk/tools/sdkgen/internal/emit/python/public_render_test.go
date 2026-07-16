package python

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func TestPublicRuntimeFileImportOrder(t *testing.T) {
	t.Parallel()
	got := publicRuntimeFile
	grpcIdx := strings.Index(got, "import grpc\n")
	googleIdx := strings.Index(got, "from google.protobuf import")
	gestaltIdx := strings.Index(got, "from gestalt.rpc_support import")
	if grpcIdx < 0 || googleIdx < 0 || gestaltIdx < 0 {
		t.Fatalf("missing expected imports in publicRuntimeFile")
	}
	if !(grpcIdx < googleIdx && googleIdx < gestaltIdx) {
		t.Fatalf("publicRuntimeFile import order = grpc@%d google@%d gestalt@%d; want third-party before first-party gestalt", grpcIdx, googleIdx, gestaltIdx)
	}
}
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

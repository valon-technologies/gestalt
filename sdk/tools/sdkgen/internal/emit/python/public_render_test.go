package python

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func TestTransportKernelEmittedWithoutIOImports(t *testing.T) {
	t.Parallel()
	got := transportKernelFile
	for _, forbidden := range []string{"import httpx", "import grpc", "import requests"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("transport kernel must not import %s", forbidden)
		}
	}
	if !strings.Contains(got, "def prepare_rest_request(") {
		t.Fatalf("missing prepare_rest_request in transport kernel")
	}
}

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

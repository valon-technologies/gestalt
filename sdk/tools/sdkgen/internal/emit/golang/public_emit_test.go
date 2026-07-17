package golang

import (
	"strings"
	"testing"
)

func TestPublicTransportKernelEmittedWithoutHTTPImports(t *testing.T) {
	t.Parallel()
	got := publicTransportKernelFile
	for _, forbidden := range []string{`"net/http"`, `"google.golang.org/grpc"`} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("transport kernel must not import %s", forbidden)
		}
	}
	if !strings.Contains(got, "func PrepareRESTRequest(") {
		t.Fatalf("missing PrepareRESTRequest in transport kernel")
	}
}

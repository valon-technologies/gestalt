package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestValidateMountedHTTPBindingRoutesRejectsCoreRouteNamespace(t *testing.T) {
	t.Parallel()

	err := validateMountedHTTPBindingRoutes([]MountedHTTPBinding{
		{
			AppName: "tokens",
			Name:    "issue",
			Method:  http.MethodPost,
			Path:    "/api/v1/tokens/issue",
		},
	}, nil)
	if err == nil {
		t.Fatal("validateMountedHTTPBindingRoutes returned nil, want error")
	}
	if got := err.Error(); !strings.Contains(got, `conflicts with core route namespace "/api/v1/tokens"`) {
		t.Fatalf("error = %q, want tokens namespace conflict", got)
	}
}

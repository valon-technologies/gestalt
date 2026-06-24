package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestValidateMountedHTTPBindingRoutesRejectsAuthorizationNamespace(t *testing.T) {
	err := validateMountedHTTPBindingRoutes([]MountedHTTPBinding{
		{
			AppName: "authorization",
			Name:    "check",
			Method:  http.MethodPost,
			Path:    "/api/v1/authorization/check-access",
		},
	}, nil)
	if err == nil {
		t.Fatal("validateMountedHTTPBindingRoutes returned nil, want error")
	}
	if got := err.Error(); !strings.Contains(got, `conflicts with core route namespace "/api/v1/authorization"`) {
		t.Fatalf("error = %q, want authorization namespace conflict", got)
	}
}

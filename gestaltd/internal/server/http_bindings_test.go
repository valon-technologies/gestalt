package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestMountedHTTPBindingPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		relativePath string
		canonical    string
		legacy       string
	}{
		{name: "binding path", relativePath: "/event", canonical: "/api/v1/github/webhooks/event", legacy: "/api/v1/github/event"},
		{name: "root binding", relativePath: "/", canonical: "/api/v1/github/webhooks", legacy: "/api/v1/github"},
		{name: "nested webhooks suffix", relativePath: "/webhooks/event", canonical: "/api/v1/github/webhooks/webhooks/event", legacy: "/api/v1/github/webhooks/event"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := mountedHTTPBindingPath("github", test.relativePath); got != test.canonical {
				t.Fatalf("mountedHTTPBindingPath() = %q, want %q", got, test.canonical)
			}
			if got := legacyMountedHTTPBindingPath("github", test.relativePath); got != test.legacy {
				t.Fatalf("legacyMountedHTTPBindingPath() = %q, want %q", got, test.legacy)
			}
		})
	}
}

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

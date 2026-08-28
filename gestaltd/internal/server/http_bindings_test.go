package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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

func TestMountedHTTPBindingsSkipLegacyAliasThatConflictsWithCanonicalRoute(t *testing.T) {
	t.Parallel()

	bindings, err := mountedHTTPBindingsFromEntries(map[string]*config.ProviderEntry{
		"events": {
			SecuritySchemes: map[string]*config.HTTPSecurityScheme{
				"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
			},
			HTTP: map[string]*config.HTTPBinding{
				"event": {
					Path:     "/event",
					Method:   http.MethodPost,
					Security: "none",
					Target:   "receive_event",
				},
				"nested_event": {
					Path:     "/webhooks/event",
					Method:   http.MethodPost,
					Security: "none",
					Target:   "receive_nested_event",
				},
			},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("mountedHTTPBindingsFromEntries: %v", err)
	}

	routes := make(map[string]string, len(bindings))
	for i := range bindings {
		routes[bindings[i].Method+" "+bindings[i].Path] = bindings[i].Name
	}
	want := map[string]string{
		"POST /api/v1/events/webhooks/event":          "event",
		"POST /api/v1/events/webhooks/webhooks/event": "nested_event",
		"POST /api/v1/events/event":                   "event",
	}
	if len(routes) != len(want) {
		t.Fatalf("routes = %#v, want %#v", routes, want)
	}
	for route, bindingName := range want {
		if got := routes[route]; got != bindingName {
			t.Fatalf("route %q binding = %q, want %q", route, got, bindingName)
		}
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

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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

func TestMountedHTTPBindingsAllowsRegistryAppBeforeStartup(t *testing.T) {
	t.Parallel()

	bindings, err := mountedHTTPBindingsFromEntries(map[string]*config.ProviderEntry{
		"g-issues": {
			Source: config.ProviderSource{Registry: "toolshed"},
			SecuritySchemes: map[string]*config.HTTPSecurityScheme{
				"none": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
			},
			HTTP: map[string]*config.HTTPBinding{
				"status": {
					Path:     "/status",
					Method:   http.MethodGet,
					Security: "none",
					Target:   "status",
				},
			},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("mountedHTTPBindingsFromEntries: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("bindings = %d, want 1", len(bindings))
	}
}

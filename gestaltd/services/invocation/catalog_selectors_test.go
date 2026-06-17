package invocation

import (
	"errors"
	"fmt"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

type stubMCPConnectionBroker struct {
	connections map[string]string
}

func (b stubMCPConnectionBroker) MCPConnection(providerName string) string {
	if b.connections == nil {
		return ""
	}
	return b.connections[providerName]
}

func TestSessionCatalogConnections_UsesMCPConnectionForDynamicResolution(t *testing.T) {
	t.Parallel()

	cfg := CatalogSelectorConfig{
		CatalogConnection: map[string]string{"notion": "OAuth"},
		MCPConnection:     map[string]string{"notion": "MCP"},
		DefaultConnection: map[string]string{"notion": "OAuth"},
	}

	got := cfg.SessionCatalogConnections("notion", "")
	if len(got) != 1 || got[0] != "MCP" {
		t.Fatalf("SessionCatalogConnections() = %v, want [MCP]", got)
	}
}

func TestAPICatalogConnections_UsesAPIConnectionForListing(t *testing.T) {
	t.Parallel()

	cfg := CatalogSelectorConfig{
		CatalogConnection: map[string]string{"notion": "OAuth"},
		MCPConnection:     map[string]string{"notion": "MCP"},
		DefaultConnection: map[string]string{"notion": "OAuth"},
	}

	got := cfg.APICatalogConnections("notion", "")
	if len(got) != 1 || got[0] != "OAuth" {
		t.Fatalf("APICatalogConnections() = %v, want [OAuth]", got)
	}
}

func TestSessionCatalogConnections_FallsBackToDefaultWithoutSurfaceMaps(t *testing.T) {
	t.Parallel()

	cfg := CatalogSelectorConfig{
		DefaultConnection: map[string]string{"sample-int": "default"},
	}

	got := cfg.SessionCatalogConnections("sample-int", "")
	if len(got) != 1 || got[0] != "default" {
		t.Fatalf("SessionCatalogConnections() = %v, want [default]", got)
	}
}

func TestSessionCatalogConnections_UsesBrokerMCPConnection(t *testing.T) {
	t.Parallel()

	cfg := CatalogSelectorConfig{
		Invoker: stubMCPConnectionBroker{connections: map[string]string{"notion": "MCP"}},
	}

	got := cfg.SessionCatalogConnections("notion", "")
	if len(got) != 1 || got[0] != "MCP" {
		t.Fatalf("SessionCatalogConnections() = %v, want [MCP]", got)
	}
}

func TestSessionCatalogConnections_ExplicitConnectionOverridesSurfaceMaps(t *testing.T) {
	t.Parallel()

	cfg := CatalogSelectorConfig{
		CatalogConnection: map[string]string{"notion": "OAuth"},
		MCPConnection:     map[string]string{"notion": "MCP"},
	}

	got := cfg.SessionCatalogConnections("notion", "workspace")
	if len(got) != 1 || got[0] != "workspace" {
		t.Fatalf("SessionCatalogConnections() = %v, want [workspace]", got)
	}
}

func TestClassifySessionCatalogError_MapsUnauthorizedToReconnectRequired(t *testing.T) {
	t.Parallel()

	err := ClassifySessionCatalogError(fmt.Errorf("mcpupstream notion: initialize: transport error: unauthorized (401)"))
	if !errors.Is(err, ErrReconnectRequired) {
		t.Fatalf("ClassifySessionCatalogError() = %v, want ErrReconnectRequired", err)
	}
}

func TestAPICatalogTargets_SetsAPISurface(t *testing.T) {
	t.Parallel()

	cfg := CatalogSelectorConfig{
		CatalogConnection: map[string]string{"notion": "OAuth"},
		MCPConnection:     map[string]string{"notion": "MCP"},
	}
	targets := cfg.APICatalogTargets("notion", "", "")
	if len(targets) != 1 || targets[0].Surface != core.CatalogSurfaceAPI {
		t.Fatalf("APICatalogTargets() = %#v, want API surface", targets)
	}
}

func TestSessionCatalogTargets_SetsMCPSurface(t *testing.T) {
	t.Parallel()

	cfg := CatalogSelectorConfig{
		MCPConnection: map[string]string{"notion": "MCP"},
	}
	targets := cfg.SessionCatalogTargets("notion", "", "")
	if len(targets) != 1 || targets[0].Surface != core.CatalogSurfaceMCP {
		t.Fatalf("SessionCatalogTargets() = %#v, want MCP surface", targets)
	}
}

func TestHTTPListCatalogTargets_DefaultsToAPI(t *testing.T) {
	t.Parallel()

	cfg := CatalogSelectorConfig{
		CatalogConnection: map[string]string{"notion": "OAuth"},
		MCPConnection:     map[string]string{"notion": "MCP"},
	}
	targets := cfg.HTTPListCatalogTargets("notion", "", "")
	if len(targets) != 1 || targets[0].Surface != core.CatalogSurfaceAPI || targets[0].Connection != "OAuth" {
		t.Fatalf("HTTPListCatalogTargets() = %#v, want OAuth/API surface", targets)
	}
}

func TestHTTPListCatalogTargets_ExplicitMCPUsesMCPSurface(t *testing.T) {
	t.Parallel()

	cfg := CatalogSelectorConfig{
		CatalogConnection: map[string]string{"notion": "OAuth"},
		MCPConnection:     map[string]string{"notion": "MCP"},
	}
	targets := cfg.HTTPListCatalogTargets("notion", "MCP", "")
	if len(targets) != 1 || targets[0].Surface != core.CatalogSurfaceMCP || targets[0].Connection != "MCP" {
		t.Fatalf("HTTPListCatalogTargets() = %#v, want MCP surface", targets)
	}
}

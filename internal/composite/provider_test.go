package composite

import (
	"context"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type stubCompositeMCPUpstream struct {
	cat *catalog.Catalog
}

func (s *stubCompositeMCPUpstream) Catalog() *catalog.Catalog { return s.cat }

func (s *stubCompositeMCPUpstream) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	return s.cat, nil
}

func (s *stubCompositeMCPUpstream) CallTool(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error) {
	return mcpgo.NewToolResultText("ok"), nil
}

func (s *stubCompositeMCPUpstream) SupportsManualAuth() bool { return true }
func (s *stubCompositeMCPUpstream) Close() error             { return nil }

type stubLegacyMetadataProvider struct {
	stubProvider
	authTypes []string
	defs      map[string]core.ConnectionParamDef
	discovery *core.DiscoveryConfig
}

func (s *stubLegacyMetadataProvider) AuthTypes() []string {
	return slices.Clone(s.authTypes)
}

func (s *stubLegacyMetadataProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	out := make(map[string]core.ConnectionParamDef, len(s.defs))
	for name, def := range s.defs {
		out[name] = def
	}
	return out
}

func (s *stubLegacyMetadataProvider) DiscoveryConfig() *core.DiscoveryConfig {
	if s.discovery == nil {
		return nil
	}
	clone := *s.discovery
	if s.discovery.MetadataMapping != nil {
		clone.MetadataMapping = make(map[string]string, len(s.discovery.MetadataMapping))
		for key, value := range s.discovery.MetadataMapping {
			clone.MetadataMapping[key] = value
		}
	}
	return &clone
}

func TestProviderConnectionSpecDelegatesToAPIAndFallsBackToLegacyMetadata(t *testing.T) {
	t.Parallel()

	api := &stubLegacyMetadataProvider{
		stubProvider: stubProvider{name: "api", operations: []core.Operation{{Name: "list"}}},
		authTypes:    []string{"oauth", "manual"},
		defs: map[string]core.ConnectionParamDef{
			"tenant": {Required: true, Description: "Tenant"},
		},
		discovery: &core.DiscoveryConfig{
			URL:             "https://example.com/discover",
			MetadataMapping: map[string]string{"tenant_id": "tenant.id"},
		},
	}
	mcp := &stubCompositeMCPUpstream{
		cat: &catalog.Catalog{Name: "mcp", Operations: []catalog.CatalogOperation{{ID: "search"}}},
	}

	prov := New("test", api, mcp)
	csp, ok := prov.(core.ConnectionSpecProvider)
	if !ok {
		t.Fatal("expected composite provider to implement ConnectionSpecProvider")
	}

	got := csp.ConnectionSpec()
	if !slices.Equal(got.AuthTypes, []string{"oauth", "manual"}) {
		t.Fatalf("auth types = %+v", got.AuthTypes)
	}
	if got.ConnectionParams["tenant"].Description != "Tenant" {
		t.Fatalf("connection params = %+v", got.ConnectionParams)
	}
	if got.Discovery == nil || got.Discovery.URL != "https://example.com/discover" {
		t.Fatalf("discovery = %+v", got.Discovery)
	}

	got.AuthTypes[0] = "mutated"
	got.ConnectionParams["tenant"] = core.ConnectionParamDef{Description: "mutated"}
	got.Discovery.URL = "https://mutated.invalid"
	got.Discovery.MetadataMapping["tenant_id"] = "mutated"

	if api.authTypes[0] != "oauth" {
		t.Fatalf("api auth types were mutated: %+v", api.authTypes)
	}
	if api.defs["tenant"].Description != "Tenant" {
		t.Fatalf("api connection params were mutated: %+v", api.defs["tenant"])
	}
	if api.discovery.URL != "https://example.com/discover" {
		t.Fatalf("api discovery URL was mutated: %q", api.discovery.URL)
	}
	if api.discovery.MetadataMapping["tenant_id"] != "tenant.id" {
		t.Fatalf("api discovery metadata was mutated: %+v", api.discovery.MetadataMapping)
	}
}

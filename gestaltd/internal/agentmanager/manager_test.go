package agentmanager

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type catalogCountingProvider struct {
	coretesting.StubIntegration
	catalogCalls int
}

func (p *catalogCountingProvider) Catalog() *catalog.Catalog {
	p.catalogCalls++
	return p.CatalogVal
}

func TestResolveToolsDoesNotImplicitlyHydrateDefaultTools(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "docs",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:       "search",
				Title:    "Search",
				ReadOnly: true,
			}}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	tools, err := manager.resolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, "", nil, coreagent.ToolSourceModeUnspecified)
	if err != nil {
		t.Fatalf("resolveTools: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("resolveTools returned %d tools, want none", len(tools))
	}
	if provider.catalogCalls != 0 {
		t.Fatalf("provider catalog calls = %d, want 0", provider.catalogCalls)
	}
}

func TestResolveToolsStillResolvesExplicitTools(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "docs",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:       "search",
				Title:    "Search",
				ReadOnly: true,
			}}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	tools, err := manager.resolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, "", []coreagent.ToolRef{{
		PluginName: "docs",
		Operation:  "search",
	}}, coreagent.ToolSourceModeUnspecified)
	if err != nil {
		t.Fatalf("resolveTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("resolveTools returned %d tools, want 1", len(tools))
	}
	if tools[0].Target.PluginName != "docs" || tools[0].Target.Operation != "search" {
		t.Fatalf("tool target = %#v, want docs.search", tools[0].Target)
	}
}

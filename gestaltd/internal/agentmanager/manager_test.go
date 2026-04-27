package agentmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
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

type sessionCatalogProvider struct {
	catalogCountingProvider
	sessionCatalog *catalog.Catalog
}

func (p *sessionCatalogProvider) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	return p.sessionCatalog, nil
}

func TestSearchToolsSearchesAuthorizedCatalogWhenNoRefsDefined(t *testing.T) {
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
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "search",
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "docs" || resp.Tools[0].Target.Operation != "search" {
		t.Fatalf("tool target = %#v, want docs.search", resp.Tools[0].Target)
	}
}

func TestSearchToolsRestrictsToDefinedRefs(t *testing.T) {
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
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "search",
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "docs",
			Operation: "search",
		}},
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "docs" || resp.Tools[0].Target.Operation != "search" {
		t.Fatalf("tool target = %#v, want docs.search", resp.Tools[0].Target)
	}
}

func TestSearchToolsRejectsBlankToolRefPlugin(t *testing.T) {
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
	_, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "search",
		ToolRefs: []coreagent.ToolRef{{
			Operation: "search",
		}},
	})
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("SearchTools error = %v, want %v", err, invocation.ErrProviderNotFound)
	}
}

func TestSearchToolsDiscoversSessionCatalogOperations(t *testing.T) {
	t.Parallel()

	provider := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "docs",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
					ID:       "static_search",
					Title:    "Static Search",
					ReadOnly: true,
				}}},
			},
		},
		sessionCatalog: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID:          "dynamic_search",
			Title:       "Dynamic Search",
			Description: "Search the session catalog",
			ReadOnly:    true,
		}}},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "dynamic",
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "docs" || resp.Tools[0].Target.Operation != "dynamic_search" {
		t.Fatalf("tool target = %#v, want docs.dynamic_search", resp.Tools[0].Target)
	}
}

func TestResolveToolsReturnsEmptyWhenNoRefsDefined(t *testing.T) {
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
	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("ResolveTools returned %d tools, want 0", len(tools))
	}
}

func TestResolveToolsExpandsPluginOnlyRefs(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "docs",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
				{
					ID:       "search",
					Title:    "Search",
					ReadOnly: true,
				},
				{
					ID:       "summarize",
					Title:    "Summarize",
					ReadOnly: true,
				},
			}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		ToolRefs: []coreagent.ToolRef{{Plugin: "docs"}},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("ResolveTools returned %d tools, want 2", len(tools))
	}
	if tools[0].Target.Operation != "search" || tools[1].Target.Operation != "summarize" {
		t.Fatalf("ResolveTools operations = %q, %q; want search, summarize", tools[0].Target.Operation, tools[1].Target.Operation)
	}
}

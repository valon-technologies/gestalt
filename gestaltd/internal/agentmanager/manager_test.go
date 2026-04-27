package agentmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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

type tokenErrorInvoker struct {
	providerName string
	err          error
}

func (i tokenErrorInvoker) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return nil, nil
}

func (i tokenErrorInvoker) ResolveToken(ctx context.Context, _ *principal.Principal, providerName, _, _ string) (context.Context, string, error) {
	if i.providerName != "" && providerName != i.providerName {
		return ctx, "", nil
	}
	return ctx, "", i.err
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

func TestSearchToolsOmitsHiddenOperationsUnlessExplicitlyScoped(t *testing.T) {
	t.Parallel()

	hidden := false
	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
				{
					ID:       "chat.postMessage",
					Title:    "Post Message",
					ReadOnly: false,
				},
				{
					ID:      "events.reply",
					Title:   "Reply to Event",
					Visible: &hidden,
				},
			}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "reply",
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 0 {
		t.Fatalf("SearchTools returned %d hidden tools, want 0", len(resp.Tools))
	}

	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		ToolRefs: []coreagent.ToolRef{{Plugin: "slack", Operation: "events.reply"}},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Target.Operation != "events.reply" {
		t.Fatalf("ResolveTools = %#v, want explicit hidden events.reply", tools)
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

func TestSearchToolsSkipsUnavailablePluginScopedProviders(t *testing.T) {
	t.Parallel()

	ashby := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "ashby",
				ConnMode: core.ConnectionModeUser,
			},
		},
	}
	linear := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				DisplayName: "Linear",
				Operations: []catalog.CatalogOperation{{
					ID:       "searchIssues",
					Title:    "Search Linear Issues",
					ReadOnly: true,
				}},
			},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, ashby, linear),
		Invoker:   tokenErrorInvoker{providerName: "ashby", err: invocation.ErrNoCredential},
	})

	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "Linear",
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "ashby"},
			{Plugin: "linear"},
		},
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "linear" || resp.Tools[0].Target.Operation != "searchIssues" {
		t.Fatalf("tool target = %#v, want linear.searchIssues", resp.Tools[0].Target)
	}
}

func TestSearchToolsReturnsUnavailableWhenScopedSearchHasNoCandidates(t *testing.T) {
	t.Parallel()

	ashby := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "ashby",
				ConnMode: core.ConnectionModeUser,
			},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, ashby),
		Invoker:   tokenErrorInvoker{providerName: "ashby", err: invocation.ErrNoCredential},
	})

	_, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query:    "Ashby",
		ToolRefs: []coreagent.ToolRef{{Plugin: "ashby"}},
	})
	if !errors.Is(err, invocation.ErrNoCredential) {
		t.Fatalf("SearchTools error = %v, want ErrNoCredential", err)
	}
}

func TestSearchToolsKeepsExactOperationRefsStrict(t *testing.T) {
	t.Parallel()

	ashby := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "ashby",
				ConnMode: core.ConnectionModeUser,
			},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, ashby),
		Invoker:   tokenErrorInvoker{providerName: "ashby", err: invocation.ErrNoCredential},
	})

	_, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "candidate",
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "ashby",
			Operation: "candidateSearch",
		}},
	})
	if !errors.Is(err, invocation.ErrNoCredential) {
		t.Fatalf("SearchTools error = %v, want ErrNoCredential", err)
	}
}

func TestSearchToolsKeepsMixedExactOperationRefsStrict(t *testing.T) {
	t.Parallel()

	ashby := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "ashby",
				ConnMode: core.ConnectionModeUser,
			},
		},
	}
	linear := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				DisplayName: "Linear",
				Operations: []catalog.CatalogOperation{{
					ID:       "searchIssues",
					Title:    "Search Linear Issues",
					ReadOnly: true,
				}},
			},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, ashby, linear),
		Invoker:   tokenErrorInvoker{providerName: "ashby", err: invocation.ErrNoCredential},
	})

	_, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "Linear",
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "linear"},
			{Plugin: "ashby", Operation: "candidateSearch"},
		},
	})
	if !errors.Is(err, invocation.ErrNoCredential) {
		t.Fatalf("SearchTools error = %v, want ErrNoCredential", err)
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

func TestResolveToolsAppliesDeclaredInvokeCredentialMode(t *testing.T) {
	t.Parallel()

	hidden := false
	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:      "events.reply",
				Title:   "Reply",
				Visible: &hidden,
			}}},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		PluginInvokes: map[string][]config.PluginInvocationDependency{
			"slackbot": {{
				Plugin:         "slack",
				Operation:      "events.reply",
				CredentialMode: providermanifestv1.ConnectionModeNone,
			}},
		},
	})

	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		CallerPluginName: "slackbot",
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "slack",
			Operation: "events.reply",
		}},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ResolveTools returned %d tools, want 1", len(tools))
	}
	if tools[0].Target.Plugin != "slack" || tools[0].Target.Operation != "events.reply" {
		t.Fatalf("tool target = %#v, want slack.events.reply", tools[0].Target)
	}
	if tools[0].Target.CredentialMode != core.ConnectionModeNone {
		t.Fatalf("tool credential mode = %q, want %q", tools[0].Target.CredentialMode, core.ConnectionModeNone)
	}
}

func TestResolveToolsRejectsUndeclaredCredentialMode(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:    "events.reply",
				Title: "Reply",
			}}},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		PluginInvokes: map[string][]config.PluginInvocationDependency{
			"slackbot": {{
				Plugin:         "slack",
				Operation:      "chat.postMessage",
				CredentialMode: providermanifestv1.ConnectionModeNone,
			}},
		},
	})

	for _, tc := range []struct {
		name             string
		callerPluginName string
	}{
		{name: "public request"},
		{name: "caller without matching invoke", callerPluginName: "slackbot"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := manager.ResolveTools(context.Background(), &principal.Principal{
				SubjectID: principal.UserSubjectID("user-1"),
			}, coreagent.ResolveToolsRequest{
				CallerPluginName: tc.callerPluginName,
				ToolRefs: []coreagent.ToolRef{{
					Plugin:         "slack",
					Operation:      "events.reply",
					CredentialMode: core.ConnectionModeNone,
				}},
			})
			if !errors.Is(err, invocation.ErrAuthorizationDenied) {
				t.Fatalf("ResolveTools error = %v, want ErrAuthorizationDenied", err)
			}
		})
	}
}

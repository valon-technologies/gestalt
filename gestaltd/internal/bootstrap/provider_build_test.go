package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

type providerBuildOrderingInvoker struct{}

func (providerBuildOrderingInvoker) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusAccepted}, nil
}

type providerBuildOrderingWorkflowManager struct {
	unavailableWorkflowManager
}

func (providerBuildOrderingWorkflowManager) ListSchedules(context.Context, *principal.Principal) ([]*workflowmanager.ManagedSchedule, error) {
	return nil, nil
}

type providerBuildOrderingAgentProvider struct {
	coreagent.UnimplementedProvider
}

func (providerBuildOrderingAgentProvider) SupportsWorkspaceRequests() bool { return true }

func (providerBuildOrderingAgentProvider) CreateSession(_ context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	return &coreagent.Session{
		ID:           req.SessionID,
		ProviderName: "managed",
		Model:        req.Model,
		CreatedBy:    req.CreatedBy,
	}, nil
}

func TestPreparedProviderBuildsStartAfterHostServiceTargetsAvailable(t *testing.T) {
	t.Parallel()

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "caller",
		Operations: []catalog.CatalogOperation{{
			ID:     "sync",
			Method: http.MethodPost,
		}},
	})
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"caller": {
				ResolvedManifest:     newExecutableManifest("Caller", "Checks host service targets during startup"),
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"managed": {Source: config.ProviderSource{Path: "stub"}},
			},
		},
	}
	workflowRuntime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	agentRuntime, err := newAgentRuntime(cfg, workflowRuntime.StartupWaitTracker())
	if err != nil {
		t.Fatalf("newAgentRuntime: %v", err)
	}
	appInvoker := newLazyInvoker()
	workflowManager := newLazyWorkflowManager()
	agentManager := newLazyAgentManager()
	deps := Deps{
		AppInvocation:   appInvoker,
		WorkflowManager: workflowManager,
		AgentManager:    agentManager,
		WorkflowRuntime: workflowRuntime,
		AgentRuntime:    agentRuntime,
	}

	builds, err := prepareProviderBuilds(cfg, NewFactoryRegistry(), deps)
	if err != nil {
		t.Fatalf("prepareProviderBuilds: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(builds.providers) })

	var builderStarted atomic.Bool
	builderWaitingForAgent := make(chan struct{})
	var builderWaitingOnce atomic.Bool
	builder := func(ctx context.Context, name string, entry *config.ProviderEntry, deps Deps) (*ProviderBuildResult, error) {
		builderStarted.Store(true)
		caller := invocation.CallerProviderFromContext(ctx)
		if caller.Kind != invocation.ProviderKindApp || caller.Name != name {
			return nil, fmt.Errorf("provider build caller = %#v, want app/%s", caller, name)
		}
		if _, err := deps.AppInvocation.Invoke(ctx, nil, "roadmap", "", "sync", nil); err != nil {
			return nil, fmt.Errorf("app invocation target unavailable during %s build: %w", name, err)
		}
		if _, err := deps.WorkflowManager.ListSchedules(ctx, nil); err != nil {
			return nil, fmt.Errorf("workflow manager target unavailable during %s build: %w", name, err)
		}
		if !deps.AgentManager.Available() {
			return nil, fmt.Errorf("agent manager target unavailable during %s build", name)
		}
		if builderWaitingOnce.CompareAndSwap(false, true) {
			close(builderWaitingForAgent)
		}
		session, err := deps.AgentManager.CreateSession(ctx, &principal.Principal{
			SubjectID: "user:startup",
			Kind:      principal.KindUser,
			Source:    principal.SourceSession,
		}, coreagent.ManagerCreateSessionRequest{
			ProviderName: "managed",
			Model:        "gpt-startup",
			Workspace: &coreagent.Workspace{
				Checkouts: []coreagent.WorkspaceGitCheckout{{
					URL:  "https://github.com/valon-technologies/gestalt.git",
					Path: "repo",
				}},
				CWD: "repo",
			},
		})
		if err != nil {
			return nil, fmt.Errorf("agent manager target unavailable during %s build: %w", name, err)
		}
		if session == nil || session.ProviderName != "managed" {
			return nil, fmt.Errorf("agent manager returned session %#v, want managed provider", session)
		}
		return &ProviderBuildResult{
			Provider: &coretesting.StubIntegration{
				N:        name,
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name: name,
				},
			},
		}, nil
	}

	appInvoker.SetTarget(providerBuildOrderingInvoker{})
	workflowManager.SetTarget(providerBuildOrderingWorkflowManager{})
	agentManager.SetTarget(agentmanager.New(agentmanager.Config{Agent: agentRuntime}))
	ready, _, _, errResolver := builds.Start(context.Background(), deps, builder)
	select {
	case <-builderWaitingForAgent:
	case <-ready:
		if errs := errResolver(); len(errs) != 0 {
			t.Fatalf("provider build errors = %v", errs)
		}
		t.Fatal("provider build finished before waiting for configured agent provider")
	case <-time.After(2 * time.Second):
		t.Fatal("provider build did not reach configured agent provider wait")
	}
	select {
	case <-ready:
		t.Fatal("provider build finished before configured agent provider was published")
	default:
	}
	agentRuntime.PublishProvider("managed", providerBuildOrderingAgentProvider{})
	<-ready
	if errs := errResolver(); len(errs) != 0 {
		t.Fatalf("provider build errors = %v, want none", errs)
	}
	if !builderStarted.Load() {
		t.Fatal("provider builder was not started")
	}
}

func TestApplyProviderPaginationUsesExposedAlias(t *testing.T) {
	t.Parallel()

	def := &declarative.Definition{
		Operations: map[string]declarative.OperationDef{
			"listNotes": {
				Method: "GET",
				Path:   "/v1/notes",
			},
			"getNote": {
				Method: "GET",
				Path:   "/v1/notes/{note_id}",
			},
		},
	}
	manifestApp := &providermanifestv1.Spec{
		Pagination: &providermanifestv1.ManifestPaginationConfig{
			Style:        providermanifestv1.PaginationStyleCursor,
			CursorParam:  "cursor",
			LimitParam:   "page_size",
			DefaultLimit: 10,
			ResultsPath:  "notes",
		},
	}
	allowedOperations := map[string]*config.OperationOverride{
		"list_notes": {
			Alias:    "listNotes",
			Paginate: true,
		},
		"mcp_only": {
			Paginate: true,
		},
	}

	applyProviderPagination(def, manifestApp, allowedOperations)

	listOp := def.Operations["listNotes"]
	if listOp.Pagination == nil {
		t.Fatal("listNotes pagination = nil, want pagination on exposed alias")
	}
	if listOp.Pagination.CursorParam != "cursor" {
		t.Fatalf("CursorParam = %q, want cursor", listOp.Pagination.CursorParam)
	}
	if listOp.Pagination.LimitParam != "page_size" {
		t.Fatalf("LimitParam = %q, want page_size", listOp.Pagination.LimitParam)
	}
	if listOp.Pagination.DefaultLimit != 10 {
		t.Fatalf("DefaultLimit = %d, want 10", listOp.Pagination.DefaultLimit)
	}
	if listOp.Pagination.ResultsPath != "notes" {
		t.Fatalf("ResultsPath = %q, want notes", listOp.Pagination.ResultsPath)
	}
	if _, ok := def.Operations["list_notes"]; ok {
		t.Fatal("applyProviderPagination created original list_notes operation; want only exposed alias")
	}
	if _, ok := def.Operations["mcp_only"]; ok {
		t.Fatal("applyProviderPagination created absent mcp_only operation")
	}
}

func TestMCPAllowedOperationsForSpecCompositeFiltersOnlyWhenAPIIsPresent(t *testing.T) {
	t.Parallel()

	allowedOperations := map[string]*config.OperationOverride{
		"lookup": {
			Description: "GraphQL search",
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				SelectionSet: "nodes { id }",
			},
		},
		"mcp_lookup": {Description: "MCP lookup"},
		"list_notes": {Alias: "listNotes"},
	}
	apiCatalog := &catalog.Catalog{Operations: []catalog.CatalogOperation{
		{ID: "listNotes"},
	}}
	mcpCatalog := &catalog.Catalog{Operations: []catalog.CatalogOperation{
		{ID: "lookup"},
		{ID: "mcp_lookup"},
	}}

	filtered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, true, apiCatalog, mcpCatalog, nil)
	if !includeMCP {
		t.Fatal("includeMCP = false, want true for matching static MCP catalog")
	}
	if len(filtered) != 1 || filtered["mcp_lookup"] == nil {
		t.Fatalf("filtered allowedOperations = %#v, want only mcp_lookup", filtered)
	}
	if _, ok := filtered["lookup"]; ok {
		t.Fatal("GraphQL-tagged operation should not be passed to MCP filter when API surface exists")
	}

	unfiltered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, false, nil, mcpCatalog, nil)
	if !includeMCP {
		t.Fatal("includeMCP = false, want true for MCP-only provider")
	}
	if len(unfiltered) != len(allowedOperations) {
		t.Fatalf("unfiltered allowedOperations = %#v, want all operations for MCP-only provider", unfiltered)
	}
}

func TestMCPAllowedOperationsForSpecCompositePreservesDynamicAllowlist(t *testing.T) {
	t.Parallel()

	allowedOperations := map[string]*config.OperationOverride{
		"searchIssues": {
			Description: "GraphQL search",
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				SelectionSet: "nodes { id }",
			},
		},
		"lookup":     {Description: "MCP lookup"},
		"list_notes": {Alias: "listNotes"},
	}
	apiCatalog := &catalog.Catalog{Operations: []catalog.CatalogOperation{
		{ID: "listNotes"},
	}}

	filtered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, true, apiCatalog, &catalog.Catalog{}, nil)
	if !includeMCP {
		t.Fatal("includeMCP = false, want true for dynamic MCP allowlist")
	}
	if len(filtered) != 1 || filtered["lookup"] == nil {
		t.Fatalf("filtered allowedOperations = %#v, want only dynamic MCP operation", filtered)
	}
}

func TestMCPAllowedOperationsForSpecCompositeFiltersLegacyGraphQLSelections(t *testing.T) {
	t.Parallel()

	allowedOperations := map[string]*config.OperationOverride{
		"searchIssues": {Description: "legacy GraphQL search"},
		"lookup":       {Description: "MCP lookup"},
	}

	filtered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, true, &catalog.Catalog{}, &catalog.Catalog{}, map[string]string{
		"searchIssues": "nodes { id }",
	})
	if !includeMCP {
		t.Fatal("includeMCP = false, want true for dynamic MCP allowlist")
	}
	if len(filtered) != 1 || filtered["lookup"] == nil {
		t.Fatalf("filtered allowedOperations = %#v, want only lookup", filtered)
	}
	if _, ok := filtered["searchIssues"]; ok {
		t.Fatal("legacy GraphQL selection operation should not be passed to dynamic MCP filter")
	}
}

func TestMCPAllowedOperationsForSpecCompositeOmitsMCPWhenNoMCPAllowlistEntries(t *testing.T) {
	t.Parallel()

	allowedOperations := map[string]*config.OperationOverride{
		"searchIssues": {
			Description: "GraphQL search",
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				SelectionSet: "nodes { id }",
			},
		},
	}

	filtered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, true, nil, &catalog.Catalog{}, nil)
	if includeMCP {
		t.Fatalf("includeMCP = true with filtered allowedOperations %#v, want false", filtered)
	}
	if filtered != nil {
		t.Fatalf("filtered allowedOperations = %#v, want nil", filtered)
	}
}

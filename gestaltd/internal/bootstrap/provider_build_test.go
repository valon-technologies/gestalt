package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"
)

type providerBuildOrderingAgentProvider struct {
	coreagent.UnimplementedProvider
}

func (providerBuildOrderingAgentProvider) SupportsWorkspaceRequests() bool { return true }

func (providerBuildOrderingAgentProvider) CreateSession(_ context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	return &coreagent.Session{
		ID:                 uuid.NewString(),
		ProviderName:       "managed",
		Model:              req.GetModel(),
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId()),
	}, nil
}

type closeRecordingAgentProvider struct {
	providerBuildOrderingAgentProvider
	closed *atomic.Bool
}

func (p closeRecordingAgentProvider) Close() error {
	if p.closed != nil {
		p.closed.Store(true)
	}
	return nil
}

type closeRecordingWorkflowProvider struct {
	startupTestWorkflowProvider
	closed *atomic.Bool
}

func (p closeRecordingWorkflowProvider) Close() error {
	if p.closed != nil {
		p.closed.Store(true)
	}
	return nil
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
	workflowRuntime.InitProviderPlaceholders(cfg.Providers.Workflow)
	agentRuntime, err := newAgentRuntime(cfg, workflowRuntime.StartupWaitTracker())
	if err != nil {
		t.Fatalf("newAgentRuntime: %v", err)
	}
	agentManager := newLazyAgentManager()
	deps := Deps{
		AgentManager:    agentManager,
		WorkflowRuntime: workflowRuntime,
		AgentRuntime:    agentRuntime,
	}

	builds, err := prepareProviderBuilds(cfg, NewFactoryRegistry(), deps)
	if err != nil {
		t.Fatalf("prepareProviderBuilds: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(builds.providers) })
	factories := NewFactoryRegistry()
	factories.Agent = func(context.Context, string, yaml.Node, []runtimehost.HostService, Deps) (coreagent.Provider, error) {
		return providerBuildOrderingAgentProvider{}, nil
	}

	var builderStarted atomic.Bool
	builderWaitingForAgent := make(chan struct{})
	var builderWaitingOnce atomic.Bool
	builder := func(ctx context.Context, name string, entry *config.ProviderEntry, deps Deps) (*ProviderBuildResult, error) {
		builderStarted.Store(true)
		caller := invocation.CallerProviderFromContext(ctx)
		if caller.Kind != invocation.ProviderKindApp || caller.Name != name {
			return nil, fmt.Errorf("provider build caller = %#v, want app/%s", caller, name)
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
			Source:    principal.SourceBearer,
		}, &proto.CreateAgentProviderSessionRequest{
			ProviderName: "managed",
			Model:        "gpt-startup",
			Workspace: testAgentWorkspaceToProto(&coreagent.Workspace{
				Checkouts: []coreagent.WorkspaceGitCheckout{{
					URL:  "https://github.com/valon-technologies/gestalt.git",
					Path: "repo",
				}},
				CWD: "repo",
			}),
		})
		if err != nil {
			return nil, fmt.Errorf("agent manager target unavailable during %s build: %w", name, err)
		}
		if session == nil || session.ProviderName != "managed" {
			return nil, fmt.Errorf("agent manager returned session %#v, want managed provider", session)
		}
		return &ProviderBuildResult{
			Provider: &coretesting.StubIntegration{
				N: name,
				CatalogVal: &catalog.Catalog{
					Name: name,
				},
			},
		}, nil
	}

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
	workflows, agents, err := buildWorkflowsAndAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildWorkflowsAndAgents: %v", err)
	}
	defer func() { _ = closeWorkflows(workflows...) }()
	defer func() { _ = closeAgents(agents...) }()
	<-ready
	if errs := errResolver(); len(errs) != 0 {
		t.Fatalf("provider build errors = %v, want none", errs)
	}
	if !builderStarted.Load() {
		t.Fatal("provider builder was not started")
	}
}

func TestBuildConfiguredProvidersUnpublishesSuccessesOnPartialFailure(t *testing.T) {
	t.Parallel()

	closed := &atomic.Bool{}
	runtime := &agentRuntime{providers: map[string]coreagent.Provider{}}
	boom := errors.New("boom")
	providers, _, err := buildConfiguredProviders(context.Background(), map[string]*config.ProviderEntry{
		"ok":  {Source: config.ProviderSource{Path: "stub"}},
		"bad": {Source: config.ProviderSource{Path: "stub"}},
	}, func(_ context.Context, name string, _ *config.ProviderEntry) (coreagent.Provider, error) {
		if name == "bad" {
			return nil, boom
		}
		return closeRecordingAgentProvider{closed: closed}, nil
	}, runtime.PublishProvider, func(name string, err error) {
		runtime.FailStartupProvider(name, err)
	}, runtime.UnpublishProvider, runtime.FailPendingProviders, closeAgents, func(name string, err error) error {
		return fmt.Errorf("bootstrap: agent from resource %q: %w", name, err)
	})
	if !errors.Is(err, boom) {
		t.Fatalf("buildConfiguredProviders err = %v, want boom", err)
	}
	if len(providers) != 0 {
		t.Fatalf("providers = %d, want none on partial failure", len(providers))
	}
	if !closed.Load() {
		t.Fatal("successful provider was not closed after partial failure")
	}
	if _, _, err := runtime.ResolveProvider(context.Background(), "ok"); err == nil {
		t.Fatal("successful provider remained published after partial failure")
	}
}

func TestBuildWorkflowsAndAgentsClosesSuccessesOnCrossGroupFailure(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Workflow: map[string]*config.ProviderEntry{
				"temporal": {Source: config.ProviderSource{Path: "stub"}},
			},
			Agent: map[string]*config.ProviderEntry{
				"managed": {Source: config.ProviderSource{Path: "stub"}},
			},
		},
	}
	workflowRuntime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	workflowRuntime.InitProviderPlaceholders(cfg.Providers.Workflow)
	agentRuntime, err := newAgentRuntime(cfg, workflowRuntime.StartupWaitTracker())
	if err != nil {
		t.Fatalf("newAgentRuntime: %v", err)
	}
	deps := Deps{
		WorkflowRuntime: workflowRuntime,
		AgentRuntime:    agentRuntime,
	}
	closed := &atomic.Bool{}
	boom := errors.New("boom")
	factories := NewFactoryRegistry()
	factories.Workflow = func(context.Context, string, yaml.Node, []runtimehost.HostService, Deps) (coreworkflow.Provider, error) {
		return closeRecordingWorkflowProvider{closed: closed}, nil
	}
	factories.Agent = func(context.Context, string, yaml.Node, []runtimehost.HostService, Deps) (coreagent.Provider, error) {
		return nil, boom
	}

	workflows, agents, err := buildWorkflowsAndAgents(context.Background(), cfg, factories, deps)
	if !errors.Is(err, boom) {
		t.Fatalf("buildWorkflowsAndAgents err = %v, want boom", err)
	}
	if got := len(workflows); got != 0 {
		t.Fatalf("workflows = %d, want 0 after cross-group cleanup", got)
	}
	if got := len(agents); got != 0 {
		t.Fatalf("agents = %d, want 0", got)
	}
	if !closed.Load() {
		t.Fatal("successful workflow provider was not closed by cross-group cleanup")
	}
	if _, _, err := workflowRuntime.ResolveProvider(context.Background(), "temporal"); err == nil {
		t.Fatal("successful workflow provider remained published after cross-group failure")
	}
}

func TestBuildConfiguredProvidersLeavesLateSuccessUnpublishedAfterFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	firstPublished := make(chan struct{})
	failProcessed := make(chan struct{})
	var firstPublishedOnce sync.Once
	var mu sync.Mutex
	var published []string
	var failed []string
	var unpublished []string
	var closed atomic.Int32

	providers, _, err := buildConfiguredProviders(context.Background(), map[string]*config.ProviderEntry{
		"first": {Source: config.ProviderSource{Path: "stub"}},
		"bad":   {Source: config.ProviderSource{Path: "stub"}},
		"late":  {Source: config.ProviderSource{Path: "stub"}},
	}, func(_ context.Context, name string, _ *config.ProviderEntry) (coreagent.Provider, error) {
		switch name {
		case "first":
			return providerBuildOrderingAgentProvider{}, nil
		case "bad":
			select {
			case <-firstPublished:
			case <-time.After(2 * time.Second):
				return nil, fmt.Errorf("timed out waiting for first provider publish")
			}
			return nil, boom
		case "late":
			select {
			case <-failProcessed:
			case <-time.After(2 * time.Second):
				return nil, fmt.Errorf("timed out waiting for failure handling")
			}
			return providerBuildOrderingAgentProvider{}, nil
		default:
			return nil, fmt.Errorf("unexpected provider %q", name)
		}
	}, func(name string, _ coreagent.Provider) {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, name)
		if name == "first" {
			firstPublishedOnce.Do(func() { close(firstPublished) })
		}
	}, func(name string, _ error) {
		mu.Lock()
		defer mu.Unlock()
		failed = append(failed, name)
	}, func(name string) {
		mu.Lock()
		defer mu.Unlock()
		unpublished = append(unpublished, name)
	}, func(error) {
		close(failProcessed)
	}, func(providers ...coreagent.Provider) error {
		closed.Add(int32(len(providers)))
		return nil
	}, func(name string, err error) error {
		return fmt.Errorf("bootstrap: agent from resource %q: %w", name, err)
	})

	if !errors.Is(err, boom) {
		t.Fatalf("buildConfiguredProviders err = %v, want boom", err)
	}
	if len(providers) != 0 {
		t.Fatalf("providers = %d, want none on partial failure", len(providers))
	}
	if got, want := published, []string{"first"}; !slices.Equal(got, want) {
		t.Fatalf("published providers = %v, want %v", got, want)
	}
	if got, want := failed, []string{"bad"}; !slices.Equal(got, want) {
		t.Fatalf("failed providers = %v, want %v", got, want)
	}
	if got, want := unpublished, []string{"first"}; !slices.Equal(got, want) {
		t.Fatalf("unpublished providers = %v, want %v", got, want)
	}
	if got := closed.Load(); got != 2 {
		t.Fatalf("closed providers = %d, want 2", got)
	}
}

func TestAgentRuntimeSkipsConfiguredProviderPublishAfterFailure(t *testing.T) {
	t.Parallel()

	runtime, err := newAgentRuntime(&config.Config{
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"managed": {},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("newAgentRuntime: %v", err)
	}
	runtime.FailPendingProviders(errors.New("boom"))
	runtime.PublishProvider("managed", providerBuildOrderingAgentProvider{})
	if _, _, err := runtime.ResolveProvider(context.Background(), "managed"); err == nil {
		t.Fatal("configured provider was published after startup failure")
	}
}

func TestAgentRuntimeFailStartupProviderKeepsPublishedProvider(t *testing.T) {
	t.Parallel()

	runtime, err := newAgentRuntime(&config.Config{
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				" managed ": {},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("newAgentRuntime: %v", err)
	}
	original := &providerBuildOrderingAgentProvider{}
	runtime.PublishProvider("managed", original)
	runtime.FailStartupProvider(" managed ", errors.New("boom"))
	_, got, err := runtime.ResolveProvider(context.Background(), "managed")
	if err != nil {
		t.Fatalf("ResolveProvider after published FailStartupProvider: %v", err)
	}
	if got != original {
		t.Fatalf("ResolveProvider after published FailStartupProvider = %T, want original provider", got)
	}
}

func TestAgentRuntimeSkipsConfiguredProviderPublishAfterStartupFailure(t *testing.T) {
	t.Parallel()

	runtime, err := newAgentRuntime(&config.Config{
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"managed": {},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("newAgentRuntime: %v", err)
	}
	runtime.FailStartupProvider("managed", errors.New("boom"))
	runtime.PublishProvider("managed", providerBuildOrderingAgentProvider{})
	if _, _, err := runtime.ResolveProvider(context.Background(), "managed"); err == nil {
		t.Fatal("configured provider was published after startup failure")
	}
}

func TestWorkflowRuntimeFailStartupProviderKeepsPublishedProvider(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Workflow: map[string]*config.ProviderEntry{
				" temporal ": {},
			},
		},
	}
	runtime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	runtime.InitProviderPlaceholders(cfg.Providers.Workflow)
	original := &startupTestWorkflowProvider{}
	runtime.PublishProvider("temporal", original)
	runtime.FailStartupProvider(" temporal ", errors.New("boom"))
	_, got, err := runtime.ResolveProvider(context.Background(), "temporal")
	if err != nil {
		t.Fatalf("ResolveProvider after published FailStartupProvider: %v", err)
	}
	if got != original {
		t.Fatalf("ResolveProvider after published FailStartupProvider = %T, want original provider", got)
	}
}

func TestWorkflowRuntimeSkipsConfiguredProviderPublishAfterStartupFailure(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Workflow: map[string]*config.ProviderEntry{
				"temporal": {},
			},
		},
	}
	runtime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	runtime.InitProviderPlaceholders(cfg.Providers.Workflow)
	runtime.FailStartupProvider("temporal", errors.New("boom"))
	runtime.PublishProvider("temporal", startupTestWorkflowProvider{})
	if _, _, err := runtime.ResolveProvider(context.Background(), "temporal"); err == nil {
		t.Fatal("configured provider was published after startup failure")
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
				Document: "query { lookup { nodes { id } } }",
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

	filtered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, true, apiCatalog, mcpCatalog)
	if !includeMCP {
		t.Fatal("includeMCP = false, want true for matching static catalog")
	}
	if len(filtered) != 1 || filtered["mcp_lookup"] == nil {
		t.Fatalf("filtered allowedOperations = %#v, want only mcp_lookup", filtered)
	}
	if _, ok := filtered["lookup"]; ok {
		t.Fatal("GraphQL-tagged operation should not be passed to MCP filter when API surface exists")
	}

	unfiltered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, false, nil, mcpCatalog)
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
				Document: "query { searchIssues { nodes { id } } }",
			},
		},
		"lookup":     {Description: "MCP lookup"},
		"list_notes": {Alias: "listNotes"},
	}
	apiCatalog := &catalog.Catalog{Operations: []catalog.CatalogOperation{
		{ID: "listNotes"},
	}}

	filtered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, true, apiCatalog, &catalog.Catalog{})
	if !includeMCP {
		t.Fatal("includeMCP = false, want true for dynamic MCP allowlist")
	}
	if len(filtered) != 1 || filtered["lookup"] == nil {
		t.Fatalf("filtered allowedOperations = %#v, want only dynamic MCP operation", filtered)
	}
}

func TestMCPAllowedOperationsForSpecCompositeOmitsMCPWhenNoMCPAllowlistEntries(t *testing.T) {
	t.Parallel()

	allowedOperations := map[string]*config.OperationOverride{
		"searchIssues": {
			Description: "GraphQL search",
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				Document: "query { searchIssues { nodes { id } } }",
			},
		},
	}

	filtered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, true, nil, &catalog.Catalog{})
	if includeMCP {
		t.Fatalf("includeMCP = true with filtered allowedOperations %#v, want false", filtered)
	}
	if filtered != nil {
		t.Fatalf("filtered allowedOperations = %#v, want nil", filtered)
	}
}

type recordingRemoteAppClient struct {
	mu    sync.Mutex
	calls []remoteAppInvokeCall
	err   error
}

type remoteAppInvokeCall struct {
	app       string
	operation string
}

func (c *recordingRemoteAppClient) Invoke(_ context.Context, req *proto.AppInvokeRequest, _ ...grpc.CallOption) (*proto.OperationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	c.calls = append(c.calls, remoteAppInvokeCall{
		app:       req.GetApp(),
		operation: req.GetOperation(),
	})
	return &proto.OperationResult{Status: 202, Body: []byte("relayed")}, nil
}

func (c *recordingRemoteAppClient) InvokeGraphQL(context.Context, *proto.AppInvokeGraphQLRequest, ...grpc.CallOption) (*proto.OperationResult, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAppClient) snapshot() []remoteAppInvokeCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]remoteAppInvokeCall, len(c.calls))
	copy(out, c.calls)
	return out
}

func remoteRoutingAppEntry(t *testing.T, name string, operations ...string) *config.ProviderEntry {
	t.Helper()
	catalogOps := make([]catalog.CatalogOperation, len(operations))
	for i, op := range operations {
		catalogOps[i] = catalog.CatalogOperation{ID: op}
	}
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name:       name,
		Operations: catalogOps,
	})
	return &config.ProviderEntry{
		ResolvedManifest:     newExecutableManifest(name, name),
		ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
	}
}

func remoteRoutingConfig(t *testing.T, localDevActive map[string]bool) *config.Config {
	t.Helper()
	apps := map[string]*config.ProviderEntry{
		"linear":        remoteRoutingAppEntry(t, "linear", "issues.list"),
		"valon-profile": remoteRoutingAppEntry(t, "valon-profile", "issues.list"),
		"ci-cd":         remoteRoutingAppEntry(t, "ci-cd", "ping"),
	}
	for name, active := range localDevActive {
		if entry := apps[name]; entry != nil {
			entry.DevActive = active
		}
	}
	return &config.Config{
		Server: config.ServerConfig{Remote: "https://remote.test"},
		Apps:   apps,
	}
}

func localRoutingAppStub(name string) *coretesting.StubIntegration {
	return &coretesting.StubIntegration{
		N:        name,
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Operations: []catalog.CatalogOperation{{ID: "ping"}},
		},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: 201, Body: []byte("local")}, nil
		},
	}
}

func newRemoteRoutingBroker(t *testing.T, cfg *config.Config, remoteApp proto.AppClient, localApps ...core.Provider) *invocation.Broker {
	t.Helper()
	reg := registry.New()
	for _, provider := range localApps {
		if err := reg.Providers.Register(provider.Name(), provider); err != nil {
			t.Fatalf("Register %q: %v", provider.Name(), err)
		}
	}
	if err := registerRemoteApps(&reg.Providers, cfg, Deps{RemoteClients: &remote.ClientSet{App: remoteApp}}); err != nil {
		t.Fatalf("registerRemoteApps: %v", err)
	}
	svc := testutil.NewStubServices(t)
	return invocation.NewBroker(&reg.Providers, svc.Users, svc.ExternalCredentials)
}

func remoteRoutingPrincipal(scopes ...string) *principal.Principal {
	return &principal.Principal{
		SubjectID: "user:dev@example.com",
		Kind:      principal.KindUser,
		Scopes:    scopes,
	}
}

func invokeRemoteRoutingApp(t *testing.T, broker *invocation.Broker, app, operation string) *core.OperationResult {
	t.Helper()
	result, err := broker.Invoke(context.Background(), remoteRoutingPrincipal(app), app, "", operation, nil)
	if err != nil {
		t.Fatalf("Invoke(%q): %v", app, err)
	}
	return result
}

func TestRemoteAppRoutingLifecycles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		localApps     map[string]bool
		localStubs    []core.Provider
		localChecks   []struct{ app, operation string }
		remoteChecks  []struct{ app, operation string }
		wantRemoteApp string
		wantRemoteN   int
	}{
		{
			name:      "nothing local",
			localApps: nil,
			remoteChecks: []struct{ app, operation string }{
				{"linear", "issues.list"},
				{"valon-profile", "issues.list"},
			},
			wantRemoteN: 2,
		},
		{
			name:       "ci-cd local",
			localApps:  map[string]bool{"ci-cd": true},
			localStubs: []core.Provider{localRoutingAppStub("ci-cd")},
			localChecks: []struct{ app, operation string }{
				{"ci-cd", "ping"},
			},
			remoteChecks: []struct{ app, operation string }{
				{"linear", "issues.list"},
				{"valon-profile", "issues.list"},
			},
			wantRemoteN: 2,
		},
		{
			name:      "ci-cd and valon-profile local",
			localApps: map[string]bool{"ci-cd": true, "valon-profile": true},
			localStubs: []core.Provider{
				localRoutingAppStub("ci-cd"),
				localRoutingAppStub("valon-profile"),
			},
			localChecks: []struct{ app, operation string }{
				{"valon-profile", "ping"},
			},
			remoteChecks: []struct{ app, operation string }{
				{"linear", "issues.list"},
			},
			wantRemoteApp: "linear",
			wantRemoteN:   1,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			remoteClient := &recordingRemoteAppClient{}
			broker := newRemoteRoutingBroker(t, remoteRoutingConfig(t, tc.localApps), remoteClient, tc.localStubs...)

			for _, check := range tc.localChecks {
				result := invokeRemoteRoutingApp(t, broker, check.app, check.operation)
				if result.Status != 201 || string(result.Body) != "local" {
					t.Fatalf("Invoke(%q) = %#v, want local 201", check.app, result)
				}
			}
			for _, check := range tc.remoteChecks {
				result := invokeRemoteRoutingApp(t, broker, check.app, check.operation)
				if result.Status != 202 || string(result.Body) != "relayed" {
					t.Fatalf("Invoke(%q) = %#v, want remote 202 relayed", check.app, result)
				}
			}

			calls := remoteClient.snapshot()
			if len(calls) != tc.wantRemoteN {
				t.Fatalf("remote client calls = %d, want %d", len(calls), tc.wantRemoteN)
			}
			if tc.wantRemoteApp != "" && (len(calls) != 1 || calls[0].app != tc.wantRemoteApp) {
				t.Fatalf("remote client calls = %#v, want %q only", calls, tc.wantRemoteApp)
			}
		})
	}
}

func TestRemoteAppRoutingFailureSemantics(t *testing.T) {
	t.Parallel()

	t.Run("undeclared provider remains not found", func(t *testing.T) {
		t.Parallel()

		remoteClient := &recordingRemoteAppClient{}
		broker := newRemoteRoutingBroker(t, remoteRoutingConfig(t, nil), remoteClient)

		_, err := broker.Invoke(context.Background(), remoteRoutingPrincipal("missing"), "missing", "", "op", nil)
		if !errors.Is(err, invocation.ErrProviderNotFound) {
			t.Fatalf("err = %v, want ErrProviderNotFound", err)
		}
	})

	t.Run("dev active does not fall back to remote", func(t *testing.T) {
		t.Parallel()

		remoteClient := &recordingRemoteAppClient{}
		cfg := remoteRoutingConfig(t, map[string]bool{"linear": true})
		broker := newRemoteRoutingBroker(t, cfg, remoteClient)

		_, err := broker.Invoke(context.Background(), remoteRoutingPrincipal("linear"), "linear", "", "issues.list", nil)
		if !errors.Is(err, invocation.ErrProviderNotFound) {
			t.Fatalf("err = %v, want ErrProviderNotFound without local provider", err)
		}
		if len(remoteClient.snapshot()) != 0 {
			t.Fatalf("remote client calls = %d, want 0", len(remoteClient.snapshot()))
		}
	})

	t.Run("remote client auth error surfaces not authenticated", func(t *testing.T) {
		t.Parallel()

		remoteClient := &recordingRemoteAppClient{
			err: status.Error(codes.Unauthenticated, "invalid token"),
		}
		broker := newRemoteRoutingBroker(t, remoteRoutingConfig(t, nil), remoteClient)

		_, err := broker.Invoke(context.Background(), remoteRoutingPrincipal("linear"), "linear", "", "issues.list", nil)
		if err == nil {
			t.Fatal("expected auth error, got nil")
		}
		if !errors.Is(err, invocation.ErrNotAuthenticated) {
			t.Fatalf("err = %v, want ErrNotAuthenticated", err)
		}
		if len(remoteClient.snapshot()) != 0 {
			t.Fatalf("remote client calls = %d, want 0", len(remoteClient.snapshot()))
		}
	})
}

func TestProviderBuildsLocal(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Server: config.ServerConfig{Remote: "https://remote.test"}}

	if !providerBuildsLocal(cfg, &config.ProviderEntry{DevActive: true}) {
		t.Fatal("DevActive entry should build local")
	}
	if providerBuildsLocal(cfg, &config.ProviderEntry{}) {
		t.Fatal("non-DevActive entry with remote configured should not build local")
	}
	if !providerBuildsLocal(&config.Config{}, &config.ProviderEntry{}) {
		t.Fatal("entry without remote configured should build local")
	}
	if providerBuildsLocal(cfg, nil) {
		t.Fatal("nil entry should not build local")
	}
}

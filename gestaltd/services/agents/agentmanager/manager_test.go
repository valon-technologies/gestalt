package agentmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/agentgrant"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
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

func newAgentManagerTestToolGrants(t testing.TB) *agentgrant.Manager {
	t.Helper()
	grants, err := agentgrant.NewManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("agentgrant.NewManager: %v", err)
	}
	return grants
}

func newTestManager(t testing.TB, cfg Config) *Manager {
	t.Helper()
	if cfg.ToolGrants == nil {
		cfg.ToolGrants = newAgentManagerTestToolGrants(t)
	}
	return New(cfg)
}

type routeCountingAgentControl struct {
	defaultName string
	names       []string
	providers   map[string]*routeCountingAgentProvider
}

func (c *routeCountingAgentControl) ResolveProviderSelection(name string) (string, coreagent.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(c.defaultName)
	}
	provider, err := c.ResolveProvider(name)
	if err != nil {
		return "", nil, err
	}
	return name, provider, nil
}

func (c *routeCountingAgentControl) ResolveProvider(name string) (coreagent.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrAgentProviderRequired
	}
	provider := c.providers[name]
	if provider == nil {
		return nil, NewAgentProviderNotAvailableError(name)
	}
	return provider, nil
}

func (c *routeCountingAgentControl) ProviderNames() []string {
	return append([]string(nil), c.names...)
}

type routeCountingAgentProvider struct {
	coreagent.UnimplementedProvider
	name            string
	sessions        map[string]*coreagent.Session
	turns           map[string]*coreagent.Turn
	turnIDOverride  string
	cancelStatus    coreagent.ExecutionStatus
	getSessionCalls int
	getTurnCalls    int
}

func newRouteCountingAgentProvider(name string) *routeCountingAgentProvider {
	return &routeCountingAgentProvider{
		name:     name,
		sessions: map[string]*coreagent.Session{},
		turns:    map[string]*coreagent.Turn{},
	}
}

func (p *routeCountingAgentProvider) CreateSession(_ context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	session := &coreagent.Session{
		ID:           req.SessionID,
		ProviderName: p.name,
		Model:        req.Model,
		ClientRef:    req.ClientRef,
		State:        coreagent.SessionStateActive,
		CreatedBy:    req.CreatedBy,
	}
	p.sessions[session.ID] = session
	return cloneRouteSession(session), nil
}

func (p *routeCountingAgentProvider) GetSession(_ context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	p.getSessionCalls++
	session := p.sessions[strings.TrimSpace(req.SessionID)]
	if session == nil {
		return nil, core.ErrNotFound
	}
	return cloneRouteSession(session), nil
}

func (p *routeCountingAgentProvider) CreateTurn(_ context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	turnID := req.TurnID
	if strings.TrimSpace(p.turnIDOverride) != "" {
		turnID = p.turnIDOverride
	}
	turn := &coreagent.Turn{
		ID:           turnID,
		SessionID:    req.SessionID,
		ProviderName: p.name,
		Model:        req.Model,
		Status:       coreagent.ExecutionStatusRunning,
		Messages:     append([]coreagent.Message(nil), req.Messages...),
		CreatedBy:    req.CreatedBy,
		ExecutionRef: req.ExecutionRef,
	}
	p.turns[turn.ID] = turn
	return cloneRouteTurn(turn), nil
}

func (p *routeCountingAgentProvider) GetTurn(_ context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	p.getTurnCalls++
	turn := p.turns[strings.TrimSpace(req.TurnID)]
	if turn == nil {
		return nil, core.ErrNotFound
	}
	return cloneRouteTurn(turn), nil
}

func (p *routeCountingAgentProvider) CancelTurn(_ context.Context, req coreagent.CancelTurnRequest) (*coreagent.Turn, error) {
	turn := p.turns[strings.TrimSpace(req.TurnID)]
	if turn == nil {
		return nil, core.ErrNotFound
	}
	status := p.cancelStatus
	if status == "" {
		status = coreagent.ExecutionStatusCanceled
	}
	turn.Status = status
	return cloneRouteTurn(turn), nil
}

func (p *routeCountingAgentProvider) GetCapabilities(context.Context, coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{NativeToolSearch: true}, nil
}

func cloneRouteSession(session *coreagent.Session) *coreagent.Session {
	if session == nil {
		return nil
	}
	cloned := *session
	return &cloned
}

func cloneRouteTurn(turn *coreagent.Turn) *coreagent.Turn {
	if turn == nil {
		return nil
	}
	cloned := *turn
	cloned.Messages = append([]coreagent.Message(nil), turn.Messages...)
	return &cloned
}

func TestAgentRouteCacheEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	var cache agentRouteCache
	cache.remember("old", "alpha")
	cache.remember("warm", "alpha")
	if got := cache.get("old"); got != "alpha" {
		t.Fatalf("cache.get(old) = %q, want alpha", got)
	}
	cache.remember("new", "alpha")
	cache.trim(2)

	if got := cache.get("warm"); got != "" {
		t.Fatalf("cache.get(warm) = %q, want evicted", got)
	}
	if got := cache.get("old"); got != "alpha" {
		t.Fatalf("cache.get(old) = %q, want retained alpha", got)
	}
	if got := cache.get("new"); got != "alpha" {
		t.Fatalf("cache.get(new) = %q, want retained alpha", got)
	}
}

func TestManagerCachesProviderRoutesForOwnedSessionAndTurn(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	beta := newRouteCountingAgentProvider("beta")
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"beta", "alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
				"beta":  beta,
			},
		},
		ToolGrants: newAgentManagerTestToolGrants(t),
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	alpha.getSessionCalls = 0
	beta.getSessionCalls = 0
	if _, err := manager.GetSession(context.Background(), p, session.ID); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if alpha.getSessionCalls != 1 || beta.getSessionCalls != 0 {
		t.Fatalf("GetSession calls = alpha:%d beta:%d, want alpha:1 beta:0", alpha.getSessionCalls, beta.getSessionCalls)
	}

	alpha.getSessionCalls = 0
	beta.getSessionCalls = 0
	turn, err := manager.CreateTurn(context.Background(), p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "test-model",
		Messages:  []coreagent.Message{{Role: "user", Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if alpha.getSessionCalls != 1 || beta.getSessionCalls != 0 {
		t.Fatalf("CreateTurn session lookup calls = alpha:%d beta:%d, want alpha:1 beta:0", alpha.getSessionCalls, beta.getSessionCalls)
	}

	alpha.getTurnCalls = 0
	beta.getTurnCalls = 0
	if _, err := manager.GetTurn(context.Background(), p, turn.ID); err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if alpha.getTurnCalls != 1 || beta.getTurnCalls != 0 {
		t.Fatalf("GetTurn calls = alpha:%d beta:%d, want alpha:1 beta:0", alpha.getTurnCalls, beta.getTurnCalls)
	}
}

func TestManagerCreateTurnAcceptsProviderOwnedIDForIdempotentReplay(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	alpha.turnIDOverride = "provider-turn-1"
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		ToolGrants: newAgentManagerTestToolGrants(t),
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := manager.CreateTurn(context.Background(), p, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "turn-replay",
		Model:          "test-model",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn.ID != "provider-turn-1" {
		t.Fatalf("CreateTurn ID = %q, want provider-turn-1", turn.ID)
	}

	alpha.getTurnCalls = 0
	if _, err := manager.GetTurn(context.Background(), p, "provider-turn-1"); err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if alpha.getTurnCalls != 1 {
		t.Fatalf("GetTurn calls = %d, want 1 cached provider lookup", alpha.getTurnCalls)
	}
}

func TestManagerCancelTurnRevokesToolGrantWithoutBootstrapWrapper(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	grants := newAgentManagerTestToolGrants(t)
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		ToolGrants: grants,
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := manager.CreateTurn(context.Background(), p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "test-model",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	grant, err := grants.Mint(agentgrant.Grant{
		ProviderName: "alpha",
		SessionID:    session.ID,
		TurnID:       turn.ID,
		SubjectID:    principal.UserSubjectID("user-1"),
		SubjectKind:  string(principal.KindUser),
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if _, err := grants.Resolve(grant); err != nil {
		t.Fatalf("Resolve before cancel: %v", err)
	}

	if _, err := manager.CancelTurn(context.Background(), p, turn.ID, "done"); err != nil {
		t.Fatalf("CancelTurn: %v", err)
	}
	if _, err := grants.Resolve(grant); err == nil {
		t.Fatal("Resolve after cancel error = nil, want revoked grant")
	} else if !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("Resolve after cancel error = %v, want revoked grant", err)
	}
}

func TestManagerCancelTurnRevokesExecutionRefGrantWithoutBootstrapWrapper(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	alpha.turnIDOverride = "provider-turn-1"
	grants := newAgentManagerTestToolGrants(t)
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		ToolGrants: grants,
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := manager.CreateTurn(context.Background(), p, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "provider-owned-turn",
		Model:          "test-model",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn.ID != "provider-turn-1" {
		t.Fatalf("CreateTurn ID = %q, want provider-turn-1", turn.ID)
	}
	if strings.TrimSpace(turn.ExecutionRef) == "" || turn.ExecutionRef == turn.ID {
		t.Fatalf("CreateTurn ExecutionRef = %q, want generated requested ID distinct from provider turn ID %q", turn.ExecutionRef, turn.ID)
	}
	grant, err := grants.Mint(agentgrant.Grant{
		ProviderName: "alpha",
		SessionID:    session.ID,
		TurnID:       turn.ExecutionRef,
		SubjectID:    principal.UserSubjectID("user-1"),
		SubjectKind:  string(principal.KindUser),
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if _, err := grants.Resolve(grant); err != nil {
		t.Fatalf("Resolve before cancel: %v", err)
	}

	if _, err := manager.CancelTurn(context.Background(), p, turn.ID, "done"); err != nil {
		t.Fatalf("CancelTurn: %v", err)
	}
	if _, err := grants.Resolve(grant); err == nil {
		t.Fatal("Resolve after cancel error = nil, want revoked grant")
	} else if !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("Resolve after cancel error = %v, want revoked grant", err)
	}
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
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
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
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
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

func TestSearchToolsCompactsOversizedInputSchemaFromParameters(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:          "pulls.list",
				Title:       "List Pull Requests",
				Description: "List pull requests for a repository.",
				InputSchema: []byte(`{"type":"object","properties":{"payload":{"type":"string","description":"` + strings.Repeat("x", agentToolInputSchemaMaxBytes) + `"}}}`),
				Parameters: []catalog.CatalogParameter{
					{Name: "owner", Type: "string", Description: "Repository owner.", Required: true},
					{Name: "repo", Type: "string", Description: "Repository name.", Required: true},
					{Name: "state", Type: "string", Description: "Pull request state."},
				},
				ReadOnly: true,
			}}},
		},
	}
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "github pull requests",
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	schema := resp.Tools[0].ParametersSchema
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want object", schema["properties"])
	}
	if _, ok := properties["payload"]; ok {
		t.Fatalf("schema properties include oversized raw payload: %#v", properties)
	}
	if _, ok := properties["owner"]; !ok {
		t.Fatalf("schema properties = %#v, want owner", properties)
	}
	required, ok := schema["required"].([]any)
	if !ok || len(required) != 2 || required[0] != "owner" || required[1] != "repo" {
		t.Fatalf("schema required = %#v, want owner and repo", schema["required"])
	}
}

func TestSearchToolsUsesOpenObjectSchemaForOversizedInputSchemaWithoutParameters(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:          "search.code",
				Title:       "Search Code",
				Description: "Search code.",
				InputSchema: []byte(`{"type":"object","properties":{"payload":{"type":"string","description":"` + strings.Repeat("x", agentToolInputSchemaMaxBytes) + `"}}}`),
				ReadOnly:    true,
			}}},
		},
	}
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "github code",
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	schema := resp.Tools[0].ParametersSchema
	if schema["type"] != "object" || schema["additionalProperties"] != true {
		t.Fatalf("schema = %#v, want open object fallback", schema)
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
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
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

func TestSearchToolsGlobalWildcardKeepsSearchOpenWithExactRefs(t *testing.T) {
	t.Parallel()

	hidden := false
	slack := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:      "events.reply",
				Title:   "Reply to Event",
				Visible: &hidden,
			}}},
		},
	}
	linear := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:       "list_issues",
				Title:    "List Linear Issues",
				ReadOnly: true,
			}}},
		},
	}
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, slack, linear)})

	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "linear issues",
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "*"},
			{Plugin: "slack", Operation: "events.reply"},
		},
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "linear" || resp.Tools[0].Target.Operation != "list_issues" {
		t.Fatalf("tool target = %#v, want linear.list_issues", resp.Tools[0].Target)
	}
}

func TestSearchToolsGlobalWildcardFindsProviderQualifiedCatalogOperation(t *testing.T) {
	t.Parallel()

	hidden := false
	slack := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:      "events.reply",
				Title:   "Reply to Event",
				Visible: &hidden,
			}}},
		},
	}
	linear := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				DisplayName: "Linear",
				Operations: []catalog.CatalogOperation{{
					ID:          "issues",
					Description: "All issues. Returns a paginated list of issues visible to the authenticated user.",
					ReadOnly:    true,
				}},
			},
		},
	}
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, slack, linear)})

	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "Linear list issues assigned to me",
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "*"},
			{Plugin: "slack", Operation: "events.reply"},
		},
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "linear" || resp.Tools[0].Target.Operation != "issues" {
		t.Fatalf("tool target = %#v, want linear.issues", resp.Tools[0].Target)
	}
}

func TestSearchToolsRanksProviderConfiguredTags(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "codehost",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
				{
					ID:          "pulls.list",
					Title:       "List Pull Requests",
					Description: "List pull requests for a repository.",
					Tags:        []string{"pr", "prs"},
					ReadOnly:    true,
				},
				{
					ID:          "pulls.merge",
					Title:       "Merge Pull Request",
					Description: "Merge a pull request into the base branch.",
					Tags:        []string{"merge"},
				},
			}},
		},
	}
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})

	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query:      "prs",
		MaxResults: 1,
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1: %#v", len(resp.Tools), resp.Tools)
	}
	if resp.Tools[0].Target.Plugin != "codehost" || resp.Tools[0].Target.Operation != "pulls.list" {
		t.Fatalf("tool target = %#v, want codehost.pulls.list", resp.Tools[0].Target)
	}
}

func TestSearchToolsDoesNotInventSemanticAliases(t *testing.T) {
	t.Parallel()

	search := func(t *testing.T, tags []string) *coreagent.SearchToolsResponse {
		t.Helper()

		provider := &catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "chat",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
					ID:          "messages.send",
					Title:       "Send Message",
					Description: "Send a private message.",
					Tags:        tags,
				}}},
			},
		}
		manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
		resp, err := manager.SearchTools(context.Background(), &principal.Principal{
			SubjectID: principal.UserSubjectID("user-1"),
		}, coreagent.SearchToolsRequest{
			Query:      "dm",
			MaxResults: 1,
		})
		if err != nil {
			t.Fatalf("SearchTools: %v", err)
		}
		return resp
	}

	withoutTags := search(t, nil)
	if len(withoutTags.Tools) != 0 {
		t.Fatalf("SearchTools without dm tags returned %#v, want no tools", withoutTags.Tools)
	}

	withTags := search(t, []string{"dm", "direct message"})
	if len(withTags.Tools) != 1 {
		t.Fatalf("SearchTools with dm tags returned %d tools, want 1: %#v", len(withTags.Tools), withTags.Tools)
	}
	if withTags.Tools[0].Target.Plugin != "chat" || withTags.Tools[0].Target.Operation != "messages.send" {
		t.Fatalf("tool target = %#v, want chat.messages.send", withTags.Tools[0].Target)
	}
}

func TestSearchToolsDoesNotTreatPullRequestsAsMergeWithoutMetadata(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "codehost",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:          "pulls.list",
				Title:       "List Pull Requests",
				Description: "List open pull requests for a repository.",
				ReadOnly:    true,
			}}},
		},
	}
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query:      "merge",
		MaxResults: 1,
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 0 {
		t.Fatalf("SearchTools returned %#v, want no tools without merge metadata", resp.Tools)
	}
}

func TestSearchToolsRanksTitleAheadOfTags(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "records",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
				{
					ID:          "records.lookup",
					Title:       "Lookup Records",
					Description: "Lookup records by ID.",
					Tags:        []string{"audit"},
					ReadOnly:    true,
				},
				{
					ID:          "logs.list",
					Title:       "Audit Logs",
					Description: "List log entries.",
					ReadOnly:    true,
				},
			}},
		},
	}
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query:      "audit",
		MaxResults: 1,
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1: %#v", len(resp.Tools), resp.Tools)
	}
	if resp.Tools[0].Target.Plugin != "records" || resp.Tools[0].Target.Operation != "logs.list" {
		t.Fatalf("tool target = %#v, want records.logs.list", resp.Tools[0].Target)
	}
}

func TestAgentRunPermissionsKeepsAPITokenRestrictionsForHTTPWildcard(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "linear",
		Operations: []string{"issues"},
	}})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	got := agentRunPermissions(ctx, p, "slack", []coreagent.ToolRef{{Plugin: "*"}})
	if len(got) != 1 || got[0].Plugin != "linear" || len(got[0].Operations) != 1 || got[0].Operations[0] != "issues" {
		t.Fatalf("agentRunPermissions = %#v, want API token permissions preserved", got)
	}
}

func TestAgentRunPermissionsClearsHTTPResolvedUserWildcardRestrictions(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "slack",
		Operations: []string{"events.reply"},
	}})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	if got := agentRunPermissions(ctx, p, "slack", []coreagent.ToolRef{{Plugin: "*"}}); got != nil {
		t.Fatalf("agentRunPermissions = %#v, want nil permissions for resolved user wildcard search", got)
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
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
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
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
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

func TestSearchToolsExpandsAmbiguousCredentialInstances(t *testing.T) {
	t.Parallel()

	provider := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "github",
				ConnMode: core.ConnectionModeUser,
			},
		},
		sessionCatalog: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID:       "list_pull_requests",
			Title:    "List Pull Requests",
			ReadOnly: true,
		}}},
	}
	providers := testutil.NewProviderRegistry(t, provider)
	services := coretesting.NewStubServices(t)
	subjectID := principal.UserSubjectID("user-1")
	for _, instance := range []string{"z", "sa"} {
		if err := services.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
			SubjectID:   subjectID,
			Integration: "github",
			Connection:  "default",
			Instance:    instance,
			AccessToken: "token-" + instance,
		}); err != nil {
			t.Fatalf("PutCredential(%s): %v", instance, err)
		}
	}
	broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials)
	manager := newTestManager(t, Config{
		Providers: providers,
		Invoker:   broker,
		DefaultConnection: map[string]string{
			"github": "default",
		},
	})

	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: subjectID,
	}, coreagent.SearchToolsRequest{
		Query:    "github pull requests",
		ToolRefs: []coreagent.ToolRef{{Plugin: "*"}},
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 2 {
		t.Fatalf("SearchTools returned %d tools, want 2: %#v", len(resp.Tools), resp.Tools)
	}
	instances := map[string]bool{}
	for _, tool := range resp.Tools {
		if tool.Target.Plugin != "github" || tool.Target.Operation != "list_pull_requests" {
			t.Fatalf("tool target = %#v, want github.list_pull_requests", tool.Target)
		}
		if tool.Target.Connection != "default" {
			t.Fatalf("tool connection = %q, want default", tool.Target.Connection)
		}
		instances[tool.Target.Instance] = true
		if tool.ID == "github/list_pull_requests" {
			t.Fatalf("tool ID %q is missing credential binding", tool.ID)
		}
	}
	if !instances["z"] || !instances["sa"] {
		t.Fatalf("tool instances = %#v, want z and sa", instances)
	}

	exactScopedResp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: subjectID,
	}, coreagent.SearchToolsRequest{
		Query: "github pull requests",
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "github", Operation: "list_pull_requests", Connection: "default", Instance: "z"},
			{Plugin: "github", Operation: "list_pull_requests", Connection: "default", Instance: "sa"},
		},
	})
	if err != nil {
		t.Fatalf("SearchTools exact scoped refs: %v", err)
	}
	if len(exactScopedResp.Tools) != 2 {
		t.Fatalf("exact scoped SearchTools returned %d tools, want 2: %#v", len(exactScopedResp.Tools), exactScopedResp.Tools)
	}

	candidateResp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: subjectID,
	}, coreagent.SearchToolsRequest{
		Query:          "github pull requests",
		MaxResults:     1,
		CandidateLimit: 5,
		ToolRefs:       []coreagent.ToolRef{{Plugin: "*"}},
	})
	if err != nil {
		t.Fatalf("SearchTools candidates: %v", err)
	}
	if len(candidateResp.Tools) != 1 || len(candidateResp.Candidates) != 1 {
		t.Fatalf("candidate SearchTools = tools %#v candidates %#v, want one loaded and one candidate", candidateResp.Tools, candidateResp.Candidates)
	}
	if candidateResp.Candidates[0].Ref.Connection != "default" || candidateResp.Candidates[0].Ref.Instance == "" {
		t.Fatalf("candidate ref = %#v, want resolved connection and instance", candidateResp.Candidates[0].Ref)
	}

	broadLoadResp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: subjectID,
	}, coreagent.SearchToolsRequest{
		LoadRefs: []coreagent.ToolRef{{Plugin: "github", Operation: "list_pull_requests"}},
		ToolRefs: []coreagent.ToolRef{{Plugin: "*"}},
	})
	if err != nil {
		t.Fatalf("SearchTools broad load_ref: %v", err)
	}
	if len(broadLoadResp.Tools) != 0 {
		t.Fatalf("broad load_ref loaded %#v, want no tools without exact identity", broadLoadResp.Tools)
	}

	exactLoadResp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: subjectID,
	}, coreagent.SearchToolsRequest{
		LoadRefs: []coreagent.ToolRef{candidateResp.Candidates[0].Ref},
		ToolRefs: []coreagent.ToolRef{{Plugin: "*"}},
	})
	if err != nil {
		t.Fatalf("SearchTools exact load_ref: %v", err)
	}
	if len(exactLoadResp.Tools) != 1 || exactLoadResp.Tools[0].Target.Instance != candidateResp.Candidates[0].Ref.Instance {
		t.Fatalf("exact load_ref loaded %#v, want candidate instance %q", exactLoadResp.Tools, candidateResp.Candidates[0].Ref.Instance)
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
	manager := newTestManager(t, Config{
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
	manager := newTestManager(t, Config{
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
	manager := newTestManager(t, Config{
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
	manager := newTestManager(t, Config{
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
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
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
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
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
	manager := newTestManager(t, Config{
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
	manager := newTestManager(t, Config{
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

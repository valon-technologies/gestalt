package agentmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
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
	capabilities    *coreagent.ProviderCapabilities
	capabilitiesErr error
	createTurnReqs  []coreagent.CreateTurnRequest
	listSessionReqs []coreagent.ListSessionsRequest
	listTurnReqs    []coreagent.ListTurnsRequest
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

func (p *routeCountingAgentProvider) ListSessions(_ context.Context, req coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	p.listSessionReqs = append(p.listSessionReqs, req)
	var sessions []*coreagent.Session
	requested := map[string]struct{}{}
	for _, id := range req.SessionIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			requested[id] = struct{}{}
		}
	}
	for _, session := range p.sessions {
		if len(requested) > 0 {
			if _, ok := requested[session.ID]; !ok {
				continue
			}
		}
		if req.Subject.SubjectID != "" && session.CreatedBy.SubjectID != req.Subject.SubjectID {
			continue
		}
		if req.State != "" && session.State != req.State {
			continue
		}
		sessions = append(sessions, cloneRouteSession(session))
	}
	if req.Limit > 0 && len(sessions) > req.Limit {
		sessions = sessions[:req.Limit]
	}
	return sessions, nil
}

func (p *routeCountingAgentProvider) CreateTurn(_ context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	p.createTurnReqs = append(p.createTurnReqs, req)
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

func (p *routeCountingAgentProvider) ListTurns(_ context.Context, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	p.listTurnReqs = append(p.listTurnReqs, req)
	var turns []*coreagent.Turn
	requested := map[string]struct{}{}
	for _, id := range req.TurnIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			requested[id] = struct{}{}
		}
	}
	for _, turn := range p.turns {
		if len(requested) > 0 {
			if _, ok := requested[turn.ID]; !ok {
				continue
			}
		}
		if req.SessionID != "" && turn.SessionID != req.SessionID {
			continue
		}
		if req.Subject.SubjectID != "" && turn.CreatedBy.SubjectID != req.Subject.SubjectID {
			continue
		}
		if req.Status != "" && turn.Status != req.Status {
			continue
		}
		turns = append(turns, cloneRouteTurn(turn))
	}
	if req.Limit > 0 && len(turns) > req.Limit {
		turns = turns[:req.Limit]
	}
	return turns, nil
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
	if p.capabilitiesErr != nil {
		return nil, p.capabilitiesErr
	}
	if p.capabilities != nil {
		caps := *p.capabilities
		caps.SupportedToolSources = append([]coreagent.ToolSourceMode(nil), p.capabilities.SupportedToolSources...)
		return &caps, nil
	}
	return &coreagent.ProviderCapabilities{
		BoundedListHydration: true,
		SupportedToolSources: []coreagent.ToolSourceMode{
			coreagent.ToolSourceModeMCPCatalog,
		},
	}, nil
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
	alpha.capabilities = &coreagent.ProviderCapabilities{
		SupportedToolSources: []coreagent.ToolSourceMode{coreagent.ToolSourceModeMCPCatalog},
	}
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

func TestManagerListSessionsRequiresBoundedHydrationForLimitedLists(t *testing.T) {
	t.Parallel()

	provider := newRouteCountingAgentProvider("unbounded")
	provider.capabilities = &coreagent.ProviderCapabilities{
		SupportedToolSources: []coreagent.ToolSourceMode{coreagent.ToolSourceModeMCPCatalog},
	}
	subjectID := principal.UserSubjectID("user-1")
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "unbounded",
			names:       []string{"unbounded"},
			providers: map[string]*routeCountingAgentProvider{
				"unbounded": provider,
			},
		},
		ToolGrants: newAgentManagerTestToolGrants(t),
	})

	_, err := manager.ListSessions(context.Background(), &principal.Principal{SubjectID: subjectID}, coreagent.ManagerListSessionsRequest{
		ProviderName: "unbounded",
		Limit:        1,
	})
	if !errors.Is(err, ErrAgentBoundedListUnsupported) {
		t.Fatalf("ListSessions error = %v, want ErrAgentBoundedListUnsupported", err)
	}
	if len(provider.listSessionReqs) != 0 {
		t.Fatalf("provider ListSessions calls = %d, want 0", len(provider.listSessionReqs))
	}
}

func TestManagerListTurnsRequiresBoundedHydrationForSummaryLists(t *testing.T) {
	t.Parallel()

	provider := newRouteCountingAgentProvider("unbounded")
	provider.capabilities = &coreagent.ProviderCapabilities{
		SupportedToolSources: []coreagent.ToolSourceMode{coreagent.ToolSourceModeMCPCatalog},
	}
	subjectID := principal.UserSubjectID("user-1")
	provider.sessions["session-1"] = &coreagent.Session{
		ID:           "session-1",
		ProviderName: "unbounded",
		State:        coreagent.SessionStateActive,
		CreatedBy:    coreagent.Actor{SubjectID: subjectID},
	}
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "unbounded",
			names:       []string{"unbounded"},
			providers: map[string]*routeCountingAgentProvider{
				"unbounded": provider,
			},
		},
		ToolGrants: newAgentManagerTestToolGrants(t),
	})
	p := &principal.Principal{SubjectID: subjectID}

	_, err := manager.ListTurns(context.Background(), p, coreagent.ManagerListTurnsRequest{
		SessionID:   "session-1",
		SummaryOnly: true,
	})
	if !errors.Is(err, ErrAgentBoundedListUnsupported) {
		t.Fatalf("ListTurns error = %v, want ErrAgentBoundedListUnsupported", err)
	}
	if len(provider.listTurnReqs) != 0 {
		t.Fatalf("provider ListTurns calls = %d, want 0", len(provider.listTurnReqs))
	}
}

func TestManagerCreateTurnLeavesToolSourceUnsetWhenNoToolsRequested(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	alpha.capabilities = &coreagent.ProviderCapabilities{
		BoundedListHydration: true,
	}
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
	_, err = manager.CreateTurn(context.Background(), p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "test-model",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	if got := alpha.createTurnReqs[0].ToolSource; got != coreagent.ToolSourceModeUnspecified {
		t.Fatalf("CreateTurn tool source = %q, want empty", got)
	}
	if got := alpha.createTurnReqs[0].Tools; len(got) != 0 {
		t.Fatalf("CreateTurn tools = %#v, want no preloaded tools", got)
	}
	if got := alpha.createTurnReqs[0].ToolGrant; got != "" {
		t.Fatalf("CreateTurn tool grant = %q, want empty", got)
	}
}

func TestManagerCreateTurnDefaultsToCatalogToolsForCatalogOnlyProvider(t *testing.T) {
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
	_, err = manager.CreateTurn(context.Background(), p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "test-model",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	req := alpha.createTurnReqs[0]
	if req.ToolSource != coreagent.ToolSourceModeMCPCatalog {
		t.Fatalf("CreateTurn tool source = %q, want mcp_catalog", req.ToolSource)
	}
	if got := req.ToolRefs; len(got) != 1 || got[0].Plugin != agentToolSearchAllPlugin || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want global broad catalog ref", got)
	}
	if strings.TrimSpace(req.ToolGrant) == "" {
		t.Fatal("CreateTurn tool grant is empty")
	}
	grant, err := grants.Resolve(req.ToolGrant)
	if err != nil {
		t.Fatalf("Resolve tool grant: %v", err)
	}
	if grant.ToolSource != coreagent.ToolSourceModeMCPCatalog {
		t.Fatalf("grant tool source = %q, want mcp_catalog", grant.ToolSource)
	}
	if got := grant.ToolRefs; len(got) != 1 || got[0].Plugin != agentToolSearchAllPlugin || got[0].Operation != "" {
		t.Fatalf("grant tool refs = %#v, want global broad catalog ref", got)
	}
}

func TestManagerCreateTurnHonorsExplicitCatalogSourceWithNoToolRefs(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, coreagent.ManagerCreateTurnRequest{
		SessionID:  session.ID,
		Model:      "test-model",
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	req := alpha.createTurnReqs[0]
	if req.ToolSource != coreagent.ToolSourceModeMCPCatalog {
		t.Fatalf("CreateTurn tool source = %q, want mcp_catalog", req.ToolSource)
	}
	if got := req.ToolRefs; len(got) != 0 {
		t.Fatalf("CreateTurn tool refs = %#v, want none for explicit empty catalog source", got)
	}
	if strings.TrimSpace(req.ToolGrant) == "" {
		t.Fatal("CreateTurn tool grant is empty")
	}
}

func TestManagerCreateTurnHonorsExplicitEmptyToolRefsWithoutToolSource(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, coreagent.ManagerCreateTurnRequest{
		SessionID:   session.ID,
		Model:       "test-model",
		ToolRefsSet: true,
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	req := alpha.createTurnReqs[0]
	if req.ToolSource != coreagent.ToolSourceModeUnspecified {
		t.Fatalf("CreateTurn tool source = %q, want empty", req.ToolSource)
	}
	if got := req.ToolRefs; len(got) != 0 {
		t.Fatalf("CreateTurn tool refs = %#v, want none for explicit empty tool refs", got)
	}
	if req.ToolGrant != "" {
		t.Fatalf("CreateTurn tool grant = %q, want empty", req.ToolGrant)
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

func TestListToolsClampsOversizedPageSize(t *testing.T) {
	t.Parallel()

	const toolCount = agentToolListMaxPageSize + 7
	docsOps := make([]catalog.CatalogOperation, toolCount)
	for i := range docsOps {
		docsOps[i] = catalog.CatalogOperation{
			ID:       fmt.Sprintf("fetch_%04d", i+1),
			Title:    "Fetch",
			ReadOnly: true,
		}
	}
	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:          "docs",
			ConnMode:   core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: docsOps},
		},
	}
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	firstPage, err := manager.ListTools(context.Background(), p, coreagent.ListToolsRequest{
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
		ToolRefs:   []coreagent.ToolRef{{Plugin: "docs"}},
		PageSize:   5,
	})
	if err != nil {
		t.Fatalf("ListTools first page: %v", err)
	}
	if len(firstPage.Tools) != 5 || firstPage.NextPageToken != "5" {
		t.Fatalf("ListTools first page = %d tools, next %q; want 5 tools and token 5", len(firstPage.Tools), firstPage.NextPageToken)
	}

	clampedPage, err := manager.ListTools(context.Background(), p, coreagent.ListToolsRequest{
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
		ToolRefs:   []coreagent.ToolRef{{Plugin: "docs"}},
		PageSize:   10000,
		PageToken:  firstPage.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListTools clamped page: %v", err)
	}
	wantNextPageToken := fmt.Sprintf("%d", 5+agentToolListMaxPageSize)
	if len(clampedPage.Tools) != agentToolListMaxPageSize || clampedPage.NextPageToken != wantNextPageToken {
		t.Fatalf("ListTools clamped page = %d tools, next %q; want %d tools and token %s", len(clampedPage.Tools), clampedPage.NextPageToken, agentToolListMaxPageSize, wantNextPageToken)
	}

	lastPage, err := manager.ListTools(context.Background(), p, coreagent.ListToolsRequest{
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
		ToolRefs:   []coreagent.ToolRef{{Plugin: "docs"}},
		PageSize:   10000,
		PageToken:  clampedPage.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListTools last page: %v", err)
	}
	if len(lastPage.Tools) != 2 || lastPage.NextPageToken != "" {
		t.Fatalf("ListTools last page = %d tools, next %q; want 2 tools and no token", len(lastPage.Tools), lastPage.NextPageToken)
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
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"slackbot": {{
				Plugin:         "slack",
				Operation:      "events.reply",
				CredentialMode: core.ConnectionModeNone,
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
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"slackbot": {{
				Plugin:         "slack",
				Operation:      "chat.postMessage",
				CredentialMode: core.ConnectionModeNone,
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

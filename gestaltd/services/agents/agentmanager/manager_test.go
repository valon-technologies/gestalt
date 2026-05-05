package agentmanager

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
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

type unavailableAgentCatalogTestProvider struct {
	*catalogCountingProvider
	err error
}

func (p *unavailableAgentCatalogTestProvider) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	return nil, p.err
}

func agentCatalogTestProvider(name, displayName string, operations ...string) *catalogCountingProvider {
	catalogOperations := make([]catalog.CatalogOperation, 0, len(operations))
	for _, operation := range operations {
		catalogOperations = append(catalogOperations, catalog.CatalogOperation{
			ID:       operation,
			Title:    operation,
			ReadOnly: true,
		})
	}
	return &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        name,
			DN:       displayName,
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:        name,
				DisplayName: displayName,
				Operations:  catalogOperations,
			},
		},
	}
}

func newAgentManagerTestRunGrants(t testing.TB) *agentgrant.Manager {
	t.Helper()
	grants, err := agentgrant.NewManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("agentgrant.NewManager: %v", err)
	}
	return grants
}

func newTestManager(t testing.TB, cfg Config) *Manager {
	t.Helper()
	if cfg.RunGrants == nil {
		cfg.RunGrants = newAgentManagerTestRunGrants(t)
	}
	return New(cfg)
}

func newTestRouteStore(t testing.TB, db *coretesting.StubIndexedDB) RouteStore {
	t.Helper()
	store, err := NewIndexedDBRouteStore(context.Background(), db)
	if err != nil {
		t.Fatalf("NewIndexedDBRouteStore: %v", err)
	}
	return store
}

type alreadyExistsCreateIndexedDB struct {
	*coretesting.StubIndexedDB
}

func (db *alreadyExistsCreateIndexedDB) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	if db.HasObjectStore(name) {
		return indexeddb.ErrAlreadyExists
	}
	return db.StubIndexedDB.CreateObjectStore(ctx, name, schema)
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
	name              string
	sessions          map[string]*coreagent.Session
	turns             map[string]*coreagent.Turn
	capabilities      *coreagent.ProviderCapabilities
	capabilitiesErr   error
	createSessionReqs []coreagent.CreateSessionRequest
	createTurnReqs    []coreagent.CreateTurnRequest
	listSessionReqs   []coreagent.ListSessionsRequest
	listTurnReqs      []coreagent.ListTurnsRequest
	turnIDOverride    string
	cancelStatus      coreagent.ExecutionStatus
	getSessionCalls   int
	getTurnCalls      int
}

func newRouteCountingAgentProvider(name string) *routeCountingAgentProvider {
	return &routeCountingAgentProvider{
		name:     name,
		sessions: map[string]*coreagent.Session{},
		turns:    map[string]*coreagent.Turn{},
	}
}

func (p *routeCountingAgentProvider) CreateSession(_ context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	p.createSessionReqs = append(p.createSessionReqs, req)
	session := &coreagent.Session{
		ID:           req.SessionID,
		ProviderName: p.name,
		Model:        req.Model,
		ClientRef:    req.ClientRef,
		State:        coreagent.SessionStateActive,
		Metadata:     mapsCloneAny(req.Metadata),
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

func (p *routeCountingAgentProvider) UpdateSession(_ context.Context, req coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	session := p.sessions[strings.TrimSpace(req.SessionID)]
	if session == nil {
		return nil, core.ErrNotFound
	}
	if req.ClientRef != "" {
		session.ClientRef = req.ClientRef
	}
	if req.State != "" {
		session.State = req.State
	}
	if req.Metadata != nil {
		session.Metadata = mapsCloneAny(req.Metadata)
	}
	return cloneRouteSession(session), nil
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
	cloned.Metadata = mapsCloneAny(session.Metadata)
	return &cloned
}

func mapsCloneAny(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
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
	cache.remember("old", AgentRoute{ProviderName: "alpha", SessionID: "session-old"})
	cache.remember("warm", AgentRoute{ProviderName: "alpha", SessionID: "session-warm"})
	if got, ok := cache.get("old"); !ok || got.ProviderName != "alpha" {
		t.Fatalf("cache.get(old) = %+v, %t, want alpha", got, ok)
	}
	cache.remember("new", AgentRoute{ProviderName: "alpha", SessionID: "session-new"})
	cache.trim(2)

	if got, ok := cache.get("warm"); ok {
		t.Fatalf("cache.get(warm) = %+v, %t, want evicted", got, ok)
	}
	if got, ok := cache.get("old"); !ok || got.ProviderName != "alpha" {
		t.Fatalf("cache.get(old) = %+v, %t, want retained alpha", got, ok)
	}
	if got, ok := cache.get("new"); !ok || got.ProviderName != "alpha" {
		t.Fatalf("cache.get(new) = %+v, %t, want retained alpha", got, ok)
	}
}

func TestCreateSessionForwardsSessionStartWhenProviderSupportsIt(t *testing.T) {
	t.Parallel()

	provider := newRouteCountingAgentProvider("alpha")
	provider.capabilities = &coreagent.ProviderCapabilities{SupportsSessionStart: true}
	sessionStart := &coreagent.SessionStartConfig{Hooks: []coreagent.SessionStartHook{{
		ID:      "load-memory",
		Type:    "command",
		Command: []string{"bash", "-lc", "printf context"},
		CWD:     "/tmp",
		Timeout: "5s",
		Env:     map[string]string{"MEMORY_ROOT": "/tmp/memory"},
		Output:  coreagent.SessionStartHookOutput{AdditionalContext: true, Metadata: true},
	}}}
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers:   map[string]*routeCountingAgentProvider{"alpha": provider},
		},
		SessionStart: map[string]*coreagent.SessionStartConfig{"alpha": sessionStart},
	})
	sessionStart.Hooks[0].Command[0] = "mutated"
	sessionStart.Hooks[0].Env["MEMORY_ROOT"] = "mutated"

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(provider.createSessionReqs) != 1 {
		t.Fatalf("CreateSession calls = %d, want 1", len(provider.createSessionReqs))
	}
	got := provider.createSessionReqs[0].SessionStart
	want := &coreagent.SessionStartConfig{Hooks: []coreagent.SessionStartHook{{
		ID:      "load-memory",
		Type:    "command",
		Command: []string{"bash", "-lc", "printf context"},
		CWD:     "/tmp",
		Timeout: "5s",
		Env:     map[string]string{"MEMORY_ROOT": "/tmp/memory"},
		Output:  coreagent.SessionStartHookOutput{AdditionalContext: true, Metadata: true},
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SessionStart = %#v, want %#v", got, want)
	}
}

func TestCreateSessionRejectsSessionStartWhenProviderDoesNotSupportIt(t *testing.T) {
	t.Parallel()

	provider := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers:   map[string]*routeCountingAgentProvider{"alpha": provider},
		},
		SessionStart: map[string]*coreagent.SessionStartConfig{"alpha": &coreagent.SessionStartConfig{Hooks: []coreagent.SessionStartHook{{ID: "setup", Type: "command", Command: []string{"true"}}}}},
	})

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if !errors.Is(err, ErrAgentSessionStartUnsupported) {
		t.Fatalf("CreateSession error = %v, want ErrAgentSessionStartUnsupported", err)
	}
	if len(provider.createSessionReqs) != 0 {
		t.Fatalf("CreateSession calls = %d, want 0", len(provider.createSessionReqs))
	}
}

func TestCreateSessionRejectsReservedLifecycleMetadata(t *testing.T) {
	t.Parallel()

	provider := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers:   map[string]*routeCountingAgentProvider{"alpha": provider},
		},
	})

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Metadata:     map[string]any{"__gestalt.lifecycle.sessionStart.results.setup": "spoofed"},
	})
	if !errors.Is(err, ErrAgentSessionMetadataInvalid) || !strings.Contains(err.Error(), "reserved for Gestalt lifecycle data") {
		t.Fatalf("CreateSession error = %v, want reserved lifecycle metadata error", err)
	}
	if len(provider.createSessionReqs) != 0 {
		t.Fatalf("CreateSession calls = %d, want 0", len(provider.createSessionReqs))
	}
}

func TestUpdateSessionPreservesReservedLifecycleMetadata(t *testing.T) {
	t.Parallel()

	provider := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers:   map[string]*routeCountingAgentProvider{"alpha": provider},
		},
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Metadata: map[string]any{
			"caller": "original",
		},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	provider.sessions[session.ID].Metadata["__gestalt.lifecycle.sessionStart.results.setup"] = map[string]any{"exitCode": 0}

	updated, err := manager.UpdateSession(context.Background(), p, coreagent.ManagerUpdateSessionRequest{
		SessionID: session.ID,
		Metadata:  map[string]any{"caller": "updated"},
	})
	if err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if updated.Metadata["caller"] != "updated" {
		t.Fatalf("caller metadata = %#v, want updated", updated.Metadata["caller"])
	}
	if updated.Metadata["__gestalt.lifecycle.sessionStart.results.setup"] == nil {
		t.Fatalf("reserved lifecycle metadata was not preserved: %#v", updated.Metadata)
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
		RunGrants: newAgentManagerTestRunGrants(t),
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

func TestManagerUsesDurableProviderRoutesAcrossManagers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	alpha := newRouteCountingAgentProvider("alpha")
	alpha.capabilities = &coreagent.ProviderCapabilities{
		SupportedToolSources: []coreagent.ToolSourceMode{coreagent.ToolSourceModeMCPCatalog},
	}
	beta := newRouteCountingAgentProvider("beta")
	control := &routeCountingAgentControl{
		defaultName: "alpha",
		names:       []string{"beta", "alpha"},
		providers: map[string]*routeCountingAgentProvider{
			"alpha": alpha,
			"beta":  beta,
		},
	}
	managerA := newTestManager(t, Config{
		Agent:      control,
		RunGrants:  newAgentManagerTestRunGrants(t),
		RouteStore: newTestRouteStore(t, db),
	})
	managerB := newTestManager(t, Config{
		Agent:      control,
		RunGrants:  newAgentManagerTestRunGrants(t),
		RouteStore: newTestRouteStore(t, db),
	})
	managerC := newTestManager(t, Config{
		Agent:      control,
		RunGrants:  newAgentManagerTestRunGrants(t),
		RouteStore: newTestRouteStore(t, db),
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := managerA.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	alpha.getSessionCalls = 0
	beta.getSessionCalls = 0
	turn, err := managerB.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "test-model",
		Messages:  []coreagent.Message{{Role: "user", Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn(cold manager): %v", err)
	}
	if alpha.getSessionCalls != 1 || beta.getSessionCalls != 0 {
		t.Fatalf("CreateTurn session lookup calls = alpha:%d beta:%d, want alpha:1 beta:0", alpha.getSessionCalls, beta.getSessionCalls)
	}

	alpha.getTurnCalls = 0
	beta.getTurnCalls = 0
	if _, err := managerC.GetTurn(ctx, p, turn.ID); err != nil {
		t.Fatalf("GetTurn(cold manager): %v", err)
	}
	if alpha.getTurnCalls != 1 || beta.getTurnCalls != 0 {
		t.Fatalf("GetTurn calls = alpha:%d beta:%d, want alpha:1 beta:0", alpha.getTurnCalls, beta.getTurnCalls)
	}
}

func TestManagerWrongPrincipalDoesNotDeleteDurableSessionRoute(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	routeStore := newTestRouteStore(t, db)
	alpha := newRouteCountingAgentProvider("alpha")
	control := &routeCountingAgentControl{
		defaultName: "alpha",
		names:       []string{"alpha"},
		providers: map[string]*routeCountingAgentProvider{
			"alpha": alpha,
		},
	}
	managerA := newTestManager(t, Config{Agent: control, RouteStore: routeStore})
	managerB := newTestManager(t, Config{Agent: control, RouteStore: newTestRouteStore(t, db)})
	managerC := newTestManager(t, Config{Agent: control, RouteStore: newTestRouteStore(t, db)})
	owner := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	other := &principal.Principal{SubjectID: principal.UserSubjectID("user-2")}

	session, err := managerA.CreateSession(ctx, owner, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := managerB.GetSession(ctx, other, session.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetSession(wrong principal) error = %v, want not found", err)
	}
	route, ok, err := routeStore.LookupSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("LookupSession: %v", err)
	}
	if !ok || route.ProviderName != "alpha" {
		t.Fatalf("LookupSession route = %+v, %t, want alpha", route, ok)
	}
	if _, err := managerC.GetSession(ctx, owner, session.ID); err != nil {
		t.Fatalf("GetSession(owner after wrong principal): %v", err)
	}
}

func TestManagerDurableTurnRouteValidatesStoredSessionID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	routeStore := newTestRouteStore(t, db)
	alpha := newRouteCountingAgentProvider("alpha")
	control := &routeCountingAgentControl{
		defaultName: "alpha",
		names:       []string{"alpha"},
		providers: map[string]*routeCountingAgentProvider{
			"alpha": alpha,
		},
	}
	managerA := newTestManager(t, Config{Agent: control, RouteStore: routeStore})
	managerB := newTestManager(t, Config{Agent: control, RouteStore: newTestRouteStore(t, db)})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := managerA.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := managerA.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "test-model",
		Messages:  []coreagent.Message{{Role: "user", Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if err := routeStore.RememberTurn(ctx, turn.ID, "wrong-session", "alpha"); err != nil {
		t.Fatalf("RememberTurn(wrong session): %v", err)
	}
	_, err = managerB.GetTurn(ctx, p, turn.ID)
	if err == nil || !strings.Contains(err.Error(), `turn session id`) || !strings.Contains(err.Error(), `wrong-session`) {
		t.Fatalf("GetTurn error = %v, want turn session id mismatch", err)
	}
}

func TestManagerDurableRoutesBeatStaleProcessCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	routeStore := newTestRouteStore(t, db)
	alpha := newRouteCountingAgentProvider("alpha")
	beta := newRouteCountingAgentProvider("beta")
	control := &routeCountingAgentControl{
		defaultName: "alpha",
		names:       []string{"alpha", "beta"},
		providers: map[string]*routeCountingAgentProvider{
			"alpha": alpha,
			"beta":  beta,
		},
	}
	managerA := newTestManager(t, Config{Agent: control, RouteStore: routeStore})
	managerB := newTestManager(t, Config{Agent: control, RouteStore: newTestRouteStore(t, db)})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := managerA.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := managerA.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "test-model",
		Messages:  []coreagent.Message{{Role: "user", Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if _, err := managerB.GetSession(ctx, p, session.ID); err != nil {
		t.Fatalf("GetSession(warm stale cache): %v", err)
	}
	if _, err := managerB.GetTurn(ctx, p, turn.ID); err != nil {
		t.Fatalf("GetTurn(warm stale cache): %v", err)
	}

	beta.sessions[session.ID] = cloneRouteSession(session)
	beta.sessions[session.ID].ProviderName = "beta"
	beta.turns[turn.ID] = cloneRouteTurn(turn)
	beta.turns[turn.ID].ProviderName = "beta"
	if err := routeStore.RememberSession(ctx, session.ID, "beta"); err != nil {
		t.Fatalf("RememberSession(beta): %v", err)
	}
	if err := routeStore.RememberTurn(ctx, turn.ID, session.ID, "beta"); err != nil {
		t.Fatalf("RememberTurn(beta): %v", err)
	}

	alpha.getSessionCalls = 0
	beta.getSessionCalls = 0
	fetchedSession, err := managerB.GetSession(ctx, p, session.ID)
	if err != nil {
		t.Fatalf("GetSession(after durable route update): %v", err)
	}
	if fetchedSession.ProviderName != "beta" {
		t.Fatalf("GetSession provider = %q, want beta", fetchedSession.ProviderName)
	}
	if alpha.getSessionCalls != 0 || beta.getSessionCalls != 1 {
		t.Fatalf("GetSession calls = alpha:%d beta:%d, want alpha:0 beta:1", alpha.getSessionCalls, beta.getSessionCalls)
	}

	alpha.getTurnCalls = 0
	beta.getTurnCalls = 0
	fetchedTurn, err := managerB.GetTurn(ctx, p, turn.ID)
	if err != nil {
		t.Fatalf("GetTurn(after durable route update): %v", err)
	}
	if fetchedTurn.ProviderName != "beta" {
		t.Fatalf("GetTurn provider = %q, want beta", fetchedTurn.ProviderName)
	}
	if alpha.getTurnCalls != 0 || beta.getTurnCalls != 1 {
		t.Fatalf("GetTurn calls = alpha:%d beta:%d, want alpha:0 beta:1", alpha.getTurnCalls, beta.getTurnCalls)
	}
}

func TestManagerStaleDurableRouteDoesNotPopulateProcessCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	routeStore := newTestRouteStore(t, db)
	alpha := newRouteCountingAgentProvider("alpha")
	control := &routeCountingAgentControl{
		defaultName: "alpha",
		names:       []string{"alpha"},
		providers: map[string]*routeCountingAgentProvider{
			"alpha": alpha,
		},
	}
	manager := newTestManager(t, Config{Agent: control, RouteStore: routeStore})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	if err := routeStore.RememberSession(ctx, "missing-session", "alpha"); err != nil {
		t.Fatalf("RememberSession: %v", err)
	}
	if err := routeStore.RememberTurn(ctx, "missing-turn", "missing-session", "alpha"); err != nil {
		t.Fatalf("RememberTurn: %v", err)
	}

	if _, err := manager.GetSession(ctx, p, "missing-session"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetSession error = %v, want not found", err)
	}
	if route, ok := manager.cachedSessionRoute("missing-session"); ok {
		t.Fatalf("cachedSessionRoute = %+v, want none", route)
	}
	if _, err := manager.GetTurn(ctx, p, "missing-turn"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetTurn error = %v, want not found", err)
	}
	if route, ok := manager.cachedTurnRoute("missing-turn"); ok {
		t.Fatalf("cachedTurnRoute = %+v, want none", route)
	}
}

func TestManagerBestEffortRoutesDoNotCacheDurableConflicts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	routeStore := newTestRouteStore(t, db)
	manager := newTestManager(t, Config{RouteStore: routeStore})
	if err := routeStore.RememberSession(ctx, "session-1", "beta"); err != nil {
		t.Fatalf("RememberSession: %v", err)
	}
	if err := routeStore.RememberTurn(ctx, "turn-1", "session-1", "beta"); err != nil {
		t.Fatalf("RememberTurn: %v", err)
	}
	manager.rememberCachedSessionRoute("session-1", "alpha")
	manager.rememberCachedTurnRoute("turn-1", "session-1", "alpha")

	manager.rememberSessionRouteBestEffort(ctx, "session-1", "alpha")
	manager.rememberTurnRouteBestEffort(ctx, "turn-1", "session-1", "alpha")

	if route, ok := manager.cachedSessionRoute("session-1"); ok {
		t.Fatalf("cachedSessionRoute = %+v, want none", route)
	}
	if route, ok := manager.cachedTurnRoute("turn-1"); ok {
		t.Fatalf("cachedTurnRoute = %+v, want none", route)
	}
}

func TestIndexedDBRouteStoreAcceptsExistingObjectStores(t *testing.T) {
	t.Parallel()

	db := &alreadyExistsCreateIndexedDB{StubIndexedDB: &coretesting.StubIndexedDB{}}
	if _, err := NewIndexedDBRouteStore(context.Background(), db); err != nil {
		t.Fatalf("NewIndexedDBRouteStore(first): %v", err)
	}
	if _, err := NewIndexedDBRouteStore(context.Background(), db); err != nil {
		t.Fatalf("NewIndexedDBRouteStore(existing stores): %v", err)
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
		RunGrants: newAgentManagerTestRunGrants(t),
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
		RunGrants: newAgentManagerTestRunGrants(t),
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
		RunGrants: newAgentManagerTestRunGrants(t),
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
		RunGrants: newAgentManagerTestRunGrants(t),
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
	if got := alpha.createTurnReqs[0].RunGrant; got == "" {
		t.Fatal("CreateTurn run grant is empty")
	}
}

func TestManagerCreateTurnDefaultsToCatalogToolsForCatalogOnlyProvider(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	grants := newAgentManagerTestRunGrants(t)
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		RunGrants: grants,
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
	if strings.TrimSpace(req.RunGrant) == "" {
		t.Fatal("CreateTurn run grant is empty")
	}
	grant, err := grants.Resolve(req.RunGrant)
	if err != nil {
		t.Fatalf("Resolve run grant: %v", err)
	}
	if grant.ToolSource != coreagent.ToolSourceModeMCPCatalog {
		t.Fatalf("grant tool source = %q, want mcp_catalog", grant.ToolSource)
	}
	if got := grant.ToolRefs; len(got) != 1 || got[0].Plugin != agentToolSearchAllPlugin || got[0].Operation != "" {
		t.Fatalf("grant tool refs = %#v, want global broad catalog ref", got)
	}
}

func TestManagerCreateTurnNarrowsImplicitDefaultCatalogRefsForLargeMentionedProvider(t *testing.T) {
	t.Parallel()

	threshold := 1
	linear := agentCatalogTestProvider("linear", "Linear", "issues")
	github := agentCatalogTestProvider("github", "GitHub", "issues", "pull_requests")
	alpha := newRouteCountingAgentProvider("alpha")
	grants := newAgentManagerTestRunGrants(t)
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, linear, github),
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		RunGrants:                     grants,
		DefaultToolNarrowingThreshold: &threshold,
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
		Messages:  []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}},
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
	if got := req.ToolRefs; len(got) != 1 || got[0].Plugin != "linear" || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want linear provider ref", got)
	}
	grant, err := grants.Resolve(req.RunGrant)
	if err != nil {
		t.Fatalf("Resolve run grant: %v", err)
	}
	if got := grant.ToolRefs; len(got) != 1 || got[0].Plugin != "linear" || got[0].Operation != "" {
		t.Fatalf("grant tool refs = %#v, want linear provider ref", got)
	}

	listed, err := manager.ListTools(context.Background(), p, coreagent.ListToolsRequest{
		ToolSource: grant.ToolSource,
		ToolRefs:   grant.ToolRefs,
	})
	if err != nil {
		t.Fatalf("ListTools narrowed grant: %v", err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Target.Plugin != "linear" || listed.Tools[0].Target.Operation != "issues" {
		t.Fatalf("ListTools narrowed grant = %#v, want only linear issues", listed.Tools)
	}
}

func TestManagerCreateTurnKeepsImplicitWildcardForSmallCatalogs(t *testing.T) {
	t.Parallel()

	threshold := 10
	linear := agentCatalogTestProvider("linear", "Linear", "issues")
	alpha := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, linear),
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		DefaultToolNarrowingThreshold: &threshold,
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
		Messages:  []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].Plugin != agentToolSearchAllPlugin || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want broad wildcard for small catalog", got)
	}
}

func TestManagerCreateTurnDoesNotEnumerateCatalogsWhenNoProviderMentionMatches(t *testing.T) {
	t.Parallel()

	threshold := 0
	linear := agentCatalogTestProvider("linear", "Linear", "issues")
	alpha := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, linear),
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		DefaultToolNarrowingThreshold: &threshold,
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
		Messages:  []coreagent.Message{{Role: "user", Text: "show me my tickets"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if linear.catalogCalls != 0 {
		t.Fatalf("linear catalog calls = %d, want no enumeration without a provider mention", linear.catalogCalls)
	}
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].Plugin != agentToolSearchAllPlugin || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want broad wildcard without provider mention", got)
	}
}

func TestManagerCreateTurnDoesNotStemProviderMentionsForImplicitNarrowing(t *testing.T) {
	t.Parallel()

	threshold := 0
	docs := agentCatalogTestProvider("docs", "Docs", "search")
	alpha := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, docs),
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		DefaultToolNarrowingThreshold: &threshold,
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
		Messages:  []coreagent.Message{{Role: "user", Text: "open a doc"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if docs.catalogCalls != 0 {
		t.Fatalf("docs catalog calls = %d, want no enumeration for non-exact provider mention", docs.catalogCalls)
	}
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].Plugin != agentToolSearchAllPlugin || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want broad wildcard for non-exact provider mention", got)
	}
}

func TestManagerCreateTurnKeepsImplicitWildcardForCallerPluginDefaults(t *testing.T) {
	t.Parallel()

	threshold := 0
	linear := agentCatalogTestProvider("linear", "Linear", "issues")
	alpha := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, linear),
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		DefaultToolNarrowingThreshold: &threshold,
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
		CallerPluginName: "slack",
		SessionID:        session.ID,
		Model:            "test-model",
		Messages:         []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].Plugin != agentToolSearchAllPlugin || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want broad wildcard for caller plugin default", got)
	}
	if linear.catalogCalls != 0 {
		t.Fatalf("linear catalog calls = %d, want caller plugin default to skip narrowing probes", linear.catalogCalls)
	}
}

func TestManagerCreateTurnNarrowsFromLatestUserTextOnly(t *testing.T) {
	t.Parallel()

	threshold := 0
	linear := agentCatalogTestProvider("linear", "Linear", "issues")
	github := agentCatalogTestProvider("github", "GitHub", "issues")
	alpha := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, linear, github),
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		DefaultToolNarrowingThreshold: &threshold,
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
		Messages: []coreagent.Message{
			{Role: "user", Text: "linear was mentioned earlier"},
			{Role: "assistant", Text: "linear is still in assistant text"},
			{
				Role: "user",
				Parts: []coreagent.MessagePart{
					{Type: coreagent.MessagePartTypeJSON, Text: "linear should be ignored"},
					{Type: coreagent.MessagePartTypeToolResult, ToolResult: &coreagent.ToolResultPart{Content: "linear should be ignored"}},
					{Type: coreagent.MessagePartTypeText, Text: "show me github issues"},
				},
				Metadata: map[string]any{"provider": "linear"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].Plugin != "github" || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want github from latest user text part only", got)
	}
}

func TestManagerCreateTurnKeepsImplicitWildcardWhenMentionedProviderCannotBeProbed(t *testing.T) {
	t.Parallel()

	threshold := 0
	linear := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			DN:       "Linear",
			ConnMode: core.ConnectionModeNone,
		},
	}
	alpha := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, linear),
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		DefaultToolNarrowingThreshold: &threshold,
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
		Messages:  []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].Plugin != agentToolSearchAllPlugin || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want fail-open broad wildcard", got)
	}
}

func TestManagerCreateTurnKeepsImplicitWildcardWhenMentionedProviderUnavailable(t *testing.T) {
	t.Parallel()

	threshold := 0
	linear := &unavailableAgentCatalogTestProvider{
		catalogCountingProvider: &catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "linear",
				DN:       "Linear",
				ConnMode: core.ConnectionModeUser,
			},
		},
		err: invocation.ErrNoCredential,
	}
	github := agentCatalogTestProvider("github", "GitHub", "issues")
	alpha := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, linear, github),
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		DefaultToolNarrowingThreshold: &threshold,
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
		Messages:  []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].Plugin != agentToolSearchAllPlugin || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want broad wildcard when mentioned provider is unavailable", got)
	}
}

func TestManagerCreateTurnKeepsImplicitWildcardWhenMentionedProviderHasNoVisibleCandidates(t *testing.T) {
	t.Parallel()

	threshold := 0
	hidden := false
	linear := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			DN:       "Linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:      "admin",
				Title:   "Admin",
				Visible: &hidden,
			}}},
		},
	}
	alpha := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, linear),
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		DefaultToolNarrowingThreshold: &threshold,
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
		Messages:  []coreagent.Message{{Role: "user", Text: "show me linear"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].Plugin != agentToolSearchAllPlugin || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want broad wildcard when provider has no visible candidates", got)
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
	if strings.TrimSpace(req.RunGrant) == "" {
		t.Fatal("CreateTurn run grant is empty")
	}
}

func TestManagerCreateTurnHonorsExplicitEmptyToolRefsWithoutToolSource(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	grants := newAgentManagerTestRunGrants(t)
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		RunGrants: grants,
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
	if strings.TrimSpace(req.RunGrant) == "" {
		t.Fatal("CreateTurn run grant is empty")
	}
	grant, err := grants.Resolve(req.RunGrant)
	if err != nil {
		t.Fatalf("Resolve run grant: %v", err)
	}
	if grant.ToolSource != coreagent.ToolSourceModeUnspecified {
		t.Fatalf("grant tool source = %q, want empty", grant.ToolSource)
	}
	if got := grant.ToolRefs; len(got) != 0 {
		t.Fatalf("grant tool refs = %#v, want none for explicit empty tool refs", got)
	}
	if got := grant.Tools; len(got) != 0 {
		t.Fatalf("grant tools = %#v, want none for explicit empty tool refs", got)
	}
}

func TestManagerCancelTurnRevokesRunGrantWithoutBootstrapWrapper(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	grants := newAgentManagerTestRunGrants(t)
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		RunGrants: grants,
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
	grants := newAgentManagerTestRunGrants(t)
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		RunGrants: grants,
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

func TestAgentRunPermissionsCompactsExplicitCatalogRefs(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{
		{Plugin: "linear", Operations: []string{"viewer", "issues.list", "issues.create"}},
		{Plugin: "slack"},
		{Plugin: "github"},
	})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	got := agentRunPermissions(ctx, p, "", []coreagent.ToolRef{
		{Plugin: "slack", Operation: "chat.postMessage"},
		{Plugin: "linear", Operation: "viewer"},
		{Plugin: "slack", Operation: "chat.postMessage"},
		{System: coreagent.SystemToolWorkflow, Operation: "run"},
	})
	want := []core.AccessPermission{
		{Plugin: "linear", Operations: []string{"viewer"}},
		{Plugin: "slack", Operations: []string{"chat.postMessage"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agentRunPermissions = %#v, want %#v", got, want)
	}
}

func TestAgentRunPermissionsCompactsExactRefsAfterAuthorization(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{
		{Plugin: "linear", Operations: []string{"mcp.call"}},
		{Plugin: "slack"},
	})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	got := agentRunPermissions(ctx, p, "", []coreagent.ToolRef{{Plugin: "linear", Operation: "viewer"}})
	want := []core.AccessPermission{{Plugin: "linear", Operations: []string{"viewer"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agentRunPermissions = %#v, want %#v", got, want)
	}
}

func TestAgentRunPermissionsCompactsProviderWideCatalogRef(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{
		{Plugin: "linear", Operations: []string{"viewer"}},
		{Plugin: "slack"},
	})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	got := agentRunPermissions(ctx, p, "", []coreagent.ToolRef{
		{Plugin: "linear", Operation: "viewer"},
		{Plugin: "linear"},
	})
	want := []core.AccessPermission{{Plugin: "linear", Operations: []string{"viewer"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agentRunPermissions = %#v, want %#v", got, want)
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

func TestResolveToolsAppliesDeclaredInvokeRunAs(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:    "bot.createPullRequest",
				Title: "Create pull request",
			}}},
		},
	}
	runAs := &core.RunAsSubject{
		SubjectID:   "service_account:github_app_installation:99:repo:acme/widgets",
		SubjectKind: "service_account",
		AuthSource:  "github_app_webhook",
	}
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"slack": {{
				Plugin:    "github",
				Operation: "bot.createPullRequest",
				RunAs:     runAs,
			}},
		},
	})

	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		CallerPluginName: "slack",
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "github",
			Operation: "bot.createPullRequest",
		}},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ResolveTools returned %d tools, want 1", len(tools))
	}
	if tools[0].Target.RunAs == nil || tools[0].Target.RunAs.SubjectID != runAs.SubjectID {
		t.Fatalf("tool runAs = %#v, want %q", tools[0].Target.RunAs, runAs.SubjectID)
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

func TestResolveToolsRejectsUndeclaredRunAs(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:    "bot.createPullRequest",
				Title: "Create pull request",
			}}},
		},
	}
	runAs := &core.RunAsSubject{
		SubjectID:   "service_account:github_app_installation:99:repo:acme/widgets",
		SubjectKind: "service_account",
		AuthSource:  "github_app_webhook",
	}
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"slack": {{
				Plugin:    "github",
				Operation: "bot.getPullRequest",
				RunAs:     runAs,
			}},
		},
	})

	for _, tc := range []struct {
		name             string
		callerPluginName string
	}{
		{name: "public request"},
		{name: "caller without matching invoke", callerPluginName: "slack"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := manager.ResolveTools(context.Background(), &principal.Principal{
				SubjectID: principal.UserSubjectID("user-1"),
			}, coreagent.ResolveToolsRequest{
				CallerPluginName: tc.callerPluginName,
				ToolRefs: []coreagent.ToolRef{{
					Plugin:    "github",
					Operation: "bot.createPullRequest",
					RunAs:     runAs,
				}},
			})
			if !errors.Is(err, invocation.ErrAuthorizationDenied) {
				t.Fatalf("ResolveTools error = %v, want ErrAuthorizationDenied", err)
			}
		})
	}
}

func TestAgentToolTargetKeyIncludesFullRunAsSubject(t *testing.T) {
	t.Parallel()

	base := coreagent.ToolRef{
		Plugin:    "github",
		Operation: "bot.createPullRequest",
		RunAs: &core.RunAsSubject{
			SubjectID:           "service_account:github_app_installation:99:repo:acme/widgets",
			SubjectKind:         "service_account",
			CredentialSubjectID: "service_account:github_app_installation:99:repo:acme/widgets",
			DisplayName:         "Toolshed app",
			AuthSource:          "github_app_webhook",
		},
	}
	same := base
	same.RunAs = &core.RunAsSubject{
		SubjectID:           " service_account:github_app_installation:99:repo:acme/widgets ",
		SubjectKind:         " service_account ",
		CredentialSubjectID: " service_account:github_app_installation:99:repo:acme/widgets ",
		DisplayName:         " Toolshed app ",
		AuthSource:          " github_app_webhook ",
	}
	differentMetadata := base
	differentMetadata.RunAs = &core.RunAsSubject{
		SubjectID:           base.RunAs.SubjectID,
		SubjectKind:         base.RunAs.SubjectKind,
		CredentialSubjectID: base.RunAs.CredentialSubjectID,
		DisplayName:         "Another display name",
		AuthSource:          base.RunAs.AuthSource,
	}

	if agentToolTargetKeyFromRef(base) != agentToolTargetKeyFromRef(same) {
		t.Fatal("agentToolTargetKeyFromRef should normalize equivalent runAs subjects")
	}
	if agentToolTargetKeyFromRef(base) == agentToolTargetKeyFromRef(differentMetadata) {
		t.Fatal("agentToolTargetKeyFromRef collapsed distinct runAs metadata")
	}
}

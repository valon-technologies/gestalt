package agentmanager

import (
	"context"
	"encoding/json"
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
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	supportsWorkspace bool
	createSessionReqs []coreagent.CreateSessionRequest
	createTurnReqs    []coreagent.CreateTurnRequest
	listSessionReqs   []coreagent.ListSessionsRequest
	listTurnReqs      []coreagent.ListTurnsRequest
	turnIDOverride    string
	cancelStatus      coreagent.ExecutionStatus
	getSessionCalls   int
	getTurnCalls      int
}

type sharedSessionAuthorizationProvider struct {
	directResources              []*core.ResourceRef
	effectiveResources           []*core.ResourceRef
	effectiveErr                 error
	allowedSessions              map[string]struct{}
	searchResourceCalls          int
	effectiveSearchResourceCalls int
}

func (p *sharedSessionAuthorizationProvider) Name() string { return "shared-session-authz" }

func (p *sharedSessionAuthorizationProvider) Evaluate(_ context.Context, req *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	_, allowed := p.allowedSessions[strings.TrimSpace(req.GetResource().GetId())]
	return &core.AccessDecision{Allowed: allowed}, nil
}

func (p *sharedSessionAuthorizationProvider) EvaluateMany(ctx context.Context, req *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	resp := &core.AccessEvaluationsResponse{Decisions: make([]*core.AccessDecision, 0, len(req.GetRequests()))}
	for _, item := range req.GetRequests() {
		decision, err := p.Evaluate(ctx, item)
		if err != nil {
			return nil, err
		}
		resp.Decisions = append(resp.Decisions, decision)
	}
	return resp, nil
}

func (p *sharedSessionAuthorizationProvider) SearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	p.searchResourceCalls++
	return &core.ResourceSearchResponse{Resources: cloneAuthzResources(p.directResources)}, nil
}

func (p *sharedSessionAuthorizationProvider) EffectiveSearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	p.effectiveSearchResourceCalls++
	if p.effectiveErr != nil {
		return nil, p.effectiveErr
	}
	return &core.ResourceSearchResponse{Resources: cloneAuthzResources(p.effectiveResources)}, nil
}

func (*sharedSessionAuthorizationProvider) SearchSubjects(context.Context, *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	return &core.SubjectSearchResponse{}, nil
}

func (*sharedSessionAuthorizationProvider) EffectiveSearchSubjects(context.Context, *core.EffectiveSubjectSearchRequest) (*core.EffectiveSubjectSearchResponse, error) {
	return &core.EffectiveSubjectSearchResponse{}, nil
}

func (*sharedSessionAuthorizationProvider) SearchActions(context.Context, *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	return &core.ActionSearchResponse{}, nil
}

func (*sharedSessionAuthorizationProvider) GetMetadata(context.Context) (*core.AuthorizationMetadata, error) {
	return &core.AuthorizationMetadata{}, nil
}

func (*sharedSessionAuthorizationProvider) ReadRelationships(context.Context, *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	return &core.ReadRelationshipsResponse{}, nil
}

func (*sharedSessionAuthorizationProvider) WriteRelationships(context.Context, *core.WriteRelationshipsRequest) error {
	return nil
}

func (*sharedSessionAuthorizationProvider) GetActiveModel(context.Context) (*core.GetActiveModelResponse, error) {
	return &core.GetActiveModelResponse{}, nil
}

func (*sharedSessionAuthorizationProvider) ListModels(context.Context, *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	return &core.ListModelsResponse{}, nil
}

func (*sharedSessionAuthorizationProvider) WriteModel(context.Context, *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
	return &core.AuthorizationModelRef{Id: "model-test", Version: "1"}, nil
}

func cloneAuthzResources(resources []*core.ResourceRef) []*core.ResourceRef {
	out := make([]*core.ResourceRef, 0, len(resources))
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		out = append(out, &core.ResourceRef{Type: resource.GetType(), Id: resource.GetId()})
	}
	return out
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

func (p *routeCountingAgentProvider) SupportsWorkspaceRequests() bool {
	return p.supportsWorkspace
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
		SessionStart: map[string]*coreagent.SessionStartConfig{
			"alpha": {
				Hooks: []coreagent.SessionStartHook{{
					ID:      "setup",
					Type:    "command",
					Command: []string{"true"},
				}},
			},
		},
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

func TestCreateSessionRejectsReservedWorkspaceMetadata(t *testing.T) {
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
		Metadata:     map[string]any{"workspacePath": "/tmp/spoofed"},
	})
	if !errors.Is(err, ErrAgentSessionMetadataInvalid) || !strings.Contains(err.Error(), "reserved for Gestalt workspace data") {
		t.Fatalf("CreateSession error = %v, want reserved workspace metadata error", err)
	}
	if len(provider.createSessionReqs) != 0 {
		t.Fatalf("CreateSession calls = %d, want 0", len(provider.createSessionReqs))
	}
}

func TestCreateSessionRejectsWorkspaceWhenProviderCannotPrepare(t *testing.T) {
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
		Workspace: &coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "git@github.com:valon-technologies/app.git",
				Path: "app",
			}},
		},
	})
	if !errors.Is(err, ErrAgentWorkspaceUnsupported) {
		t.Fatalf("CreateSession error = %v, want ErrAgentWorkspaceUnsupported", err)
	}
	if len(provider.createSessionReqs) != 0 {
		t.Fatalf("CreateSession calls = %d, want 0", len(provider.createSessionReqs))
	}
}

func TestCreateSessionValidatesWorkspaceBeforeProviderCreate(t *testing.T) {
	t.Parallel()

	provider := newRouteCountingAgentProvider("alpha")
	provider.supportsWorkspace = true
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers:   map[string]*routeCountingAgentProvider{"alpha": provider},
		},
	})

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Workspace: &coreagent.Workspace{
			CWD: "../app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "git@github.com:valon-technologies/app.git",
				Path: "app",
			}},
		},
	})
	if !errors.Is(err, ErrAgentWorkspaceInvalid) {
		t.Fatalf("CreateSession error = %v, want ErrAgentWorkspaceInvalid", err)
	}
	if len(provider.createSessionReqs) != 0 {
		t.Fatalf("CreateSession calls = %d, want 0", len(provider.createSessionReqs))
	}
}

func TestCreateSessionUsesStableWorkspaceSessionIDWithIdempotencyKey(t *testing.T) {
	t.Parallel()

	provider := newRouteCountingAgentProvider("alpha")
	provider.supportsWorkspace = true
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers:   map[string]*routeCountingAgentProvider{"alpha": provider},
		},
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	req := coreagent.ManagerCreateSessionRequest{
		ProviderName:   "alpha",
		IdempotencyKey: "workspace-create-1",
		Model:          "test-model",
		ClientRef:      "client-1",
		Workspace: &coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "git@github.com:valon-technologies/app.git",
				Ref:  "refs/heads/main",
				Path: "app",
			}},
		},
	}
	first, err := manager.CreateSession(context.Background(), p, req)
	if err != nil {
		t.Fatalf("CreateSession first: %v", err)
	}
	second, err := manager.CreateSession(context.Background(), p, req)
	if err != nil {
		t.Fatalf("CreateSession second: %v", err)
	}
	if first.ID == "" || first.ID != second.ID {
		t.Fatalf("session ids = %q, %q, want stable non-empty", first.ID, second.ID)
	}
	if len(provider.createSessionReqs) != 2 {
		t.Fatalf("CreateSession calls = %d, want 2", len(provider.createSessionReqs))
	}
	if provider.createSessionReqs[0].SessionID != provider.createSessionReqs[1].SessionID {
		t.Fatalf("provider session IDs = %q, %q, want stable", provider.createSessionReqs[0].SessionID, provider.createSessionReqs[1].SessionID)
	}
	if provider.createSessionReqs[0].Workspace == nil {
		t.Fatal("provider did not receive manager workspace")
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
	p := &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
		Identity: &core.UserIdentity{
			Email:       "ada@example.com",
			DisplayName: "Ada Lovelace",
		},
	}

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
	p := &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
		Identity: &core.UserIdentity{
			Email:       "ada@example.com",
			DisplayName: "Ada Lovelace",
		},
	}

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

func TestManagerListSharedSessionsUsesEffectiveSearchResources(t *testing.T) {
	t.Parallel()

	provider := newRouteCountingAgentProvider("alpha")
	provider.sessions["shared-session"] = &coreagent.Session{
		ID:           "shared-session",
		ProviderName: "alpha",
		State:        coreagent.SessionStateActive,
		CreatedBy:    coreagent.Actor{SubjectID: principal.UserSubjectID("owner")},
	}
	authz := &sharedSessionAuthorizationProvider{
		effectiveResources: []*core.ResourceRef{{Type: authorization.ProviderResourceTypeAgentSession, Id: "shared-session"}},
		allowedSessions:    map[string]struct{}{"shared-session": {}},
	}
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers:   map[string]*routeCountingAgentProvider{"alpha": provider},
		},
		AuthorizationProvider: authz,
	})

	sessions, err := manager.ListSessions(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("viewer")}, coreagent.ManagerListSessionsRequest{
		SummaryOnly: true,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "shared-session" {
		t.Fatalf("ListSessions shared sessions = %#v, want shared-session", sessions)
	}
	if authz.effectiveSearchResourceCalls != 1 {
		t.Fatalf("EffectiveSearchResources calls = %d, want 1", authz.effectiveSearchResourceCalls)
	}
	if authz.searchResourceCalls != 0 {
		t.Fatalf("SearchResources calls = %d, want 0", authz.searchResourceCalls)
	}
}

func TestManagerListSharedSessionsFallsBackWhenEffectiveSearchUnimplemented(t *testing.T) {
	t.Parallel()

	provider := newRouteCountingAgentProvider("alpha")
	provider.sessions["shared-session"] = &coreagent.Session{
		ID:           "shared-session",
		ProviderName: "alpha",
		State:        coreagent.SessionStateActive,
		CreatedBy:    coreagent.Actor{SubjectID: principal.UserSubjectID("owner")},
	}
	authz := &sharedSessionAuthorizationProvider{
		effectiveErr:    status.Error(codes.Unimplemented, "effective search is not supported"),
		directResources: []*core.ResourceRef{{Type: authorization.ProviderResourceTypeAgentSession, Id: "shared-session"}},
		allowedSessions: map[string]struct{}{"shared-session": {}},
	}
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers:   map[string]*routeCountingAgentProvider{"alpha": provider},
		},
		AuthorizationProvider: authz,
	})

	sessions, err := manager.ListSessions(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("viewer")}, coreagent.ManagerListSessionsRequest{
		SummaryOnly: true,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "shared-session" {
		t.Fatalf("ListSessions shared sessions = %#v, want shared-session", sessions)
	}
	if authz.effectiveSearchResourceCalls != 1 {
		t.Fatalf("EffectiveSearchResources calls = %d, want 1", authz.effectiveSearchResourceCalls)
	}
	if authz.searchResourceCalls != 1 {
		t.Fatalf("SearchResources calls = %d, want 1", authz.searchResourceCalls)
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
	p := &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
		Identity: &core.UserIdentity{
			Email:       "ada@example.com",
			DisplayName: "Ada Lovelace",
		},
	}

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

func TestManagerCreateTurnCarriesInheritedOutputDeliveryInRunGrant(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	roadmap := &catalogCountingProvider{StubIntegration: coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID: "sync",
		}}},
	}}
	notification := &catalogCountingProvider{StubIntegration: coretesting.StubIntegration{
		N:        "notification",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID: "reply",
		}}},
	}}
	grants := newAgentManagerTestRunGrants(t)
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, roadmap, notification),
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		RunGrants: grants,
	})
	p := &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
		TokenPermissions: principal.CompilePermissions([]core.AccessPermission{
			{Plugin: "alpha"},
			{Plugin: "roadmap", Operations: []string{"sync"}},
			{Plugin: "notification", Operations: []string{"reply"}},
		}),
	}
	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	ctx := WithInheritedOutputDelivery(context.Background(), &coreworkflow.OutputDelivery{
		Target: coreworkflow.PluginTarget{PluginName: "notification", Operation: "reply"},
		InputBindings: []coreworkflow.OutputBinding{
			{InputField: "text", Value: coreworkflow.OutputValueSource{AgentOutput: "text"}},
			{InputField: "reply_ref", Value: coreworkflow.OutputValueSource{Literal: "signed-ref"}},
		},
	})
	_, err = manager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "test-model",
		ToolRefs:  []coreagent.ToolRef{{Plugin: "roadmap", Operation: "sync"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	grant, err := grants.Resolve(alpha.createTurnReqs[0].RunGrant)
	if err != nil {
		t.Fatalf("Resolve run grant: %v", err)
	}
	if grant.InheritedOutputDelivery == nil || grant.InheritedOutputDelivery.Target.PluginName != "notification" || grant.InheritedOutputDelivery.Target.Operation != "reply" {
		t.Fatalf("inherited output delivery = %#v", grant.InheritedOutputDelivery)
	}
	if got := grant.ToolRefs; len(got) != 1 || got[0].Plugin != "roadmap" || got[0].Operation != "sync" {
		t.Fatalf("grant tool refs = %#v, want only visible roadmap tool", got)
	}
	if !reflect.DeepEqual(grant.Permissions, []core.AccessPermission{
		{Plugin: "notification", Operations: []string{"reply"}},
		{Plugin: "roadmap", Operations: []string{"sync"}},
	}) {
		t.Fatalf("grant permissions = %#v, want hidden delivery permission merged", grant.Permissions)
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
	grants := newAgentManagerTestRunGrants(t)
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, linear),
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
	grant, err := grants.Resolve(alpha.createTurnReqs[0].RunGrant)
	if err != nil {
		t.Fatalf("Resolve run grant: %v", err)
	}
	if grant.CallerPluginName != "slack" {
		t.Fatalf("grant caller plugin = %q, want slack", grant.CallerPluginName)
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
		SubjectID:   "service_account:github-toolshed",
		SubjectKind: "service_account",
	}
	externalIdentity := &core.ExternalIdentityRef{
		Type: "github_app_installation",
		ID:   "repo:{owner}/{repo}",
	}
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"slack": {{
				Plugin:                "github",
				Operation:             "bot.createPullRequest",
				RunAs:                 runAs,
				RunAsExternalIdentity: externalIdentity,
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
	if !core.ExternalIdentityRefsEqual(tools[0].Target.RunAsExternalIdentity, externalIdentity) {
		t.Fatalf("tool runAs external identity = %#v, want %#v", tools[0].Target.RunAsExternalIdentity, externalIdentity)
	}
}

func TestResolveToolsAppliesDeclaredInvokeCredentialModeAndRunAs(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:    "chat.postMessage",
				Title: "Post message",
			}}},
		},
	}
	runAs := &core.RunAsSubject{
		SubjectID:   "service_account:slack-bot",
		SubjectKind: "service_account",
		DisplayName: "Platform Slack Bot",
	}
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		PluginInvokes: map[string][]invocation.PluginInvocationDependency{
			"slack": {{
				Plugin:         "slack",
				Operation:      "chat.postMessage",
				CredentialMode: core.ConnectionModeNone,
				RunAs:          runAs,
			}},
		},
	})

	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		CallerPluginName: "slack",
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "slack",
			Operation: "chat.postMessage",
		}},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ResolveTools returned %d tools, want 1", len(tools))
	}
	if tools[0].Target.CredentialMode != core.ConnectionModeNone {
		t.Fatalf("tool credential mode = %q, want %q", tools[0].Target.CredentialMode, core.ConnectionModeNone)
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

func TestManagerProjectsAgentFacingPluginToolSchemas(t *testing.T) {
	t.Parallel()

	hidden := false
	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "planner",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "planner",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "choose_target",
						ProviderID:  "internal_secret_provider",
						Path:        "/planner/{x-secret}/choose",
						Title:       "Choose target",
						Description: "Choose a public target",
						InputSchema: json.RawMessage(`{
							"type":["object","null"],
							"properties":{
								"root_choice":{"type":"string"},
								"public_body":{
									"type":"object",
									"description":"public wrapper around internal tunnel token",
									"$ref":"#/$defs/internal_secret",
									"properties":{"internal_secret":{"type":"string","description":"internal tunnel token"},"visible_child":{"type":"string"}},
									"required":["internal_secret","visible_child"]
								},
								"internal_secret":{"type":"string"},
								"x-secret":{"type":"string"}
							},
							"required":["root_choice","internal_secret"],
							"dependentRequired":{"root_choice":["internal_secret"]},
							"patternProperties":{"^x-secret$":{"description":"internal tunnel token"}},
							"$defs":{"internal_secret":{"description":"internal tunnel token"}},
							"oneOf":[
								{"type":"object","properties":{"mcp_name":{"type":"string"},"internal_secret":{"type":"string"}},"required":["mcp_name"]},
								{"properties":{"ref":{"type":"string"},"x-secret":{"type":"string"}},"required":["ref","x-secret"]}
							],
							"anyOf":[
								{"properties":{"optional":{"type":"string"}},"required":["optional"]}
							]
						}`),
						Parameters: []catalog.CatalogParameter{
							{Name: "root_choice", Type: "string", Required: true},
							{Name: "internal_secret", WireName: "x-secret", Type: "string", Description: "internal tunnel token", Required: true, Internal: true},
						},
					},
					{
						ID:          "bad_schema",
						Title:       "Bad schema",
						Description: "Falls back to public parameter metadata",
						InputSchema: json.RawMessage(`"not an object"`),
						Parameters: []catalog.CatalogParameter{
							{Name: "visible_query", Type: "string", Description: "Visible query", Required: true},
							{Name: "private_header", WireName: "X-Private-Header", Type: "string", Description: "private header value", Required: true, Internal: true},
						},
					},
					{
						ID:          "conflict_schema",
						Title:       "Conflict schema",
						Description: "Falls back when merged branches disagree",
						InputSchema: json.RawMessage(`{
							"allOf":[
								{"properties":{"same":{"type":"integer"}}}
							],
							"properties":{"local":{"type":"string"},"same":{"type":"string"}}
						}`),
						Parameters: []catalog.CatalogParameter{
							{Name: "fallback_query", Type: "string", Description: "Fallback query", Required: true},
							{Name: "private_conflict", Type: "string", Description: "private conflict value", Internal: true},
						},
					},
					{
						ID:          "hidden_admin",
						Title:       "Hidden admin",
						Description: "Hidden exact-grant operation",
						Visible:     &hidden,
						InputSchema: json.RawMessage(`{
							"type":"object",
							"properties":{"public_action":{"type":"string"},"internal_admin":{"type":"string"}},
							"required":["public_action","internal_admin"]
						}`),
						Parameters: []catalog.CatalogParameter{
							{Name: "public_action", Type: "string", Required: true},
							{Name: "internal_admin", Type: "string", Description: "admin-only token", Required: true, Internal: true},
						},
					},
					{
						ID:    "empty_schema",
						Title: "Empty schema",
					},
				},
			},
		},
	}
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	refs := []coreagent.ToolRef{
		{Plugin: "planner", Operation: "choose_target"},
		{Plugin: "planner", Operation: "bad_schema"},
		{Plugin: "planner", Operation: "conflict_schema"},
		{Plugin: "planner", Operation: "hidden_admin"},
	}

	listed, err := manager.ListTools(context.Background(), p, coreagent.ListToolsRequest{
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
		ToolRefs:   refs,
		PageSize:   10,
	})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) != len(refs) {
		t.Fatalf("ListTools returned %d tools, want %d: %#v", len(listed.Tools), len(refs), listed.Tools)
	}

	choose := requireListedOperation(t, listed.Tools, "choose_target")
	chooseSchema := mustDecodeAgentToolSchema(t, choose.InputSchemaJSON)
	requireAgentToolSchemaProperties(t, chooseSchema, "root_choice", "public_body", "mcp_name", "ref", "optional")
	requireAgentToolSchemaMissingProperties(t, chooseSchema, "internal_secret", "x-secret")
	requireAgentToolSchemaMissingKeys(t, chooseSchema, "oneOf", "anyOf", "allOf", "dependentRequired", "patternProperties", "$defs")
	requireAgentToolSchemaRequired(t, chooseSchema, "root_choice")
	requireAgentToolSchemaJSONOmits(t, chooseSchema, "internal_secret", "x-secret", "internal tunnel token", "$ref", "internal_secret_provider")
	requireSearchTextOmits(t, choose.SearchText, "internal_secret", "x-secret", "internal tunnel token", "internal_secret_provider")

	bad := requireListedOperation(t, listed.Tools, "bad_schema")
	badSchema := mustDecodeAgentToolSchema(t, bad.InputSchemaJSON)
	requireAgentToolSchemaProperties(t, badSchema, "visible_query")
	requireAgentToolSchemaMissingProperties(t, badSchema, "private_header", "X-Private-Header")
	requireAgentToolSchemaRequired(t, badSchema, "visible_query")
	requireSearchTextOmits(t, bad.SearchText, "private_header", "X-Private-Header", "private header value")

	conflict := requireListedOperation(t, listed.Tools, "conflict_schema")
	conflictSchema := mustDecodeAgentToolSchema(t, conflict.InputSchemaJSON)
	requireAgentToolSchemaProperties(t, conflictSchema, "fallback_query")
	requireAgentToolSchemaMissingProperties(t, conflictSchema, "local", "same", "private_conflict")
	requireAgentToolSchemaRequired(t, conflictSchema, "fallback_query")

	hiddenTool := requireListedOperation(t, listed.Tools, "hidden_admin")
	if !hiddenTool.Hidden {
		t.Fatal("hidden exact operation was not marked hidden")
	}
	hiddenSchema := mustDecodeAgentToolSchema(t, hiddenTool.InputSchemaJSON)
	requireAgentToolSchemaProperties(t, hiddenSchema, "public_action")
	requireAgentToolSchemaMissingProperties(t, hiddenSchema, "internal_admin")
	requireAgentToolSchemaRequired(t, hiddenSchema, "public_action")
	requireSearchTextOmits(t, hiddenTool.SearchText, "internal_admin", "admin-only token")

	resolved, err := manager.ResolveTools(context.Background(), p, coreagent.ResolveToolsRequest{
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "planner", Operation: "bad_schema"},
			{Plugin: "planner", Operation: "empty_schema"},
			{Plugin: "planner", Operation: "hidden_admin"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	badResolved := requireResolvedOperation(t, resolved, "bad_schema")
	requireAgentToolSchemaProperties(t, badResolved.ParametersSchema, "visible_query")
	requireAgentToolSchemaMissingProperties(t, badResolved.ParametersSchema, "private_header", "X-Private-Header")
	emptyResolved := requireResolvedOperation(t, resolved, "empty_schema")
	if emptyResolved.ParametersSchema["type"] != "object" || emptyResolved.ParametersSchema["additionalProperties"] != true {
		t.Fatalf("empty resolved schema = %#v, want permissive object", emptyResolved.ParametersSchema)
	}
	hiddenResolved := requireResolvedOperation(t, resolved, "hidden_admin")
	requireAgentToolSchemaProperties(t, hiddenResolved.ParametersSchema, "public_action")
	requireAgentToolSchemaMissingProperties(t, hiddenResolved.ParametersSchema, "internal_admin")
}

func TestManagerProjectsWorkflowSystemToolSchemas(t *testing.T) {
	t.Parallel()

	ref := coreagent.ToolRef{System: coreagent.SystemToolWorkflow, Operation: "schedules.update"}
	workflowTools := projectionWorkflowSystemTools{
		tools: map[string]coreagent.Tool{
			ref.Operation: {
				Name:        "Update schedule",
				Description: "Update a workflow schedule",
				ParametersSchema: map[string]any{
					"allOf": []any{
						map[string]any{
							"type": "object",
							"properties": map[string]any{
								"scheduleId": map[string]any{"type": "string"},
							},
							"required": []any{"scheduleId"},
						},
						map[string]any{
							"properties": map[string]any{
								"cron": map[string]any{"type": "string"},
							},
							"required": []any{"cron"},
						},
					},
				},
				Target: coreagent.ToolTarget{System: ref.System, Operation: ref.Operation},
			},
		},
	}
	provider := agentCatalogTestProvider("docs", "Docs")
	manager := newTestManager(t, Config{
		Providers:     testutil.NewProviderRegistry(t, provider),
		WorkflowTools: workflowTools,
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	listed, err := manager.ListTools(context.Background(), p, coreagent.ListToolsRequest{
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
		ToolRefs:   []coreagent.ToolRef{ref},
	})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) != 1 {
		t.Fatalf("ListTools returned %d tools, want one", len(listed.Tools))
	}
	listedSchema := mustDecodeAgentToolSchema(t, listed.Tools[0].InputSchemaJSON)
	requireAgentToolSchemaProperties(t, listedSchema, "scheduleId", "cron")
	requireAgentToolSchemaRequired(t, listedSchema, "cron", "scheduleId")
	requireAgentToolSchemaMissingKeys(t, listedSchema, "allOf", "oneOf", "anyOf")

	resolved, err := manager.ResolveTools(context.Background(), p, coreagent.ResolveToolsRequest{
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
		ToolRefs:   []coreagent.ToolRef{ref},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("ResolveTools returned %d tools, want one", len(resolved))
	}
	requireAgentToolSchemaProperties(t, resolved[0].ParametersSchema, "scheduleId", "cron")
	requireAgentToolSchemaRequired(t, resolved[0].ParametersSchema, "cron", "scheduleId")
	requireAgentToolSchemaMissingKeys(t, resolved[0].ParametersSchema, "allOf", "oneOf", "anyOf")

	one, err := manager.ResolveTool(context.Background(), p, ref)
	if err != nil {
		t.Fatalf("ResolveTool: %v", err)
	}
	requireAgentToolSchemaProperties(t, one.ParametersSchema, "scheduleId", "cron")
	requireAgentToolSchemaRequired(t, one.ParametersSchema, "cron", "scheduleId")
	requireAgentToolSchemaMissingKeys(t, one.ParametersSchema, "allOf", "oneOf", "anyOf")
}

type projectionWorkflowSystemTools struct {
	tools map[string]coreagent.Tool
}

func (t projectionWorkflowSystemTools) Available() bool {
	return true
}

func (t projectionWorkflowSystemTools) ResolveTool(_ context.Context, _ *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error) {
	tool, ok := t.tools[strings.TrimSpace(ref.Operation)]
	if !ok {
		return coreagent.Tool{}, fmt.Errorf("%w: %s", invocation.ErrOperationNotFound, ref.Operation)
	}
	tool.Target = coreagent.ToolTarget{System: strings.TrimSpace(ref.System), Operation: strings.TrimSpace(ref.Operation)}
	return tool, nil
}

func (t projectionWorkflowSystemTools) ResolveTools(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef) ([]coreagent.Tool, error) {
	out := make([]coreagent.Tool, 0, len(refs))
	for _, ref := range refs {
		tool, err := t.ResolveTool(ctx, p, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, tool)
	}
	return out, nil
}

func (t projectionWorkflowSystemTools) AllowTool(context.Context, *principal.Principal, coreagent.Tool) bool {
	return true
}

func requireListedOperation(t *testing.T, tools []coreagent.ListedTool, operation string) coreagent.ListedTool {
	t.Helper()
	for _, tool := range tools {
		if tool.Ref.Operation == operation {
			return tool
		}
	}
	t.Fatalf("listed tools missing operation %q: %#v", operation, tools)
	return coreagent.ListedTool{}
}

func requireResolvedOperation(t *testing.T, tools []coreagent.Tool, operation string) coreagent.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Target.Operation == operation {
			return tool
		}
	}
	t.Fatalf("resolved tools missing operation %q: %#v", operation, tools)
	return coreagent.Tool{}
}

func mustDecodeAgentToolSchema(t *testing.T, raw string) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatalf("decode schema %q: %v", raw, err)
	}
	return schema
}

func requireAgentToolSchemaProperties(t *testing.T, schema map[string]any, names ...string) {
	t.Helper()
	properties := agentToolSchemaPropertiesForTest(t, schema)
	for _, name := range names {
		if _, ok := properties[name]; !ok {
			t.Fatalf("schema properties = %#v, missing %q", properties, name)
		}
	}
}

func requireAgentToolSchemaMissingProperties(t *testing.T, schema map[string]any, names ...string) {
	t.Helper()
	properties := agentToolSchemaPropertiesForTest(t, schema)
	for _, name := range names {
		if _, ok := properties[name]; ok {
			t.Fatalf("schema properties = %#v, unexpectedly contained %q", properties, name)
		}
	}
}

func requireAgentToolSchemaMissingKeys(t *testing.T, schema map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := schema[key]; ok {
			t.Fatalf("schema = %#v, unexpectedly contained key %q", schema, key)
		}
	}
}

func requireAgentToolSchemaJSONOmits(t *testing.T, schema map[string]any, values ...string) {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	text := string(raw)
	for _, value := range values {
		if strings.Contains(text, value) {
			t.Fatalf("schema JSON %q unexpectedly contained %q", text, value)
		}
	}
}

func requireAgentToolSchemaRequired(t *testing.T, schema map[string]any, names ...string) {
	t.Helper()
	got := map[string]struct{}{}
	switch required := schema["required"].(type) {
	case []any:
		for _, item := range required {
			if name, ok := item.(string); ok {
				got[name] = struct{}{}
			}
		}
	case []string:
		for _, name := range required {
			got[name] = struct{}{}
		}
	case nil:
	default:
		t.Fatalf("schema required = %#v, want array", schema["required"])
	}
	want := map[string]struct{}{}
	for _, name := range names {
		want[name] = struct{}{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema required = %#v, want %#v", got, want)
	}
}

func requireSearchTextOmits(t *testing.T, searchText string, values ...string) {
	t.Helper()
	for _, value := range values {
		needle := agentToolSearchText(value)
		if needle == "" {
			needle = value
		}
		if strings.Contains(searchText, needle) {
			t.Fatalf("search text %q unexpectedly contained %q", searchText, value)
		}
	}
}

func agentToolSchemaPropertiesForTest(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want object", schema["properties"])
	}
	return properties
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
		RunAsExternalIdentity: &core.ExternalIdentityRef{
			Type: "github_app_installation",
			ID:   "repo:acme/widgets",
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
	same.RunAsExternalIdentity = &core.ExternalIdentityRef{
		Type: " github_app_installation ",
		ID:   " repo:acme/widgets ",
	}
	differentMetadata := base
	differentMetadata.RunAs = &core.RunAsSubject{
		SubjectID:           base.RunAs.SubjectID,
		SubjectKind:         base.RunAs.SubjectKind,
		CredentialSubjectID: base.RunAs.CredentialSubjectID,
		DisplayName:         "Another display name",
		AuthSource:          base.RunAs.AuthSource,
	}
	differentExternalIdentity := base
	differentExternalIdentity.RunAsExternalIdentity = &core.ExternalIdentityRef{
		Type: "github_app_installation",
		ID:   "repo:acme/other",
	}

	if agentToolTargetKeyFromRef(base) != agentToolTargetKeyFromRef(same) {
		t.Fatal("agentToolTargetKeyFromRef should normalize equivalent runAs subjects and external identities")
	}
	if agentToolTargetKeyFromRef(base) == agentToolTargetKeyFromRef(differentMetadata) {
		t.Fatal("agentToolTargetKeyFromRef collapsed distinct runAs metadata")
	}
	if agentToolTargetKeyFromRef(base) == agentToolTargetKeyFromRef(differentExternalIdentity) {
		t.Fatal("agentToolTargetKeyFromRef collapsed distinct runAs external identity")
	}
}

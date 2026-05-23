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
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
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
	getSessionErr     error
	listSessionsErr   error
	getTurnErr        error
	listTurnsErr      error
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

func (p *routeCountingAgentProvider) SupportsWorkspaceRequests() bool {
	return p.supportsWorkspace
}

func (p *routeCountingAgentProvider) GetSession(_ context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	p.getSessionCalls++
	if p.getSessionErr != nil {
		return nil, p.getSessionErr
	}
	session := p.sessions[strings.TrimSpace(req.SessionID)]
	if session == nil {
		return nil, core.ErrNotFound
	}
	return cloneRouteSession(session), nil
}

func (p *routeCountingAgentProvider) ListSessions(_ context.Context, req coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	p.listSessionReqs = append(p.listSessionReqs, req)
	if p.listSessionsErr != nil {
		return nil, p.listSessionsErr
	}
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
	if p.getTurnErr != nil {
		return nil, p.getTurnErr
	}
	turn := p.turns[strings.TrimSpace(req.TurnID)]
	if turn == nil {
		return nil, core.ErrNotFound
	}
	return cloneRouteTurn(turn), nil
}

func (p *routeCountingAgentProvider) ListTurns(_ context.Context, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	p.listTurnReqs = append(p.listTurnReqs, req)
	if p.listTurnsErr != nil {
		return nil, p.listTurnsErr
	}
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

func TestManagerGetSessionContinuesAfterProviderUnavailable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	alpha := newRouteCountingAgentProvider("alpha")
	beta := newRouteCountingAgentProvider("beta")
	control := &routeCountingAgentControl{
		defaultName: "alpha",
		names:       []string{"beta", "alpha"},
		providers: map[string]*routeCountingAgentProvider{
			"alpha": alpha,
			"beta":  beta,
		},
	}
	manager := newTestManager(t, Config{Agent: control})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	session := &coreagent.Session{
		ID:           "session-1",
		ProviderName: "alpha",
		State:        coreagent.SessionStateActive,
		CreatedBy:    coreagent.Actor{SubjectID: principal.UserSubjectID("user-1")},
	}
	alpha.sessions[session.ID] = session
	beta.getSessionErr = status.Error(codes.Unavailable, "provider restarting")

	got, err := manager.GetSession(ctx, p, session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ProviderName != "alpha" || got.ID != session.ID {
		t.Fatalf("GetSession = %+v, want alpha session", got)
	}
	if beta.getSessionCalls != 1 || alpha.getSessionCalls != 1 {
		t.Fatalf("GetSession calls = beta:%d alpha:%d, want 1 each", beta.getSessionCalls, alpha.getSessionCalls)
	}
}

func TestManagerGetSessionReturnsRetainedProviderErrorAfterFanoutMiss(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	alpha := newRouteCountingAgentProvider("alpha")
	beta := newRouteCountingAgentProvider("beta")
	control := &routeCountingAgentControl{
		defaultName: "alpha",
		names:       []string{"beta", "alpha"},
		providers: map[string]*routeCountingAgentProvider{
			"alpha": alpha,
			"beta":  beta,
		},
	}
	manager := newTestManager(t, Config{Agent: control})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	beta.getSessionErr = status.Error(codes.Unavailable, "provider restarting")

	_, err := manager.GetSession(ctx, p, "session-1")
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("GetSession error = %v, want retained unavailable", err)
	}
	if beta.getSessionCalls != 1 || alpha.getSessionCalls != 1 {
		t.Fatalf("GetSession calls = beta:%d alpha:%d, want 1 each", beta.getSessionCalls, alpha.getSessionCalls)
	}
}

func TestManagerGetTurnContinuesAfterProviderUnavailable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	alpha := newRouteCountingAgentProvider("alpha")
	beta := newRouteCountingAgentProvider("beta")
	control := &routeCountingAgentControl{
		defaultName: "alpha",
		names:       []string{"beta", "alpha"},
		providers: map[string]*routeCountingAgentProvider{
			"alpha": alpha,
			"beta":  beta,
		},
	}
	manager := newTestManager(t, Config{Agent: control})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	session := &coreagent.Session{
		ID:           "session-1",
		ProviderName: "alpha",
		State:        coreagent.SessionStateActive,
		CreatedBy:    coreagent.Actor{SubjectID: principal.UserSubjectID("user-1")},
	}
	turn := &coreagent.Turn{
		ID:           "turn-1",
		SessionID:    session.ID,
		ProviderName: "alpha",
		Status:       coreagent.ExecutionStatusSucceeded,
		CreatedBy:    coreagent.Actor{SubjectID: principal.UserSubjectID("user-1")},
	}
	alpha.sessions[session.ID] = session
	alpha.turns[turn.ID] = turn
	beta.getTurnErr = status.Error(codes.Unavailable, "provider restarting")

	got, err := manager.GetTurn(ctx, p, turn.ID)
	if err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if got.ProviderName != "alpha" || got.ID != turn.ID {
		t.Fatalf("GetTurn = %+v, want alpha turn", got)
	}
	if beta.getTurnCalls != 1 || alpha.getTurnCalls != 1 {
		t.Fatalf("GetTurn calls = beta:%d alpha:%d, want 1 each", beta.getTurnCalls, alpha.getTurnCalls)
	}
}

func TestManagerVisibleNonOwnedSessionReadsButCannotWrite(t *testing.T) {
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
		RunGrants: newAgentManagerTestRunGrants(t),
	})
	owner := &principal.Principal{SubjectID: principal.UserSubjectID("owner")}
	viewer := &principal.Principal{SubjectID: principal.UserSubjectID("viewer")}

	session, err := manager.CreateSession(context.Background(), owner, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := manager.CreateTurn(context.Background(), owner, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "test-model",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}

	if _, err := manager.GetSession(context.Background(), viewer, session.ID); err != nil {
		t.Fatalf("GetSession as visible non-owner: %v", err)
	}
	if _, err := manager.GetTurn(context.Background(), viewer, turn.ID); err != nil {
		t.Fatalf("GetTurn as visible non-owner: %v", err)
	}
	if _, err := manager.UpdateSession(context.Background(), viewer, coreagent.ManagerUpdateSessionRequest{SessionID: session.ID, ClientRef: "viewer-edit"}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("UpdateSession as visible non-owner error = %v, want not found", err)
	}
	if _, err := manager.CreateTurn(context.Background(), viewer, coreagent.ManagerCreateTurnRequest{SessionID: session.ID, Model: "test-model"}); !errors.Is(err, ErrAgentSessionNotFound) {
		t.Fatalf("CreateTurn as visible non-owner error = %v, want session not found", err)
	}
	if _, err := manager.CancelTurn(context.Background(), viewer, turn.ID, "viewer-cancel"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("CancelTurn as visible non-owner error = %v, want not found", err)
	}
	if got := alpha.turns[turn.ID].Status; got != coreagent.ExecutionStatusRunning {
		t.Fatalf("turn status after non-owner cancel = %q, want running", got)
	}
	if _, err := manager.ResolveInteraction(context.Background(), viewer, turn.ID, "interaction-1", map[string]any{"value": true}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("ResolveInteraction as visible non-owner error = %v, want not found", err)
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
		t.Fatalf("GetTurn calls = %d, want 1 provider lookup", alpha.getTurnCalls)
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

func TestManagerListSessionsReturnsCapabilityErrorWhenCapabilityReadUnavailable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	provider := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers:   map[string]*routeCountingAgentProvider{"alpha": provider},
		},
		RunGrants: newAgentManagerTestRunGrants(t),
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	if _, err := manager.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	provider.capabilitiesErr = status.Error(codes.Unavailable, "sandbox is redeploying")

	_, err := manager.ListSessions(ctx, p, coreagent.ManagerListSessionsRequest{
		ProviderName: "alpha",
		SummaryOnly:  true,
		Limit:        10,
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("ListSessions error = %v, want unavailable capability error", err)
	}
	if len(provider.listSessionReqs) != 0 {
		t.Fatalf("provider ListSessions calls = %d, want 0 after capability error", len(provider.listSessionReqs))
	}
}

func TestManagerListSessionsReturnsProviderErrorWithoutPartialResults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
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
	manager := newTestManager(t, Config{Agent: control})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	if _, err := manager.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	beta.listSessionsErr = status.Error(codes.Unavailable, "sandbox is redeploying")

	_, err := manager.ListSessions(ctx, p, coreagent.ManagerListSessionsRequest{
		SummaryOnly: true,
		Limit:       10,
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("ListSessions error = %v, want unavailable from provider without projection", err)
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

func TestManagerListTurnsReturnsCapabilityErrorWhenCapabilityReadUnavailable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	provider := newRouteCountingAgentProvider("alpha")
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers:   map[string]*routeCountingAgentProvider{"alpha": provider},
		},
		RunGrants: newAgentManagerTestRunGrants(t),
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}
	session, err := manager.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := manager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "test-model",
		Messages:  []coreagent.Message{{Role: "user", Text: "hello"}},
	}); err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	provider.capabilitiesErr = status.Error(codes.Unavailable, "sandbox is redeploying")

	_, err = manager.ListTurns(ctx, p, coreagent.ManagerListTurnsRequest{
		SessionID:   session.ID,
		SummaryOnly: true,
		Limit:       10,
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("ListTurns error = %v, want unavailable capability error", err)
	}
	if len(provider.listTurnReqs) != 0 {
		t.Fatalf("provider ListTurns calls = %d, want 0 after capability error", len(provider.listTurnReqs))
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
	if got := req.ToolRefs; len(got) != 1 || got[0].App != agentToolSearchAllApp || got[0].Operation != "" {
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
	if got := grant.ToolRefs; len(got) != 1 || got[0].App != agentToolSearchAllApp || got[0].Operation != "" {
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
	if got := req.ToolRefs; len(got) != 1 || got[0].App != "linear" || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want linear provider ref", got)
	}
	grant, err := grants.Resolve(req.RunGrant)
	if err != nil {
		t.Fatalf("Resolve run grant: %v", err)
	}
	if got := grant.ToolRefs; len(got) != 1 || got[0].App != "linear" || got[0].Operation != "" {
		t.Fatalf("grant tool refs = %#v, want linear provider ref", got)
	}

	listed, err := manager.ListTools(context.Background(), p, coreagent.ListToolsRequest{
		ToolSource: grant.ToolSource,
		ToolRefs:   grant.ToolRefs,
	})
	if err != nil {
		t.Fatalf("ListTools narrowed grant: %v", err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Target.App != "linear" || listed.Tools[0].Target.Operation != "issues" {
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
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].App != agentToolSearchAllApp || got[0].Operation != "" {
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
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].App != agentToolSearchAllApp || got[0].Operation != "" {
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
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].App != agentToolSearchAllApp || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want broad wildcard for non-exact provider mention", got)
	}
}

func TestManagerCreateTurnKeepsImplicitWildcardForCallerAppDefaults(t *testing.T) {
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
		CallerAppName: "slack",
		SessionID:     session.ID,
		Model:         "test-model",
		Messages:      []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].App != agentToolSearchAllApp || got[0].Operation != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want broad wildcard for caller app default", got)
	}
	grant, err := grants.Resolve(alpha.createTurnReqs[0].RunGrant)
	if err != nil {
		t.Fatalf("Resolve run grant: %v", err)
	}
	if grant.CallerAppName != "slack" {
		t.Fatalf("grant caller app = %q, want slack", grant.CallerAppName)
	}
	if linear.catalogCalls != 0 {
		t.Fatalf("linear catalog calls = %d, want caller app default to skip narrowing probes", linear.catalogCalls)
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
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].App != "github" || got[0].Operation != "" {
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
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].App != agentToolSearchAllApp || got[0].Operation != "" {
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
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].App != agentToolSearchAllApp || got[0].Operation != "" {
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
	if got := alpha.createTurnReqs[0].ToolRefs; len(got) != 1 || got[0].App != agentToolSearchAllApp || got[0].Operation != "" {
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

func TestManagerCreateTurnHonorsNoneToolSource(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	alpha.capabilities = &coreagent.ProviderCapabilities{
		StructuredOutput: true,
		SupportedToolSources: []coreagent.ToolSourceMode{
			coreagent.ToolSourceModeNone,
		},
	}
	grants := newAgentManagerTestRunGrants(t)
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		RunGrants:        grants,
		AgentConnections: map[string][]string{"alpha": {"anthropic"}},
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
		ToolSource: coreagent.ToolSourceModeNone,
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	req := alpha.createTurnReqs[0]
	if req.ToolSource != coreagent.ToolSourceModeNone {
		t.Fatalf("CreateTurn tool source = %q, want none", req.ToolSource)
	}
	if len(req.ToolRefs) != 0 || len(req.Tools) != 0 {
		t.Fatalf("CreateTurn tools = refs:%#v resolved:%#v, want none", req.ToolRefs, req.Tools)
	}
	grant, err := grants.Resolve(req.RunGrant)
	if err != nil {
		t.Fatalf("Resolve run grant: %v", err)
	}
	if grant.ToolSource != coreagent.ToolSourceModeNone {
		t.Fatalf("grant tool source = %q, want none", grant.ToolSource)
	}
	if grant.ToolRefsSet {
		t.Fatalf("grant tool refs set = true, want false for unset tool refs")
	}
	if len(grant.ToolRefs) != 0 || len(grant.Tools) != 0 || len(grant.Connections) != 0 {
		t.Fatalf("grant scope = refs:%#v tools:%#v connections:%#v, want no tool or connection scope", grant.ToolRefs, grant.Tools, grant.Connections)
	}
}

func TestManagerCreateTurnValidatesStructuredOutputSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		caps        *coreagent.ProviderCapabilities
		req         coreagent.ManagerCreateTurnRequest
		wantErr     error
		wantCreated bool
	}{
		{
			name: "valid schema forwards presence",
			caps: &coreagent.ProviderCapabilities{
				StructuredOutput: true,
				SupportedToolSources: []coreagent.ToolSourceMode{
					coreagent.ToolSourceModeNone,
				},
			},
			req: coreagent.ManagerCreateTurnRequest{
				ToolSource:        coreagent.ToolSourceModeNone,
				ResponseSchema:    map[string]any{"type": "object"},
				ResponseSchemaSet: true,
			},
			wantCreated: true,
		},
		{
			name: "empty schema is invalid when present",
			caps: &coreagent.ProviderCapabilities{
				StructuredOutput: true,
				SupportedToolSources: []coreagent.ToolSourceMode{
					coreagent.ToolSourceModeNone,
				},
			},
			req: coreagent.ManagerCreateTurnRequest{
				ToolSource:        coreagent.ToolSourceModeNone,
				ResponseSchema:    map[string]any{},
				ResponseSchemaSet: true,
			},
			wantErr: invocation.ErrInvalidInvocation,
		},
		{
			name: "provider must advertise structured output",
			caps: &coreagent.ProviderCapabilities{
				SupportedToolSources: []coreagent.ToolSourceMode{
					coreagent.ToolSourceModeNone,
				},
			},
			req: coreagent.ManagerCreateTurnRequest{
				ToolSource:        coreagent.ToolSourceModeNone,
				ResponseSchema:    map[string]any{"type": "object"},
				ResponseSchemaSet: true,
			},
			wantErr: ErrAgentStructuredOutputUnsupported,
		},
		{
			name: "none source rejects tool refs",
			caps: &coreagent.ProviderCapabilities{
				StructuredOutput: true,
				SupportedToolSources: []coreagent.ToolSourceMode{
					coreagent.ToolSourceModeNone,
				},
			},
			req: coreagent.ManagerCreateTurnRequest{
				ToolSource: coreagent.ToolSourceModeNone,
				ToolRefs:   []coreagent.ToolRef{{App: "docs"}},
			},
			wantErr: invocation.ErrInvalidInvocation,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			alpha := newRouteCountingAgentProvider("alpha")
			alpha.capabilities = tt.caps
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
			req := tt.req
			req.SessionID = session.ID
			req.Model = "test-model"
			_, err = manager.CreateTurn(context.Background(), p, req)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("CreateTurn error = %v, want %v", err, tt.wantErr)
				}
				if len(alpha.createTurnReqs) != 0 {
					t.Fatalf("CreateTurn requests = %d, want 0", len(alpha.createTurnReqs))
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateTurn: %v", err)
			}
			if !tt.wantCreated {
				t.Fatal("test case missing assertion")
			}
			if len(alpha.createTurnReqs) != 1 {
				t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
			}
			got := alpha.createTurnReqs[0]
			if !got.ResponseSchemaSet {
				t.Fatal("CreateTurn response schema presence = false, want true")
			}
			if got.ResponseSchema["type"] != "object" {
				t.Fatalf("CreateTurn response schema = %#v, want object schema", got.ResponseSchema)
			}
		})
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
	if !grant.ToolRefsSet {
		t.Fatalf("grant tool refs set = false, want true for explicit empty tool refs")
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
		App:        "linear",
		Operations: []string{"issues"},
	}})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: perms,
		Scopes:           principal.PermissionApps(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	got := agentRunPermissions(ctx, p, "slack", []coreagent.ToolRef{{App: "*"}})
	if len(got) != 1 || got[0].App != "linear" || len(got[0].Operations) != 1 || got[0].Operations[0] != "issues" {
		t.Fatalf("agentRunPermissions = %#v, want API token permissions preserved", got)
	}
}

func TestAgentRunPermissionsCompactsExplicitCatalogRefs(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{
		{App: "linear", Operations: []string{"viewer", "issues.list", "issues.create"}},
		{App: "slack"},
		{App: "github"},
	})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: perms,
		Scopes:           principal.PermissionApps(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	got := agentRunPermissions(ctx, p, "", []coreagent.ToolRef{
		{App: "slack", Operation: "chat.postMessage"},
		{App: "linear", Operation: "viewer"},
		{App: "slack", Operation: "chat.postMessage"},
		{System: coreagent.SystemToolWorkflow, Operation: "run"},
	})
	want := []core.AccessPermission{
		{App: "linear", Operations: []string{"viewer"}},
		{App: "slack", Operations: []string{"chat.postMessage"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agentRunPermissions = %#v, want %#v", got, want)
	}
}

func TestAgentRunPermissionsCompactsExactRefsAfterAuthorization(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{
		{App: "linear", Operations: []string{"mcp.call"}},
		{App: "slack"},
	})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: perms,
		Scopes:           principal.PermissionApps(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	got := agentRunPermissions(ctx, p, "", []coreagent.ToolRef{{App: "linear", Operation: "viewer"}})
	want := []core.AccessPermission{{App: "linear", Operations: []string{"viewer"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agentRunPermissions = %#v, want %#v", got, want)
	}
}

func TestAgentRunPermissionsCompactsProviderWideCatalogRef(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{
		{App: "linear", Operations: []string{"viewer"}},
		{App: "slack"},
	})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: perms,
		Scopes:           principal.PermissionApps(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	got := agentRunPermissions(ctx, p, "", []coreagent.ToolRef{
		{App: "linear", Operation: "viewer"},
		{App: "linear"},
	})
	want := []core.AccessPermission{{App: "linear", Operations: []string{"viewer"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agentRunPermissions = %#v, want %#v", got, want)
	}
}

func TestAgentRunPermissionsClearsHTTPResolvedUserWildcardRestrictions(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{{
		App:        "slack",
		Operations: []string{"events.reply"},
	}})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		TokenPermissions: perms,
		Scopes:           principal.PermissionApps(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	if got := agentRunPermissions(ctx, p, "slack", []coreagent.ToolRef{{App: "*"}}); got != nil {
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

func TestResolveToolsExpandsAppOnlyRefs(t *testing.T) {
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
		ToolRefs: []coreagent.ToolRef{{App: "docs"}},
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
		ToolRefs:   []coreagent.ToolRef{{App: "docs"}},
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
		ToolRefs:   []coreagent.ToolRef{{App: "docs"}},
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
		ToolRefs:   []coreagent.ToolRef{{App: "docs"}},
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
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"slackbot": {{
				App:            "slack",
				Operation:      "events.reply",
				CredentialMode: core.ConnectionModeNone,
			}},
		},
	})

	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		CallerAppName: "slackbot",
		ToolRefs: []coreagent.ToolRef{{
			App:       "slack",
			Operation: "events.reply",
		}},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ResolveTools returned %d tools, want 1", len(tools))
	}
	if tools[0].Target.App != "slack" || tools[0].Target.Operation != "events.reply" {
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
		SubjectID:           "service_account:github-toolshed",
		SubjectKind:         "service_account",
		CredentialSubjectID: "service_account:github-toolshed-credential",
		DisplayName:         "GitHub Toolshed",
		AuthSource:          "github_app_webhook",
	}
	externalIdentity := &core.ExternalIdentityRef{
		Type: "github_app_installation",
		ID:   "repo:{owner}/{repo}",
	}
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"slack": {{
				App:                   "github",
				Operation:             "bot.createPullRequest",
				RunAs:                 runAs,
				RunAsExternalIdentity: externalIdentity,
			}},
		},
	})

	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		CallerAppName: "slack",
		ToolRefs: []coreagent.ToolRef{{
			App:       "github",
			Operation: "bot.createPullRequest",
			RunAs: &core.RunAsSubject{
				SubjectID:   runAs.SubjectID,
				DisplayName: "Spoofed display name",
				AuthSource:  "spoofed_auth_source",
			},
			RunAsExternalIdentity: externalIdentity,
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
	if tools[0].Target.RunAs.CredentialSubjectID != runAs.CredentialSubjectID {
		t.Fatalf("tool runAs credential subject = %q, want %q", tools[0].Target.RunAs.CredentialSubjectID, runAs.CredentialSubjectID)
	}
	if tools[0].Target.RunAs.DisplayName != runAs.DisplayName || tools[0].Target.RunAs.AuthSource != runAs.AuthSource {
		t.Fatalf("tool runAs metadata = %#v, want declared invoke metadata", tools[0].Target.RunAs)
	}
	if !core.ExternalIdentityRefsEqual(tools[0].Target.RunAsExternalIdentity, externalIdentity) {
		t.Fatalf("tool runAs external identity = %#v, want %#v", tools[0].Target.RunAsExternalIdentity, externalIdentity)
	}
}

func TestResolveToolsExplicitOnlyInvokeRunAsDoesNotApplyImplicitly(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "notion",
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:    "search",
				Title: "Search",
			}}},
		},
	}
	runAs := &core.RunAsSubject{
		SubjectID:           "service_account:gestalt-support-notion",
		SubjectKind:         "service_account",
		CredentialSubjectID: "service_account:gestalt-support-notion",
		DisplayName:         "Gestalt Support Notion",
	}
	externalIdentity := &core.ExternalIdentityRef{
		Type: "notion_workspace",
		ID:   "valon-support",
	}
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"slack": {{
				App:                   "notion",
				Operation:             "search",
				RunAs:                 runAs,
				RunAsExternalIdentity: externalIdentity,
				RunAsExplicitOnly:     true,
			}},
		},
	})

	implicitTools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		CallerAppName: "slack",
		ToolRefs: []coreagent.ToolRef{{
			App:       "notion",
			Operation: "search",
		}},
	})
	if err != nil {
		t.Fatalf("ResolveTools implicit: %v", err)
	}
	if len(implicitTools) != 1 {
		t.Fatalf("implicit ResolveTools returned %d tools, want 1", len(implicitTools))
	}
	if implicitTools[0].Target.RunAs != nil {
		t.Fatalf("implicit tool runAs = %#v, want nil", implicitTools[0].Target.RunAs)
	}
	if implicitTools[0].Target.RunAsExternalIdentity != nil {
		t.Fatalf("implicit tool runAs external identity = %#v, want nil", implicitTools[0].Target.RunAsExternalIdentity)
	}

	explicitTools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		CallerAppName: "slack",
		ToolRefs: []coreagent.ToolRef{{
			App:       "notion",
			Operation: "search",
			RunAs: &core.RunAsSubject{
				SubjectID: runAs.SubjectID,
			},
		}},
	})
	if err != nil {
		t.Fatalf("ResolveTools explicit: %v", err)
	}
	if len(explicitTools) != 1 {
		t.Fatalf("explicit ResolveTools returned %d tools, want 1", len(explicitTools))
	}
	if !core.RunAsSubjectsEqual(explicitTools[0].Target.RunAs, runAs) {
		t.Fatalf("explicit tool runAs = %#v, want %#v", explicitTools[0].Target.RunAs, runAs)
	}
	if !core.ExternalIdentityRefsEqual(explicitTools[0].Target.RunAsExternalIdentity, externalIdentity) {
		t.Fatalf("explicit tool runAs external identity = %#v, want %#v", explicitTools[0].Target.RunAsExternalIdentity, externalIdentity)
	}
}

func TestApplyCallerInvokePoliciesExplicitOnlyExternalIdentityRequestAppliesRunAs(t *testing.T) {
	t.Parallel()

	runAs := &core.RunAsSubject{
		SubjectID:           "service_account:gestalt-support-notion",
		SubjectKind:         "service_account",
		CredentialSubjectID: "service_account:gestalt-support-notion",
		DisplayName:         "Gestalt Support Notion",
	}
	externalIdentity := &core.ExternalIdentityRef{
		Type: "notion_workspace",
		ID:   "valon-support",
	}
	manager := newTestManager(t, Config{
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"slack": {{
				App:                   "notion",
				Operation:             "search",
				RunAs:                 runAs,
				RunAsExternalIdentity: externalIdentity,
				RunAsExplicitOnly:     true,
			}},
		},
	})

	// Exercise the policy helper directly because normalizeToolRefs rejects this
	// user-facing shape before policy application.
	refs, err := manager.applyCallerInvokePolicies("slack", []coreagent.ToolRef{{
		App:                   "notion",
		Operation:             "search",
		RunAsExternalIdentity: externalIdentity,
	}})
	if err != nil {
		t.Fatalf("applyCallerInvokePolicies: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("applyCallerInvokePolicies returned %d refs, want 1", len(refs))
	}
	if !core.RunAsSubjectsEqual(refs[0].RunAs, runAs) {
		t.Fatalf("tool ref runAs = %#v, want %#v", refs[0].RunAs, runAs)
	}
	if !core.ExternalIdentityRefsEqual(refs[0].RunAsExternalIdentity, externalIdentity) {
		t.Fatalf("tool ref runAs external identity = %#v, want %#v", refs[0].RunAsExternalIdentity, externalIdentity)
	}
}

func TestManagerCreateTurnAppliesExplicitInvokeRunAsToProviderAndRunGrant(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	grants := newAgentManagerTestRunGrants(t)
	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "notion",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:    "search",
				Title: "Search",
			}}},
		},
	}
	runAs := &core.RunAsSubject{
		SubjectID:           "service_account:gestalt-support-notion",
		SubjectKind:         "service_account",
		CredentialSubjectID: "service_account:gestalt-support-notion-credential",
		DisplayName:         "Gestalt Support Notion",
		AuthSource:          "notion_service_account",
	}
	externalIdentity := &core.ExternalIdentityRef{
		Type: "notion_workspace",
		ID:   "valon-support",
	}
	manager := newTestManager(t, Config{
		Agent: &routeCountingAgentControl{
			defaultName: "alpha",
			names:       []string{"alpha"},
			providers: map[string]*routeCountingAgentProvider{
				"alpha": alpha,
			},
		},
		Providers: testutil.NewProviderRegistry(t, provider),
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"slack": {{
				App:                   "notion",
				Operation:             "search",
				RunAs:                 runAs,
				RunAsExternalIdentity: externalIdentity,
			}},
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
		SessionID:     session.ID,
		Model:         "test-model",
		CallerAppName: "slack",
		ToolSource:    coreagent.ToolSourceModeMCPCatalog,
		ToolRefsSet:   true,
		ToolRefs: []coreagent.ToolRef{{
			App:       "notion",
			Operation: "search",
			RunAs: &core.RunAsSubject{
				SubjectID: runAs.SubjectID,
			},
			RunAsExternalIdentity: externalIdentity,
		}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	req := alpha.createTurnReqs[0]
	if len(req.ToolRefs) != 1 {
		t.Fatalf("CreateTurn tool refs = %d, want 1", len(req.ToolRefs))
	}
	if !core.RunAsSubjectsEqual(req.ToolRefs[0].RunAs, runAs) {
		t.Fatalf("CreateTurn tool ref runAs = %#v, want %#v", req.ToolRefs[0].RunAs, runAs)
	}
	if !core.ExternalIdentityRefsEqual(req.ToolRefs[0].RunAsExternalIdentity, externalIdentity) {
		t.Fatalf("CreateTurn tool ref runAs external identity = %#v, want %#v", req.ToolRefs[0].RunAsExternalIdentity, externalIdentity)
	}
	grant, err := grants.Resolve(req.RunGrant)
	if err != nil {
		t.Fatalf("Resolve run grant: %v", err)
	}
	if grant.CallerAppName != "slack" {
		t.Fatalf("run grant caller app = %q, want slack", grant.CallerAppName)
	}
	if len(grant.ToolRefs) != 1 {
		t.Fatalf("run grant tool refs = %d, want 1", len(grant.ToolRefs))
	}
	if !core.RunAsSubjectsEqual(grant.ToolRefs[0].RunAs, runAs) {
		t.Fatalf("run grant tool ref runAs = %#v, want %#v", grant.ToolRefs[0].RunAs, runAs)
	}
	if !core.ExternalIdentityRefsEqual(grant.ToolRefs[0].RunAsExternalIdentity, externalIdentity) {
		t.Fatalf("run grant tool ref runAs external identity = %#v, want %#v", grant.ToolRefs[0].RunAsExternalIdentity, externalIdentity)
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
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"slack": {{
				App:            "slack",
				Operation:      "chat.postMessage",
				CredentialMode: core.ConnectionModeNone,
				RunAs:          runAs,
			}},
		},
	})

	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		CallerAppName: "slack",
		ToolRefs: []coreagent.ToolRef{{
			App:       "slack",
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
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"slackbot": {{
				App:            "slack",
				Operation:      "chat.postMessage",
				CredentialMode: core.ConnectionModeNone,
			}},
		},
	})

	for _, tc := range []struct {
		name          string
		callerAppName string
	}{
		{name: "public request"},
		{name: "caller without matching invoke", callerAppName: "slackbot"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := manager.ResolveTools(context.Background(), &principal.Principal{
				SubjectID: principal.UserSubjectID("user-1"),
			}, coreagent.ResolveToolsRequest{
				CallerAppName: tc.callerAppName,
				ToolRefs: []coreagent.ToolRef{{
					App:            "slack",
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
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"slack": {{
				App:       "github",
				Operation: "bot.getPullRequest",
				RunAs:     runAs,
			}},
		},
	})

	for _, tc := range []struct {
		name          string
		callerAppName string
	}{
		{name: "public request"},
		{name: "caller without matching invoke", callerAppName: "slack"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := manager.ResolveTools(context.Background(), &principal.Principal{
				SubjectID: principal.UserSubjectID("user-1"),
			}, coreagent.ResolveToolsRequest{
				CallerAppName: tc.callerAppName,
				ToolRefs: []coreagent.ToolRef{{
					App:       "github",
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

func TestResolveToolsRejectsMismatchedRunAsExternalIdentity(t *testing.T) {
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
		SubjectID:           "service_account:github-toolshed",
		SubjectKind:         "service_account",
		CredentialSubjectID: "service_account:github-toolshed",
	}
	manager := newTestManager(t, Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		AppInvokes: map[string][]invocation.AppInvocationDependency{
			"slack": {{
				App:       "github",
				Operation: "bot.createPullRequest",
				RunAs:     runAs,
				RunAsExternalIdentity: &core.ExternalIdentityRef{
					Type: "github_app_installation",
					ID:   "repo:acme/widgets",
				},
			}},
		},
	})

	_, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		CallerAppName: "slack",
		ToolRefs: []coreagent.ToolRef{{
			App:       "github",
			Operation: "bot.createPullRequest",
			RunAs:     runAs,
			RunAsExternalIdentity: &core.ExternalIdentityRef{
				Type: "github_app_installation",
				ID:   "repo:acme/other",
			},
		}},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("ResolveTools error = %v, want ErrAuthorizationDenied", err)
	}
}

func TestManagerProjectsAgentFacingAppToolSchemas(t *testing.T) {
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
		{App: "planner", Operation: "choose_target"},
		{App: "planner", Operation: "bad_schema"},
		{App: "planner", Operation: "conflict_schema"},
		{App: "planner", Operation: "hidden_admin"},
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
			{App: "planner", Operation: "bad_schema"},
			{App: "planner", Operation: "empty_schema"},
			{App: "planner", Operation: "hidden_admin"},
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

func TestAgentToolTargetKeyIgnoresRunAsDisplayMetadata(t *testing.T) {
	t.Parallel()

	base := coreagent.ToolRef{
		App:       "github",
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
	differentDisplayMetadata := base
	differentDisplayMetadata.RunAs = &core.RunAsSubject{
		SubjectID:           base.RunAs.SubjectID,
		SubjectKind:         base.RunAs.SubjectKind,
		CredentialSubjectID: base.RunAs.CredentialSubjectID,
		DisplayName:         "Another display name",
		AuthSource:          "another_auth_source",
	}
	differentCredentialSubject := base
	differentCredentialSubject.RunAs = &core.RunAsSubject{
		SubjectID:           base.RunAs.SubjectID,
		SubjectKind:         base.RunAs.SubjectKind,
		CredentialSubjectID: "service_account:github_app_installation:99:repo:acme/other",
		DisplayName:         base.RunAs.DisplayName,
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
	if agentToolTargetKeyFromRef(base) != agentToolTargetKeyFromRef(differentDisplayMetadata) {
		t.Fatal("agentToolTargetKeyFromRef should ignore runAs display/auth metadata")
	}
	if agentToolTargetKeyFromRef(base) == agentToolTargetKeyFromRef(differentCredentialSubject) {
		t.Fatal("agentToolTargetKeyFromRef collapsed distinct runAs credential subject")
	}
	if agentToolTargetKeyFromRef(base) == agentToolTargetKeyFromRef(differentExternalIdentity) {
		t.Fatal("agentToolTargetKeyFromRef collapsed distinct runAs external identity")
	}
}

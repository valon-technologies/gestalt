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
	"github.com/valon-technologies/gestalt/server/internal/agentwire"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
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

func mustProtoStruct(t testing.TB, values map[string]any) *structpb.Struct {
	t.Helper()
	out, err := protoutil.StructFromMap(values)
	if err != nil {
		t.Fatalf("StructFromMap: %v", err)
	}
	return out
}

func mustAgentMessages(t testing.TB, messages []coreagent.Message) []*proto.AgentMessage {
	t.Helper()
	out, err := agentwire.MessagesToProto(messages)
	if err != nil {
		t.Fatalf("MessagesToProto: %v", err)
	}
	return out
}

func agentTextOutputProto() *proto.AgentOutput {
	return &proto.AgentOutput{Kind: &proto.AgentOutput_Text{Text: &proto.AgentTextOutput{}}}
}

func agentStructuredOutputProto(t testing.TB, schema map[string]any) *proto.AgentOutput {
	t.Helper()
	return &proto.AgentOutput{
		Kind: &proto.AgentOutput_Structured{
			Structured: &proto.AgentStructuredOutput{Schema: mustProtoStruct(t, schema)},
		},
	}
}

type routeCountingAgentControl struct {
	defaultName string
	names       []string
	providers   map[string]*routeCountingAgentProvider
}

func (c *routeCountingAgentControl) ResolveProvider(_ context.Context, name string) (string, coreagent.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(c.defaultName)
	}
	if name == "" {
		return "", nil, ErrAgentProviderRequired
	}
	provider := c.providers[name]
	if provider == nil {
		return "", nil, NewAgentProviderNotAvailableError(name)
	}
	return name, provider, nil
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
	createSessionReqs []*proto.CreateAgentProviderSessionRequest
	createTurnReqs    []*proto.CreateAgentProviderTurnRequest
	listSessionReqs   []*proto.ListAgentProviderSessionsRequest
	listTurnReqs      []*proto.ListAgentProviderTurnsRequest
	turnIDOverride    string
	createTurnStatus  coreagent.ExecutionStatus
	createTurnOutput  coreagent.TurnOutput
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

func (p *routeCountingAgentProvider) CreateSession(_ context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.createSessionReqs = append(p.createSessionReqs, cloneAgentRequest(req, &proto.CreateAgentProviderSessionRequest{}))
	session := &coreagent.Session{
		ID:           req.GetSessionId(),
		ProviderName: p.name,
		Model:        req.GetModel(),
		ClientRef:    req.GetClientRef(),
		State:        coreagent.SessionStateActive,
		Metadata:     mapsCloneAny(protoutil.MapFromStruct(req.GetMetadata())),
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId()),
	}
	p.sessions[session.ID] = session
	return cloneRouteSession(session), nil
}

func (p *routeCountingAgentProvider) SupportsWorkspaceRequests() bool {
	return p.supportsWorkspace
}

func (p *routeCountingAgentProvider) GetSession(_ context.Context, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.getSessionCalls++
	if p.getSessionErr != nil {
		return nil, p.getSessionErr
	}
	session := p.sessions[strings.TrimSpace(req.GetSessionId())]
	if session == nil {
		return nil, core.ErrNotFound
	}
	return cloneRouteSession(session), nil
}

func (p *routeCountingAgentProvider) ListSessions(_ context.Context, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	p.listSessionReqs = append(p.listSessionReqs, cloneAgentRequest(req, &proto.ListAgentProviderSessionsRequest{}))
	if p.listSessionsErr != nil {
		return nil, p.listSessionsErr
	}
	state, err := agentSessionStateFromProto(req.GetState())
	if err != nil {
		return nil, err
	}
	var sessions []*coreagent.Session
	requested := map[string]struct{}{}
	for _, id := range req.GetSessionIds() {
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
		if req.GetSubject().GetId() != "" && session.CreatedBySubjectID != req.GetSubject().GetId() {
			continue
		}
		if state != "" && session.State != state {
			continue
		}
		sessions = append(sessions, cloneRouteSession(session))
	}
	if req.GetLimit() > 0 && len(sessions) > int(req.GetLimit()) {
		sessions = sessions[:req.GetLimit()]
	}
	return sessions, nil
}

func (p *routeCountingAgentProvider) UpdateSession(_ context.Context, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	session := p.sessions[strings.TrimSpace(req.GetSessionId())]
	if session == nil {
		return nil, core.ErrNotFound
	}
	if req.GetClientRef() != "" {
		session.ClientRef = req.GetClientRef()
	}
	state, err := agentSessionStateFromProto(req.GetState())
	if err != nil {
		return nil, err
	}
	if state != "" {
		session.State = state
	}
	if req.GetMetadata() != nil {
		session.Metadata = mapsCloneAny(protoutil.MapFromStruct(req.GetMetadata()))
	}
	return cloneRouteSession(session), nil
}

func (p *routeCountingAgentProvider) CreateTurn(_ context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.createTurnReqs = append(p.createTurnReqs, cloneAgentRequest(req, &proto.CreateAgentProviderTurnRequest{}))
	turnID := req.GetTurnId()
	if strings.TrimSpace(p.turnIDOverride) != "" {
		turnID = p.turnIDOverride
	}
	status := p.createTurnStatus
	if status == "" {
		status = coreagent.ExecutionStatusRunning
	}
	turn := &coreagent.Turn{
		ID:           turnID,
		SessionID:    req.GetSessionId(),
		ProviderName: p.name,
		Model:        req.GetModel(),
		Status:       status,
		Messages:     agentwire.MessagesFromProto(req.GetMessages()),
		Output:       p.createTurnOutput,
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId()),
		ExecutionRef: req.GetExecutionRef(),
	}
	p.turns[turn.ID] = turn
	return cloneRouteTurn(turn), nil
}

func (p *routeCountingAgentProvider) GetTurn(_ context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.getTurnCalls++
	if p.getTurnErr != nil {
		return nil, p.getTurnErr
	}
	turn := p.turns[strings.TrimSpace(req.GetTurnId())]
	if turn == nil {
		return nil, core.ErrNotFound
	}
	return cloneRouteTurn(turn), nil
}

func (p *routeCountingAgentProvider) ListTurns(_ context.Context, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	p.listTurnReqs = append(p.listTurnReqs, cloneAgentRequest(req, &proto.ListAgentProviderTurnsRequest{}))
	if p.listTurnsErr != nil {
		return nil, p.listTurnsErr
	}
	statusFilter, err := agentExecutionStatusFromProto(req.GetStatus())
	if err != nil {
		return nil, err
	}
	var turns []*coreagent.Turn
	requested := map[string]struct{}{}
	for _, id := range req.GetTurnIds() {
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
		if req.GetSessionId() != "" && turn.SessionID != req.GetSessionId() {
			continue
		}
		if req.GetSubject().GetId() != "" && turn.CreatedBySubjectID != req.GetSubject().GetId() {
			continue
		}
		if statusFilter != "" && turn.Status != statusFilter {
			continue
		}
		turns = append(turns, cloneRouteTurn(turn))
	}
	if req.GetLimit() > 0 && len(turns) > int(req.GetLimit()) {
		turns = turns[:req.GetLimit()]
	}
	return turns, nil
}

func (p *routeCountingAgentProvider) CancelTurn(_ context.Context, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	turn := p.turns[strings.TrimSpace(req.GetTurnId())]
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

func (p *routeCountingAgentProvider) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
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

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, &proto.CreateAgentProviderSessionRequest{
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
	want := sessionStartConfigToProto(&coreagent.SessionStartConfig{Hooks: []coreagent.SessionStartHook{{
		ID:      "load-memory",
		Type:    "command",
		Command: []string{"bash", "-lc", "printf context"},
		CWD:     "/tmp",
		Timeout: "5s",
		Env:     map[string]string{"MEMORY_ROOT": "/tmp/memory"},
		Output:  coreagent.SessionStartHookOutput{AdditionalContext: true, Metadata: true},
	}}})
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

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, &proto.CreateAgentProviderSessionRequest{
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

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Metadata:     mustProtoStruct(t, map[string]any{"__gestalt.lifecycle.sessionStart.results.setup": "spoofed"}),
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

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Metadata:     mustProtoStruct(t, map[string]any{"workspacePath": "/tmp/spoofed"}),
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

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Workspace: agentWorkspaceToProto(&coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "git@github.com:valon-technologies/app.git",
				Path: "app",
			}},
		}),
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

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Workspace: agentWorkspaceToProto(&coreagent.Workspace{
			CWD: "../app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "git@github.com:valon-technologies/app.git",
				Path: "app",
			}},
		}),
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
	req := &proto.CreateAgentProviderSessionRequest{
		ProviderName:   "alpha",
		IdempotencyKey: "workspace-create-1",
		Model:          "test-model",
		ClientRef:      "client-1",
		Workspace: agentWorkspaceToProto(&coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "git@github.com:valon-technologies/app.git",
				Ref:  "refs/heads/main",
				Path: "app",
			}},
		}),
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
	if provider.createSessionReqs[0].GetSessionId() != provider.createSessionReqs[1].GetSessionId() {
		t.Fatalf("provider session IDs = %q, %q, want stable", provider.createSessionReqs[0].GetSessionId(), provider.createSessionReqs[1].GetSessionId())
	}
	if provider.createSessionReqs[0].Workspace == nil {
		t.Fatal("provider did not receive manager workspace")
	}
}

func TestCreateSessionClearsCallerPreparedWorkspace(t *testing.T) {
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

	_, err := manager.CreateSession(context.Background(), &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}, &proto.CreateAgentProviderSessionRequest{
		ProviderName:      "alpha",
		Model:             "test-model",
		PreparedWorkspace: &proto.PreparedAgentWorkspace{Root: "/tmp/spoofed", Cwd: "/tmp/spoofed"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(provider.createSessionReqs) != 1 {
		t.Fatalf("CreateSession calls = %d, want 1", len(provider.createSessionReqs))
	}
	if got := provider.createSessionReqs[0].GetPreparedWorkspace(); got != nil {
		t.Fatalf("PreparedWorkspace = %#v, want nil", got)
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
	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Metadata: mustProtoStruct(t, map[string]any{
			"caller": "original",
		}),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	provider.sessions[session.ID].Metadata["__gestalt.lifecycle.sessionStart.results.setup"] = map[string]any{"exitCode": 0}

	updated, err := manager.UpdateSession(context.Background(), p, &proto.UpdateAgentProviderSessionRequest{
		SessionId: session.ID,
		Metadata:  mustProtoStruct(t, map[string]any{"caller": "updated"}),
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
		CreatedBySubjectID: principal.UserSubjectID("user-1"),
	}
	alpha.sessions[session.ID] = session
	beta.getSessionErr = status.Error(codes.Unavailable, "provider restarting")

	got, err := manager.GetSession(ctx, p, &proto.GetAgentProviderSessionRequest{SessionId: session.ID})
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

	_, err := manager.GetSession(ctx, p, &proto.GetAgentProviderSessionRequest{SessionId: "session-1"})
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
		CreatedBySubjectID: principal.UserSubjectID("user-1"),
	}
	turn := &coreagent.Turn{
		ID:           "turn-1",
		SessionID:    session.ID,
		ProviderName: "alpha",
		Status:       coreagent.ExecutionStatusSucceeded,
		Output:       coreagent.TurnOutput{Text: &coreagent.TurnTextOutput{Text: "done"}},
		CreatedBySubjectID: principal.UserSubjectID("user-1"),
	}
	alpha.sessions[session.ID] = session
	alpha.turns[turn.ID] = turn
	beta.getTurnErr = status.Error(codes.Unavailable, "provider restarting")

	got, err := manager.GetTurn(ctx, p, &proto.GetAgentProviderTurnRequest{TurnId: turn.ID})
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

	session, err := manager.CreateSession(context.Background(), owner, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := manager.CreateTurn(context.Background(), owner, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}

	if _, err := manager.GetSession(context.Background(), viewer, &proto.GetAgentProviderSessionRequest{SessionId: session.ID}); err != nil {
		t.Fatalf("GetSession as visible non-owner: %v", err)
	}
	if _, err := manager.GetTurn(context.Background(), viewer, &proto.GetAgentProviderTurnRequest{TurnId: turn.ID}); err != nil {
		t.Fatalf("GetTurn as visible non-owner: %v", err)
	}
	if _, err := manager.UpdateSession(context.Background(), viewer, &proto.UpdateAgentProviderSessionRequest{SessionId: session.ID, ClientRef: "viewer-edit"}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("UpdateSession as visible non-owner error = %v, want not found", err)
	}
	if _, err := manager.CreateTurn(context.Background(), viewer, &proto.CreateAgentProviderTurnRequest{SessionId: session.ID, Model: "test-model", Output: agentTextOutputProto()}); !errors.Is(err, ErrAgentSessionNotFound) {
		t.Fatalf("CreateTurn as visible non-owner error = %v, want session not found", err)
	}
	if _, err := manager.CancelTurn(context.Background(), viewer, &proto.CancelAgentProviderTurnRequest{TurnId: turn.ID, Reason: "viewer-cancel"}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("CancelTurn as visible non-owner error = %v, want not found", err)
	}
	if got := alpha.turns[turn.ID].Status; got != coreagent.ExecutionStatusRunning {
		t.Fatalf("turn status after non-owner cancel = %q, want running", got)
	}
	if _, err := manager.ResolveInteraction(context.Background(), viewer, &proto.ResolveAgentProviderInteractionRequest{
		TurnId:        turn.ID,
		InteractionId: "interaction-1",
		Resolution:    mustProtoStruct(t, map[string]any{"value": true}),
	}); !errors.Is(err, core.ErrNotFound) {
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "turn-replay",
		Model:          "test-model",
		Output:         agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn.ID != "provider-turn-1" {
		t.Fatalf("CreateTurn ID = %q, want provider-turn-1", turn.ID)
	}

	alpha.getTurnCalls = 0
	if _, err := manager.GetTurn(context.Background(), p, &proto.GetAgentProviderTurnRequest{TurnId: "provider-turn-1"}); err != nil {
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

	_, err := manager.ListSessions(context.Background(), &principal.Principal{SubjectID: subjectID}, &proto.ListAgentProviderSessionsRequest{
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
	if _, err := manager.CreateSession(ctx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	provider.capabilitiesErr = status.Error(codes.Unavailable, "sandbox is redeploying")

	_, err := manager.ListSessions(ctx, p, &proto.ListAgentProviderSessionsRequest{
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
	if _, err := manager.CreateSession(ctx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	beta.listSessionsErr = status.Error(codes.Unavailable, "sandbox is redeploying")

	_, err := manager.ListSessions(ctx, p, &proto.ListAgentProviderSessionsRequest{
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
		CreatedBySubjectID: subjectID,
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

	_, err := manager.ListTurns(context.Background(), p, &proto.ListAgentProviderTurnsRequest{
		SessionId:   "session-1",
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
	session, err := manager.CreateSession(ctx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := manager.CreateTurn(ctx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Messages:  mustAgentMessages(t, []coreagent.Message{{Role: "user", Text: "hello"}}),
		Output:    agentTextOutputProto(),
	}); err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	provider.capabilitiesErr = status.Error(codes.Unavailable, "sandbox is redeploying")

	_, err = manager.ListTurns(ctx, p, &proto.ListAgentProviderTurnsRequest{
		SessionId:   session.ID,
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Tools:     []*proto.ResolvedAgentTool{{Id: "spoofed-tool", Name: "spoofed_tool"}},
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	if got := alpha.createTurnReqs[0].GetToolSource(); got != proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_UNSPECIFIED {
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
					},
	}

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	req := alpha.createTurnReqs[0]
	if req.GetToolSource() != proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG {
		t.Fatalf("CreateTurn tool source = %q, want mcp_catalog", req.GetToolSource())
	}
	if got := req.GetToolRefs(); len(got) != 1 || got[0].GetApp() != agentToolSearchAllApp || got[0].GetOperation() != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want global broad catalog ref", got)
	}
	if strings.TrimSpace(req.GetRunGrant()) == "" {
		t.Fatal("CreateTurn run grant is empty")
	}
	grant, err := grants.Resolve(req.GetRunGrant())
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Messages:  mustAgentMessages(t, []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}}),
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	req := alpha.createTurnReqs[0]
	if req.GetToolSource() != proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG {
		t.Fatalf("CreateTurn tool source = %q, want mcp_catalog", req.GetToolSource())
	}
	if got := req.GetToolRefs(); len(got) != 1 || got[0].GetApp() != "linear" || got[0].GetOperation() != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want linear provider ref", got)
	}
	grant, err := grants.Resolve(req.GetRunGrant())
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Messages:  mustAgentMessages(t, []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}}),
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].GetToolRefs(); len(got) != 1 || got[0].GetApp() != agentToolSearchAllApp || got[0].GetOperation() != "" {
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Messages:  mustAgentMessages(t, []coreagent.Message{{Role: "user", Text: "show me my tickets"}}),
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if linear.catalogCalls != 0 {
		t.Fatalf("linear catalog calls = %d, want no enumeration without a provider mention", linear.catalogCalls)
	}
	if got := alpha.createTurnReqs[0].GetToolRefs(); len(got) != 1 || got[0].GetApp() != agentToolSearchAllApp || got[0].GetOperation() != "" {
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Messages:  mustAgentMessages(t, []coreagent.Message{{Role: "user", Text: "open a doc"}}),
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if docs.catalogCalls != 0 {
		t.Fatalf("docs catalog calls = %d, want no enumeration for non-exact provider mention", docs.catalogCalls)
	}
	if got := alpha.createTurnReqs[0].GetToolRefs(); len(got) != 1 || got[0].GetApp() != agentToolSearchAllApp || got[0].GetOperation() != "" {
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	ctx := WithCallerAppName(context.Background(), "slack")
	_, err = manager.CreateTurn(ctx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Messages:  mustAgentMessages(t, []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}}),
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].GetToolRefs(); len(got) != 1 || got[0].GetApp() != agentToolSearchAllApp || got[0].GetOperation() != "" {
		t.Fatalf("CreateTurn tool refs = %#v, want broad wildcard for caller app default", got)
	}
	grant, err := grants.Resolve(alpha.createTurnReqs[0].GetRunGrant())
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Messages: mustAgentMessages(t, []coreagent.Message{
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
		}),
		Output: agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].GetToolRefs(); len(got) != 1 || got[0].GetApp() != "github" || got[0].GetOperation() != "" {
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Messages:  mustAgentMessages(t, []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}}),
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].GetToolRefs(); len(got) != 1 || got[0].GetApp() != agentToolSearchAllApp || got[0].GetOperation() != "" {
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
				ConnMode: core.ConnectionModeSubject,
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Messages:  mustAgentMessages(t, []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}}),
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].GetToolRefs(); len(got) != 1 || got[0].GetApp() != agentToolSearchAllApp || got[0].GetOperation() != "" {
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Messages:  mustAgentMessages(t, []coreagent.Message{{Role: "user", Text: "show me linear"}}),
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if got := alpha.createTurnReqs[0].GetToolRefs(); len(got) != 1 || got[0].GetApp() != agentToolSearchAllApp || got[0].GetOperation() != "" {
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId:  session.ID,
		Model:      "test-model",
		ToolSource: proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG,
		Output:     agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	req := alpha.createTurnReqs[0]
	if req.GetToolSource() != proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG {
		t.Fatalf("CreateTurn tool source = %q, want mcp_catalog", req.GetToolSource())
	}
	if got := req.GetToolRefs(); len(got) != 0 {
		t.Fatalf("CreateTurn tool refs = %#v, want none for explicit empty catalog source", got)
	}
	if strings.TrimSpace(req.GetRunGrant()) == "" {
		t.Fatal("CreateTurn run grant is empty")
	}
}

func TestManagerCreateTurnHonorsNoneToolSource(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	alpha.capabilities = &coreagent.ProviderCapabilities{
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId:  session.ID,
		Model:      "test-model",
		ToolSource: proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NONE,
		Output:     agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	req := alpha.createTurnReqs[0]
	if req.GetToolSource() != proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NONE {
		t.Fatalf("CreateTurn tool source = %q, want none", req.GetToolSource())
	}
	if len(req.GetToolRefs()) != 0 || len(req.GetTools()) != 0 {
		t.Fatalf("CreateTurn tools = refs:%#v resolved:%#v, want none", req.GetToolRefs(), req.GetTools())
	}
	grant, err := grants.Resolve(req.GetRunGrant())
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
		req         *proto.CreateAgentProviderTurnRequest
		wantErr     error
		wantCreated bool
	}{
		{
			name: "valid schema forwards presence",
			caps: &coreagent.ProviderCapabilities{
				SupportedToolSources: []coreagent.ToolSourceMode{
					coreagent.ToolSourceModeNone,
				},
			},
			req: &proto.CreateAgentProviderTurnRequest{
				ToolSource: proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NONE,
				Output:     agentStructuredOutputProto(t, map[string]any{"type": "object"}),
			},
			wantCreated: true,
		},
		{
			name: "empty schema is invalid when present",
			caps: &coreagent.ProviderCapabilities{
				SupportedToolSources: []coreagent.ToolSourceMode{
					coreagent.ToolSourceModeNone,
				},
			},
			req: &proto.CreateAgentProviderTurnRequest{
				ToolSource: proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NONE,
				Output:     agentStructuredOutputProto(t, map[string]any{}),
			},
			wantErr: invocation.ErrInvalidInvocation,
		},
		{
			name: "none source rejects tool refs",
			caps: &coreagent.ProviderCapabilities{
				SupportedToolSources: []coreagent.ToolSourceMode{
					coreagent.ToolSourceModeNone,
				},
			},
			req: &proto.CreateAgentProviderTurnRequest{
				ToolSource: proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NONE,
				ToolRefs:   []*proto.AgentToolRef{{App: "docs"}},
				Output:     agentTextOutputProto(),
			},
			wantErr: invocation.ErrInvalidInvocation,
		},
		{
			name: "text output is valid",
			caps: &coreagent.ProviderCapabilities{
				SupportedToolSources: []coreagent.ToolSourceMode{
					coreagent.ToolSourceModeNone,
				},
			},
			req: &proto.CreateAgentProviderTurnRequest{
				ToolSource: proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NONE,
				Output:     agentTextOutputProto(),
			},
			wantCreated: true,
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
			session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
				ProviderName: "alpha",
				Model:        "test-model",
			})
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			req := tt.req
			req.SessionId = session.ID
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
			if tt.req.GetOutput().GetStructured() != nil && got.GetOutput().GetStructured().GetSchema().AsMap()["type"] != "object" {
				t.Fatalf("CreateTurn response schema = %#v, want object schema", got.GetOutput().GetStructured().GetSchema())
			}
			if tt.req.GetOutput().GetText() != nil && got.GetOutput().GetText() == nil {
				t.Fatal("CreateTurn output.text = nil, want text output request")
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId:   session.ID,
		Model:       "test-model",
		ToolRefsSet: true,
		Output:      agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if len(alpha.createTurnReqs) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(alpha.createTurnReqs))
	}
	req := alpha.createTurnReqs[0]
	if req.GetToolSource() != proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_UNSPECIFIED {
		t.Fatalf("CreateTurn tool source = %q, want empty", req.GetToolSource())
	}
	if got := req.GetToolRefs(); len(got) != 0 {
		t.Fatalf("CreateTurn tool refs = %#v, want none for explicit empty tool refs", got)
	}
	if strings.TrimSpace(req.GetRunGrant()) == "" {
		t.Fatal("CreateTurn run grant is empty")
	}
	grant, err := grants.Resolve(req.GetRunGrant())
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

func TestManagerRejectsAmbiguousSuccessfulTurnOutput(t *testing.T) {
	t.Parallel()

	alpha := newRouteCountingAgentProvider("alpha")
	alpha.capabilities = &coreagent.ProviderCapabilities{
		SupportedToolSources: []coreagent.ToolSourceMode{
			coreagent.ToolSourceModeNone,
		},
	}
	alpha.createTurnStatus = coreagent.ExecutionStatusSucceeded
	alpha.createTurnOutput = coreagent.TurnOutput{
		Text:       &coreagent.TurnTextOutput{Text: "plain"},
		Structured: &coreagent.TurnStructuredOutput{Text: "plain", Value: map[string]any{"summary": "plain"}},
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, err = manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId:  session.ID,
		Model:      "test-model",
		ToolSource: proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NONE,
		Output:     agentTextOutputProto(),
	})
	if err == nil {
		t.Fatal("CreateTurn error = nil, want ambiguous successful output error")
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "test-model",
		Output:    agentTextOutputProto(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	grant, err := grants.Mint(agentgrant.Grant{
		ProviderName: "alpha",
		SessionID:    session.ID,
		TurnID:       turn.ID,
		SubjectID: principal.UserSubjectID("user-1"),
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if _, err := grants.Resolve(grant); err != nil {
		t.Fatalf("Resolve before cancel: %v", err)
	}

	if _, err := manager.CancelTurn(context.Background(), p, &proto.CancelAgentProviderTurnRequest{TurnId: turn.ID, Reason: "done"}); err != nil {
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

	session, err := manager.CreateSession(context.Background(), p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := manager.CreateTurn(context.Background(), p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "provider-owned-turn",
		Model:          "test-model",
		Output:         agentTextOutputProto(),
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
		SubjectID: principal.UserSubjectID("user-1"),
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if _, err := grants.Resolve(grant); err != nil {
		t.Fatalf("Resolve before cancel: %v", err)
	}

	if _, err := manager.CancelTurn(context.Background(), p, &proto.CancelAgentProviderTurnRequest{TurnId: turn.ID, Reason: "done"}); err != nil {
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

func TestResolveToolsUsesExplicitProviderCredentialMode(t *testing.T) {
	t.Parallel()

	hidden := false
	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:      "events.reply",
				Title:   "Reply",
				Visible: &hidden,
			}}},
		},
	}
	manager := newTestManager(t, Config{Providers: testutil.NewProviderRegistry(t, provider)})

	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		ToolRefs: []coreagent.ToolRef{{
			App:            "slack",
			Operation:      "events.reply",
			CredentialMode: core.ConnectionModeNone,
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

func TestNormalizeToolRefsRejectsProviderRunAsDelegation(t *testing.T) {
	t.Parallel()

	runAs := &core.RunAsSubject{
		SubjectID:           "service_account:automation",
				CredentialSubjectID: "service_account:automation",
	}
	for _, tc := range []struct {
		name string
		ref  coreagent.ToolRef
	}{
		{
			name: "runAs",
			ref: coreagent.ToolRef{
				App:       "target",
				Operation: "automation.write",
				RunAs:     runAs,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalizeToolRefs([]coreagent.ToolRef{tc.ref})
			if !errors.Is(err, invocation.ErrAuthorizationDenied) {
				t.Fatalf("normalizeToolRefs error = %v, want authorization denied", err)
			}
		})
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

func TestAgentToolTargetKeyUsesRunAsIdentity(t *testing.T) {
	t.Parallel()

	base := coreagent.ToolRef{
		App:       "target",
		Operation: "automation.write",
		RunAs: &core.RunAsSubject{
			SubjectID:           "service_account:automation",
			CredentialSubjectID: "service_account:automation",
		},
	}
	same := base
	same.RunAs = &core.RunAsSubject{
		SubjectID:           " service_account:automation ",
		CredentialSubjectID: " service_account:automation ",
	}
	differentCredentialSubject := base
	differentCredentialSubject.RunAs = &core.RunAsSubject{
		SubjectID:           base.RunAs.SubjectID,
		CredentialSubjectID: "service_account:other-automation",
	}
	if agentToolTargetKeyFromRef(base) != agentToolTargetKeyFromRef(same) {
		t.Fatal("agentToolTargetKeyFromRef should normalize equivalent runAs subjects")
	}
	if agentToolTargetKeyFromRef(base) == agentToolTargetKeyFromRef(differentCredentialSubject) {
		t.Fatal("agentToolTargetKeyFromRef collapsed distinct runAs credential subject")
	}
}

package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

func agentRuntimeTextOutput() *proto.AgentOutput {
	return &proto.AgentOutput{Kind: &proto.AgentOutput_Text{Text: &proto.AgentTextOutput{}}}
}

func buildAgentProviderBinary(t *testing.T) string {
	t.Helper()
	if sharedAgentProviderBin == "" {
		t.Fatal("shared agent provider binary not initialized")
	}
	return sharedAgentProviderBin
}

type agentRuntimeFactoryContextKey struct{}

func testHostedAgentRuntimeConfig() *config.RuntimePlacementConfig {
	return &config.RuntimePlacementConfig{
		Pool: &config.RuntimePlacementPoolConfig{
			MinReadyInstances:   1,
			MaxReadyInstances:   1,
			StartupTimeout:      "5s",
			HealthCheckInterval: "1s",
			RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
			DrainTimeout:        "1s",
		},
	}
}

func testAgentRuntimeIndexedDBDefs() map[string]*config.ProviderEntry {
	return map[string]*config.ProviderEntry{
		"agent_state": {
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
		},
	}
}

type agentRuntimeInvokerCall struct {
	providerName           string
	operation              string
	params                 map[string]any
	subjectID              string
	credentialModeOverride core.ConnectionMode
	idempotencyKey         string
	agentSubjectID         string
	runAsSubjectID         string
	toolRefsSet            bool
	toolRefs               []coreagent.ToolRef
}

type recordingAgentRuntimeInvoker struct {
	mu    sync.Mutex
	calls []agentRuntimeInvokerCall
}

func (i *recordingAgentRuntimeInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	i.mu.Lock()
	runAsAudit := invocation.RunAsAuditFromContext(ctx)
	agentSubjectID := ""
	if runAsAudit.AgentSubject != nil {
		agentSubjectID = runAsAudit.AgentSubject.SubjectID
	}
	runAsSubjectID := ""
	if runAsAudit.RunAsSubject != nil {
		runAsSubjectID = runAsAudit.RunAsSubject.SubjectID
	}
	toolRefs := invocation.ToolRefsContextFromContext(ctx)
	i.calls = append(i.calls, agentRuntimeInvokerCall{
		providerName:           providerName,
		operation:              operation,
		params:                 cloneAnyMap(params),
		subjectID:              p.SubjectID,
		credentialModeOverride: invocation.CredentialModeOverrideFromContext(ctx),
		idempotencyKey:         invocation.IdempotencyKeyFromContext(ctx),
		agentSubjectID:         agentSubjectID,
		runAsSubjectID:         runAsSubjectID,
		toolRefsSet:            toolRefs.Set,
		toolRefs:               toolRefs.Refs,
	})
	i.mu.Unlock()

	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &core.OperationResult{Status: http.StatusAccepted, Body: string(body)}, nil
}

func (i *recordingAgentRuntimeInvoker) Calls() []agentRuntimeInvokerCall {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]agentRuntimeInvokerCall, len(i.calls))
	copy(out, i.calls)
	return out
}

type staticAgentToolResolver struct {
	tools []coreagent.Tool
}

func (r staticAgentToolResolver) ListTools(context.Context, *principal.Principal, coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error) {
	return &coreagent.ListToolsResponse{}, nil
}

func (r staticAgentToolResolver) ResolveTool(_ context.Context, _ *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error) {
	target := coreagent.ToolTarget{
		System:         strings.TrimSpace(ref.System),
		App:            strings.TrimSpace(ref.App),
		Operation:      strings.TrimSpace(ref.Operation),
		Connection:     strings.TrimSpace(ref.Connection),
		Instance:       strings.TrimSpace(ref.Instance),
		CredentialMode: ref.CredentialMode,
		RunAs:          core.NormalizeRunAsSubject(ref.RunAs),
	}
	for i := range r.tools {
		if agentToolTargetsEqual(r.tools[i].Target, target) {
			return r.tools[i], nil
		}
	}
	return coreagent.Tool{}, core.ErrNotFound
}

type reconnectingAgentRuntimeInvoker struct {
	recordingAgentRuntimeInvoker
	tokenErrs map[string]error
}

func (i *reconnectingAgentRuntimeInvoker) ResolveToken(ctx context.Context, _ *principal.Principal, providerName, _, _ string) (context.Context, string, error) {
	if err := i.tokenErrs[strings.TrimSpace(providerName)]; err != nil {
		return ctx, "", err
	}
	return ctx, "token", nil
}

type failingSessionCatalogIntegration struct {
	coretesting.StubIntegration
}

func (p *failingSessionCatalogIntegration) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	return p.CatalogVal, nil
}

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func testAgentWorkspaceToProto(workspace *coreagent.Workspace) *proto.AgentWorkspace {
	if workspace == nil {
		return nil
	}
	out := &proto.AgentWorkspace{
		Checkouts: make([]*proto.AgentWorkspaceGitCheckout, 0, len(workspace.Checkouts)),
		Cwd:       workspace.CWD,
	}
	for _, checkout := range workspace.Checkouts {
		out.Checkouts = append(out.Checkouts, &proto.AgentWorkspaceGitCheckout{
			Url:  checkout.URL,
			Ref:  checkout.Ref,
			Path: checkout.Path,
		})
	}
	return out
}

func testAgentSessionStateFromProto(state proto.AgentSessionState) coreagent.SessionState {
	switch state {
	case proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE:
		return coreagent.SessionStateActive
	case proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED:
		return coreagent.SessionStateArchived
	default:
		return ""
	}
}

func mustTestProtoStruct(t *testing.T, fields map[string]any) *structpb.Struct {
	t.Helper()
	out, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatalf("structpb.NewStruct() error = %v", err)
	}
	return out
}

type pingAgentProvider struct {
	coreagent.UnimplementedProvider
	calls *int
	err   error
	delay time.Duration
}

func (p *pingAgentProvider) Ping(ctx context.Context) error {
	if p.calls != nil {
		(*p.calls)++
	}
	if p.delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.delay):
		}
	}
	return p.err
}

type turnLookupAgentProvider struct {
	coreagent.UnimplementedProvider
	turn *coreagent.Turn
}

func (p *turnLookupAgentProvider) GetTurn(context.Context, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if p.turn == nil {
		return nil, core.ErrNotFound
	}
	return p.turn, nil
}

func TestAgentRuntimeResolveConnectionUsesAgentConnectionRuntime(t *testing.T) {
	t.Parallel()

	runtime, err := newAgentRuntime(&config.Config{}, nil)
	if err != nil {
		t.Fatalf("newAgentRuntime() error = %v", err)
	}
	grants, err := agentgrant.NewManager(nil)
	if err != nil {
		t.Fatalf("agentgrant.NewManager() error = %v", err)
	}
	runGrant, err := grants.Mint(agentgrant.Grant{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "system:agent-runtime-test",
		Connections: []agentgrant.ConnectionBinding{
			{Connection: "vertex"},
		},
	})
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}
	expiresAt := time.Now().Add(time.Hour).UTC()
	connectionRuntime := invocation.ConnectionRuntimeMap{
		"simple": {
			"vertex": {
				ConnectionID: "vertex-kimi-k2-6",
				Mode:         core.ConnectionModeSubject,
				AuthConfig:   core.ExternalCredentialAuthConfig{Type: "bearer", Token: "vertex-token"},
				Params:       map[string]string{"endpoint": "https://vertex.example.invalid"},
			},
		},
	}
	externalCredentials := coretesting.NewStubExternalCredentialProvider()
	externalCredentials.ResolveCredentialFunc = func(_ context.Context, req *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
		return &core.ResolveExternalCredentialResponse{
			Credential: &core.ExternalCredential{
				SubjectID:    req.CredentialSubjectID,
				ConnectionID: req.ConnectionID,
				Connection:   req.Connection,
				AccessToken:  req.Auth.Token,
			},
			Token:     req.Auth.Token,
			ExpiresAt: &expiresAt,
		}, nil
	}
	runtime.SetRunGrants(grants)
	runtime.SetInvoker(invocation.NewBroker(nil, nil, externalCredentials, invocation.WithConnectionRuntime(connectionRuntime.Resolve)))
	runtime.PublishProvider("simple", &turnLookupAgentProvider{turn: &coreagent.Turn{
		ID:        "turn-1",
		SessionID: "session-1",
		Status:    coreagent.ExecutionStatusRunning,
	}})

	got, err := runtime.ResolveConnection(context.Background(), coreagent.ResolveConnectionRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		Connection:   "vertex",
		RunGrant:     runGrant,
	})
	if err != nil {
		t.Fatalf("ResolveConnection() error = %v", err)
	}
	if got.ConnectionID != "vertex-kimi-k2-6" {
		t.Fatalf("ConnectionID = %q, want vertex-kimi-k2-6", got.ConnectionID)
	}
	if got.Connection != "vertex" {
		t.Fatalf("Connection = %q, want vertex", got.Connection)
	}
	if got.Mode != core.ConnectionModeSubject {
		t.Fatalf("Mode = %q, want %q", got.Mode, core.ConnectionModeSubject)
	}
	if got.Headers["Authorization"] != core.BearerScheme+"vertex-token" {
		t.Fatalf("Authorization header = %q, want bearer token", got.Headers["Authorization"])
	}
	if got.Params["endpoint"] != "https://vertex.example.invalid" {
		t.Fatalf("endpoint param = %q, want default endpoint", got.Params["endpoint"])
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, expiresAt)
	}
}

type listSessionsAgentProvider struct {
	coreagent.UnimplementedProvider
	sessions []*coreagent.Session
	err      error
}

func (p *listSessionsAgentProvider) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	if p.err != nil {
		return nil, p.err
	}
	return append([]*coreagent.Session(nil), p.sessions...), nil
}

type routingAgentProvider struct {
	coreagent.UnimplementedProvider
	createTurn func(context.Context, *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error)
	getTurn    func(context.Context, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error)
}

func (p *routingAgentProvider) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if p.createTurn == nil {
		return nil, core.ErrNotFound
	}
	return p.createTurn(ctx, req)
}

func (p *routingAgentProvider) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if p.getTurn == nil {
		return nil, core.ErrNotFound
	}
	return p.getTurn(ctx, req)
}

type workspaceAgentProvider struct {
	coreagent.UnimplementedProvider
	supportPreparedWorkspace bool
	createErr                error
	createSessionID          string
	createReqs               []*proto.CreateAgentProviderSessionRequest
	updateReqs               []*proto.UpdateAgentProviderSessionRequest
	sessions                 map[string]*coreagent.Session
}

func (p *workspaceAgentProvider) CreateSession(_ context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.createReqs = append(p.createReqs, req)
	if p.createErr != nil {
		return nil, p.createErr
	}
	sessionID := strings.TrimSpace(p.createSessionID)
	if sessionID == "" {
		sessionID = req.GetSessionId()
	}
	session := &coreagent.Session{
		ID:                 sessionID,
		ProviderName:       "simple",
		Model:              req.GetModel(),
		State:              coreagent.SessionStateActive,
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId()),
	}
	if p.sessions == nil {
		p.sessions = map[string]*coreagent.Session{}
	}
	p.sessions[session.ID] = session
	cloned := *session
	return &cloned, nil
}

func (p *workspaceAgentProvider) GetSession(_ context.Context, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	session := p.sessions[strings.TrimSpace(req.GetSessionId())]
	if session == nil {
		return nil, core.ErrNotFound
	}
	cloned := *session
	return &cloned, nil
}

func (p *workspaceAgentProvider) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{SupportsPreparedWorkspace: p.supportPreparedWorkspace}, nil
}

func (p *workspaceAgentProvider) UpdateSession(_ context.Context, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.updateReqs = append(p.updateReqs, req)
	state := testAgentSessionStateFromProto(req.GetState())
	if p.sessions != nil && state == coreagent.SessionStateArchived {
		delete(p.sessions, strings.TrimSpace(req.GetSessionId()))
	}
	return &coreagent.Session{
		ID:                 req.GetSessionId(),
		ProviderName:       "simple",
		State:              state,
		CreatedBySubjectID: "user:user-1",
	}, nil
}

func (p *workspaceAgentProvider) Ping(context.Context) error {
	return nil
}

type workspaceRuntimeProvider struct {
	*runtimeprovider.LocalProvider
	supportPrepareWorkspace bool
	prepareWorkspace        func(context.Context, *proto.PrepareRuntimeWorkspaceRequest) (*proto.PrepareRuntimeWorkspaceResponse, error)
	removeWorkspaceReqs     []*proto.RemoveRuntimeWorkspaceRequest
}

func (p *workspaceRuntimeProvider) Support(ctx context.Context) (*proto.RuntimeSupport, error) {
	support, err := p.LocalProvider.Support(ctx)
	if err != nil {
		return support, err
	}
	support.SupportsPrepareWorkspace = p.supportPrepareWorkspace
	return support, nil
}

func (p *workspaceRuntimeProvider) PrepareWorkspace(ctx context.Context, req *proto.PrepareRuntimeWorkspaceRequest) (*proto.PrepareRuntimeWorkspaceResponse, error) {
	if p.prepareWorkspace != nil {
		return p.prepareWorkspace(ctx, req)
	}
	return p.LocalProvider.PrepareWorkspace(ctx, req)
}

func (p *workspaceRuntimeProvider) RemoveWorkspace(ctx context.Context, req *proto.RemoveRuntimeWorkspaceRequest) error {
	p.removeWorkspaceReqs = append(p.removeWorkspaceReqs, req)
	return p.LocalProvider.RemoveWorkspace(ctx, req)
}

func TestHostedAgentPoolPreparesWorkspaceBeforeProviderCreate(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := createAgentRuntimeWorkspaceRepo(t)
	runtimeProvider, runtimeSession := startWorkspaceRuntimeSession(t, ctx, true)
	agentProvider := &workspaceAgentProvider{supportPreparedWorkspace: true}
	pool := hostedWorkspacePoolForTest(t, agentProvider, runtimeProvider, runtimeSession, "file://"+filepath.ToSlash(repo))
	t.Cleanup(func() { _ = pool.Close() })

	session, err := pool.CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		SessionId:          "agent-session-1",
		Model:              "gpt-test",
		CreatedBySubjectId: "user:user-1",
		Workspace: testAgentWorkspaceToProto(&coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		}),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session == nil || session.ID != "agent-session-1" {
		t.Fatalf("session = %#v", session)
	}
	if len(agentProvider.createReqs) != 1 {
		t.Fatalf("create requests len = %d, want 1", len(agentProvider.createReqs))
	}
	providerReq := agentProvider.createReqs[0]
	if providerReq.Workspace != nil {
		t.Fatalf("provider received raw workspace: %#v", providerReq.Workspace)
	}
	if providerReq.PreparedWorkspace == nil {
		t.Fatal("provider did not receive prepared workspace")
	}
	if !filepath.IsAbs(providerReq.PreparedWorkspace.Root) || !filepath.IsAbs(providerReq.PreparedWorkspace.Cwd) {
		t.Fatalf("prepared workspace = %#v, want absolute paths", providerReq.PreparedWorkspace)
	}
	data, err := os.ReadFile(filepath.Join(providerReq.PreparedWorkspace.Cwd, "README.md"))
	if err != nil {
		t.Fatalf("read prepared checkout: %v", err)
	}
	if strings.TrimSpace(string(data)) != "workspace fixture" {
		t.Fatalf("README = %q", data)
	}
	if got := pool.sessionBackend("agent-session-1"); got == nil || got.runtimeSessionID != runtimeSession.GetId() {
		t.Fatalf("session backend = %#v, want runtime session %q", got, runtimeSession.GetId())
	}
}

func TestHostedAgentPoolRejectsWorkspaceWithoutProviderCapability(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := createAgentRuntimeWorkspaceRepo(t)
	runtimeProvider, runtimeSession := startWorkspaceRuntimeSession(t, ctx, true)
	agentProvider := &workspaceAgentProvider{}
	pool := hostedWorkspacePoolForTest(t, agentProvider, runtimeProvider, runtimeSession, "file://"+filepath.ToSlash(repo))
	t.Cleanup(func() { _ = pool.Close() })

	_, err := pool.CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		SessionId: "agent-session-1",
		Workspace: testAgentWorkspaceToProto(&coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		}),
	})
	if !errors.Is(err, agentmanager.ErrAgentWorkspaceUnsupported) {
		t.Fatalf("CreateSession error = %v, want ErrAgentWorkspaceUnsupported", err)
	}
	if len(agentProvider.createReqs) != 0 {
		t.Fatalf("provider create requests len = %d, want 0", len(agentProvider.createReqs))
	}
}

func TestHostedAgentPoolCleansNonIdempotentPreparedWorkspaceAfterCreateFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := createAgentRuntimeWorkspaceRepo(t)
	runtimeProvider, runtimeSession := startWorkspaceRuntimeSession(t, ctx, true)
	agentProvider := &workspaceAgentProvider{
		supportPreparedWorkspace: true,
		createErr:                errors.New("provider create failed"),
	}
	pool := hostedWorkspacePoolForTest(t, agentProvider, runtimeProvider, runtimeSession, "file://"+filepath.ToSlash(repo))
	t.Cleanup(func() { _ = pool.Close() })

	_, err := pool.CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		SessionId: "agent-session-1",
		Workspace: testAgentWorkspaceToProto(&coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		}),
	})
	if err == nil || !strings.Contains(err.Error(), "provider create failed") {
		t.Fatalf("CreateSession error = %v, want provider create failed", err)
	}
	prepared := agentProvider.createReqs[0].PreparedWorkspace
	if prepared == nil {
		t.Fatal("provider did not receive prepared workspace before failure")
		return
	}
	if _, statErr := os.Stat(prepared.Root); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("workspace root stat error = %v, want not exist", statErr)
	}
	if got := pool.sessionBackend("agent-session-1"); got != nil {
		t.Fatalf("session backend after cleanup = %#v, want nil", got)
	}
}

func TestHostedAgentPoolReturnsExistingIdempotentWorkspaceSessionWithoutReprepare(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := createAgentRuntimeWorkspaceRepo(t)
	runtimeProvider, runtimeSession := startWorkspaceRuntimeSession(t, ctx, true)
	agentProvider := &workspaceAgentProvider{supportPreparedWorkspace: true}
	pool := hostedWorkspacePoolForTest(t, agentProvider, runtimeProvider, runtimeSession, "file://"+filepath.ToSlash(repo))
	t.Cleanup(func() { _ = pool.Close() })

	first, err := pool.CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		SessionId:          "agent-session-1",
		IdempotencyKey:     "workspace-create-1",
		CreatedBySubjectId: "user:user-1",
		Workspace: testAgentWorkspaceToProto(&coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		}),
	})
	if err != nil {
		t.Fatalf("CreateSession first: %v", err)
	}
	prepared := agentProvider.createReqs[0].PreparedWorkspace
	if prepared == nil {
		t.Fatal("provider did not receive prepared workspace")
		return
	}
	runtimeProvider.prepareWorkspace = func(context.Context, *proto.PrepareRuntimeWorkspaceRequest) (*proto.PrepareRuntimeWorkspaceResponse, error) {
		return nil, errors.New("prepare should not run for idempotent replay")
	}
	second, err := pool.CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		SessionId:          "agent-session-1",
		IdempotencyKey:     "workspace-create-1",
		CreatedBySubjectId: "user:user-1",
		Workspace: testAgentWorkspaceToProto(&coreagent.Workspace{
			CWD: "other",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "other",
			}},
		}),
	})
	if err != nil {
		t.Fatalf("CreateSession replay: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("replayed session ID = %q, want %q", second.ID, first.ID)
	}
	if len(agentProvider.createReqs) != 1 {
		t.Fatalf("provider create requests len = %d, want 1", len(agentProvider.createReqs))
	}
	if len(runtimeProvider.removeWorkspaceReqs) != 0 {
		t.Fatalf("RemoveWorkspace calls = %d, want 0", len(runtimeProvider.removeWorkspaceReqs))
	}
	if _, err := os.Stat(prepared.Root); err != nil {
		t.Fatalf("workspace root after replay stat: %v", err)
	}
}

func TestHostedAgentPoolCleansPreparedWorkspaceWhenProviderReturnsWrongSessionID(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := createAgentRuntimeWorkspaceRepo(t)
	runtimeProvider, runtimeSession := startWorkspaceRuntimeSession(t, ctx, true)
	agentProvider := &workspaceAgentProvider{
		supportPreparedWorkspace: true,
		createSessionID:          "existing-session",
	}
	pool := hostedWorkspacePoolForTest(t, agentProvider, runtimeProvider, runtimeSession, "file://"+filepath.ToSlash(repo))
	t.Cleanup(func() { _ = pool.Close() })

	_, err := pool.CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		SessionId:          "agent-session-1",
		IdempotencyKey:     "workspace-create-1",
		CreatedBySubjectId: "user:user-1",
		Workspace: testAgentWorkspaceToProto(&coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		}),
	})
	if err == nil || !strings.Contains(err.Error(), "agent provider returned session id") {
		t.Fatalf("CreateSession error = %v, want session id mismatch", err)
	}
	prepared := agentProvider.createReqs[0].PreparedWorkspace
	if prepared == nil {
		t.Fatal("provider did not receive prepared workspace before mismatch")
		return
	}
	if _, statErr := os.Stat(prepared.Root); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("workspace root stat error = %v, want not exist", statErr)
	}
	if got := pool.sessionBackend("agent-session-1"); got != nil {
		t.Fatalf("session backend after mismatch = %#v, want nil", got)
	}
	if got := pool.sessionBackend("existing-session"); got != nil {
		t.Fatalf("provider returned session backend = %#v, want nil", got)
	}
	if got := len(agentProvider.updateReqs); got != 1 {
		t.Fatalf("provider update requests len = %d, want 1", got)
	}
	if got := agentProvider.updateReqs[0].GetSessionId(); got != "existing-session" {
		t.Fatalf("provider cleanup session id = %q, want existing-session", got)
	}
	if _, ok := agentProvider.sessions["existing-session"]; ok {
		t.Fatal("provider session still exists after mismatch cleanup")
	}
}

func TestHostedAgentPoolCleansPreparedWorkspaceAfterValidationFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := createAgentRuntimeWorkspaceRepo(t)
	runtimeProvider, runtimeSession := startWorkspaceRuntimeSession(t, ctx, true)
	runtimeProvider.prepareWorkspace = func(context.Context, *proto.PrepareRuntimeWorkspaceRequest) (*proto.PrepareRuntimeWorkspaceResponse, error) {
		return &proto.PrepareRuntimeWorkspaceResponse{Workspace: &proto.PreparedAgentWorkspace{Root: "/tmp/gestalt-workspace-root", Cwd: "/tmp/outside-workspace"}}, nil
	}
	agentProvider := &workspaceAgentProvider{supportPreparedWorkspace: true}
	pool := hostedWorkspacePoolForTest(t, agentProvider, runtimeProvider, runtimeSession, "file://"+filepath.ToSlash(repo))
	t.Cleanup(func() { _ = pool.Close() })

	_, err := pool.CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		SessionId: "agent-session-1",
		Workspace: testAgentWorkspaceToProto(&coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		}),
	})
	if err == nil || !strings.Contains(err.Error(), "prepared workspace cwd must be inside root") {
		t.Fatalf("CreateSession error = %v, want invalid prepared workspace", err)
	}
	if len(runtimeProvider.removeWorkspaceReqs) != 1 {
		t.Fatalf("RemoveWorkspace calls = %d, want 1", len(runtimeProvider.removeWorkspaceReqs))
	}
	if got := pool.sessionBackend("agent-session-1"); got != nil {
		t.Fatalf("session backend after prepare failure = %#v, want nil", got)
	}
}

func TestHostedAgentPoolCleansPreparedWorkspaceWhenSessionArchived(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := createAgentRuntimeWorkspaceRepo(t)
	runtimeProvider, runtimeSession := startWorkspaceRuntimeSession(t, ctx, true)
	agentProvider := &workspaceAgentProvider{supportPreparedWorkspace: true}
	pool := hostedWorkspacePoolForTest(t, agentProvider, runtimeProvider, runtimeSession, "file://"+filepath.ToSlash(repo))
	t.Cleanup(func() { _ = pool.Close() })

	_, err := pool.CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		SessionId:          "agent-session-1",
		CreatedBySubjectId: "user:user-1",
		Workspace: testAgentWorkspaceToProto(&coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		}),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	prepared := agentProvider.createReqs[0].PreparedWorkspace
	if prepared == nil {
		t.Fatal("provider did not receive prepared workspace")
		return
	}
	if _, err := os.Stat(prepared.Root); err != nil {
		t.Fatalf("workspace root before archive stat: %v", err)
	}
	_, err = pool.UpdateSession(ctx, &proto.UpdateAgentProviderSessionRequest{
		SessionId: "agent-session-1",
		State:     proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED,
	})
	if err != nil {
		t.Fatalf("UpdateSession archive: %v", err)
	}
	if _, statErr := os.Stat(prepared.Root); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("workspace root after archive stat error = %v, want not exist", statErr)
	}
	if got := pool.sessionBackend("agent-session-1"); got != nil {
		t.Fatalf("session backend after archive = %#v, want nil", got)
	}
}

func hostedWorkspacePoolForTest(t *testing.T, agentProvider coreagent.Provider, runtimeProvider runtimeprovider.Provider, runtimeSession *proto.RuntimeSession, allowedRepos ...string) *hostedAgentProviderPool {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	return &hostedAgentProviderPool{
		name: "simple",
		launch: &hostedAgentProviderLaunch{
			name: "simple",
			runtimeConfig: config.EffectiveRuntimePlacement{
				Workspace: &config.RuntimePlacementWorkspaceConfig{
					PrepareTimeout: "10s",
					Git: &config.RuntimePlacementWorkspaceGitConfig{
						AllowedRepositories: allowedRepos,
					},
				},
			},
		},
		policy:              config.RuntimePlacementLifecyclePolicy{MaxReadyInstances: 1},
		ctx:                 ctx,
		cancel:              cancel,
		backends:            []*hostedAgentPoolBackend{{id: 1, provider: agentProvider, runtimeProvider: runtimeProvider, runtimeSessionID: runtimeSession.GetId(), runtimeSession: runtimeSession, liveTurns: map[string]struct{}{}}},
		sessionBackends:     map[string]*hostedAgentPoolBackend{},
		turnBackends:        map[string]*hostedAgentPoolBackend{},
		interactionBackends: map[string]*hostedAgentPoolBackend{},
	}
}

func startWorkspaceRuntimeSession(t *testing.T, ctx context.Context, supportsWorkspace bool) (*workspaceRuntimeProvider, *proto.RuntimeSession) {
	t.Helper()
	runtimeProvider := &workspaceRuntimeProvider{
		LocalProvider:           runtimeprovider.NewLocalProvider(),
		supportPrepareWorkspace: supportsWorkspace,
	}
	session, err := runtimeProvider.StartSession(ctx, &proto.StartRuntimeSessionRequest{AppName: "agent"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return runtimeProvider, session
}

func createAgentRuntimeWorkspaceRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo): %v", err)
	}
	runAgentRuntimeWorkspaceGit(t, repo, "init")
	runAgentRuntimeWorkspaceGit(t, repo, "config", "user.email", "workspace@example.invalid")
	runAgentRuntimeWorkspaceGit(t, repo, "config", "user.name", "Workspace Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("workspace fixture\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(README): %v", err)
	}
	runAgentRuntimeWorkspaceGit(t, repo, "add", "README.md")
	runAgentRuntimeWorkspaceGit(t, repo, "commit", "-m", "initial")
	return repo
}

func runAgentRuntimeWorkspaceGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

func TestAgentRuntimePingChecksConfiguredProviders(t *testing.T) {
	t.Parallel()

	defaultCalls := 0
	canaryCalls := 0
	runtime := &agentRuntime{
		defaultProviderName: "simple",
		configuredProviders: map[string]struct{}{
			"canary": {},
			"simple": {},
		},
		providers: map[string]coreagent.Provider{
			"canary": &pingAgentProvider{
				calls: &canaryCalls,
				err:   errors.New("canary down"),
			},
			"simple": &pingAgentProvider{calls: &defaultCalls},
		},
	}

	if err := runtime.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), `agent provider "canary" unavailable`) {
		t.Fatalf("Ping error = %v, want canary unavailable", err)
	}
	if defaultCalls != 1 {
		t.Fatalf("default provider Ping calls = %d, want 1", defaultCalls)
	}
	if canaryCalls != 1 {
		t.Fatalf("canary provider Ping calls = %d, want 1", canaryCalls)
	}

	defaultCalls = 0
	canaryCalls = 0
	runtime.UnpublishProvider("canary")
	if err := runtime.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), `agent provider "canary" unavailable`) {
		t.Fatalf("Ping after failed provider error = %v, want canary unavailable", err)
	}
	if defaultCalls != 1 {
		t.Fatalf("default provider Ping calls after failed provider = %d, want 1", defaultCalls)
	}
	if canaryCalls != 0 {
		t.Fatalf("canary provider Ping calls after failed provider = %d, want 0", canaryCalls)
	}
}

func TestAgentRuntimePingChecksConfiguredProvidersInParallel(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		runtime := &agentRuntime{
			defaultProviderName: "simple",
			configuredProviders: map[string]struct{}{
				"canary": {},
				"simple": {},
			},
			providers: map[string]coreagent.Provider{
				"canary": &pingAgentProvider{delay: 100 * time.Millisecond},
				"simple": &pingAgentProvider{delay: 100 * time.Millisecond},
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		if err := runtime.Ping(ctx); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	})
}

func TestAgentRuntimePingReportsPendingStartupProvidersUnavailable(t *testing.T) {
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

	done := make(chan error, 1)
	go func() {
		done <- runtime.Ping(context.Background())
	}()

	select {
	case err := <-done:
		if !errors.Is(err, agentmanager.ErrAgentProviderNotAvailable) {
			t.Fatalf("Ping error = %v, want ErrAgentProviderNotAvailable", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Ping blocked on pending startup agent provider")
	}
}

func TestAgentRuntimeConfigSelectedProviderStartsSessionWithRuntimeFields(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := newCapturingRuntime()
	ctxSentinel := &struct{}{}
	var factoryContextValue any

	factories := NewFactoryRegistry()
	factories.Runtime = func(ctx context.Context, _ string, _ *config.RuntimeProviderEntry, _ Deps) (runtimeprovider.Provider, error) {
		factoryContextValue = ctx.Value(agentRuntimeFactoryContextKey{})
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Template = "python-dev"
	runtimeConfig.Image = "ghcr.io/valon/gestalt-python-runtime:latest"
	runtimeConfig.ImagePullAuth = &config.RuntimePlacementImagePullAuth{
		DockerConfigJSON: `{"auths":{"ghcr.io":{"username":"ghcr-user","password":"ghcr-token"}}}`,
	}
	runtimeConfig.Metadata = map[string]string{"tenant": "eng"}
	imageEntrypointDir, err := os.MkdirTemp(".", "agent-image-entrypoint-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(imageEntrypointDir) })
	imageEntrypoint := filepath.Join(imageEntrypointDir, "agent")
	agentBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("ReadFile(agent bin): %v", err)
	}
	if err := os.WriteFile(imageEntrypoint, agentBytes, 0o755); err != nil {
		t.Fatalf("WriteFile(image entrypoint): %v", err)
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: runtimeConfig,
					ResolvedManifest: &providermanifestv1.Manifest{
						Kind: providermanifestv1.KindAgent,
						Entrypoint: &providermanifestv1.Entrypoint{
							ArtifactPath: filepath.ToSlash(imageEntrypoint),
						},
					},
				},
			},
		},
	}

	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:  &agentRuntime{providers: map[string]coreagent.Provider{}},
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	buildCtx := context.WithValue(context.Background(), agentRuntimeFactoryContextKey{}, ctxSentinel)
	agents, _, err := buildAgents(buildCtx, cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	requests := runtimeProvider.startSessionRequests()
	if len(requests) != 1 {
		t.Fatalf("start session requests = %d, want 1", len(requests))
	}
	req := requests[0]
	if req.AppName != "simple" {
		t.Fatalf("StartSession AppName = %q, want simple", req.AppName)
	}
	if req.Template != "python-dev" {
		t.Fatalf("StartSession Template = %q, want python-dev", req.Template)
	}
	if req.Image != "ghcr.io/valon/gestalt-python-runtime:latest" {
		t.Fatalf("StartSession Image = %q", req.Image)
	}
	if req.ImagePullAuth == nil {
		t.Fatal("StartSession ImagePullAuth is nil")
	}
	if req.ImagePullAuth.DockerConfigJson != `{"auths":{"ghcr.io":{"username":"ghcr-user","password":"ghcr-token"}}}` {
		t.Fatalf("StartSession ImagePullAuth.DockerConfigJson = %q", req.ImagePullAuth.DockerConfigJson)
	}
	if req.Metadata["tenant"] != "eng" {
		t.Fatalf("StartSession Metadata[tenant] = %q, want eng", req.Metadata["tenant"])
	}
	if req.Metadata["provider_kind"] != "agent" {
		t.Fatalf("StartSession Metadata[provider_kind] = %q, want agent", req.Metadata["provider_kind"])
	}
	if req.Metadata["provider_name"] != "simple" {
		t.Fatalf("StartSession Metadata[provider_name] = %q, want simple", req.Metadata["provider_name"])
	}
	if factoryContextValue != ctxSentinel {
		t.Fatalf("runtime factory context value = %#v, want %#v", factoryContextValue, ctxSentinel)
	}
}

func TestAgentRuntimeConfigStartsHostedAgentWarmPool(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	clock := newHostedAgentPoolManualClock(time.Now().UTC())
	runtimeProvider := newCapturingRuntime()
	runtimeProvider.now = clock.Now
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.MinReadyInstances = 2
	runtimeConfig.Pool.MaxReadyInstances = 2
	runtimeConfig.Pool.DrainTimeout = "2s"
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: runtimeConfig,
				},
			},
		},
	}
	services := testutil.NewStubServices(t)
	agentRuntime := &agentRuntime{providers: map[string]coreagent.Provider{}}
	agentRuntime.SetRunGrants(newTestAgentRunGrants(t))
	deps := Deps{
		BaseURL:              "https://gestalt.example.test",
		EncryptionKey:        []byte("0123456789abcdef0123456789abcdef"),
		Services:             services,
		AgentRuntime:         agentRuntime,
		hostedAgentPoolClock: clock,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	requests := runtimeProvider.startSessionRequests()
	if len(requests) != 2 {
		t.Fatalf("start session requests = %d, want 2", len(requests))
	}
	for i, sessionID := range []string{"session-1", "session-2"} {
		session, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
			SessionId: sessionID,
			Model:     "gpt-test",
		})
		if err != nil {
			t.Fatalf("CreateSession(%d): %v", i, err)
		}
		if session == nil || session.ID != sessionID {
			t.Fatalf("CreateSession(%d) = %#v, want %q", i, session, sessionID)
		}
	}
	pool := hostedAgentProviderPoolForTest(t, agents[0])
	sessionBackend := pool.sessionBackend("session-1")
	if sessionBackend == nil {
		t.Fatal("session-1 backend was not recorded")
	}
	turn, err := agents[0].CreateTurn(context.Background(), &proto.CreateAgentProviderTurnRequest{
		TimeoutSeconds: 1,
		TurnId:         "turn-1",
		SessionId:      "session-1",
		Model:          "gpt-test",
		Output:         agentRuntimeTextOutput(),
		Metadata: mustTestProtoStruct(t, map[string]any{
			"requireInteraction": true,
		}),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn == nil || turn.ID != "turn-1" || turn.SessionID != "session-1" {
		t.Fatalf("CreateTurn = %#v, want turn-1 on session-1", turn)
	}
	if turn.Status != coreagent.ExecutionStatusWaitingForInput {
		t.Fatalf("turn status = %q, want %q", turn.Status, coreagent.ExecutionStatusWaitingForInput)
	}
	waiters := clock.waiterCountSnapshot()
	drainDone := make(chan error, 1)
	go func() {
		drainDone <- pool.drainAndCloseBackend(sessionBackend)
	}()
	clock.waitForWaiterAfter(t, waiters)
	sessions, err := agents[0].ListSessions(context.Background(), &proto.ListAgentProviderSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions(during drain): %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListSessions(during drain) = %d sessions, want 2", len(sessions))
	}
	fetched, err := agents[0].GetTurn(context.Background(), &proto.GetAgentProviderTurnRequest{TurnId: "turn-1"})
	if err != nil {
		t.Fatalf("GetTurn(during drain): %v", err)
	}
	if fetched == nil || fetched.ID != "turn-1" || fetched.Status != coreagent.ExecutionStatusWaitingForInput {
		t.Fatalf("GetTurn(during drain) = %#v, want waiting turn-1", fetched)
	}
	interactions, err := agents[0].ListInteractions(context.Background(), &proto.ListAgentProviderInteractionsRequest{TurnId: "turn-1"})
	if err != nil {
		t.Fatalf("ListInteractions(during drain): %v", err)
	}
	if len(interactions) != 1 {
		t.Fatalf("ListInteractions(during drain) = %d interactions, want 1", len(interactions))
	}
	if _, err := agents[0].ResolveInteraction(context.Background(), &proto.ResolveAgentProviderInteractionRequest{
		InteractionId: interactions[0].ID,
		Resolution:    mustTestProtoStruct(t, map[string]any{"approved": true}),
	}); err != nil {
		t.Fatalf("ResolveInteraction(during drain): %v", err)
	}
	resolved, err := agents[0].GetTurn(context.Background(), &proto.GetAgentProviderTurnRequest{TurnId: "turn-1"})
	if err != nil {
		t.Fatalf("GetTurn(after ResolveInteraction): %v", err)
	}
	if resolved == nil || resolved.Status != coreagent.ExecutionStatusSucceeded {
		t.Fatalf("GetTurn(after ResolveInteraction) = %#v, want succeeded turn", resolved)
	}
	clock.Advance(25 * time.Millisecond)
	if err := <-drainDone; err != nil {
		t.Fatalf("drainAndCloseBackend: %v", err)
	}
}

func TestAgentRuntimeConfigScalesOutHostedAgentWarmPool(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := newCapturingRuntime()
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.MaxReadyInstances = 2
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: runtimeConfig,
				},
			},
		},
	}
	services := testutil.NewStubServices(t)
	agentRuntime := &agentRuntime{providers: map[string]coreagent.Provider{}}
	agentRuntime.SetRunGrants(newTestAgentRunGrants(t))
	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Services:      services,
		AgentRuntime:  agentRuntime,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	if got := len(runtimeProvider.startSessionRequests()); got != 1 {
		t.Fatalf("initial start session requests = %d, want 1", got)
	}
	pool := hostedAgentProviderPoolForTest(t, agents[0])
	initial := pool.readyBackends()
	if len(initial) != 1 {
		t.Fatalf("initial ready backends = %d, want 1", len(initial))
	}
	first, releaseFirst, err := pool.acquireBackend(context.Background(), initial[0], false)
	if err != nil {
		t.Fatalf("acquire first backend: %v", err)
	}
	defer releaseFirst()

	session, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		SessionId: "scale-out-session",
		Model:     "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession(scale out): %v", err)
	}
	if session == nil || session.ID != "scale-out-session" {
		t.Fatalf("CreateSession(scale out) = %#v, want scale-out-session", session)
	}
	sessionBackend := pool.sessionBackend("scale-out-session")
	if sessionBackend != first {
		t.Fatalf("scale-out triggering request backend = %#v, want existing ready backend", sessionBackend)
	}
	runtimeProvider.waitStartSessionRequests(t, 2)
	waitHostedAgentPoolState(t, pool, func() bool {
		return len(hostedAgentPoolReadyBackendsLocked(pool)) == 2
	})
	if got := len(runtimeProvider.startSessionRequests()); got != 2 {
		t.Fatalf("start session requests after scale out = %d, want 2", got)
	}

	var scaledBackend *hostedAgentPoolBackend
	for _, backend := range pool.readyBackends() {
		if backend != first {
			scaledBackend = backend
			break
		}
	}
	if scaledBackend == nil {
		t.Fatal("scaled backend was not started")
	}
	_, releaseSecond, err := pool.acquireBackend(context.Background(), scaledBackend, false)
	if err != nil {
		t.Fatalf("acquire scaled backend: %v", err)
	}
	defer releaseSecond()
	if _, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		SessionId: "max-capped-session",
		Model:     "gpt-test",
	}); err != nil {
		t.Fatalf("CreateSession(max capped): %v", err)
	}
	if got := len(runtimeProvider.startSessionRequests()); got != 2 {
		t.Fatalf("start session requests after max cap = %d, want 2", got)
	}
}

func TestAgentRuntimeConfigRestartsUnhealthyHostedAgent(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	clock := newHostedAgentPoolManualClock(time.Now().UTC())
	runtimeProvider := newCapturingRuntime()
	runtimeProvider.now = clock.Now
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.HealthCheckInterval = "50ms"
	runtimeConfig.Pool.RestartPolicy = config.RuntimePlacementRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command:   bin,
					IndexedDB: &config.IndexedDBBindingConfig{Provider: "agent_state"},
					Runtime:   runtimeConfig,
				},
			},
		},
	}
	deps := Deps{
		BaseURL:              "https://gestalt.example.test",
		EncryptionKey:        []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:         &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:        testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory:     func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
		hostedAgentPoolClock: clock,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	pool := hostedAgentProviderPoolForTest(t, agents[0])
	backends := pool.readyBackends()
	if len(backends) != 1 {
		t.Fatalf("ready backends = %d, want 1", len(backends))
	}
	clock.waitForTicker(t)
	if err := backends[0].provider.Close(); err != nil {
		t.Fatalf("Close backend provider: %v", err)
	}
	clock.Advance(50 * time.Millisecond)
	runtimeProvider.waitStartSessionRequests(t, 2)
	waitHostedAgentPoolState(t, pool, func() bool {
		return len(hostedAgentPoolReadyBackendsLocked(pool)) == 1 && pool.starting == 0
	})
	if got := len(runtimeProvider.startSessionRequests()); got < 2 {
		t.Fatalf("start session requests after unhealthy backend = %d, want at least 2", got)
	}
	if got := len(pool.readyBackends()); got != 1 {
		t.Fatalf("ready backends after unhealthy replacement = %d, want 1", got)
	}
	session, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		SessionId: "session-after-restart",
		Model:     "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession(after restart): %v", err)
	}
	if session == nil || session.ID != "session-after-restart" {
		t.Fatalf("CreateSession(after restart) = %#v, want session-after-restart", session)
	}
}

func TestAgentRuntimeConfigReplacesHostedAgentBeforeRuntimeDrainDeadline(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	clock := newHostedAgentPoolManualClock(time.Now().UTC())
	runtimeProvider := newCapturingRuntime()
	runtimeProvider.now = clock.Now
	var drainMu sync.Mutex
	var firstDrainAt time.Time
	runtimeProvider.lifecycleForSession = func(index int) *proto.RuntimeSessionLifecycle {
		startedAt := clock.Now().UTC()
		expiresAt := startedAt.Add(time.Hour)
		lifecycle := &proto.RuntimeSessionLifecycle{
			StartedAt: timestamppb.New(startedAt),
			ExpiresAt: timestamppb.New(expiresAt),
		}
		if index == 1 {
			recommendedDrainAt := startedAt.Add(500 * time.Millisecond)
			lifecycle.RecommendedDrainAt = timestamppb.New(recommendedDrainAt)
			drainMu.Lock()
			firstDrainAt = recommendedDrainAt
			drainMu.Unlock()
		}
		return lifecycle
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.MaxReadyInstances = 2
	runtimeConfig.Pool.HealthCheckInterval = "25ms"
	runtimeConfig.Pool.RestartPolicy = config.RuntimePlacementRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command:   bin,
					IndexedDB: &config.IndexedDBBindingConfig{Provider: "agent_state"},
					Runtime:   runtimeConfig,
				},
			},
		},
	}
	deps := Deps{
		BaseURL:              "https://gestalt.example.test",
		EncryptionKey:        []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:         &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:        testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory:     func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
		hostedAgentPoolClock: clock,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	pool := hostedAgentProviderPoolForTest(t, agents[0])
	backends := pool.readyBackends()
	if len(backends) != 1 {
		t.Fatalf("ready backends = %d, want 1", len(backends))
	}
	first := backends[0]
	clock.waitForTicker(t)
	clock.Advance(250 * time.Millisecond)
	runtimeProvider.waitStartSessionRequests(t, 2)
	waitHostedAgentPoolState(t, pool, func() bool {
		ready := hostedAgentPoolReadyBackendsLocked(pool)
		return len(ready) == 1 && ready[0] != first && pool.starting == 0
	})
	ready := pool.readyBackends()
	if len(ready) != 1 || ready[0] == first {
		t.Fatalf("ready backends after replacement = %#v, want only replacement", ready)
	}
	startTimes := runtimeProvider.startSessionTimes()
	if len(startTimes) < 2 {
		t.Fatalf("start session times = %d, want at least 2", len(startTimes))
	}
	drainMu.Lock()
	drainAt := firstDrainAt
	drainMu.Unlock()
	if drainAt.IsZero() {
		t.Fatal("first runtime drain deadline was not captured")
	}
	if !startTimes[1].Before(drainAt) {
		t.Fatalf("replacement started at %s, want before first runtime drain deadline %s", startTimes[1].Format(time.RFC3339Nano), drainAt.Format(time.RFC3339Nano))
	}
	pool.mu.Lock()
	firstRetired := first.draining || first.closed
	pool.mu.Unlock()
	if !firstRetired {
		t.Fatal("first runtime backend was not marked draining or closed")
	}
	session, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		SessionId: "session-after-runtime-drain",
		Model:     "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession(after runtime drain): %v", err)
	}
	if session == nil || session.ID != "session-after-runtime-drain" {
		t.Fatalf("CreateSession(after runtime drain) = %#v, want session-after-runtime-drain", session)
	}
}

func TestAgentRuntimeConfigKeepsHostedAgentServingWhenProactiveReplacementStartFails(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	clock := newHostedAgentPoolManualClock(time.Now().UTC())
	runtimeProvider := newCapturingRuntime()
	runtimeProvider.now = clock.Now
	runtimeProvider.lifecycleForSession = func(index int) *proto.RuntimeSessionLifecycle {
		startedAt := clock.Now().UTC()
		expiresAt := startedAt.Add(time.Hour)
		lifecycle := &proto.RuntimeSessionLifecycle{
			StartedAt: timestamppb.New(startedAt),
			ExpiresAt: timestamppb.New(expiresAt),
		}
		if index == 1 {
			recommendedDrainAt := startedAt.Add(8 * time.Second)
			lifecycle.RecommendedDrainAt = timestamppb.New(recommendedDrainAt)
		}
		return lifecycle
	}
	runtimeProvider.startErrForSession = func(index int) error {
		if index > 1 {
			return errors.New("replacement start failed")
		}
		return nil
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.MaxReadyInstances = 2
	runtimeConfig.Pool.StartupTimeout = "15s"
	runtimeConfig.Pool.HealthCheckInterval = "500ms"
	runtimeConfig.Pool.RestartPolicy = config.RuntimePlacementRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command:   bin,
					IndexedDB: &config.IndexedDBBindingConfig{Provider: "agent_state"},
					Runtime:   runtimeConfig,
				},
			},
		},
	}
	deps := Deps{
		BaseURL:              "https://gestalt.example.test",
		EncryptionKey:        []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:         &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:        testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory:     func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
		hostedAgentPoolClock: clock,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	pool := hostedAgentProviderPoolForTest(t, agents[0])
	backends := pool.readyBackends()
	if len(backends) != 1 {
		t.Fatalf("ready backends = %d, want 1", len(backends))
	}
	first := backends[0]
	clock.waitForTicker(t)
	clock.Advance(5 * time.Second)
	runtimeProvider.waitStartSessionRequests(t, 2)
	waitHostedAgentPoolState(t, pool, func() bool {
		return !first.replacing && pool.starting == 0
	})
	pool.mu.Lock()
	acceptsNewWork := pool.backendAcceptsNewWorkLocked(first, clock.Now().UTC())
	firstDraining := first.draining
	pool.mu.Unlock()
	if !acceptsNewWork || firstDraining {
		t.Fatalf("first backend acceptsNewWork=%v draining=%v, want serving after failed proactive replacement", acceptsNewWork, firstDraining)
	}
	session, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		SessionId: "session-after-failed-proactive-replacement",
		Model:     "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession(after failed proactive replacement): %v", err)
	}
	if session == nil || session.ID != "session-after-failed-proactive-replacement" {
		t.Fatalf("CreateSession(after failed proactive replacement) = %#v, want session", session)
	}
	if backend := pool.sessionBackend("session-after-failed-proactive-replacement"); backend != first {
		t.Fatalf("session backend = %#v, want first backend after failed proactive replacement", backend)
	}
}

func TestAgentRuntimeConfigProactiveReplacementRespectsMaxReadyInstances(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	clock := newHostedAgentPoolManualClock(time.Now().UTC())
	runtimeProvider := newCapturingRuntime()
	runtimeProvider.now = clock.Now
	releaseReplacement := make(chan struct{})
	replacementStarted := make(chan struct{})
	var replacementStartedOnce sync.Once
	runtimeProvider.lifecycleForSession = func(index int) *proto.RuntimeSessionLifecycle {
		startedAt := clock.Now().UTC()
		expiresAt := startedAt.Add(time.Hour)
		lifecycle := &proto.RuntimeSessionLifecycle{
			StartedAt: timestamppb.New(startedAt),
			ExpiresAt: timestamppb.New(expiresAt),
		}
		return lifecycle
	}
	runtimeProvider.startErrForSession = func(index int) error {
		if index <= 2 {
			return nil
		}
		replacementStartedOnce.Do(func() {
			close(replacementStarted)
		})
		<-releaseReplacement
		return nil
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.MinReadyInstances = 2
	runtimeConfig.Pool.MaxReadyInstances = 3
	runtimeConfig.Pool.StartupTimeout = "15s"
	runtimeConfig.Pool.HealthCheckInterval = "25ms"
	runtimeConfig.Pool.RestartPolicy = config.RuntimePlacementRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command:   bin,
					IndexedDB: &config.IndexedDBBindingConfig{Provider: "agent_state"},
					Runtime:   runtimeConfig,
				},
			},
		},
	}
	deps := Deps{
		BaseURL:              "https://gestalt.example.test",
		EncryptionKey:        []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:         &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:        testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory:     func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
		hostedAgentPoolClock: clock,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		close(releaseReplacement)
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	pool := hostedAgentProviderPoolForTest(t, agents[0])
	ready := pool.readyBackends()
	if len(ready) != 2 {
		t.Fatalf("ready backends = %d, want 2", len(ready))
	}
	clock.waitForTicker(t)
	markCapturingRuntimeBackendsDrainingSoon(runtimeProvider, ready, clock.Now().UTC().Add(250*time.Millisecond))
	clock.Advance(125 * time.Millisecond)
	<-replacementStarted
	if got := len(runtimeProvider.startSessionRequests()); got != 3 {
		t.Fatalf("start session requests while one replacement is starting = %d, want 3", got)
	}
	pool.mu.Lock()
	_, starting, _ := pool.instanceCountsLocked()
	pool.mu.Unlock()
	if starting != 1 {
		t.Fatalf("starting instances = %d, want 1", starting)
	}
}

func markCapturingRuntimeBackendsDrainingSoon(runtimeProvider *capturingRuntime, backends []*hostedAgentPoolBackend, drainAt time.Time) {
	runtimeProvider.mu.Lock()
	defer runtimeProvider.mu.Unlock()
	if runtimeProvider.sessionLifecycles == nil {
		runtimeProvider.sessionLifecycles = map[string]*proto.RuntimeSessionLifecycle{}
	}
	for _, backend := range backends {
		if backend == nil || backend.runtimeSessionID == "" {
			continue
		}
		lifecycle := cloneRuntimeSessionLifecycle(runtimeProvider.sessionLifecycles[backend.runtimeSessionID])
		if lifecycle == nil {
			lifecycle = &proto.RuntimeSessionLifecycle{}
		}
		lifecycle.RecommendedDrainAt = timestamppb.New(drainAt)
		runtimeProvider.sessionLifecycles[backend.runtimeSessionID] = lifecycle
	}
}

func TestAgentRuntimeConfigDoesNotImmediatelyChurnWhenExpiryReserveExceedsRuntimeLifetime(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	clock := newHostedAgentPoolManualClock(time.Now().UTC())
	runtimeProvider := newCapturingRuntime()
	runtimeProvider.now = clock.Now
	runtimeProvider.lifecycleForSession = func(index int) *proto.RuntimeSessionLifecycle {
		expiresAt := clock.Now().UTC().Add(5 * time.Minute)
		return &proto.RuntimeSessionLifecycle{
			ExpiresAt: timestamppb.New(expiresAt),
		}
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.StartupTimeout = "5m"
	runtimeConfig.Pool.DrainTimeout = "2m"
	runtimeConfig.Pool.HealthCheckInterval = "25ms"
	runtimeConfig.Pool.RestartPolicy = config.RuntimePlacementRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command:   bin,
					IndexedDB: &config.IndexedDBBindingConfig{Provider: "agent_state"},
					Runtime:   runtimeConfig,
				},
			},
		},
	}
	deps := Deps{
		BaseURL:              "https://gestalt.example.test",
		EncryptionKey:        []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:         &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:        testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory:     func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
		hostedAgentPoolClock: clock,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	clock.waitForTicker(t)
	getSessionCalls := runtimeProvider.getSessionCallCount()
	clock.Advance(25 * time.Millisecond)
	runtimeProvider.waitGetSessionCalls(t, getSessionCalls+1)
	if got := len(runtimeProvider.startSessionRequests()); got != 1 {
		t.Fatalf("start session requests after expiry health checks = %d, want 1", got)
	}
}

func TestAgentRuntimeConfigReplacesExpiresOnlyRuntimeBeforeExpiry(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	clock := newHostedAgentPoolManualClock(time.Now().UTC())
	runtimeProvider := newCapturingRuntime()
	runtimeProvider.now = clock.Now
	var expiryMu sync.Mutex
	var firstExpiresAt time.Time
	runtimeProvider.lifecycleForSession = func(index int) *proto.RuntimeSessionLifecycle {
		expiresAt := clock.Now().UTC().Add(time.Hour)
		if index == 1 {
			expiresAt = clock.Now().UTC().Add(2 * time.Second)
			expiryMu.Lock()
			firstExpiresAt = expiresAt
			expiryMu.Unlock()
		}
		return &proto.RuntimeSessionLifecycle{
			ExpiresAt: timestamppb.New(expiresAt),
		}
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.StartupTimeout = "5m"
	runtimeConfig.Pool.DrainTimeout = "2m"
	runtimeConfig.Pool.HealthCheckInterval = "25ms"
	runtimeConfig.Pool.RestartPolicy = config.RuntimePlacementRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command:   bin,
					IndexedDB: &config.IndexedDBBindingConfig{Provider: "agent_state"},
					Runtime:   runtimeConfig,
				},
			},
		},
	}
	deps := Deps{
		BaseURL:              "https://gestalt.example.test",
		EncryptionKey:        []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:         &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:        testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory:     func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
		hostedAgentPoolClock: clock,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	clock.waitForTicker(t)
	pool := hostedAgentProviderPoolForTest(t, agents[0])
	initialReady := pool.readyBackends()
	if len(initialReady) != 1 {
		t.Fatalf("ready backends = %d, want 1", len(initialReady))
	}
	first := initialReady[0]
	clock.Advance(time.Second)
	runtimeProvider.waitStartSessionRequests(t, 2)
	waitHostedAgentPoolState(t, pool, func() bool {
		ready := hostedAgentPoolReadyBackendsLocked(pool)
		return len(ready) == 1 && ready[0] != first && pool.starting == 0
	})
	startTimes := runtimeProvider.startSessionTimes()
	if len(startTimes) < 2 {
		t.Fatalf("start session times = %d, want at least 2", len(startTimes))
	}
	expiryMu.Lock()
	expiresAt := firstExpiresAt
	expiryMu.Unlock()
	if expiresAt.IsZero() {
		t.Fatal("first runtime expiry was not captured")
	}
	if !startTimes[1].Before(expiresAt) {
		t.Fatalf("replacement started at %s, want before first runtime expiry %s", startTimes[1].Format(time.RFC3339Nano), expiresAt.Format(time.RFC3339Nano))
	}
}

func TestHostedAgentProviderPoolPingChecksReadyBackendsInParallel(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pool := &hostedAgentProviderPool{
			name: "simple",
			backends: []*hostedAgentPoolBackend{
				{
					id:        1,
					provider:  &pingAgentProvider{delay: 100 * time.Millisecond},
					liveTurns: map[string]struct{}{},
				},
				{
					id:        2,
					provider:  &pingAgentProvider{delay: 100 * time.Millisecond},
					liveTurns: map[string]struct{}{},
				},
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	})
}

func TestHostedAgentProviderPoolListSessionsDeduplicatesSharedStoreSessions(t *testing.T) {
	t.Parallel()

	firstProvider := &listSessionsAgentProvider{
		sessions: []*coreagent.Session{{ID: "session-1", State: coreagent.SessionStateActive}},
	}
	secondProvider := &listSessionsAgentProvider{
		sessions: []*coreagent.Session{
			{ID: "session-1", State: coreagent.SessionStateActive},
			{ID: "session-2", State: coreagent.SessionStateActive},
		},
	}
	pool := &hostedAgentProviderPool{
		name:            "simple",
		ctx:             context.Background(),
		sessionBackends: map[string]*hostedAgentPoolBackend{},
		backends: []*hostedAgentPoolBackend{
			{
				id:        1,
				provider:  firstProvider,
				liveTurns: map[string]struct{}{},
			},
			{
				id:        2,
				provider:  secondProvider,
				liveTurns: map[string]struct{}{},
			},
		},
	}

	sessions, err := pool.ListSessions(context.Background(), &proto.ListAgentProviderSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListSessions returned %d sessions, want 2: %#v", len(sessions), sessions)
	}
	ids := map[string]int{}
	for _, session := range sessions {
		ids[session.ID]++
	}
	if ids["session-1"] != 1 || ids["session-2"] != 1 {
		t.Fatalf("ListSessions IDs = %#v, want session-1 and session-2 once", ids)
	}
	if backend := pool.sessionBackend("session-1"); backend != pool.backends[0] {
		t.Fatalf("session-1 backend = %#v, want first backend", backend)
	}
	if backend := pool.sessionBackend("session-2"); backend != pool.backends[1] {
		t.Fatalf("session-2 backend = %#v, want second backend", backend)
	}
}

func TestHostedAgentProviderPoolSkipsPastDrainBackendForNewTurn(t *testing.T) {
	t.Parallel()

	firstCalls := 0
	secondCalls := 0
	pastDrainAt := time.Now().UTC().Add(-time.Second)
	first := &hostedAgentPoolBackend{
		id: 1,
		provider: &routingAgentProvider{
			createTurn: func(context.Context, *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
				firstCalls++
				return nil, errors.New("past-drain backend should not receive new work")
			},
		},
		runtimeDrainAt: &pastDrainAt,
		liveTurns:      map[string]struct{}{},
	}
	second := &hostedAgentPoolBackend{
		id: 2,
		provider: &routingAgentProvider{
			createTurn: func(_ context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
				secondCalls++
				return &coreagent.Turn{
					ID:        req.GetTurnId(),
					SessionID: req.GetSessionId(),
					Status:    coreagent.ExecutionStatusRunning,
				}, nil
			},
		},
		liveTurns: map[string]struct{}{},
	}
	pool := &hostedAgentProviderPool{
		name:            "simple",
		ctx:             context.Background(),
		sessionBackends: map[string]*hostedAgentPoolBackend{"session-1": first},
		turnBackends:    map[string]*hostedAgentPoolBackend{},
		backends:        []*hostedAgentPoolBackend{first, second},
	}

	turn, err := pool.CreateTurn(context.Background(), &proto.CreateAgentProviderTurnRequest{
		TimeoutSeconds: 1,
		TurnId:         "turn-1",
		SessionId:      "session-1",
		Output:         agentRuntimeTextOutput(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn == nil || turn.ID != "turn-1" {
		t.Fatalf("CreateTurn = %#v, want turn-1", turn)
	}
	if firstCalls != 0 || secondCalls != 1 {
		t.Fatalf("CreateTurn calls: first=%d second=%d, want first=0 second=1", firstCalls, secondCalls)
	}
	if backend := pool.turnBackend("turn-1"); backend != second {
		t.Fatalf("turn backend = %#v, want second backend", backend)
	}
}

func TestHostedAgentProviderPoolGetTurnRetriesAfterPreferredTimeout(t *testing.T) {
	t.Parallel()

	firstCalls := 0
	secondCalls := 0
	first := &hostedAgentPoolBackend{
		id: 1,
		provider: &routingAgentProvider{
			getTurn: func(context.Context, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
				firstCalls++
				return nil, context.DeadlineExceeded
			},
		},
		liveTurns: map[string]struct{}{"turn-1": {}},
	}
	second := &hostedAgentPoolBackend{
		id: 2,
		provider: &routingAgentProvider{
			getTurn: func(_ context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
				secondCalls++
				return &coreagent.Turn{
					ID:        req.GetTurnId(),
					SessionID: "session-1",
					Status:    coreagent.ExecutionStatusRunning,
				}, nil
			},
		},
		liveTurns: map[string]struct{}{},
	}
	pool := &hostedAgentProviderPool{
		name:            "simple",
		ctx:             context.Background(),
		sessionBackends: map[string]*hostedAgentPoolBackend{},
		turnBackends:    map[string]*hostedAgentPoolBackend{"turn-1": first},
		backends:        []*hostedAgentPoolBackend{first, second},
	}

	turn, err := pool.GetTurn(context.Background(), &proto.GetAgentProviderTurnRequest{TurnId: "turn-1"})
	if err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if turn == nil || turn.ID != "turn-1" {
		t.Fatalf("GetTurn = %#v, want turn-1", turn)
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("GetTurn calls: first=%d second=%d, want first=1 second=1", firstCalls, secondCalls)
	}
	if backend := pool.turnBackend("turn-1"); backend != second {
		t.Fatalf("turn backend = %#v, want second backend after retry", backend)
	}
}

func TestHostedAgentProviderPoolGetTurnRetriesAfterStalePreferredMiss(t *testing.T) {
	t.Parallel()

	firstCalls := 0
	secondCalls := 0
	first := &hostedAgentPoolBackend{
		id: 1,
		provider: &routingAgentProvider{
			getTurn: func(context.Context, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
				firstCalls++
				return nil, core.ErrNotFound
			},
		},
		liveTurns: map[string]struct{}{"turn-1": {}},
	}
	second := &hostedAgentPoolBackend{
		id: 2,
		provider: &routingAgentProvider{
			getTurn: func(_ context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
				secondCalls++
				return &coreagent.Turn{
					ID:        req.GetTurnId(),
					SessionID: "session-1",
					Status:    coreagent.ExecutionStatusSucceeded,
				}, nil
			},
		},
		liveTurns: map[string]struct{}{},
	}
	pool := &hostedAgentProviderPool{
		name:            "simple",
		ctx:             context.Background(),
		sessionBackends: map[string]*hostedAgentPoolBackend{},
		turnBackends:    map[string]*hostedAgentPoolBackend{"turn-1": first},
		backends:        []*hostedAgentPoolBackend{first, second},
	}

	turn, err := pool.GetTurn(context.Background(), &proto.GetAgentProviderTurnRequest{TurnId: "turn-1"})
	if err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if turn == nil || turn.ID != "turn-1" || turn.Status != coreagent.ExecutionStatusSucceeded {
		t.Fatalf("GetTurn = %#v, want succeeded turn-1", turn)
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("GetTurn calls: first=%d second=%d, want first=1 second=1", firstCalls, secondCalls)
	}
	if backend := pool.turnBackend("turn-1"); backend != nil {
		t.Fatalf("terminal turn backend = %#v, want no sticky backend after success", backend)
	}
}

func TestHostedAgentProviderPoolListSessionsContinuesAfterTransientBackendFailure(t *testing.T) {
	t.Parallel()

	second := &hostedAgentPoolBackend{
		id:        2,
		provider:  &listSessionsAgentProvider{sessions: []*coreagent.Session{{ID: "session-1", State: coreagent.SessionStateActive}}},
		liveTurns: map[string]struct{}{},
	}
	pool := &hostedAgentProviderPool{
		name:            "simple",
		ctx:             context.Background(),
		sessionBackends: map[string]*hostedAgentPoolBackend{},
		backends: []*hostedAgentPoolBackend{
			{
				id:        1,
				provider:  &listSessionsAgentProvider{err: context.DeadlineExceeded},
				liveTurns: map[string]struct{}{},
			},
			second,
		},
	}

	sessions, err := pool.ListSessions(context.Background(), &proto.ListAgentProviderSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "session-1" {
		t.Fatalf("ListSessions = %#v, want session-1", sessions)
	}
	if backend := pool.sessionBackend("session-1"); backend != second {
		t.Fatalf("session backend = %#v, want second backend", backend)
	}
}

func TestAgentRuntimeConfigUsesPublicAgentHostBinding(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	secret := []byte("0123456789abcdef0123456789abcdef")
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	testutil.AttachStubExternalCredentials(services)
	invoker := &recordingAgentRuntimeInvoker{}
	agentRuntime := &agentRuntime{providers: map[string]coreagent.Provider{}}
	agentRuntime.SetInvoker(invoker)
	runGrants := newTestAgentRunGrants(t)
	agentRuntime.SetRunGrants(runGrants)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID:          "sync",
			Method:      http.MethodPost,
			Title:       "Sync roadmap",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"}}}`),
		}}},
	})
	agentRuntime.SetToolSearcher(agentmanager.New(agentmanager.Config{
		Providers: providers,
		RunGrants: runGrants,
		Invoker:   invoker,
	}))
	capturingRuntime := newCapturingRuntime()

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return capturingRuntime, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: testHostedAgentRuntimeConfig(),
				},
			},
		},
	}

	deps := Deps{
		BaseURL:             "https://gestalt.example.test",
		RuntimeRelayBaseURL: relaySrv.URL,
		EncryptionKey:       secret,
		Services:            services,
		AgentRuntime:        agentRuntime,
		PublicHostServices:  publicHostServices,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	agents, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()
	if len(agents) != 1 {
		t.Fatalf("agents len = %d, want 1", len(agents))
	}
	capabilities, err := agents[0].GetCapabilities(context.Background(), &proto.GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if capabilities == nil || !capabilities.Interactions || !capabilities.ResumableTurns {
		t.Fatalf("capabilities = %#v, want interactions+resumable_turns", capabilities)
	}

	session, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		SessionId:      "session-1",
		IdempotencyKey: "session-req-1",
		Model:          "gpt-test",
		ClientRef:      "cli-session-1",
		Metadata: mustTestProtoStruct(t, map[string]any{
			"source": "agent-runtime-test",
		}),
		CreatedBySubjectId: "user:user-123",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session == nil || session.ID != "session-1" || session.ProviderName != "simple" || session.State != coreagent.SessionStateActive {
		t.Fatalf("CreateSession = %#v, want active simple session-1", session)
	}

	updatedSession, err := agents[0].UpdateSession(context.Background(), &proto.UpdateAgentProviderSessionRequest{
		SessionId: "session-1",
		ClientRef: "cli-session-2",
		State:     proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED,
		Metadata: mustTestProtoStruct(t, map[string]any{
			"source": "agent-runtime-test-updated",
		}),
	})
	if err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if updatedSession == nil || updatedSession.ClientRef != "cli-session-2" || updatedSession.State != coreagent.SessionStateArchived {
		t.Fatalf("UpdateSession = %#v, want archived cli-session-2", updatedSession)
	}

	sessions, err := agents[0].ListSessions(context.Background(), &proto.ListAgentProviderSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "session-1" {
		t.Fatalf("ListSessions = %#v, want session-1", sessions)
	}

	fetchedSession, err := agents[0].GetSession(context.Background(), &proto.GetAgentProviderSessionRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if fetchedSession == nil || fetchedSession.Metadata["source"] != "agent-runtime-test-updated" {
		t.Fatalf("GetSession = %#v, want updated source metadata", fetchedSession)
	}

	turn, err := agents[0].CreateTurn(context.Background(), &proto.CreateAgentProviderTurnRequest{
		TimeoutSeconds: 1,
		TurnId:         "turn-1",
		SessionId:      "session-1",
		Model:          "gpt-test",
		Messages: []*proto.AgentMessage{{
			Role: "user",
			Text: "Plan it",
			Parts: []*proto.AgentMessagePart{{
				Type: proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TEXT,
				Text: "Plan it",
			}},
			Metadata: mustTestProtoStruct(t, map[string]any{"priority": "high"}),
		}},
		ExecutionRef: "exec-turn-1",
		Output:       agentRuntimeTextOutput(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn == nil || turn.ID != "turn-1" || turn.SessionID != "session-1" || turn.ProviderName != "simple" {
		t.Fatalf("CreateTurn = %#v, want simple turn-1 on session-1", turn)
	}

	turns, err := agents[0].ListTurns(context.Background(), &proto.ListAgentProviderTurnsRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(turns) != 1 || turns[0].ID != "turn-1" {
		t.Fatalf("ListTurns = %#v, want turn-1", turns)
	}

	fetchedTurn, err := agents[0].GetTurn(context.Background(), &proto.GetAgentProviderTurnRequest{TurnId: "turn-1"})
	if err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if fetchedTurn == nil || fetchedTurn.Status != coreagent.ExecutionStatusSucceeded || fetchedTurn.Output.Text == nil || fetchedTurn.Output.Text.Text == "" {
		t.Fatalf("GetTurn = %#v, want succeeded turn with output", fetchedTurn)
	}
	if len(fetchedTurn.Messages) != 1 || fetchedTurn.Messages[0].Metadata["priority"] != "high" || len(fetchedTurn.Messages[0].Parts) != 1 || fetchedTurn.Messages[0].Parts[0].Type != coreagent.MessagePartTypeText {
		t.Fatalf("GetTurn messages = %#v, want metadata and text part preserved", fetchedTurn.Messages)
	}

	turnEvents, err := agents[0].ListTurnEvents(context.Background(), &proto.ListAgentProviderTurnEventsRequest{
		TurnId:   "turn-1",
		AfterSeq: 0,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListTurnEvents: %v", err)
	}
	if len(turnEvents) != 3 || turnEvents[0].Type != "turn.started" || turnEvents[2].Type != "turn.completed" {
		t.Fatalf("ListTurnEvents = %#v, want started/completed event sequence", turnEvents)
	}
	if display := turnEvents[0].Display; display == nil || display.Kind != "status" || display.Phase != "started" || display.Text != "provider turn started" {
		t.Fatalf("turn.started display = %#v, want provider-authored started status", display)
	}
	if display := turnEvents[1].Display; display == nil || display.Kind != "text" || display.Phase != "completed" || display.Text != "provider assistant completed" {
		t.Fatalf("assistant.completed display = %#v, want provider-authored completed text", display)
	}
	if display := turnEvents[2].Display; display == nil || display.Kind != "status" || display.Phase != "completed" || display.Text != "provider turn completed" {
		t.Fatalf("turn.completed display = %#v, want provider-authored completed status", display)
	}
	completedOutput, ok := turnEvents[2].Display.Output.(map[string]any)
	if !ok || completedOutput["session_id"] != "session-1" {
		t.Fatalf("turn.completed display output = %#v, want session_id=session-1", turnEvents[2].Display.Output)
	}

	postTurnSession, err := agents[0].GetSession(context.Background(), &proto.GetAgentProviderSessionRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatalf("GetSession(after CreateTurn): %v", err)
	}
	if postTurnSession == nil || postTurnSession.ClientRef != "cli-session-2" {
		t.Fatalf("GetSession(after CreateTurn) = %#v, want preserved client_ref cli-session-2", postTurnSession)
	}

	wantRelayTarget := "tls://" + relaySrv.Listener.Addr().String()
	startRequests := capturingRuntime.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartApp requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].GetEnv()[runtimehost.HostServiceSocketEnv]; got != wantRelayTarget {
		t.Fatalf("agent host relay target = %q, want %q", got, wantRelayTarget)
	}
	if got := startRequests[0].GetEnv()[runtimehost.HostServiceTokenEnv]; strings.TrimSpace(got) == "" {
		t.Fatalf("StartApp env missing %s", runtimehost.HostServiceTokenEnv)
	}

	pausedTurn, err := agents[0].CreateTurn(context.Background(), &proto.CreateAgentProviderTurnRequest{
		TurnId:             "turn-2",
		SessionId:          "session-1",
		Model:              "gpt-test",
		CreatedBySubjectId: "user:user-123",
		Output:             agentRuntimeTextOutput(),
		TimeoutSeconds:     1,
		Metadata: mustTestProtoStruct(t, map[string]any{
			"requireInteraction": true,
		}),
	})
	if err != nil {
		t.Fatalf("CreateTurn(waiting): %v", err)
	}
	if pausedTurn == nil {
		t.Fatal("CreateTurn(waiting) returned nil turn")
		return
	}
	if pausedTurn.Status != coreagent.ExecutionStatusWaitingForInput {
		t.Fatalf("paused turn status = %q, want %q", pausedTurn.Status, coreagent.ExecutionStatusWaitingForInput)
	}
	var pausedOutput struct {
		InteractionRequested bool   `json:"interaction_requested"`
		InteractionID        string `json:"interaction_id"`
		InteractionError     string `json:"interaction_error"`
	}
	if pausedTurn.Output.Text == nil {
		t.Fatalf("paused turn output = %#v, want text output", pausedTurn.Output)
	}
	if err := json.Unmarshal([]byte(pausedTurn.Output.Text.Text), &pausedOutput); err != nil {
		t.Fatalf("json.Unmarshal(pausedTurn.Output.Text.Text): %v", err)
	}
	if !pausedOutput.InteractionRequested || strings.TrimSpace(pausedOutput.InteractionID) == "" || pausedOutput.InteractionError != "" {
		t.Fatalf("paused turn output = %+v", pausedOutput)
	}
	interactions, err := agents[0].ListInteractions(context.Background(), &proto.ListAgentProviderInteractionsRequest{TurnId: "turn-2"})
	if err != nil {
		t.Fatalf("ListInteractions: %v", err)
	}
	if len(interactions) != 1 {
		t.Fatalf("interactions = %d, want 1", len(interactions))
	}
	if interactions[0].Type != coreagent.InteractionTypeApproval || interactions[0].State != coreagent.InteractionStatePending {
		t.Fatalf("interaction = %#v, want pending approval", interactions[0])
	}
	pausedEvents, err := agents[0].ListTurnEvents(context.Background(), &proto.ListAgentProviderTurnEventsRequest{
		TurnId:   "turn-2",
		AfterSeq: 0,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListTurnEvents(waiting): %v", err)
	}
	if len(pausedEvents) != 2 || pausedEvents[1].Type != "interaction.requested" {
		t.Fatalf("ListTurnEvents(waiting) = %#v, want interaction.requested", pausedEvents)
	}
	if display := pausedEvents[1].Display; display == nil || display.Kind != "interaction" || display.Phase != "requested" || display.Ref != interactions[0].ID {
		t.Fatalf("interaction.requested display = %#v, want provider-authored interaction ref %q", display, interactions[0].ID)
	}
	requestedInput, ok := pausedEvents[1].Display.Input.(map[string]any)
	if !ok || requestedInput["interaction_id"] != interactions[0].ID || requestedInput["session_id"] != "session-1" {
		t.Fatalf("interaction.requested display input = %#v, want interaction/session ids", pausedEvents[1].Display.Input)
	}
	resolvedInteraction, err := agents[0].ResolveInteraction(context.Background(), &proto.ResolveAgentProviderInteractionRequest{
		InteractionId: interactions[0].ID,
		Resolution: mustTestProtoStruct(t, map[string]any{
			"approved": true,
		}),
	})
	if err != nil {
		t.Fatalf("ResolveInteraction: %v", err)
	}
	if resolvedInteraction == nil || resolvedInteraction.State != coreagent.InteractionStateResolved || resolvedInteraction.Resolution["approved"] != true {
		t.Fatalf("resolved interaction = %#v, want resolved approved interaction", resolvedInteraction)
	}
	resolvedTurn, err := agents[0].GetTurn(context.Background(), &proto.GetAgentProviderTurnRequest{TurnId: "turn-2"})
	if err != nil {
		t.Fatalf("GetTurn(after ResolveInteraction): %v", err)
	}
	if resolvedTurn == nil || resolvedTurn.Status != coreagent.ExecutionStatusSucceeded || resolvedTurn.StatusMessage != interactions[0].ID {
		t.Fatalf("GetTurn(after ResolveInteraction) = %#v, want succeeded turn status_message=%q", resolvedTurn, interactions[0].ID)
	}
}

func TestAgentRuntimeExecuteToolRejectsHiddenOperationWithoutExactGrant(t *testing.T) {
	t.Parallel()

	hidden := false
	invoker := &recordingAgentRuntimeInvoker{}
	runGrants := newTestAgentRunGrants(t)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{
				ID:    "chat.postMessage",
				Title: "Post Message",
			},
			{
				ID:      "events.reply",
				Title:   "Reply to Event",
				Visible: &hidden,
			},
		}},
	})
	manager := agentmanager.New(agentmanager.Config{
		Providers: providers,
		RunGrants: runGrants,
		Invoker:   invoker,
	})
	runtime := &agentRuntime{
		providers: map[string]coreagent.Provider{
			"simple": &routingAgentProvider{
				getTurn: func(context.Context, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:                 "turn-1",
						SessionID:          "session-1",
						Status:             coreagent.ExecutionStatusRunning,
						CreatedBySubjectID: "user:user-123",
					}, nil
				},
			},
		},
	}
	runtime.SetInvoker(invoker)
	runtime.SetRunGrants(runGrants)
	runtime.SetToolSearcher(manager)

	modeGrant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "slack",
			Operations: []string{"chat.postMessage"},
		}},
		ToolRefs:   []coreagent.ToolRef{{App: "slack", Operation: "chat.postMessage"}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint credential mode grant: %v", err)
	}
	_, err = runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID: mustMintAgentToolID(t, runGrants, coreagent.ToolTarget{
			App:            "slack",
			Operation:      "chat.postMessage",
			CredentialMode: core.ConnectionModeNone,
		}),
		RunGrant:  modeGrant,
		Arguments: map[string]any{"text": "hello"},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("ExecuteTool forged credential mode error = %v, want ErrAuthorizationDenied", err)
	}
	if calls := invoker.Calls(); len(calls) != 0 {
		t.Fatalf("invoker calls after rejected credential mode = %#v, want none", calls)
	}

	exactTools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-123"),
	}, coreagent.ResolveToolsRequest{
		ToolRefs: []coreagent.ToolRef{{
			App:       "slack",
			Operation: "events.reply",
		}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("ResolveTools exact hidden: %v", err)
	}
	if len(exactTools) != 1 || !exactTools[0].Hidden {
		t.Fatalf("ResolveTools exact hidden = %#v, want one hidden tool", exactTools)
	}
	exactGrant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "slack",
			Operations: []string{"events.reply"},
		}},
		ToolRefs: []coreagent.ToolRef{{
			App:       "slack",
			Operation: "events.reply",
		}},
		Tools:      exactTools,
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint exact grant: %v", err)
	}
	resp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolCallID:   "call-1",
		ToolID:       exactTools[0].ID,
		RunGrant:     exactGrant,
		Arguments:    map[string]any{"eventId": "evt-1"},
	})
	if err != nil {
		t.Fatalf("ExecuteTool exact hidden: %v", err)
	}
	if resp == nil || resp.Status != http.StatusAccepted || resp.Body != `{"eventId":"evt-1"}` {
		t.Fatalf("ExecuteTool exact hidden response = %#v", resp)
	}
	calls := invoker.Calls()
	if len(calls) != 1 || calls[0].providerName != "slack" || calls[0].operation != "events.reply" {
		t.Fatalf("invoker calls = %#v, want slack events.reply once", calls)
	}
	if calls[0].idempotencyKey != "agent-tool:turn-1:call-1" {
		t.Fatalf("invoker idempotency key = %q, want agent-tool:turn-1:call-1", calls[0].idempotencyKey)
	}
	if !calls[0].toolRefsSet || len(calls[0].toolRefs) != 1 || calls[0].toolRefs[0].App != "slack" || calls[0].toolRefs[0].Operation != "events.reply" {
		t.Fatalf("invoker tool refs = set %t refs %#v, want slack events.reply", calls[0].toolRefsSet, calls[0].toolRefs)
	}

	calls = invoker.Calls()
	if len(calls) != 1 || calls[0].providerName != "slack" || calls[0].operation != "events.reply" {
		t.Fatalf("invoker calls = %#v, want slack events.reply once", calls)
	}
}

func TestAgentRuntimeExecuteToolAppliesCredentialModeAndRunAsOnlyForDelegatedTool(t *testing.T) {
	t.Parallel()

	invoker := &recordingAgentRuntimeInvoker{}
	runGrants := newTestAgentRunGrants(t)
	runtime := &agentRuntime{
		providers: map[string]coreagent.Provider{
			"simple": &routingAgentProvider{
				getTurn: func(context.Context, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:                 "turn-1",
						SessionID:          "session-1",
						Status:             coreagent.ExecutionStatusRunning,
						CreatedBySubjectID: "user:user-123",
					}, nil
				},
			},
		},
	}
	runtime.SetInvoker(invoker)
	runtime.SetRunGrants(runGrants)

	runAs := &core.RunAsSubject{
		SubjectID: "service_account:review-worker",
	}
	messageTarget := coreagent.ToolTarget{
		App:       "source",
		Operation: "messages.reply",
	}
	reviewTarget := coreagent.ToolTarget{
		App:            "target",
		Operation:      "reviews.create",
		CredentialMode: core.ConnectionModeNone,
		RunAs:          runAs,
	}
	messageToolID := mustMintAgentToolID(t, runGrants, messageTarget)
	reviewToolID := mustMintAgentToolID(t, runGrants, reviewTarget)
	runtime.SetToolSearcher(staticAgentToolResolver{tools: []coreagent.Tool{
		{ID: messageToolID, Target: messageTarget},
		{ID: reviewToolID, Target: reviewTarget},
	}})

	grant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{
			{App: "source", Operations: []string{"messages.reply"}},
			{App: "target", Operations: []string{"reviews.create"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{App: "source", Operation: "messages.reply"},
			{App: "target", Operation: "reviews.create", CredentialMode: core.ConnectionModeNone, RunAs: runAs},
		},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint runAs grant: %v", err)
	}

	if _, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID:       messageToolID,
		RunGrant:     grant,
		Arguments:    map[string]any{"eventId": "evt-1"},
	}); err != nil {
		t.Fatalf("ExecuteTool source: %v", err)
	}

	if _, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID:       reviewToolID,
		RunGrant:     grant,
		Arguments:    map[string]any{"owner": "acme", "repo": "widgets"},
	}); err != nil {
		t.Fatalf("ExecuteTool target: %v", err)
	}

	calls := invoker.Calls()
	if len(calls) != 2 {
		t.Fatalf("invoker calls = %#v, want two calls", calls)
	}
	if calls[0].providerName != "source" || calls[0].subjectID != "user:user-123" || calls[0].runAsSubjectID != "" {
		t.Fatalf("source call = %#v, want original user subject without runAs", calls[0])
	}
	if calls[0].credentialModeOverride != "" {
		t.Fatalf("source credential mode override = %q, want none", calls[0].credentialModeOverride)
	}
	if calls[1].providerName != "target" || calls[1].subjectID != runAs.SubjectID {
		t.Fatalf("target call = %#v, want delegated subject %q", calls[1], runAs.SubjectID)
	}
	if calls[1].credentialModeOverride != core.ConnectionModeNone {
		t.Fatalf("target credential mode override = %q, want %q", calls[1].credentialModeOverride, core.ConnectionModeNone)
	}
	if calls[1].agentSubjectID != "user:user-123" || calls[1].runAsSubjectID != runAs.SubjectID {
		t.Fatalf("target audit context = %#v, want original and delegated subjects", calls[1])
	}
}

func TestAgentRuntimeExecuteToolRejectsTerminalTurnGrant(t *testing.T) {
	t.Parallel()

	invoker := &recordingAgentRuntimeInvoker{}
	runGrants := newTestAgentRunGrants(t)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID:    "sync",
			Title: "Sync roadmap",
		}}},
	})
	manager := agentmanager.New(agentmanager.Config{
		Providers: providers,
		RunGrants: runGrants,
		Invoker:   invoker,
	})
	runtime := &agentRuntime{
		providers: map[string]coreagent.Provider{
			"simple": &routingAgentProvider{
				getTurn: func(context.Context, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:                 "turn-1",
						SessionID:          "session-1",
						Status:             coreagent.ExecutionStatusSucceeded,
						CreatedBySubjectID: "user:user-123",
					}, nil
				},
			},
		},
	}
	runtime.SetInvoker(invoker)
	runtime.SetRunGrants(runGrants)
	runtime.SetToolSearcher(manager)

	grant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs:   []coreagent.ToolRef{{App: "roadmap", Operation: "sync"}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint grant: %v", err)
	}
	_, err = runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID: mustMintAgentToolID(t, runGrants, coreagent.ToolTarget{
			App:       "roadmap",
			Operation: "sync",
		}),
		RunGrant:  grant,
		Arguments: map[string]any{"taskId": "task-123"},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("ExecuteTool terminal turn error = %v, want ErrAuthorizationDenied", err)
	}
	if calls := invoker.Calls(); len(calls) != 0 {
		t.Fatalf("invoker calls after terminal turn = %#v, want none", calls)
	}
}

func TestAgentRuntimeAcceptsProviderOwnedTurnIDWithExecutionRefGrant(t *testing.T) {
	t.Parallel()

	invoker := &recordingAgentRuntimeInvoker{}
	runGrants := newTestAgentRunGrants(t)
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID:    "sync",
			Title: "Sync roadmap",
		}}},
	})
	manager := agentmanager.New(agentmanager.Config{
		Providers: providers,
		RunGrants: runGrants,
		Invoker:   invoker,
	})
	runtime := &agentRuntime{
		providers: map[string]coreagent.Provider{
			"simple": &routingAgentProvider{
				getTurn: func(_ context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
					if req.GetTurnId() != "provider-turn-1" {
						t.Fatalf("GetTurn TurnID = %q, want provider-turn-1", req.GetTurnId())
					}
					return &coreagent.Turn{
						ID:                 "provider-turn-1",
						SessionID:          "session-1",
						Status:             coreagent.ExecutionStatusRunning,
						ExecutionRef:       "requested-turn-1",
						CreatedBySubjectID: "user:user-123",
					}, nil
				},
			},
		},
	}
	runtime.SetInvoker(invoker)
	runtime.SetRunGrants(runGrants)
	runtime.SetToolSearcher(manager)

	grant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "requested-turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs:   []coreagent.ToolRef{{App: "roadmap", Operation: "sync"}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint grant: %v", err)
	}
	listResp, err := runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "provider-turn-1",
		PageSize:     5,
		RunGrant:     grant,
	})
	if err != nil {
		t.Fatalf("ListTools with provider-owned turn ID: %v", err)
	}
	if len(listResp.Tools) != 1 {
		t.Fatalf("ListTools returned %d tools, want 1", len(listResp.Tools))
	}
	resp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "provider-turn-1",
		ToolID:       listResp.Tools[0].ToolID,
		RunGrant:     grant,
		Arguments:    map[string]any{"taskId": "task-123"},
	})
	if err != nil {
		t.Fatalf("ExecuteTool with provider-owned turn ID: %v", err)
	}
	if resp == nil || resp.Status != http.StatusAccepted || resp.Body != `{"taskId":"task-123"}` {
		t.Fatalf("ExecuteTool response = %#v, want accepted task body", resp)
	}

	wrongGrant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "other-requested-turn",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs:   []coreagent.ToolRef{{App: "roadmap", Operation: "sync"}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint wrong grant: %v", err)
	}
	_, err = runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "provider-turn-1",
		PageSize:     5,
		RunGrant:     wrongGrant,
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("ListTools wrong execution ref error = %v, want ErrAuthorizationDenied", err)
	}
}

func TestAgentRuntimeRejectsToolsAndConnectionsForNoneSourceBeforeResolvers(t *testing.T) {
	t.Parallel()

	runGrants := newTestAgentRunGrants(t)
	runtime := &agentRuntime{
		providers: map[string]coreagent.Provider{
			"simple": &routingAgentProvider{
				getTurn: func(_ context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:        req.GetTurnId(),
						SessionID: "session-1",
						Status:    coreagent.ExecutionStatusRunning,
					}, nil
				},
			},
		},
	}
	runtime.SetRunGrants(runGrants)

	grant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		ToolSource:   coreagent.ToolSourceModeNone,
	})
	if err != nil {
		t.Fatalf("Mint grant: %v", err)
	}

	_, err = runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		RunGrant:     grant,
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("ListTools error = %v, want authorization denied before tool searcher", err)
	}
	_, err = runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID:       "not-a-minted-tool-id",
		RunGrant:     grant,
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) || strings.Contains(err.Error(), "tool id is invalid") {
		t.Fatalf("ExecuteTool error = %v, want source denial before tool id resolution", err)
	}
	_, err = runtime.ResolveConnection(context.Background(), coreagent.ResolveConnectionRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		Connection:   "anthropic",
		RunGrant:     grant,
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) || strings.Contains(err.Error(), "credential resolver is not configured") {
		t.Fatalf("ResolveConnection error = %v, want source denial before credential resolver", err)
	}
}

func TestAgentRuntimeListsMCPCatalogToolsForGrantedTurn(t *testing.T) {
	t.Parallel()

	invoker := &recordingAgentRuntimeInvoker{}
	runGrants := newTestAgentRunGrants(t)
	readOnly := true
	roadmapProvider := &coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{
				ID:          "sync",
				Title:       "Sync roadmap",
				Description: "Sync roadmap tasks",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"}}}`),
				Annotations: catalog.CapabilityAnnotations{
					ReadOnlyHint: &readOnly,
				},
			},
			{
				ID:          "sync!",
				Title:       "Sync roadmap loudly",
				Description: "Sync roadmap tasks with a colliding MCP name",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"}}}`),
				Tags:        []string{"loud"},
			},
			{
				ID:          "sync_2",
				Title:       "Sync roadmap second",
				Description: "Sync roadmap tasks with a naturally suffixed MCP name",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"}}}`),
			},
		}},
	}
	docsOps := make([]catalog.CatalogOperation, 12)
	docsRefs := make([]coreagent.ToolRef, 12)
	docsPermissions := make([]string, 12)
	oversizedOutputSchema := json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string","description":"` + strings.Repeat("x", 129*1024) + `"}}}`)
	for i := range docsOps {
		id := fmt.Sprintf("fetch_%02d", i+1)
		docsOps[i] = catalog.CatalogOperation{
			ID:           id,
			Title:        fmt.Sprintf("Fetch docs %02d", i+1),
			Description:  "Fetch a docs page",
			InputSchema:  json.RawMessage(`{"type":"object"}`),
			OutputSchema: oversizedOutputSchema,
		}
		docsRefs[i] = coreagent.ToolRef{App: "docs", Operation: id}
		docsPermissions[i] = id
	}
	docsProvider := &coretesting.StubIntegration{
		N:        "docs",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name:       "docs",
			Operations: docsOps,
		},
	}
	providers := testutil.NewProviderRegistry(t, roadmapProvider, docsProvider)
	manager := agentmanager.New(agentmanager.Config{
		Providers: providers,
		RunGrants: runGrants,
		Invoker:   invoker,
	})
	runtime := &agentRuntime{
		providers: map[string]coreagent.Provider{
			"claude": &routingAgentProvider{
				getTurn: func(_ context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:                 req.GetTurnId(),
						SessionID:          "session-1",
						Status:             coreagent.ExecutionStatusRunning,
						CreatedBySubjectID: "user:user-123",
					}, nil
				},
			},
		},
	}
	runtime.SetInvoker(invoker)
	runtime.SetRunGrants(runGrants)
	runtime.SetToolSearcher(manager)

	grant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
			Operations: []string{"sync", "sync!", "sync_2"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{App: "roadmap", Operation: "sync"},
			{App: "roadmap", Operation: "sync!"},
			{App: "roadmap", Operation: "sync_2"},
		},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint grant: %v", err)
	}

	listResp, err := runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		PageSize:     10,
		RunGrant:     grant,
	})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if listResp.NextPageToken != "" || len(listResp.Tools) != 3 {
		t.Fatalf("ListTools response = %#v, want three tools on one final page", listResp)
	}
	names := []string{listResp.Tools[0].MCPName, listResp.Tools[1].MCPName, listResp.Tools[2].MCPName}
	if strings.Join(names, ",") != "roadmap__sync,roadmap__sync_2,roadmap__sync_2_2" {
		t.Fatalf("listed MCP names = %#v, want stable sorted unique names", names)
	}
	var tool coreagent.ListedTool
	for i := range listResp.Tools {
		if listResp.Tools[i].Ref.Operation == "sync" {
			tool = listResp.Tools[i]
			break
		}
	}
	if tool.MCPName == "" || tool.Title != "Sync roadmap" {
		t.Fatalf("listed sync tool = %#v, want Sync roadmap", tool)
	}
	if tool.InputSchemaJSON == "" || tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatalf("listed tool schema/annotations = %#v", tool)
	}
	queryResp, err := runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		PageSize:     1,
		Query:        "loud roadmap",
		RunGrant:     grant,
	})
	if err != nil {
		t.Fatalf("ListTools with query: %v", err)
	}
	if len(queryResp.Tools) != 1 || queryResp.NextPageToken != "1" || queryResp.Tools[0].Ref.Operation != "sync!" {
		t.Fatalf("ListTools query response = %#v, want first ranked page with sync!", queryResp)
	}
	if queryResp.Tools[0].MCPName != "roadmap__sync_2" {
		t.Fatalf("query-ranked MCP name = %q, want stable duplicate name roadmap__sync_2", queryResp.Tools[0].MCPName)
	}
	if strings.Join(queryResp.Tools[0].Tags, ",") != "loud" || !strings.Contains(queryResp.Tools[0].SearchText, "loud") {
		t.Fatalf("listed query metadata = tags %#v search %q", queryResp.Tools[0].Tags, queryResp.Tools[0].SearchText)
	}

	emptyGrant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
			Operations: []string{"sync"},
		}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint empty grant: %v", err)
	}
	emptyListResp, err := runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		PageSize:     10,
		RunGrant:     emptyGrant,
	})
	if err != nil {
		t.Fatalf("ListTools with empty mcp catalog grant: %v", err)
	}
	if len(emptyListResp.Tools) != 0 || emptyListResp.NextPageToken != "" {
		t.Fatalf("ListTools empty grant = %#v, want no tools", emptyListResp)
	}

	manyDocsGrant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "docs",
			Operations: docsPermissions,
		}},
		ToolRefs:   docsRefs,
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint docs grant: %v", err)
	}
	pagedDocs, err := runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		PageSize:     5,
		RunGrant:     manyDocsGrant,
	})
	if err != nil {
		t.Fatalf("ListTools with small page size: %v", err)
	}
	if len(pagedDocs.Tools) != 5 || pagedDocs.NextPageToken != "5" {
		t.Fatalf("ListTools small page = %d tools, next %q; want 5 tools and token 5", len(pagedDocs.Tools), pagedDocs.NextPageToken)
	}
	for _, listed := range pagedDocs.Tools {
		if listed.OutputSchemaJSON != "" {
			t.Fatalf("listed output schema for %q is %d bytes, want omitted oversized schema", listed.MCPName, len(listed.OutputSchemaJSON))
		}
	}
	finalDocs, err := runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		PageSize:     10000,
		PageToken:    pagedDocs.NextPageToken,
		RunGrant:     manyDocsGrant,
	})
	if err != nil {
		t.Fatalf("ListTools final large page: %v", err)
	}
	if len(finalDocs.Tools) != 7 || finalDocs.NextPageToken != "" {
		t.Fatalf("ListTools final large page = %d tools, next %q; want 7 tools and no token", len(finalDocs.Tools), finalDocs.NextPageToken)
	}
	for _, listed := range finalDocs.Tools {
		if listed.OutputSchemaJSON != "" {
			t.Fatalf("listed final output schema for %q is %d bytes, want omitted oversized schema", listed.MCPName, len(listed.OutputSchemaJSON))
		}
	}

	_, err = runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID:       tool.ToolID,
		RunGrant:     emptyGrant,
		Arguments:    map[string]any{"taskId": "task-123"},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("ExecuteTool with empty mcp catalog grant error = %v, want ErrAuthorizationDenied", err)
	}

	broadGrant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs:   []coreagent.ToolRef{{App: "roadmap"}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint broad grant: %v", err)
	}
	broadListResp, err := runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		PageSize:     10,
		RunGrant:     broadGrant,
	})
	if err != nil {
		t.Fatalf("ListTools with broad mcp catalog grant: %v", err)
	}
	if broadListResp.NextPageToken != "" || len(broadListResp.Tools) != 1 || broadListResp.Tools[0].Ref.Operation != "sync" {
		t.Fatalf("ListTools broad grant response = %#v, want sync tool", broadListResp)
	}

	execResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID:       tool.ToolID,
		RunGrant:     grant,
		Arguments:    map[string]any{"taskId": "task-123"},
	})
	if err != nil {
		t.Fatalf("ExecuteTool with mcp catalog grant: %v", err)
	}
	if execResp == nil || execResp.Status != http.StatusAccepted || execResp.Body != `{"taskId":"task-123"}` {
		t.Fatalf("ExecuteTool response = %#v, want accepted task body", execResp)
	}

	broadExecResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID:       broadListResp.Tools[0].ToolID,
		RunGrant:     broadGrant,
		Arguments:    map[string]any{"taskId": "task-123"},
	})
	if err != nil {
		t.Fatalf("ExecuteTool with broad mcp catalog grant: %v", err)
	}
	if broadExecResp == nil || broadExecResp.Status != http.StatusAccepted || broadExecResp.Body != `{"taskId":"task-123"}` {
		t.Fatalf("ExecuteTool broad response = %#v, want accepted task body", broadExecResp)
	}
}

func TestAgentRuntimeListsUnavailableMCPCatalogSentinelForBroadGrants(t *testing.T) {
	t.Parallel()

	invoker := &reconnectingAgentRuntimeInvoker{
		tokenErrs: map[string]error{
			"linear": fmt.Errorf("%w: token expired and refresh failed", invocation.ErrReconnectRequired),
		},
	}
	runGrants := newTestAgentRunGrants(t)
	githubProvider := &coretesting.StubIntegration{
		N:        "github",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID:          "issues",
			Title:       "List GitHub issues",
			Description: "List issues assigned to the current user",
		}}},
	}
	linearProvider := &failingSessionCatalogIntegration{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:    "viewer",
				Title: "Current Linear viewer",
			}}},
		},
	}
	manager := agentmanager.New(agentmanager.Config{
		Providers: testutil.NewProviderRegistry(t, githubProvider, linearProvider),
		RunGrants: runGrants,
		Invoker:   invoker,
	})
	runtime := &agentRuntime{
		providers: map[string]coreagent.Provider{
			"claude": &routingAgentProvider{
				getTurn: func(context.Context, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:                 "turn-1",
						SessionID:          "session-1",
						Status:             coreagent.ExecutionStatusRunning,
						CreatedBySubjectID: "user:user-123",
					}, nil
				},
			},
		},
	}
	runtime.SetInvoker(invoker)
	runtime.SetRunGrants(runGrants)
	runtime.SetToolSearcher(manager)

	broadGrant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{
			{App: "github", Operations: []string{"issues"}},
			{App: "linear", Operations: []string{"viewer"}},
		},
		ToolRefs:   []coreagent.ToolRef{{App: "*"}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint broad grant: %v", err)
	}
	broadListResp, err := runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		PageSize:     10,
		RunGrant:     broadGrant,
	})
	if err != nil {
		t.Fatalf("ListTools with broad grant: %v", err)
	}
	if len(broadListResp.Tools) != 2 || broadListResp.NextPageToken != "" {
		t.Fatalf("ListTools broad response = %#v, want GitHub tool plus Linear sentinel", broadListResp)
	}
	var linearSentinel coreagent.ListedTool
	for _, listed := range broadListResp.Tools {
		if listed.MCPName == "linear__reconnect_required" {
			linearSentinel = listed
		}
	}
	if linearSentinel.ToolID == "" || linearSentinel.Ref.App != "linear" || linearSentinel.Ref.Operation != "" || linearSentinel.Target.Unavailable == nil {
		t.Fatalf("Linear sentinel = %#v, want unavailable plugin-level tool", linearSentinel)
	}

	sentinelResp, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID:       linearSentinel.ToolID,
		RunGrant:     broadGrant,
	})
	if err != nil {
		t.Fatalf("ExecuteTool sentinel: %v", err)
	}
	if sentinelResp == nil || sentinelResp.Status != http.StatusFailedDependency || !strings.Contains(sentinelResp.Body, `"code":"reconnect_required"`) {
		t.Fatalf("ExecuteTool sentinel response = %#v, want reconnect error body", sentinelResp)
	}
	if calls := invoker.Calls(); len(calls) != 0 {
		t.Fatalf("ExecuteTool sentinel invoked provider = %#v, want no provider invoke", calls)
	}

	pluginGrant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "linear",
			Operations: []string{"viewer"},
		}},
		ToolRefs:   []coreagent.ToolRef{{App: "linear"}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint app grant: %v", err)
	}
	pluginListResp, err := runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		PageSize:     10,
		RunGrant:     pluginGrant,
	})
	if err != nil {
		t.Fatalf("ListTools plugin-level unavailable: %v", err)
	}
	if len(pluginListResp.Tools) != 1 || pluginListResp.Tools[0].MCPName != "linear__reconnect_required" {
		t.Fatalf("ListTools plugin-level unavailable = %#v, want only Linear sentinel", pluginListResp)
	}

	exactGrant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		Permissions: []core.AccessPermission{{
			App:        "linear",
			Operations: []string{"viewer"},
		}},
		ToolRefs:   []coreagent.ToolRef{{App: "linear", Operation: "viewer"}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint exact grant: %v", err)
	}
	_, err = runtime.ListTools(context.Background(), coreagent.ListToolsRequest{
		ProviderName: "claude",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		PageSize:     10,
		RunGrant:     exactGrant,
	})
	if !errors.Is(err, invocation.ErrReconnectRequired) {
		t.Fatalf("ListTools exact unavailable error = %v, want ErrReconnectRequired", err)
	}
}

func waitHostedAgentPoolState(t *testing.T, pool *hostedAgentProviderPool, ready func() bool) {
	t.Helper()
	if pool == nil || pool.stateChanged == nil {
		t.Fatal("hosted agent pool state notifications are unavailable")
	}
	waitOnTestCond(t, pool.stateChanged, "hosted agent pool state", ready)
}

func hostedAgentPoolReadyBackendsLocked(pool *hostedAgentProviderPool) []*hostedAgentPoolBackend {
	now := pool.nowUTC()
	ready := make([]*hostedAgentPoolBackend, 0, len(pool.backends))
	for _, backend := range pool.backends {
		if pool.backendAcceptsNewWorkLocked(backend, now) {
			ready = append(ready, backend)
		}
	}
	return ready
}

type hostedAgentPoolManualClock struct {
	mu          sync.Mutex
	cond        *sync.Cond
	now         time.Time
	tickerCount int
	waiterCount int
	waiters     []hostedAgentPoolManualClockWaiter
	tickers     []*hostedAgentPoolManualTicker
}

type hostedAgentPoolManualClockWaiter struct {
	at time.Time
	ch chan time.Time
}

type hostedAgentPoolManualTicker struct {
	clock    *hostedAgentPoolManualClock
	interval time.Duration
	next     time.Time
	ch       chan time.Time
	stopped  bool
}

func newHostedAgentPoolManualClock(now time.Time) *hostedAgentPoolManualClock {
	clock := &hostedAgentPoolManualClock{now: now}
	clock.cond = sync.NewCond(&clock.mu)
	return clock
}

func (c *hostedAgentPoolManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *hostedAgentPoolManualClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	at := c.now.Add(d)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, hostedAgentPoolManualClockWaiter{at: at, ch: ch})
	c.waiterCount++
	c.cond.Broadcast()
	return ch
}

func (c *hostedAgentPoolManualClock) NewTicker(d time.Duration) hostedAgentPoolTicker {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d <= 0 {
		d = time.Nanosecond
	}
	ticker := &hostedAgentPoolManualTicker{
		clock:    c,
		interval: d,
		next:     c.now.Add(d),
		ch:       make(chan time.Time, 1),
	}
	c.tickers = append(c.tickers, ticker)
	c.tickerCount++
	c.cond.Broadcast()
	return ticker
}

func (c *hostedAgentPoolManualClock) Sleep(d time.Duration) {
	<-c.After(d)
}

func (c *hostedAgentPoolManualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	waiters := c.waiters[:0]
	for _, waiter := range c.waiters {
		if now.Before(waiter.at) {
			waiters = append(waiters, waiter)
			continue
		}
		waiter.ch <- now
	}
	c.waiters = waiters
	for _, ticker := range c.tickers {
		if ticker.stopped {
			continue
		}
		for !now.Before(ticker.next) {
			select {
			case ticker.ch <- now:
			default:
			}
			ticker.next = ticker.next.Add(ticker.interval)
		}
	}
	c.mu.Unlock()
}

func (c *hostedAgentPoolManualClock) waitForTicker(t *testing.T) {
	t.Helper()
	c.waitFor(t, "manual clock ticker", func() bool {
		return c.tickerCount > 0
	})
}

func (c *hostedAgentPoolManualClock) waitFor(t *testing.T, description string, ready func() bool) {
	t.Helper()
	waitOnTestCond(t, c.cond, description, ready)
}

func (c *hostedAgentPoolManualClock) waiterCountSnapshot() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waiterCount
}

func (c *hostedAgentPoolManualClock) waitForWaiterAfter(t *testing.T, count int) {
	t.Helper()
	c.waitFor(t, "manual clock waiter", func() bool {
		return c.waiterCount > count
	})
}

func (t *hostedAgentPoolManualTicker) C() <-chan time.Time {
	return t.ch
}

func (t *hostedAgentPoolManualTicker) Stop() {
	t.clock.mu.Lock()
	t.stopped = true
	t.clock.mu.Unlock()
}

func hostedAgentProviderPoolForTest(t *testing.T, provider coreagent.Provider) *hostedAgentProviderPool {
	t.Helper()
	tracked, ok := provider.(*agentProviderWithTracking)
	if !ok {
		t.Fatalf("agent provider type = %T, want *agentProviderWithTracking", provider)
	}
	delegate := tracked.delegate
	if wrapper, ok := delegate.(interface{ Unwrap() coreagent.Provider }); ok {
		delegate = wrapper.Unwrap()
	}
	pool, ok := delegate.(*hostedAgentProviderPool)
	if !ok {
		t.Fatalf("tracked delegate type = %T, want *hostedAgentProviderPool", delegate)
	}
	return pool
}

func TestAgentRuntimeConfigUsesPublicAgentHostRelayBinding(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	secret := []byte("0123456789abcdef0123456789abcdef")
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	runtimeProvider := newCapturingBundleRuntime()

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: testHostedAgentRuntimeConfig(),
				},
			},
		},
	}

	runtimeState := &agentRuntime{providers: map[string]coreagent.Provider{}}
	runtimeState.SetRunGrants(newTestAgentRunGrants(t))
	deps := Deps{
		BaseURL:            relaySrv.URL,
		EncryptionKey:      secret,
		AgentRuntime:       runtimeState,
		PublicHostServices: publicHostServices,
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	agents, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	if _, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		SessionId: "session-1",
		Model:     "gpt-test",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	turn, err := agents[0].CreateTurn(context.Background(), &proto.CreateAgentProviderTurnRequest{
		TimeoutSeconds: 1,
		TurnId:         "turn-1",
		SessionId:      "session-1",
		Model:          "gpt-test",
		Output:         agentRuntimeTextOutput(),
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn == nil || turn.Output.Text == nil || turn.Output.Text.Text != `{"provider_name":"simple"}` {
		t.Fatalf("turn = %#v, want provider-only output", turn)
	}

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("start app requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].GetEnv()[runtimehost.HostServiceSocketEnv]; got != "tls://"+relaySrv.Listener.Addr().String() {
		t.Fatalf("StartApp env %s = %q, want tls relay target", runtimehost.HostServiceSocketEnv, got)
	}
	if got := startRequests[0].GetEnv()[runtimehost.HostServiceTokenEnv]; strings.TrimSpace(got) == "" {
		t.Fatalf("StartApp env missing %s: %#v", runtimehost.HostServiceTokenEnv, startRequests[0].GetEnv())
	}
}

func TestAgentRuntimeImageLaunchUsesManifestEntrypoint(t *testing.T) {
	t.Parallel()

	runtimeProvider := newCapturingBundleRuntime()
	entry := &config.ProviderEntry{
		ResolvedManifest: &providermanifestv1.Manifest{
			Kind: providermanifestv1.KindAgent,
			Entrypoint: &providermanifestv1.Entrypoint{
				ArtifactPath: "bin/gestalt-agent-simple",
				Args:         []string{"--serve"},
			},
		},
		Runtime: &config.RuntimePlacementConfig{
			Image: "ghcr.io/example/simple-agent@sha256:abc123",
			ImagePullAuth: &config.RuntimePlacementImagePullAuth{
				DockerConfigJSON: `{"auths":{"ghcr.io":{"username":"ghcr-user","password":" ghcr-token "}}}`,
			},
		},
	}

	launch, err := prepareHostedAgentProviderLaunch(context.Background(), "simple", entry, mustNode(t, map[string]any{
		"name": "simple",
	}), Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Runtime:       runtimeProvider,
	})
	if err != nil {
		t.Fatalf("prepareHostedAgentProviderLaunch: %v", err)
	}
	defer launch.close()

	if launch.launch.command != "./bin/gestalt-agent-simple" {
		t.Fatalf("agent command = %q, want manifest image entrypoint", launch.launch.command)
	}
	if !slices.Equal(launch.launch.args, []string{"--serve"}) {
		t.Fatalf("agent args = %#v, want manifest image args", launch.launch.args)
	}
}

func TestAgentRuntimeProviderEntryRuntimePlacementConfigIncludesImagePullAuth(t *testing.T) {
	t.Parallel()

	dockerConfigJSON := `{"auths":{"ghcr.io":{"username":"ghcr-user","password":" ghcr-token "}}}`
	entry := &config.ProviderEntry{
		Runtime: &config.RuntimePlacementConfig{
			Image: "ghcr.io/example/simple-agent@sha256:abc123",
			ImagePullAuth: &config.RuntimePlacementImagePullAuth{
				DockerConfigJSON: dockerConfigJSON,
			},
		},
	}

	runtimeConfig := providerEntryRuntimePlacementConfig(entry)
	if runtimeConfig.ImagePullAuth == nil {
		t.Fatal("ImagePullAuth = nil")
	}
	if runtimeConfig.ImagePullAuth.DockerConfigJSON != dockerConfigJSON {
		t.Fatalf("ImagePullAuth.DockerConfigJSON = %q, want opaque Docker config JSON preserved", runtimeConfig.ImagePullAuth.DockerConfigJSON)
	}

	entry.Runtime.ImagePullAuth.DockerConfigJSON = `{"auths":{"ghcr.io":{"username":"mutated","password":"mutated"}}}`
	if runtimeConfig.ImagePullAuth.DockerConfigJSON != dockerConfigJSON {
		t.Fatalf("ImagePullAuth.DockerConfigJSON aliasing original config = %q, want opaque Docker config JSON preserved", runtimeConfig.ImagePullAuth.DockerConfigJSON)
	}
}

func TestAgentRuntimeTemplateLaunchUsesManifestEntrypoint(t *testing.T) {
	t.Parallel()

	runtimeProvider := newCapturingBundleRuntime()
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}
	cfg := &config.Config{
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {
					Driver: config.RuntimeProviderDriver("capture"),
				},
			},
		},
	}
	entry := &config.ProviderEntry{
		ResolvedManifest: &providermanifestv1.Manifest{
			Kind: providermanifestv1.KindAgent,
			Entrypoint: &providermanifestv1.Entrypoint{
				ArtifactPath: "bin/gestalt-agent-simple",
				Args:         []string{"--serve"},
			},
		},
		Runtime: &config.RuntimePlacementConfig{
			Provider: "hosted",
			Template: "python-runtime",
		},
	}

	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	launch, err := prepareHostedAgentProviderLaunch(context.Background(), "simple", entry, mustNode(t, map[string]any{
		"name":    "simple",
		"command": "/host/only/agent",
		"args":    []string{"host-arg"},
	}), deps)
	if err != nil {
		t.Fatalf("prepareHostedAgentProviderLaunch: %v", err)
	}
	defer launch.close()

	if launch.launch.command != "./bin/gestalt-agent-simple" {
		t.Fatalf("agent command = %q, want manifest template entrypoint", launch.launch.command)
	}
	if !slices.Equal(launch.launch.args, []string{"--serve"}) {
		t.Fatalf("agent args = %#v, want manifest template args", launch.launch.args)
	}
}

func TestAgentRuntimeLocalFallbackImageLaunchUsesConfiguredCommand(t *testing.T) {
	t.Parallel()

	entry := &config.ProviderEntry{
		ResolvedManifest: &providermanifestv1.Manifest{
			Kind: providermanifestv1.KindAgent,
			Entrypoint: &providermanifestv1.Entrypoint{
				ArtifactPath: "bin/gestalt-agent-simple",
				Args:         []string{"--serve"},
			},
		},
		Runtime: &config.RuntimePlacementConfig{
			Image: "ghcr.io/example/simple-agent@sha256:abc123",
		},
	}

	launch, err := prepareHostedAgentProviderLaunch(context.Background(), "simple", entry, mustNode(t, map[string]any{
		"name":    "simple",
		"command": "/host/only/agent",
		"args":    []string{"host-arg"},
	}), Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("prepareHostedAgentProviderLaunch: %v", err)
	}
	defer launch.close()

	if launch.launch.command != "/host/only/agent" {
		t.Fatalf("agent command = %q, want configured command", launch.launch.command)
	}
	if !slices.Equal(launch.launch.args, []string{"host-arg"}) {
		t.Fatalf("agent args = %#v, want configured args", launch.launch.args)
	}
}

func TestAgentRuntimeConfigRejectsMissingHostServiceRelay(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := &staticCapabilityRuntime{
		inner: newCapturingRuntime(),
		support: &proto.RuntimeSupport{
			CanHostApps: true,
		},
	}

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (runtimeprovider.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultProvider: "hosted"},
		},
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"hosted": {Driver: config.RuntimeProviderDriver("capture")},
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"simple": {
					Command: bin,
					Runtime: testHostedAgentRuntimeConfig(),
				},
			},
		},
	}

	deps := Deps{
		AgentRuntime: &agentRuntime{providers: map[string]coreagent.Provider{}},
	}
	deps.RuntimeRegistry = newRuntimeRegistry(cfg, factories.Runtime, deps)

	_, _, err := buildAgents(context.Background(), cfg, factories, deps)
	if err == nil {
		t.Fatal("buildAgents error = nil, want host service access failure")
	}
	if got := err.Error(); got != `bootstrap: agent from resource "simple": agent provider: runtime provider "hosted" cannot provide host service access required by this provider` {
		t.Fatalf("buildAgents error = %q", got)
	}
}

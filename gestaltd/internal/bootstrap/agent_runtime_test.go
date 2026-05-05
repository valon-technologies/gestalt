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
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
	"gopkg.in/yaml.v3"
)

func buildAgentProviderBinary(t *testing.T) string {
	t.Helper()
	if sharedAgentProviderBin == "" {
		t.Fatal("shared agent provider binary not initialized")
	}
	return sharedAgentProviderBin
}

type agentRuntimeFactoryContextKey struct{}

func testHostedAgentRuntimeConfig() *config.HostedRuntimeConfig {
	return &config.HostedRuntimeConfig{
		Pool: &config.HostedRuntimePoolConfig{
			MinReadyInstances:   1,
			MaxReadyInstances:   1,
			StartupTimeout:      "5s",
			HealthCheckInterval: "1s",
			RestartPolicy:       config.HostedRuntimeRestartPolicyNever,
			DrainTimeout:        "1s",
		},
	}
}

func testAgentRuntimeIndexedDBDefs() map[string]*config.ProviderEntry {
	return map[string]*config.ProviderEntry{
		"agent_state": {
			Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
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
	i.calls = append(i.calls, agentRuntimeInvokerCall{
		providerName:           providerName,
		operation:              operation,
		params:                 cloneAnyMap(params),
		subjectID:              p.SubjectID,
		credentialModeOverride: invocation.CredentialModeOverrideFromContext(ctx),
		idempotencyKey:         invocation.IdempotencyKeyFromContext(ctx),
		agentSubjectID:         agentSubjectID,
		runAsSubjectID:         runAsSubjectID,
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

func (p *turnLookupAgentProvider) GetTurn(context.Context, coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	if p.turn == nil {
		return nil, core.ErrNotFound
	}
	return p.turn, nil
}

func TestAgentRuntimeResolveConnectionUsesAgentConnectionRuntime(t *testing.T) {
	t.Parallel()

	runtime, err := newAgentRuntime(&config.Config{})
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
				Mode:         core.ConnectionModePlatform,
				AuthConfig:   core.ExternalCredentialAuthConfig{Type: "bearer", Token: "vertex-token"},
				Params:       map[string]string{"endpoint": "https://vertex.example.invalid"},
			},
		},
	}
	externalCredentials := coretesting.NewStubExternalCredentialProvider()
	externalCredentials.ResolveCredentialFunc = func(_ context.Context, req *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
		return &core.ResolveExternalCredentialResponse{
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
	if got.Mode != core.ConnectionModePlatform {
		t.Fatalf("Mode = %q, want %q", got.Mode, core.ConnectionModePlatform)
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

func (p *listSessionsAgentProvider) ListSessions(context.Context, coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	if p.err != nil {
		return nil, p.err
	}
	return append([]*coreagent.Session(nil), p.sessions...), nil
}

type routingAgentProvider struct {
	coreagent.UnimplementedProvider
	createTurn func(context.Context, coreagent.CreateTurnRequest) (*coreagent.Turn, error)
	getTurn    func(context.Context, coreagent.GetTurnRequest) (*coreagent.Turn, error)
}

func (p *routingAgentProvider) CreateTurn(ctx context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	if p.createTurn == nil {
		return nil, core.ErrNotFound
	}
	return p.createTurn(ctx, req)
}

func (p *routingAgentProvider) GetTurn(ctx context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
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
	createReqs               []coreagent.CreateSessionRequest
	updateReqs               []coreagent.UpdateSessionRequest
	sessions                 map[string]*coreagent.Session
}

func (p *workspaceAgentProvider) CreateSession(_ context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	p.createReqs = append(p.createReqs, req)
	if p.createErr != nil {
		return nil, p.createErr
	}
	sessionID := strings.TrimSpace(p.createSessionID)
	if sessionID == "" {
		sessionID = req.SessionID
	}
	session := &coreagent.Session{
		ID:           sessionID,
		ProviderName: "simple",
		Model:        req.Model,
		State:        coreagent.SessionStateActive,
		CreatedBy:    req.CreatedBy,
	}
	if p.sessions == nil {
		p.sessions = map[string]*coreagent.Session{}
	}
	p.sessions[session.ID] = session
	cloned := *session
	return &cloned, nil
}

func (p *workspaceAgentProvider) GetSession(_ context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	session := p.sessions[strings.TrimSpace(req.SessionID)]
	if session == nil {
		return nil, core.ErrNotFound
	}
	cloned := *session
	return &cloned, nil
}

func (p *workspaceAgentProvider) GetCapabilities(context.Context, coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{SupportsPreparedWorkspace: p.supportPreparedWorkspace}, nil
}

func (p *workspaceAgentProvider) UpdateSession(_ context.Context, req coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	p.updateReqs = append(p.updateReqs, req)
	return &coreagent.Session{
		ID:           req.SessionID,
		ProviderName: "simple",
		State:        req.State,
		CreatedBy:    coreagent.Actor{SubjectID: "user:user-1"},
	}, nil
}

func (p *workspaceAgentProvider) Ping(context.Context) error {
	return nil
}

type workspaceRuntimeProvider struct {
	*pluginruntime.LocalProvider
	supportPrepareWorkspace bool
	prepareWorkspace        func(context.Context, pluginruntime.PrepareWorkspaceRequest) (*pluginruntime.PreparedWorkspace, error)
	removeWorkspaceReqs     []pluginruntime.RemoveWorkspaceRequest
}

func (p *workspaceRuntimeProvider) Support(ctx context.Context) (pluginruntime.Support, error) {
	support, err := p.LocalProvider.Support(ctx)
	if err != nil {
		return support, err
	}
	support.SupportsPrepareWorkspace = p.supportPrepareWorkspace
	return support, nil
}

func (p *workspaceRuntimeProvider) PrepareWorkspace(ctx context.Context, req pluginruntime.PrepareWorkspaceRequest) (*pluginruntime.PreparedWorkspace, error) {
	if p.prepareWorkspace != nil {
		return p.prepareWorkspace(ctx, req)
	}
	return p.LocalProvider.PrepareWorkspace(ctx, req)
}

func (p *workspaceRuntimeProvider) RemoveWorkspace(ctx context.Context, req pluginruntime.RemoveWorkspaceRequest) error {
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

	session, err := pool.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID: "agent-session-1",
		Model:     "gpt-test",
		CreatedBy: coreagent.Actor{SubjectID: "user:user-1"},
		Workspace: &coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		},
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
	if !filepath.IsAbs(providerReq.PreparedWorkspace.Root) || !filepath.IsAbs(providerReq.PreparedWorkspace.CWD) {
		t.Fatalf("prepared workspace = %#v, want absolute paths", providerReq.PreparedWorkspace)
	}
	data, err := os.ReadFile(filepath.Join(providerReq.PreparedWorkspace.CWD, "README.md"))
	if err != nil {
		t.Fatalf("read prepared checkout: %v", err)
	}
	if strings.TrimSpace(string(data)) != "workspace fixture" {
		t.Fatalf("README = %q", data)
	}
	if got := pool.sessionBackend("agent-session-1"); got == nil || got.runtimeSessionID != runtimeSession.ID {
		t.Fatalf("session backend = %#v, want runtime session %q", got, runtimeSession.ID)
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

	_, err := pool.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID: "agent-session-1",
		Workspace: &coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		},
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

	_, err := pool.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID: "agent-session-1",
		Workspace: &coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "provider create failed") {
		t.Fatalf("CreateSession error = %v, want provider create failed", err)
	}
	prepared := agentProvider.createReqs[0].PreparedWorkspace
	if prepared == nil {
		t.Fatal("provider did not receive prepared workspace before failure")
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

	first, err := pool.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID:      "agent-session-1",
		IdempotencyKey: "workspace-create-1",
		CreatedBy:      coreagent.Actor{SubjectID: "user:user-1"},
		Workspace: &coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateSession first: %v", err)
	}
	prepared := agentProvider.createReqs[0].PreparedWorkspace
	if prepared == nil {
		t.Fatal("provider did not receive prepared workspace")
	}
	runtimeProvider.prepareWorkspace = func(context.Context, pluginruntime.PrepareWorkspaceRequest) (*pluginruntime.PreparedWorkspace, error) {
		return nil, errors.New("prepare should not run for idempotent replay")
	}
	second, err := pool.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID:      "agent-session-1",
		IdempotencyKey: "workspace-create-1",
		CreatedBy:      coreagent.Actor{SubjectID: "user:user-1"},
		Workspace: &coreagent.Workspace{
			CWD: "other",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "other",
			}},
		},
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

	_, err := pool.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID:      "agent-session-1",
		IdempotencyKey: "workspace-create-1",
		CreatedBy:      coreagent.Actor{SubjectID: "user:user-1"},
		Workspace: &coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "agent provider returned session id") {
		t.Fatalf("CreateSession error = %v, want session id mismatch", err)
	}
	prepared := agentProvider.createReqs[0].PreparedWorkspace
	if prepared == nil {
		t.Fatal("provider did not receive prepared workspace before mismatch")
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
}

func TestHostedAgentPoolCleansPreparedWorkspaceAfterValidationFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := createAgentRuntimeWorkspaceRepo(t)
	runtimeProvider, runtimeSession := startWorkspaceRuntimeSession(t, ctx, true)
	runtimeProvider.prepareWorkspace = func(context.Context, pluginruntime.PrepareWorkspaceRequest) (*pluginruntime.PreparedWorkspace, error) {
		return &pluginruntime.PreparedWorkspace{Root: "/tmp/gestalt-workspace-root", CWD: "/tmp/outside-workspace"}, nil
	}
	agentProvider := &workspaceAgentProvider{supportPreparedWorkspace: true}
	pool := hostedWorkspacePoolForTest(t, agentProvider, runtimeProvider, runtimeSession, "file://"+filepath.ToSlash(repo))
	t.Cleanup(func() { _ = pool.Close() })

	_, err := pool.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID: "agent-session-1",
		Workspace: &coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		},
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

	_, err := pool.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID: "agent-session-1",
		CreatedBy: coreagent.Actor{SubjectID: "user:user-1"},
		Workspace: &coreagent.Workspace{
			CWD: "app",
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "file://" + filepath.ToSlash(repo),
				Path: "app",
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	prepared := agentProvider.createReqs[0].PreparedWorkspace
	if prepared == nil {
		t.Fatal("provider did not receive prepared workspace")
	}
	if _, err := os.Stat(prepared.Root); err != nil {
		t.Fatalf("workspace root before archive stat: %v", err)
	}
	_, err = pool.UpdateSession(ctx, coreagent.UpdateSessionRequest{
		SessionID: "agent-session-1",
		State:     coreagent.SessionStateArchived,
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

func hostedWorkspacePoolForTest(t *testing.T, agentProvider coreagent.Provider, runtimeProvider pluginruntime.Provider, runtimeSession *pluginruntime.Session, allowedRepos ...string) *hostedAgentProviderPool {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	return &hostedAgentProviderPool{
		name: "simple",
		launch: &hostedAgentProviderLaunch{
			name: "simple",
			runtimeConfig: config.EffectiveHostedRuntime{
				Workspace: &config.HostedRuntimeWorkspaceConfig{
					PrepareTimeout: "10s",
					Git: &config.HostedRuntimeWorkspaceGitConfig{
						AllowedRepositories: allowedRepos,
					},
				},
			},
		},
		policy:              config.HostedRuntimeLifecyclePolicy{MaxReadyInstances: 1},
		ctx:                 ctx,
		cancel:              cancel,
		backends:            []*hostedAgentPoolBackend{{id: 1, provider: agentProvider, runtimeProvider: runtimeProvider, runtimeSessionID: runtimeSession.ID, runtimeSession: runtimeSession, liveTurns: map[string]struct{}{}}},
		sessionBackends:     map[string]*hostedAgentPoolBackend{},
		turnBackends:        map[string]*hostedAgentPoolBackend{},
		interactionBackends: map[string]*hostedAgentPoolBackend{},
	}
}

func startWorkspaceRuntimeSession(t *testing.T, ctx context.Context, supportsWorkspace bool) (*workspaceRuntimeProvider, *pluginruntime.Session) {
	t.Helper()
	runtimeProvider := &workspaceRuntimeProvider{
		LocalProvider:           pluginruntime.NewLocalProvider(),
		supportPrepareWorkspace: supportsWorkspace,
	}
	session, err := runtimeProvider.StartSession(ctx, pluginruntime.StartSessionRequest{PluginName: "agent"})
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
	runtime.FailProvider("canary")
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
}

func TestAgentRuntimeConfigSelectedProviderStartsSessionWithRuntimeFields(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := newCapturingPluginRuntime()
	ctxSentinel := &struct{}{}
	var factoryContextValue any

	factories := NewFactoryRegistry()
	factories.Runtime = func(ctx context.Context, _ string, _ *config.RuntimeProviderEntry, _ Deps) (pluginruntime.Provider, error) {
		factoryContextValue = ctx.Value(agentRuntimeFactoryContextKey{})
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Template = "python-dev"
	runtimeConfig.Image = "ghcr.io/valon/gestalt-python-runtime:latest"
	runtimeConfig.ImagePullAuth = &config.HostedRuntimeImagePullAuth{
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
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: runtimeConfig},
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
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)
	buildCtx := context.WithValue(context.Background(), agentRuntimeFactoryContextKey{}, ctxSentinel)
	agents, err := buildAgents(buildCtx, cfg, factories, deps)
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
	if req.PluginName != "simple" {
		t.Fatalf("StartSession PluginName = %q, want simple", req.PluginName)
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
	if req.ImagePullAuth.DockerConfigJSON != `{"auths":{"ghcr.io":{"username":"ghcr-user","password":"ghcr-token"}}}` {
		t.Fatalf("StartSession ImagePullAuth.DockerConfigJSON = %q", req.ImagePullAuth.DockerConfigJSON)
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
	runtimeProvider := newCapturingPluginRuntime()
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.MinReadyInstances = 2
	runtimeConfig.Pool.MaxReadyInstances = 2
	runtimeConfig.Pool.DrainTimeout = "2s"
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: runtimeConfig},
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
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, err := buildAgents(context.Background(), cfg, factories, deps)
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
		session, err := agents[0].CreateSession(context.Background(), coreagent.CreateSessionRequest{
			SessionID: sessionID,
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
	turn, err := agents[0].CreateTurn(context.Background(), coreagent.CreateTurnRequest{
		TurnID:    "turn-1",
		SessionID: "session-1",
		Model:     "gpt-test",
		Metadata: map[string]any{
			"requireInteraction": true,
		},
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
	drainDone := make(chan error, 1)
	go func() {
		drainDone <- pool.drainAndCloseBackend(sessionBackend)
	}()
	waitForAgentRuntimeCondition(t, 2*time.Second, func() bool {
		pool.mu.Lock()
		defer pool.mu.Unlock()
		return sessionBackend.closing
	})
	sessions, err := agents[0].ListSessions(context.Background(), coreagent.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions(during drain): %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListSessions(during drain) = %d sessions, want 2", len(sessions))
	}
	fetched, err := agents[0].GetTurn(context.Background(), coreagent.GetTurnRequest{TurnID: "turn-1"})
	if err != nil {
		t.Fatalf("GetTurn(during drain): %v", err)
	}
	if fetched == nil || fetched.ID != "turn-1" || fetched.Status != coreagent.ExecutionStatusWaitingForInput {
		t.Fatalf("GetTurn(during drain) = %#v, want waiting turn-1", fetched)
	}
	interactions, err := agents[0].ListInteractions(context.Background(), coreagent.ListInteractionsRequest{TurnID: "turn-1"})
	if err != nil {
		t.Fatalf("ListInteractions(during drain): %v", err)
	}
	if len(interactions) != 1 {
		t.Fatalf("ListInteractions(during drain) = %d interactions, want 1", len(interactions))
	}
	if _, err := agents[0].ResolveInteraction(context.Background(), coreagent.ResolveInteractionRequest{
		InteractionID: interactions[0].ID,
		Resolution:    map[string]any{"approved": true},
	}); err != nil {
		t.Fatalf("ResolveInteraction(during drain): %v", err)
	}
	resolved, err := agents[0].GetTurn(context.Background(), coreagent.GetTurnRequest{TurnID: "turn-1"})
	if err != nil {
		t.Fatalf("GetTurn(after ResolveInteraction): %v", err)
	}
	if resolved == nil || resolved.Status != coreagent.ExecutionStatusSucceeded {
		t.Fatalf("GetTurn(after ResolveInteraction) = %#v, want succeeded turn", resolved)
	}
	select {
	case err := <-drainDone:
		if err != nil {
			t.Fatalf("drainAndCloseBackend: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drainAndCloseBackend did not finish after live turn completed")
	}
}

func TestAgentRuntimeConfigScalesOutHostedAgentWarmPool(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := newCapturingPluginRuntime()
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.MaxReadyInstances = 2
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: runtimeConfig},
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
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, err := buildAgents(context.Background(), cfg, factories, deps)
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

	session, err := agents[0].CreateSession(context.Background(), coreagent.CreateSessionRequest{
		SessionID: "scale-out-session",
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
	waitForAgentRuntimeCondition(t, 2*time.Second, func() bool {
		return len(runtimeProvider.startSessionRequests()) == 2 && len(pool.readyBackends()) == 2
	})

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
	if _, err := agents[0].CreateSession(context.Background(), coreagent.CreateSessionRequest{
		SessionID: "max-capped-session",
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
	runtimeProvider := newCapturingPluginRuntime()
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.HealthCheckInterval = "50ms"
	runtimeConfig.Pool.RestartPolicy = config.HostedRuntimeRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					IndexedDB: &config.HostIndexedDBBindingConfig{Provider: "agent_state"},
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: runtimeConfig},
				},
			},
		},
	}
	deps := Deps{
		BaseURL:          "https://gestalt.example.test",
		EncryptionKey:    []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:     &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:    testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, err := buildAgents(context.Background(), cfg, factories, deps)
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
	if err := backends[0].provider.Close(); err != nil {
		t.Fatalf("Close backend provider: %v", err)
	}
	waitForAgentRuntimeCondition(t, 2*time.Second, func() bool {
		return len(runtimeProvider.startSessionRequests()) >= 2 && len(pool.readyBackends()) == 1
	})
	session, err := agents[0].CreateSession(context.Background(), coreagent.CreateSessionRequest{
		SessionID: "session-after-restart",
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
	runtimeProvider := newCapturingPluginRuntime()
	var drainMu sync.Mutex
	var firstDrainAt time.Time
	runtimeProvider.lifecycleForSession = func(index int) *pluginruntime.SessionLifecycle {
		startedAt := time.Now().UTC()
		expiresAt := startedAt.Add(time.Hour)
		lifecycle := &pluginruntime.SessionLifecycle{
			StartedAt: &startedAt,
			ExpiresAt: &expiresAt,
		}
		if index == 1 {
			recommendedDrainAt := startedAt.Add(500 * time.Millisecond)
			lifecycle.RecommendedDrainAt = &recommendedDrainAt
			drainMu.Lock()
			firstDrainAt = recommendedDrainAt
			drainMu.Unlock()
		}
		return lifecycle
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.MaxReadyInstances = 2
	runtimeConfig.Pool.HealthCheckInterval = "25ms"
	runtimeConfig.Pool.RestartPolicy = config.HostedRuntimeRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					IndexedDB: &config.HostIndexedDBBindingConfig{Provider: "agent_state"},
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: runtimeConfig},
				},
			},
		},
	}
	deps := Deps{
		BaseURL:          "https://gestalt.example.test",
		EncryptionKey:    []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:     &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:    testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, err := buildAgents(context.Background(), cfg, factories, deps)
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
	waitForAgentRuntimeCondition(t, 3*time.Second, func() bool {
		ready := pool.readyBackends()
		return len(runtimeProvider.startSessionRequests()) >= 2 && len(ready) == 1 && ready[0] != first
	})
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
	session, err := agents[0].CreateSession(context.Background(), coreagent.CreateSessionRequest{
		SessionID: "session-after-runtime-drain",
		Model:     "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession(after runtime drain): %v", err)
	}
	if session == nil || session.ID != "session-after-runtime-drain" {
		t.Fatalf("CreateSession(after runtime drain) = %#v, want session-after-runtime-drain", session)
	}
}

//nolint:paralleltest // Uses short lifecycle timing assertions that are flaky under parallel package load.
func TestAgentRuntimeConfigKeepsHostedAgentServingWhenProactiveReplacementStartFails(t *testing.T) {
	bin := buildAgentProviderBinary(t)
	runtimeProvider := newCapturingPluginRuntime()
	runtimeProvider.lifecycleForSession = func(index int) *pluginruntime.SessionLifecycle {
		startedAt := time.Now().UTC()
		expiresAt := startedAt.Add(time.Hour)
		lifecycle := &pluginruntime.SessionLifecycle{
			StartedAt: &startedAt,
			ExpiresAt: &expiresAt,
		}
		if index == 1 {
			recommendedDrainAt := startedAt.Add(3 * time.Second)
			lifecycle.RecommendedDrainAt = &recommendedDrainAt
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
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.MaxReadyInstances = 2
	runtimeConfig.Pool.StartupTimeout = "5s"
	runtimeConfig.Pool.HealthCheckInterval = "25ms"
	runtimeConfig.Pool.RestartPolicy = config.HostedRuntimeRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					IndexedDB: &config.HostIndexedDBBindingConfig{Provider: "agent_state"},
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: runtimeConfig},
				},
			},
		},
	}
	deps := Deps{
		BaseURL:          "https://gestalt.example.test",
		EncryptionKey:    []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:     &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:    testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, err := buildAgents(context.Background(), cfg, factories, deps)
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
	waitForAgentRuntimeCondition(t, 2*time.Second, func() bool {
		pool.mu.Lock()
		defer pool.mu.Unlock()
		return len(runtimeProvider.startSessionRequests()) >= 2 && !first.replacing
	})
	pool.mu.Lock()
	acceptsNewWork := pool.backendAcceptsNewWorkLocked(first, time.Now().UTC())
	firstDraining := first.draining
	pool.mu.Unlock()
	if !acceptsNewWork || firstDraining {
		t.Fatalf("first backend acceptsNewWork=%v draining=%v, want serving after failed proactive replacement", acceptsNewWork, firstDraining)
	}
	session, err := agents[0].CreateSession(context.Background(), coreagent.CreateSessionRequest{
		SessionID: "session-after-failed-proactive-replacement",
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

//nolint:paralleltest // Uses short lifecycle timing assertions that are flaky under parallel package load.
func TestAgentRuntimeConfigProactiveReplacementRespectsMaxReadyInstances(t *testing.T) {
	bin := buildAgentProviderBinary(t)
	runtimeProvider := newCapturingPluginRuntime()
	releaseReplacement := make(chan struct{})
	replacementStarted := make(chan struct{})
	var replacementStartedOnce sync.Once
	runtimeProvider.lifecycleForSession = func(index int) *pluginruntime.SessionLifecycle {
		startedAt := time.Now().UTC()
		expiresAt := startedAt.Add(time.Hour)
		lifecycle := &pluginruntime.SessionLifecycle{
			StartedAt: &startedAt,
			ExpiresAt: &expiresAt,
		}
		if index <= 2 {
			recommendedDrainAt := startedAt.Add(500 * time.Millisecond)
			lifecycle.RecommendedDrainAt = &recommendedDrainAt
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
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.MinReadyInstances = 2
	runtimeConfig.Pool.MaxReadyInstances = 3
	runtimeConfig.Pool.HealthCheckInterval = "25ms"
	runtimeConfig.Pool.RestartPolicy = config.HostedRuntimeRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					IndexedDB: &config.HostIndexedDBBindingConfig{Provider: "agent_state"},
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: runtimeConfig},
				},
			},
		},
	}
	deps := Deps{
		BaseURL:          "https://gestalt.example.test",
		EncryptionKey:    []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:     &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:    testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, err := buildAgents(context.Background(), cfg, factories, deps)
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
	if ready := pool.readyBackends(); len(ready) != 2 {
		t.Fatalf("ready backends = %d, want 2", len(ready))
	}
	select {
	case <-replacementStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("proactive replacement did not start")
	}
	time.Sleep(150 * time.Millisecond)
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

//nolint:paralleltest // Uses short lifecycle timing assertions that are flaky under parallel package load.
func TestAgentRuntimeConfigDoesNotImmediatelyChurnWhenExpiryReserveExceedsRuntimeLifetime(t *testing.T) {
	bin := buildAgentProviderBinary(t)
	runtimeProvider := newCapturingPluginRuntime()
	runtimeProvider.lifecycleForSession = func(index int) *pluginruntime.SessionLifecycle {
		expiresAt := time.Now().UTC().Add(5 * time.Minute)
		return &pluginruntime.SessionLifecycle{
			ExpiresAt: &expiresAt,
		}
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.StartupTimeout = "5m"
	runtimeConfig.Pool.DrainTimeout = "2m"
	runtimeConfig.Pool.HealthCheckInterval = "25ms"
	runtimeConfig.Pool.RestartPolicy = config.HostedRuntimeRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					IndexedDB: &config.HostIndexedDBBindingConfig{Provider: "agent_state"},
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: runtimeConfig},
				},
			},
		},
	}
	deps := Deps{
		BaseURL:          "https://gestalt.example.test",
		EncryptionKey:    []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:     &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:    testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	time.Sleep(150 * time.Millisecond)
	if got := len(runtimeProvider.startSessionRequests()); got != 1 {
		t.Fatalf("start session requests after expiry health checks = %d, want 1", got)
	}
}

func TestAgentRuntimeConfigReplacesExpiresOnlyRuntimeBeforeExpiry(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := newCapturingPluginRuntime()
	var expiryMu sync.Mutex
	var firstExpiresAt time.Time
	runtimeProvider.lifecycleForSession = func(index int) *pluginruntime.SessionLifecycle {
		expiresAt := time.Now().UTC().Add(time.Hour)
		if index == 1 {
			expiresAt = time.Now().UTC().Add(2 * time.Second)
			expiryMu.Lock()
			firstExpiresAt = expiresAt
			expiryMu.Unlock()
		}
		return &pluginruntime.SessionLifecycle{
			ExpiresAt: &expiresAt,
		}
	}
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}
	runtimeConfig := testHostedAgentRuntimeConfig()
	runtimeConfig.Pool.StartupTimeout = "5m"
	runtimeConfig.Pool.DrainTimeout = "2m"
	runtimeConfig.Pool.HealthCheckInterval = "25ms"
	runtimeConfig.Pool.RestartPolicy = config.HostedRuntimeRestartPolicyAlways
	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					IndexedDB: &config.HostIndexedDBBindingConfig{Provider: "agent_state"},
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: runtimeConfig},
				},
			},
		},
	}
	deps := Deps{
		BaseURL:          "https://gestalt.example.test",
		EncryptionKey:    []byte("0123456789abcdef0123456789abcdef"),
		AgentRuntime:     &agentRuntime{providers: map[string]coreagent.Provider{}},
		IndexedDBDefs:    testAgentRuntimeIndexedDBDefs(),
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) { return &coretesting.StubIndexedDB{}, nil },
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)
	agents, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	waitForAgentRuntimeCondition(t, 3*time.Second, func() bool {
		return len(runtimeProvider.startSessionRequests()) >= 2
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

	sessions, err := pool.ListSessions(context.Background(), coreagent.ListSessionsRequest{})
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
			createTurn: func(context.Context, coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
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
			createTurn: func(_ context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
				secondCalls++
				return &coreagent.Turn{
					ID:        req.TurnID,
					SessionID: req.SessionID,
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

	turn, err := pool.CreateTurn(context.Background(), coreagent.CreateTurnRequest{
		TurnID:    "turn-1",
		SessionID: "session-1",
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
			getTurn: func(context.Context, coreagent.GetTurnRequest) (*coreagent.Turn, error) {
				firstCalls++
				return nil, context.DeadlineExceeded
			},
		},
		liveTurns: map[string]struct{}{"turn-1": {}},
	}
	second := &hostedAgentPoolBackend{
		id: 2,
		provider: &routingAgentProvider{
			getTurn: func(_ context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
				secondCalls++
				return &coreagent.Turn{
					ID:        req.TurnID,
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

	turn, err := pool.GetTurn(context.Background(), coreagent.GetTurnRequest{TurnID: "turn-1"})
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
			getTurn: func(context.Context, coreagent.GetTurnRequest) (*coreagent.Turn, error) {
				firstCalls++
				return nil, core.ErrNotFound
			},
		},
		liveTurns: map[string]struct{}{"turn-1": {}},
	}
	second := &hostedAgentPoolBackend{
		id: 2,
		provider: &routingAgentProvider{
			getTurn: func(_ context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
				secondCalls++
				return &coreagent.Turn{
					ID:        req.TurnID,
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

	turn, err := pool.GetTurn(context.Background(), coreagent.GetTurnRequest{TurnID: "turn-1"})
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

	sessions, err := pool.ListSessions(context.Background(), coreagent.ListSessionsRequest{})
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
	capturingRuntime := newCapturingPluginRuntime()

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return capturingRuntime, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: testHostedAgentRuntimeConfig()},
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
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	agents, err := buildAgents(context.Background(), cfg, factories, deps)
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
	capabilities, err := agents[0].GetCapabilities(context.Background(), coreagent.GetCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if capabilities == nil || !capabilities.Interactions || !capabilities.ResumableTurns {
		t.Fatalf("capabilities = %#v, want interactions+resumable_turns", capabilities)
	}

	session, err := agents[0].CreateSession(context.Background(), coreagent.CreateSessionRequest{
		SessionID:      "session-1",
		IdempotencyKey: "session-req-1",
		Model:          "gpt-test",
		ClientRef:      "cli-session-1",
		Metadata: map[string]any{
			"source": "agent-runtime-test",
		},
		CreatedBy: coreagent.Actor{SubjectID: "user:user-123"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session == nil || session.ID != "session-1" || session.ProviderName != "simple" || session.State != coreagent.SessionStateActive {
		t.Fatalf("CreateSession = %#v, want active simple session-1", session)
	}

	updatedSession, err := agents[0].UpdateSession(context.Background(), coreagent.UpdateSessionRequest{
		SessionID: "session-1",
		ClientRef: "cli-session-2",
		State:     coreagent.SessionStateArchived,
		Metadata: map[string]any{
			"source": "agent-runtime-test-updated",
		},
	})
	if err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if updatedSession == nil || updatedSession.ClientRef != "cli-session-2" || updatedSession.State != coreagent.SessionStateArchived {
		t.Fatalf("UpdateSession = %#v, want archived cli-session-2", updatedSession)
	}

	sessions, err := agents[0].ListSessions(context.Background(), coreagent.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "session-1" {
		t.Fatalf("ListSessions = %#v, want session-1", sessions)
	}

	fetchedSession, err := agents[0].GetSession(context.Background(), coreagent.GetSessionRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if fetchedSession == nil || fetchedSession.Metadata["source"] != "agent-runtime-test-updated" {
		t.Fatalf("GetSession = %#v, want updated source metadata", fetchedSession)
	}

	turn, err := agents[0].CreateTurn(context.Background(), coreagent.CreateTurnRequest{
		TurnID:       "turn-1",
		SessionID:    "session-1",
		Model:        "gpt-test",
		Messages:     []coreagent.Message{{Role: "user", Text: "Plan it"}},
		ExecutionRef: "exec-turn-1",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn == nil || turn.ID != "turn-1" || turn.SessionID != "session-1" || turn.ProviderName != "simple" {
		t.Fatalf("CreateTurn = %#v, want simple turn-1 on session-1", turn)
	}

	turns, err := agents[0].ListTurns(context.Background(), coreagent.ListTurnsRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(turns) != 1 || turns[0].ID != "turn-1" {
		t.Fatalf("ListTurns = %#v, want turn-1", turns)
	}

	fetchedTurn, err := agents[0].GetTurn(context.Background(), coreagent.GetTurnRequest{TurnID: "turn-1"})
	if err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if fetchedTurn == nil || fetchedTurn.Status != coreagent.ExecutionStatusSucceeded || fetchedTurn.OutputText == "" {
		t.Fatalf("GetTurn = %#v, want succeeded turn with output", fetchedTurn)
	}

	turnEvents, err := agents[0].ListTurnEvents(context.Background(), coreagent.ListTurnEventsRequest{
		TurnID:   "turn-1",
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

	postTurnSession, err := agents[0].GetSession(context.Background(), coreagent.GetSessionRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetSession(after CreateTurn): %v", err)
	}
	if postTurnSession == nil || postTurnSession.ClientRef != "cli-session-2" {
		t.Fatalf("GetSession(after CreateTurn) = %#v, want preserved client_ref cli-session-2", postTurnSession)
	}

	wantRelayTarget := "tls://" + relaySrv.Listener.Addr().String()
	startRequests := capturingRuntime.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("StartPlugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env[agentservice.DefaultHostSocketEnv]; got != wantRelayTarget {
		t.Fatalf("agent host relay target = %q, want %q", got, wantRelayTarget)
	}
	if got := startRequests[0].Env[agentservice.HostSocketTokenEnv()]; strings.TrimSpace(got) == "" {
		t.Fatalf("StartPlugin env missing %s", agentservice.HostSocketTokenEnv())
	}
	if got := startRequests[0].Env[runtimehost.DefaultRuntimeLogHostSocketEnv]; got != wantRelayTarget {
		t.Fatalf("runtime log host relay target = %q, want %q", got, wantRelayTarget)
	}
	if got := startRequests[0].Env[runtimehost.DefaultRuntimeLogHostSocketEnv+"_TOKEN"]; strings.TrimSpace(got) == "" {
		t.Fatalf("StartPlugin env missing %s_TOKEN", runtimehost.DefaultRuntimeLogHostSocketEnv)
	}

	pausedTurn, err := agents[0].CreateTurn(context.Background(), coreagent.CreateTurnRequest{
		TurnID:    "turn-2",
		SessionID: "session-1",
		Model:     "gpt-test",
		CreatedBy: coreagent.Actor{SubjectID: "user:user-123"},
		Metadata: map[string]any{
			"requireInteraction": true,
		},
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
	if err := json.Unmarshal([]byte(pausedTurn.OutputText), &pausedOutput); err != nil {
		t.Fatalf("json.Unmarshal(pausedTurn.OutputText): %v", err)
	}
	if !pausedOutput.InteractionRequested || strings.TrimSpace(pausedOutput.InteractionID) == "" || pausedOutput.InteractionError != "" {
		t.Fatalf("paused turn output = %+v", pausedOutput)
	}
	interactions, err := agents[0].ListInteractions(context.Background(), coreagent.ListInteractionsRequest{TurnID: "turn-2"})
	if err != nil {
		t.Fatalf("ListInteractions: %v", err)
	}
	if len(interactions) != 1 {
		t.Fatalf("interactions = %d, want 1", len(interactions))
	}
	if interactions[0].Type != coreagent.InteractionTypeApproval || interactions[0].State != coreagent.InteractionStatePending {
		t.Fatalf("interaction = %#v, want pending approval", interactions[0])
	}
	pausedEvents, err := agents[0].ListTurnEvents(context.Background(), coreagent.ListTurnEventsRequest{
		TurnID:   "turn-2",
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
	resolvedInteraction, err := agents[0].ResolveInteraction(context.Background(), coreagent.ResolveInteractionRequest{
		InteractionID: interactions[0].ID,
		Resolution: map[string]any{
			"approved": true,
		},
	})
	if err != nil {
		t.Fatalf("ResolveInteraction: %v", err)
	}
	if resolvedInteraction == nil || resolvedInteraction.State != coreagent.InteractionStateResolved || resolvedInteraction.Resolution["approved"] != true {
		t.Fatalf("resolved interaction = %#v, want resolved approved interaction", resolvedInteraction)
	}
	resolvedTurn, err := agents[0].GetTurn(context.Background(), coreagent.GetTurnRequest{TurnID: "turn-2"})
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
				getTurn: func(context.Context, coreagent.GetTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:        "turn-1",
						SessionID: "session-1",
						Status:    coreagent.ExecutionStatusRunning,
						CreatedBy: coreagent.Actor{SubjectID: "user:user-123"},
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "slack",
			Operations: []string{"chat.postMessage"},
		}},
		ToolRefs:   []coreagent.ToolRef{{Plugin: "slack", Operation: "chat.postMessage"}},
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
			Plugin:         "slack",
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
			Plugin:    "slack",
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "slack",
			Operations: []string{"events.reply"},
		}},
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "slack",
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

	calls = invoker.Calls()
	if len(calls) != 1 || calls[0].providerName != "slack" || calls[0].operation != "events.reply" {
		t.Fatalf("invoker calls = %#v, want slack events.reply once", calls)
	}
}

func TestAgentRuntimeExecuteToolAppliesRunAsOnlyForDelegatedTool(t *testing.T) {
	t.Parallel()

	invoker := &recordingAgentRuntimeInvoker{}
	runGrants := newTestAgentRunGrants(t)
	providers := testutil.NewProviderRegistry(t,
		&coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:    "events.reply",
				Title: "Reply",
			}}},
		},
		&coretesting.StubIntegration{
			N:        "github",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:    "bot.createPullRequest",
				Title: "Create pull request",
			}}},
		},
	)
	manager := agentmanager.New(agentmanager.Config{
		Providers: providers,
		RunGrants: runGrants,
		Invoker:   invoker,
	})
	runtime := &agentRuntime{
		providers: map[string]coreagent.Provider{
			"simple": &routingAgentProvider{
				getTurn: func(context.Context, coreagent.GetTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:        "turn-1",
						SessionID: "session-1",
						Status:    coreagent.ExecutionStatusRunning,
						CreatedBy: coreagent.Actor{SubjectID: "user:user-123"},
					}, nil
				},
			},
		},
	}
	runtime.SetInvoker(invoker)
	runtime.SetRunGrants(runGrants)
	runtime.SetToolSearcher(manager)

	runAs := &core.RunAsSubject{
		SubjectID:   "service_account:github_app_installation:99:repo:acme/widgets",
		SubjectKind: "service_account",
		AuthSource:  "github_app_webhook",
	}
	grant, err := runGrants.Mint(agentgrant.Grant{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		SubjectKind:  string(principal.KindUser),
		DisplayName:  "Hugh",
		AuthSource:   "slack",
		Permissions: []core.AccessPermission{
			{Plugin: "slack", Operations: []string{"events.reply"}},
			{Plugin: "github", Operations: []string{"bot.createPullRequest"}},
		},
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "slack", Operation: "events.reply"},
			{Plugin: "github", Operation: "bot.createPullRequest", RunAs: runAs},
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
		ToolID: mustMintAgentToolID(t, runGrants, coreagent.ToolTarget{
			Plugin:    "slack",
			Operation: "events.reply",
		}),
		RunGrant:  grant,
		Arguments: map[string]any{"eventId": "evt-1"},
	}); err != nil {
		t.Fatalf("ExecuteTool slack: %v", err)
	}

	if _, err := runtime.ExecuteTool(context.Background(), coreagent.ExecuteToolRequest{
		ProviderName: "simple",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		ToolID: mustMintAgentToolID(t, runGrants, coreagent.ToolTarget{
			Plugin:    "github",
			Operation: "bot.createPullRequest",
			RunAs:     runAs,
		}),
		RunGrant:  grant,
		Arguments: map[string]any{"owner": "acme", "repo": "widgets"},
	}); err != nil {
		t.Fatalf("ExecuteTool github: %v", err)
	}

	calls := invoker.Calls()
	if len(calls) != 2 {
		t.Fatalf("invoker calls = %#v, want two calls", calls)
	}
	if calls[0].providerName != "slack" || calls[0].subjectID != "user:user-123" || calls[0].runAsSubjectID != "" {
		t.Fatalf("slack call = %#v, want original user subject without runAs", calls[0])
	}
	if calls[1].providerName != "github" || calls[1].subjectID != runAs.SubjectID {
		t.Fatalf("github call = %#v, want delegated subject %q", calls[1], runAs.SubjectID)
	}
	if calls[1].agentSubjectID != "user:user-123" || calls[1].runAsSubjectID != runAs.SubjectID {
		t.Fatalf("github audit context = %#v, want original and delegated subjects", calls[1])
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
				getTurn: func(context.Context, coreagent.GetTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:        "turn-1",
						SessionID: "session-1",
						Status:    coreagent.ExecutionStatusSucceeded,
						CreatedBy: coreagent.Actor{SubjectID: "user:user-123"},
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs:   []coreagent.ToolRef{{Plugin: "roadmap", Operation: "sync"}},
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
			Plugin:    "roadmap",
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
				getTurn: func(_ context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
					if req.TurnID != "provider-turn-1" {
						t.Fatalf("GetTurn TurnID = %q, want provider-turn-1", req.TurnID)
					}
					return &coreagent.Turn{
						ID:           "provider-turn-1",
						SessionID:    "session-1",
						Status:       coreagent.ExecutionStatusRunning,
						ExecutionRef: "requested-turn-1",
						CreatedBy:    coreagent.Actor{SubjectID: "user:user-123"},
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs:   []coreagent.ToolRef{{Plugin: "roadmap", Operation: "sync"}},
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs:   []coreagent.ToolRef{{Plugin: "roadmap", Operation: "sync"}},
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
				Annotations: catalog.OperationAnnotations{
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
		docsRefs[i] = coreagent.ToolRef{Plugin: "docs", Operation: id}
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
				getTurn: func(_ context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:        req.TurnID,
						SessionID: "session-1",
						Status:    coreagent.ExecutionStatusRunning,
						CreatedBy: coreagent.Actor{SubjectID: "user:user-123"},
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync", "sync!", "sync_2"},
		}},
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "roadmap", Operation: "sync"},
			{Plugin: "roadmap", Operation: "sync!"},
			{Plugin: "roadmap", Operation: "sync_2"},
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "docs",
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		}},
		ToolRefs:   []coreagent.ToolRef{{Plugin: "roadmap"}},
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
			ConnMode: core.ConnectionModeUser,
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
				getTurn: func(context.Context, coreagent.GetTurnRequest) (*coreagent.Turn, error) {
					return &coreagent.Turn{
						ID:        "turn-1",
						SessionID: "session-1",
						Status:    coreagent.ExecutionStatusRunning,
						CreatedBy: coreagent.Actor{SubjectID: "user:user-123"},
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{
			{Plugin: "github", Operations: []string{"issues"}},
			{Plugin: "linear", Operations: []string{"viewer"}},
		},
		ToolRefs:   []coreagent.ToolRef{{Plugin: "*"}},
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
	if linearSentinel.ToolID == "" || linearSentinel.Ref.Plugin != "linear" || linearSentinel.Ref.Operation != "" || linearSentinel.Target.Unavailable == nil {
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "linear",
			Operations: []string{"viewer"},
		}},
		ToolRefs:   []coreagent.ToolRef{{Plugin: "linear"}},
		ToolSource: coreagent.ToolSourceModeMCPCatalog,
	})
	if err != nil {
		t.Fatalf("Mint plugin grant: %v", err)
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
		SubjectKind:  string(principal.KindUser),
		Permissions: []core.AccessPermission{{
			Plugin:     "linear",
			Operations: []string{"viewer"},
		}},
		ToolRefs:   []coreagent.ToolRef{{Plugin: "linear", Operation: "viewer"}},
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

func waitForAgentRuntimeCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if fn() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("condition was not satisfied before timeout")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func hostedAgentProviderPoolForTest(t *testing.T, provider coreagent.Provider) *hostedAgentProviderPool {
	t.Helper()
	if withCleanup, ok := provider.(*agentProviderWithCleanup); ok {
		provider = withCleanup.Provider
	}
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

//nolint:paralleltest // Hosted public-relay startup is serialized to avoid Linux CI contention.
func TestAgentRuntimeConfigUsesPublicAgentHostRelayBinding(t *testing.T) {
	// This exercises the hosted agent startup path over the public relay and is
	// sensitive to Linux CI contention when it runs alongside the other hosted
	// runtime bootstrap tests.

	bin := buildAgentProviderBinary(t)
	secret := []byte("0123456789abcdef0123456789abcdef")
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	relaySrv := httptest.NewUnstartedServer(newRuntimeRelayTestHandler(t, secret, publicHostServices))
	relaySrv.EnableHTTP2 = true
	relaySrv.StartTLS()
	testutil.CloseOnCleanup(t, relaySrv)

	runtimeProvider := newCapturingBundlePluginRuntime()

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: testHostedAgentRuntimeConfig()},
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
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	agents, err := buildAgents(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	defer func() {
		if err := closeAgents(agents...); err != nil {
			t.Fatalf("closeAgents: %v", err)
		}
	}()

	if _, err := agents[0].CreateSession(context.Background(), coreagent.CreateSessionRequest{
		SessionID: "session-1",
		Model:     "gpt-test",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	turn, err := agents[0].CreateTurn(context.Background(), coreagent.CreateTurnRequest{
		TurnID:    "turn-1",
		SessionID: "session-1",
		Model:     "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn == nil || turn.OutputText != `{"provider_name":"simple"}` {
		t.Fatalf("turn = %#v, want provider-only output", turn)
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 1 {
		t.Fatalf("start plugin requests = %d, want 1", len(startRequests))
	}
	if got := startRequests[0].Env[agentservice.DefaultHostSocketEnv]; got != "tls://"+relaySrv.Listener.Addr().String() {
		t.Fatalf("StartPlugin env %s = %q, want tls relay target", agentservice.DefaultHostSocketEnv, got)
	}
	if got := startRequests[0].Env[agentservice.HostSocketTokenEnv()]; strings.TrimSpace(got) == "" {
		t.Fatalf("StartPlugin env missing %s_TOKEN: %#v", agentservice.DefaultHostSocketEnv, startRequests[0].Env)
	}
}

func TestAgentRuntimeImageLaunchUsesManifestEntrypoint(t *testing.T) {
	t.Parallel()

	runtimeProvider := newCapturingBundlePluginRuntime()
	entry := &config.ProviderEntry{
		ResolvedManifest: &providermanifestv1.Manifest{
			Kind: providermanifestv1.KindAgent,
			Entrypoint: &providermanifestv1.Entrypoint{
				ArtifactPath: "bin/gestalt-agent-simple",
				Args:         []string{"--serve"},
			},
		},
		Execution: &config.ExecutionConfig{
			Mode: config.ExecutionModeHosted,
			Runtime: &config.HostedRuntimeConfig{
				Image: "ghcr.io/example/simple-agent@sha256:abc123",
				ImagePullAuth: &config.HostedRuntimeImagePullAuth{
					DockerConfigJSON: `{"auths":{"ghcr.io":{"username":"ghcr-user","password":" ghcr-token "}}}`,
				},
			},
		},
	}

	launch, err := prepareHostedAgentProviderLaunch(context.Background(), "simple", entry, mustNode(t, map[string]any{
		"name": "simple",
	}), Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		PluginRuntime: runtimeProvider,
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

func TestAgentRuntimeProviderEntryHostedRuntimeConfigIncludesImagePullAuth(t *testing.T) {
	t.Parallel()

	dockerConfigJSON := `{"auths":{"ghcr.io":{"username":"ghcr-user","password":" ghcr-token "}}}`
	entry := &config.ProviderEntry{
		Execution: &config.ExecutionConfig{
			Mode: config.ExecutionModeHosted,
			Runtime: &config.HostedRuntimeConfig{
				Image: "ghcr.io/example/simple-agent@sha256:abc123",
				ImagePullAuth: &config.HostedRuntimeImagePullAuth{
					DockerConfigJSON: dockerConfigJSON,
				},
			},
		},
	}

	runtimeConfig := providerEntryHostedRuntimeConfig(entry)
	if runtimeConfig.ImagePullAuth == nil {
		t.Fatal("ImagePullAuth = nil")
	}
	if runtimeConfig.ImagePullAuth.DockerConfigJSON != dockerConfigJSON {
		t.Fatalf("ImagePullAuth.DockerConfigJSON = %q, want opaque Docker config JSON preserved", runtimeConfig.ImagePullAuth.DockerConfigJSON)
	}

	entry.Execution.Runtime.ImagePullAuth.DockerConfigJSON = `{"auths":{"ghcr.io":{"username":"mutated","password":"mutated"}}}`
	if runtimeConfig.ImagePullAuth.DockerConfigJSON != dockerConfigJSON {
		t.Fatalf("ImagePullAuth.DockerConfigJSON aliasing original config = %q, want opaque Docker config JSON preserved", runtimeConfig.ImagePullAuth.DockerConfigJSON)
	}
}

func TestAgentRuntimeTemplateLaunchUsesManifestEntrypoint(t *testing.T) {
	t.Parallel()

	runtimeProvider := newCapturingBundlePluginRuntime()
	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
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
		Execution: &config.ExecutionConfig{
			Mode: config.ExecutionModeHosted,
			Runtime: &config.HostedRuntimeConfig{
				Provider: "hosted",
				Template: "python-runtime",
			},
		},
	}

	deps := Deps{
		BaseURL:       "https://gestalt.example.test",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

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
		Execution: &config.ExecutionConfig{
			Mode: config.ExecutionModeHosted,
			Runtime: &config.HostedRuntimeConfig{
				Image: "ghcr.io/example/simple-agent@sha256:abc123",
			},
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

func TestAgentRuntimeConfigRejectsMissingHostServiceAccess(t *testing.T) {
	t.Parallel()

	bin := buildAgentProviderBinary(t)
	runtimeProvider := &staticCapabilityPluginRuntime{
		inner: newCapturingPluginRuntime(),
		support: pluginruntime.Support{
			CanHostPlugins: true,
		},
	}

	factories := NewFactoryRegistry()
	factories.Runtime = func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
		return runtimeProvider, nil
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Runtime: config.ServerRuntimeConfig{DefaultHostedProvider: "hosted"},
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
					Execution: &config.ExecutionConfig{Mode: config.ExecutionModeHosted, Runtime: testHostedAgentRuntimeConfig()},
				},
			},
		},
	}

	deps := Deps{
		AgentRuntime: &agentRuntime{providers: map[string]coreagent.Provider{}},
	}
	deps.PluginRuntimeRegistry = newPluginRuntimeRegistry(cfg, factories.Runtime, deps)

	_, err := buildAgents(context.Background(), cfg, factories, deps)
	if err == nil {
		t.Fatal("buildAgents error = nil, want host service access failure")
	}
	if got := err.Error(); got != `bootstrap: agent from resource "simple": agent provider: runtime provider "hosted" cannot provide host service access required by this provider` {
		t.Fatalf("buildAgents error = %q", got)
	}
}

package bootstrap

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type funcInvoker struct {
	invoke func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
}

func (f funcInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	return f.invoke(ctx, p, providerName, instance, operation, params)
}

type workflowRuntimeExecutionRefProvider struct {
	startupTestWorkflowProvider
	refs map[string]*coreworkflow.ExecutionReference
	err  error
}

func newWorkflowRuntimeExecutionRefProvider() *workflowRuntimeExecutionRefProvider {
	return &workflowRuntimeExecutionRefProvider{refs: map[string]*coreworkflow.ExecutionReference{}}
}

func (p *workflowRuntimeExecutionRefProvider) PutExecutionReference(_ context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.refs == nil {
		p.refs = map[string]*coreworkflow.ExecutionReference{}
	}
	stored := cloneRuntimeExecutionRef(ref)
	p.refs[stored.ID] = stored
	return cloneRuntimeExecutionRef(stored), nil
}

func (p *workflowRuntimeExecutionRefProvider) GetExecutionReference(_ context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	if p.err != nil {
		return nil, p.err
	}
	ref := p.refs[id]
	if ref == nil {
		return nil, core.ErrNotFound
	}
	return cloneRuntimeExecutionRef(ref), nil
}

func (p *workflowRuntimeExecutionRefProvider) ListExecutionReferences(_ context.Context, subjectID string) ([]*coreworkflow.ExecutionReference, error) {
	if p.err != nil {
		return nil, p.err
	}
	out := make([]*coreworkflow.ExecutionReference, 0, len(p.refs))
	for _, ref := range p.refs {
		if subjectID != "" && ref.SubjectID != subjectID {
			continue
		}
		out = append(out, cloneRuntimeExecutionRef(ref))
	}
	return out, nil
}

func cloneRuntimeExecutionRef(ref *coreworkflow.ExecutionReference) *coreworkflow.ExecutionReference {
	if ref == nil {
		return nil
	}
	clone := *ref
	clone.Target.Input = cloneMapAny(ref.Target.Input)
	clone.Permissions = append([]core.AccessPermission(nil), ref.Permissions...)
	for i := range clone.Permissions {
		clone.Permissions[i].Operations = append([]string(nil), ref.Permissions[i].Operations...)
	}
	if ref.CreatedAt != nil {
		createdAt := ref.CreatedAt.UTC()
		clone.CreatedAt = &createdAt
	}
	if ref.RevokedAt != nil {
		revokedAt := ref.RevokedAt.UTC()
		clone.RevokedAt = &revokedAt
	}
	return &clone
}

func cloneMapAny(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

type workflowRuntimeAgentManagerStub struct {
	agentmanager.Service
	createSessionRequests []coreagent.ManagerCreateSessionRequest
	createTurnRequests    []coreagent.ManagerCreateTurnRequest
	cancelTurnIDs         []string
	returnNilTurn         bool
}

func (m *workflowRuntimeAgentManagerStub) CreateSession(_ context.Context, _ *principal.Principal, req coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error) {
	m.createSessionRequests = append(m.createSessionRequests, req)
	return &coreagent.Session{
		ID:           "session-1",
		ProviderName: req.ProviderName,
		Model:        req.Model,
		State:        coreagent.SessionStateActive,
	}, nil
}

func (m *workflowRuntimeAgentManagerStub) CreateTurn(_ context.Context, _ *principal.Principal, req coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error) {
	m.createTurnRequests = append(m.createTurnRequests, req)
	if m.returnNilTurn {
		return nil, nil
	}
	return &coreagent.Turn{
		ID:           "turn-1",
		SessionID:    req.SessionID,
		ProviderName: "managed",
		Model:        req.Model,
		Status:       coreagent.ExecutionStatusSucceeded,
		OutputText:   "turn completed",
	}, nil
}

func (m *workflowRuntimeAgentManagerStub) CancelTurn(_ context.Context, _ *principal.Principal, turnID, _ string) (*coreagent.Turn, error) {
	m.cancelTurnIDs = append(m.cancelTurnIDs, turnID)
	return &coreagent.Turn{ID: turnID, Status: coreagent.ExecutionStatusCanceled}, nil
}

type workflowRoundTripProvider struct {
	workflowContext map[string]any
}

func (p *workflowRoundTripProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}
func (p *workflowRoundTripProvider) Name() string        { return "workflow-roundtrip" }
func (p *workflowRoundTripProvider) DisplayName() string { return "Workflow Round Trip" }
func (p *workflowRoundTripProvider) Description() string { return "workflow round trip test provider" }
func (p *workflowRoundTripProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeNone
}
func (p *workflowRoundTripProvider) AuthTypes() []string { return nil }
func (p *workflowRoundTripProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *workflowRoundTripProvider) CredentialFields() []core.CredentialFieldDef {
	return nil
}
func (p *workflowRoundTripProvider) DiscoveryConfig() *core.DiscoveryConfig { return nil }
func (p *workflowRoundTripProvider) ConnectionForOperation(string) string   { return "" }
func (p *workflowRoundTripProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name:        "workflow-roundtrip",
		DisplayName: "Workflow Round Trip",
		Description: "workflow round trip test provider",
		Operations: []catalog.CatalogOperation{
			{ID: "sync", Method: http.MethodPost},
		},
	}
}
func (p *workflowRoundTripProvider) Execute(ctx context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	p.workflowContext = invocation.WorkflowContextFromContext(ctx)
	return &core.OperationResult{Status: http.StatusAccepted, Body: `{"ok":true}`}, nil
}

func newWorkflowRoundTripClient(t *testing.T, server proto.IntegrationProviderServer) proto.IntegrationProviderClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	proto.RegisterIntegrationProviderServer(srv, server)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return proto.NewIntegrationProviderClient(conn)
}

func TestWorkflowRuntimeInvokeMergesConfiguredAndPerRunInput(t *testing.T) {
	t.Parallel()

	scheduledFor := time.Date(2026, time.April, 15, 12, 30, 0, 0, time.UTC)
	roundTripProvider := &workflowRoundTripProvider{}
	roundTripClient := newWorkflowRoundTripClient(t, providerhost.NewProviderServer(roundTripProvider))
	roundTripRemote, err := providerhost.NewRemoteProvider(context.Background(), roundTripClient, providerhost.StaticProviderSpec{
		Name:           "workflow-roundtrip",
		DisplayName:    "Workflow Round Trip",
		Description:    "workflow round trip test provider",
		ConnectionMode: core.ConnectionModeNone,
		Catalog: &catalog.Catalog{
			Name:        "workflow-roundtrip",
			DisplayName: "Workflow Round Trip",
			Description: "workflow round trip test provider",
			Operations: []catalog.CatalogOperation{
				{ID: "sync", Method: http.MethodPost},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewRemoteProvider: %v", err)
	}
	t.Cleanup(func() {
		if closer, ok := roundTripRemote.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})
	runtime := &workflowRuntime{}

	var gotPrincipal *principal.Principal
	var gotProvider string
	var gotInstance string
	var gotConnection string
	var gotOperation string
	var gotParams map[string]any
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, p *principal.Principal, providerName, instance string, operation string, params map[string]any) (*core.OperationResult, error) {
			gotPrincipal = p
			gotProvider = providerName
			gotInstance = instance
			gotConnection = invocation.ConnectionFromContext(ctx)
			gotOperation = operation
			gotParams = params
			ctx = principal.WithPrincipal(ctx, p)
			return roundTripRemote.Execute(ctx, operation, params, "")
		},
	})

	req := coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-123",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Connection: "analytics",
			Instance:   "tenant-a",
			Input: map[string]any{
				"mode":   "full",
				"source": "scheduled",
			},
		},
		Input: map[string]any{
			"source": "event",
			"taskId": "task-456",
		},
		Metadata: map[string]any{
			"attempt": 2,
		},
		CreatedBy: coreworkflow.Actor{
			SubjectID:   principal.UserSubjectID("user-123"),
			SubjectKind: string(principal.KindUser),
			DisplayName: "Ada",
			AuthSource:  principal.SourceAPIToken.String(),
		},
		Trigger: coreworkflow.RunTrigger{
			Schedule: &coreworkflow.ScheduleTrigger{
				ScheduleID:   "sched-1",
				ScheduledFor: &scheduledFor,
			},
		},
	}
	configPermissions := principal.CompilePermissions(workflowExecutionRefPermissionsForTarget(req.Target))
	configPrincipal := principal.Canonicalize(&principal.Principal{
		SubjectID:           "system:config",
		CredentialSubjectID: "system:config",
		Scopes:              principal.PermissionPlugins(configPermissions),
		TokenPermissions:    configPermissions,
	})
	resp, err := runtime.Invoke(principal.WithPrincipal(context.Background(), configPrincipal), req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusAccepted || resp.Body != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}
	if gotPrincipal == nil || gotPrincipal.SubjectID != "system:config" {
		t.Fatalf("principal = %#v", gotPrincipal)
	}
	if gotPrincipal.CredentialSubjectID != "system:config" {
		t.Fatalf("credential subject = %q, want %q", gotPrincipal.CredentialSubjectID, "system:config")
	}
	if !principal.AllowsOperationPermission(gotPrincipal, "roadmap", "sync") {
		t.Fatalf("principal operation permissions = %#v, want roadmap.sync", gotPrincipal.TokenPermissions)
	}
	if gotProvider != "roadmap" {
		t.Fatalf("provider = %q, want %q", gotProvider, "roadmap")
	}
	if gotInstance != "" {
		t.Fatalf("instance = %q, want empty instance for workload invocations", gotInstance)
	}
	if gotConnection != "" {
		t.Fatalf("connection = %q, want empty connection for workload invocations", gotConnection)
	}
	if gotOperation != "sync" {
		t.Fatalf("operation = %q, want %q", gotOperation, "sync")
	}
	if gotParams["mode"] != "full" {
		t.Fatalf("mode = %#v, want %q", gotParams["mode"], "full")
	}
	if gotParams["source"] != "event" {
		t.Fatalf("source = %#v, want %q", gotParams["source"], "event")
	}
	if gotParams["taskId"] != "task-456" {
		t.Fatalf("taskId = %#v, want %q", gotParams["taskId"], "task-456")
	}
	if roundTripProvider.workflowContext == nil {
		t.Fatal("workflow context = nil")
	}
	if roundTripProvider.workflowContext["runId"] != "run-123" || roundTripProvider.workflowContext["provider"] != "temporal" {
		t.Fatalf("workflow context ids = %#v", roundTripProvider.workflowContext)
	}
	metadata, ok := roundTripProvider.workflowContext["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("workflow metadata = %#v", roundTripProvider.workflowContext["metadata"])
	}
	if got := metadata["attempt"]; got != 2 && got != float64(2) {
		t.Fatalf("workflow metadata attempt = %#v", got)
	}
	createdBy, ok := roundTripProvider.workflowContext["createdBy"].(map[string]any)
	if !ok || createdBy["subjectId"] != principal.UserSubjectID("user-123") || createdBy["authSource"] != principal.SourceAPIToken.String() {
		t.Fatalf("workflow createdBy = %#v", roundTripProvider.workflowContext["createdBy"])
	}
	target, ok := roundTripProvider.workflowContext["target"].(map[string]any)
	if !ok || target["pluginName"] != "roadmap" || target["operation"] != "sync" {
		t.Fatalf("workflow target = %#v", roundTripProvider.workflowContext["target"])
	}
	if got := target["connection"]; got != "analytics" {
		t.Fatalf("workflow target connection = %#v, want %q", got, "analytics")
	}
	if got := target["instance"]; got != "tenant-a" {
		t.Fatalf("workflow target instance = %#v, want %q", got, "tenant-a")
	}
	trigger, ok := roundTripProvider.workflowContext["trigger"].(map[string]any)
	if !ok || trigger["kind"] != "schedule" || trigger["scheduleId"] != "sched-1" {
		t.Fatalf("workflow trigger = %#v", roundTripProvider.workflowContext["trigger"])
	}
	if got := trigger["scheduledFor"]; got != scheduledFor.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("scheduledFor = %#v, want %q", got, scheduledFor.UTC().Format(time.RFC3339Nano))
	}
}

func TestWorkflowRuntimeInvokeAgentTargetCreatesAndSupervisesTurn(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	reg := registry.New()
	if err := reg.Providers.Register("roadmap", &coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{
				{ID: "sync", Method: http.MethodPost},
			},
		},
	}); err != nil {
		t.Fatalf("Register roadmap: %v", err)
	}
	agentProvider := newStubAgentTurnManagerProvider()
	agentRuntime := &agentRuntime{
		defaultProviderName: "managed",
		providers:           map[string]coreagent.Provider{"managed": agentProvider},
	}
	agentRuntime.SetRunMetadata(services.AgentRunMetadata)
	manager := agentmanager.New(agentmanager.Config{
		Providers:       &reg.Providers,
		Agent:           agentRuntime,
		SessionMetadata: services.AgentSessions,
		RunMetadata:     services.AgentRunMetadata,
	})
	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{},
	}
	runtime.SetAgentManager(manager)

	req := coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-agent-123",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName:   "managed",
			Model:          "deep",
			Prompt:         "Send the status summary",
			ToolSource:     coreagent.ToolSourceModeNativeSearch,
			ToolRefs:       []coreagent.ToolRef{{Plugin: "roadmap", Operation: "sync"}},
			TimeoutSeconds: 5,
		}},
		Trigger: coreworkflow.RunTrigger{Manual: true},
	}
	p := principal.Canonicalize(&principal.Principal{
		SubjectID:           principal.UserSubjectID("ada"),
		CredentialSubjectID: principal.UserSubjectID("ada"),
		TokenPermissions: principal.CompilePermissions([]core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		}}),
	})

	resp, err := runtime.Invoke(principal.WithPrincipal(context.Background(), p), req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusOK || resp.Body != "turn completed" {
		t.Fatalf("response = %#v", resp)
	}
	if len(agentProvider.createSessionRequests) != 1 {
		t.Fatalf("session requests = %d, want 1", len(agentProvider.createSessionRequests))
	}
	if got := agentProvider.createSessionRequests[0].IdempotencyKey; got != "workflow:temporal:run-agent-123:session" {
		t.Fatalf("session idempotency key = %q", got)
	}
	if len(agentProvider.createTurnRequests) != 1 {
		t.Fatalf("turn requests = %d, want 1", len(agentProvider.createTurnRequests))
	}
	turnReq := agentProvider.createTurnRequests[0]
	if got := turnReq.IdempotencyKey; got != "workflow:temporal:run-agent-123:turn" {
		t.Fatalf("turn idempotency key = %q", got)
	}
	if len(turnReq.Messages) != 1 || turnReq.Messages[0].Text != "Send the status summary" {
		t.Fatalf("turn messages = %#v", turnReq.Messages)
	}
	if len(turnReq.Tools) != 0 {
		t.Fatalf("turn tools = %#v, want no preloaded tools", turnReq.Tools)
	}
	if turnReq.ToolSource != coreagent.ToolSourceModeNativeSearch {
		t.Fatalf("turn tool source = %q, want native search", turnReq.ToolSource)
	}
	if len(turnReq.ToolRefs) != 1 || turnReq.ToolRefs[0].Plugin != "roadmap" || turnReq.ToolRefs[0].Operation != "sync" {
		t.Fatalf("turn tool refs = %#v", turnReq.ToolRefs)
	}
}

func TestWorkflowRuntimeInvokeAgentTargetWithExecutionRefIgnoresWhitespaceLegacyPluginFields(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{
		PluginName: " \t",
		Operation:  "\n",
		Connection: " ",
		Instance:   "\t",
		Agent: &coreworkflow.AgentTarget{
			ProviderName:   "managed",
			Model:          "deep",
			Prompt:         "Send the status summary",
			ToolSource:     coreagent.ToolSourceModeNativeSearch,
			TimeoutSeconds: 5,
		},
	}
	fingerprint, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		t.Fatalf("TargetFingerprint: %v", err)
	}
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                "agent-ref",
		ProviderName:      "temporal",
		Target:            target,
		TargetFingerprint: fingerprint,
		SubjectID:         principal.WorkloadSubjectID("scheduler"),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}
	agentManager := &workflowRuntimeAgentManagerStub{}
	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	runtime.SetAgentManager(agentManager)

	resp, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "agent-ref",
		RunID:        "run-agent-123",
		Target:       target,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusOK || resp.Body != "turn completed" {
		t.Fatalf("response = %#v", resp)
	}
	if len(agentManager.createTurnRequests) != 1 {
		t.Fatalf("turn requests = %d, want 1", len(agentManager.createTurnRequests))
	}
}

func TestWorkflowRuntimeInvokeAgentTargetHandlesMissingTurn(t *testing.T) {
	t.Parallel()

	agentManager := &workflowRuntimeAgentManagerStub{returnNilTurn: true}
	runtime := &workflowRuntime{}
	runtime.SetAgentManager(agentManager)
	p := principal.Canonicalize(&principal.Principal{
		SubjectID:           principal.UserSubjectID("ada"),
		CredentialSubjectID: principal.UserSubjectID("ada"),
	})

	_, err := runtime.Invoke(principal.WithPrincipal(context.Background(), p), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-agent-123",
		Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
			ProviderName:   "managed",
			Model:          "deep",
			Prompt:         "Send the status summary",
			ToolSource:     coreagent.ToolSourceModeNativeSearch,
			TimeoutSeconds: 5,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow agent turn is missing") {
		t.Fatalf("Invoke error = %v, want missing turn error", err)
	}
	if len(agentManager.cancelTurnIDs) != 0 {
		t.Fatalf("cancel turn IDs = %#v, want none for missing turn", agentManager.cancelTurnIDs)
	}
}

func TestWorkflowRuntimeRejectsMixedAgentPluginTargetWithLegacyExecutionRef(t *testing.T) {
	t.Parallel()

	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "legacy-ref",
		ProviderName: "temporal",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
		},
		SubjectID: principal.WorkloadSubjectID("scheduler"),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	_, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "legacy-ref",
		RunID:        "run-123",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Agent: &coreworkflow.AgentTarget{
				ProviderName:   "managed",
				Prompt:         "send reminder",
				ToolSource:     coreagent.ToolSourceModeNativeSearch,
				TimeoutSeconds: 5,
			},
		},
	})
	if err == nil {
		t.Fatal("Invoke mixed agent/plugin target succeeded, want error")
	}
}

func TestWorkflowRuntimeRejectsAgentExecutionRefWithoutFingerprint(t *testing.T) {
	t.Parallel()

	refProvider := newWorkflowRuntimeExecutionRefProvider()
	target := coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
		ProviderName: "managed",
		Prompt:       "send reminder",
		ToolSource:   coreagent.ToolSourceModeNativeSearch,
	}}
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "agent-ref-without-fingerprint",
		ProviderName: "temporal",
		Target:       target,
		SubjectID:    principal.WorkloadSubjectID("scheduler"),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	_, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "agent-ref-without-fingerprint",
		RunID:        "run-123",
		Target:       target,
	})
	if err == nil {
		t.Fatal("Invoke agent target without execution-ref fingerprint succeeded, want error")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefUsesStoredHumanPrincipalAndSelectors(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user, err := services.Users.FindOrCreateUser(context.Background(), "ada@example.test")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "exec-ref-123",
		ProviderName: "temporal",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
		SubjectID:           principal.UserSubjectID(user.ID),
		CredentialSubjectID: principal.WorkloadSubjectID("workflow-credential"),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}

	var gotPrincipal *principal.Principal
	var gotProvider string
	var gotInstance string
	var gotConnection string
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
			gotPrincipal = p
			gotProvider = providerName
			gotInstance = instance
			gotConnection = invocation.ConnectionFromContext(ctx)
			if operation != "sync" {
				t.Fatalf("operation = %q, want %q", operation, "sync")
			}
			if params["taskId"] != "task-123" {
				t.Fatalf("params = %#v", params)
			}
			return &core.OperationResult{Status: http.StatusAccepted, Body: `{"ok":true}`}, nil
		},
	})

	resp, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "exec-ref-123",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Connection: "analytics",
			Instance:   "tenant-a",
			Input: map[string]any{
				"mode": "full",
			},
		},
		Input: map[string]any{
			"taskId": "task-123",
		},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusAccepted || resp.Body != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}
	if gotPrincipal == nil || gotPrincipal.Kind != principal.KindUser || gotPrincipal.UserID != user.ID || gotPrincipal.SubjectID != principal.UserSubjectID(user.ID) {
		t.Fatalf("principal = %#v", gotPrincipal)
	}
	if gotPrincipal.CredentialSubjectID != principal.WorkloadSubjectID("workflow-credential") {
		t.Fatalf("credential subject = %q, want %q", gotPrincipal.CredentialSubjectID, principal.WorkloadSubjectID("workflow-credential"))
	}
	if gotProvider != "roadmap" {
		t.Fatalf("provider = %q, want %q", gotProvider, "roadmap")
	}
	if gotInstance != "tenant-a" {
		t.Fatalf("instance = %q, want %q", gotInstance, "tenant-a")
	}
	if gotConnection != "analytics" {
		t.Fatalf("connection = %q, want %q", gotConnection, "analytics")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefUsesStoredWorkloadPrincipal(t *testing.T) {
	t.Parallel()

	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "exec-ref-workload",
		ProviderName: "temporal",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
		},
		SubjectID: principal.WorkloadSubjectID("scheduler"),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}

	var gotPrincipal *principal.Principal
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
			gotPrincipal = p
			return &core.OperationResult{Status: http.StatusAccepted, Body: `{"ok":true}`}, nil
		},
	})

	if _, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "exec-ref-workload",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
		},
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if gotPrincipal == nil {
		t.Fatal("principal = nil")
	}
	if gotPrincipal.Kind != principal.KindWorkload {
		t.Fatalf("principal kind = %q, want %q", gotPrincipal.Kind, principal.KindWorkload)
	}
	if gotPrincipal.SubjectID != principal.WorkloadSubjectID("scheduler") {
		t.Fatalf("subjectID = %q, want %q", gotPrincipal.SubjectID, principal.WorkloadSubjectID("scheduler"))
	}
}

func TestWorkflowRuntimeInvokeExecutionRefRechecksAuthorizationThroughBroker(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user, err := services.Users.FindOrCreateUser(context.Background(), "ada@example.test")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "exec-ref-denied",
		ProviderName: "temporal",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
		SubjectID: principal.UserSubjectID(user.ID),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}
	if err := services.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
		SubjectID:   principal.UserSubjectID(user.ID),
		Integration: "roadmap",
		Connection:  "analytics",
		Instance:    "tenant-a",
		AccessToken: "user-token",
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	providers := registry.New()
	executed := false
	if err := providers.Providers.Register("roadmap", &coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeUser,
		CatalogVal: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{
				{ID: "sync", Method: http.MethodPost},
			},
		},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			executed = true
			return &core.OperationResult{Status: http.StatusAccepted, Body: `{"ok":true}`}, nil
		},
	}); err != nil {
		t.Fatalf("Register provider: %v", err)
	}

	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.SubjectPolicyDef{
			"roadmap-policy": {
				Default: "deny",
				Members: []config.SubjectPolicyMemberDef{
					{SubjectID: "user:other-user", Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"roadmap": {AuthorizationPolicy: "roadmap-policy"},
	})

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	runtime.SetInvoker(invocation.NewBroker(&providers.Providers, services.Users, services.ExternalCredentials, invocation.WithAuthorizer(authz)))

	_, err = runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "exec-ref-denied",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
	})
	if err == nil {
		t.Fatal("expected authorization error, got nil")
	}
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("error = %v, want ErrAuthorizationDenied", err)
	}
	if executed {
		t.Fatal("expected provider execution to be skipped")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefPreservesTokenPermissionCeiling(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	ctx := context.Background()

	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(ctx, &coreworkflow.ExecutionReference{
		ID:           "exec-ref-123",
		ProviderName: "basic",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "export",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
		SubjectID: principal.UserSubjectID("user-123"),
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		}},
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	providers := registry.New()
	executed := false
	if err := providers.Providers.Register("roadmap", &coretesting.StubIntegration{
		N: "roadmap",
		CatalogVal: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{
				{ID: "export", Method: http.MethodPost},
			},
		},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			executed = true
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	}); err != nil {
		t.Fatalf("Register provider: %v", err)
	}

	broker := invocation.NewBroker(&providers.Providers, services.Users, services.ExternalCredentials)
	runtime := &workflowRuntime{
		invoker:   broker,
		providers: map[string]coreworkflow.Provider{"basic": refProvider},
	}

	_, err := runtime.Invoke(ctx, coreworkflow.InvokeOperationRequest{
		ProviderName: "basic",
		RunID:        "run-123",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "export",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
		ExecutionRef: "exec-ref-123",
	})
	if !errors.Is(err, invocation.ErrScopeDenied) {
		t.Fatalf("Invoke error = %v, want scope denied", err)
	}
	if executed {
		t.Fatal("expected provider Execute not to run when execution-ref permissions do not allow the operation")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefLookupInfrastructureErrorIsInternal(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("boom")
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	refProvider.err = lookupErr
	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"basic": refProvider},
	}
	runtime.SetInvoker(funcInvoker{
		invoke: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
			t.Fatal("invoke should not be called when execution-ref lookup fails")
			return nil, nil
		},
	})

	_, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "basic",
		ExecutionRef: "exec-ref-123",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
		},
	})
	if err == nil {
		t.Fatal("expected internal error, got nil")
	}
	if !errors.Is(err, invocation.ErrInternal) {
		t.Fatalf("error = %v, want ErrInternal", err)
	}
	if errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("error = %v, should not be ErrAuthorizationDenied", err)
	}
}

func TestWorkflowTriggerContextPrefersScheduleOverManual(t *testing.T) {
	t.Parallel()

	scheduledFor := time.Date(2026, time.April, 15, 12, 30, 0, 0, time.UTC)
	trigger := workflowTriggerContext(coreworkflow.RunTrigger{
		Manual: true,
		Schedule: &coreworkflow.ScheduleTrigger{
			ScheduleID:   "sched-1",
			ScheduledFor: &scheduledFor,
		},
	})
	if trigger == nil {
		t.Fatal("trigger context = nil")
	}
	if got := trigger["kind"]; got != "schedule" {
		t.Fatalf("trigger kind = %#v, want %q", got, "schedule")
	}
	if got := trigger["scheduleId"]; got != "sched-1" {
		t.Fatalf("scheduleId = %#v, want %q", got, "sched-1")
	}
}

func TestWorkflowTriggerContextIncludesEventMetadataInLowerCamelCase(t *testing.T) {
	t.Parallel()

	eventTime := time.Date(2026, time.April, 15, 13, 45, 0, 0, time.UTC)
	trigger := workflowTriggerContext(coreworkflow.RunTrigger{
		Event: &coreworkflow.EventTriggerInvocation{
			TriggerID: "trigger-1",
			Event: coreworkflow.Event{
				ID:              "evt-1",
				Source:          "urn:test",
				SpecVersion:     "1.0",
				Type:            "demo.refresh",
				Subject:         "customer/cust_123",
				Time:            &eventTime,
				DataContentType: "application/json",
				Data: map[string]any{
					"customerId": "cust_123",
				},
				Extensions: map[string]any{
					"attempt": 2,
				},
			},
		},
	})
	if trigger == nil {
		t.Fatal("trigger context = nil")
	}
	if got := trigger["kind"]; got != "event" {
		t.Fatalf("trigger kind = %#v, want %q", got, "event")
	}
	if got := trigger["triggerId"]; got != "trigger-1" {
		t.Fatalf("triggerId = %#v, want %q", got, "trigger-1")
	}
	event, ok := trigger["event"].(map[string]any)
	if !ok {
		t.Fatalf("trigger event = %#v", trigger["event"])
	}
	if got := event["specVersion"]; got != "1.0" {
		t.Fatalf("specVersion = %#v, want %q", got, "1.0")
	}
	if got := event["dataContentType"]; got != "application/json" {
		t.Fatalf("dataContentType = %#v, want %q", got, "application/json")
	}
	if got := event["type"]; got != "demo.refresh" {
		t.Fatalf("event type = %#v, want %q", got, "demo.refresh")
	}
}

package bootstrap

import (
	"context"
	"errors"
	"net"
	"net/http"
	"slices"
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
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type funcInvoker struct {
	invoke func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
}

func testWorkflowPluginTarget(pluginName, operation string) coreworkflow.Target {
	return testWorkflowPluginTargetWithInput(pluginName, operation, "", "", nil)
}

func testWorkflowPluginTargetWithInput(pluginName, operation, connection, instance string, input map[string]any) coreworkflow.Target {
	return coreworkflow.Target{
		Plugin: &coreworkflow.PluginTarget{
			PluginName: pluginName,
			Operation:  operation,
			Connection: connection,
			Instance:   instance,
			Input:      input,
		},
	}
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
	clone.Target = cloneRuntimeTarget(ref.Target)
	clone.Permissions = append([]core.AccessPermission(nil), ref.Permissions...)
	for i := range clone.Permissions {
		clone.Permissions[i].Operations = append([]string(nil), ref.Permissions[i].Operations...)
		clone.Permissions[i].Actions = append([]string(nil), ref.Permissions[i].Actions...)
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

func cloneRuntimeTarget(target coreworkflow.Target) coreworkflow.Target {
	clone := coreworkflow.Target{}
	if target.Plugin != nil {
		plugin := *target.Plugin
		plugin.Input = cloneMapAny(plugin.Input)
		clone.Plugin = &plugin
	}
	if target.Agent != nil {
		agent := *target.Agent
		agent.Messages = slices.Clone(agent.Messages)
		agent.ToolRefs = slices.Clone(agent.ToolRefs)
		agent.ResponseSchema = cloneMapAny(agent.ResponseSchema)
		agent.ProviderOptions = cloneMapAny(agent.ProviderOptions)
		agent.Metadata = cloneMapAny(agent.Metadata)
		clone.Agent = &agent
	}
	return clone
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

func workflowSignalsFromTestContext(value any) []map[string]any {
	switch signals := value.(type) {
	case []map[string]any:
		return signals
	case []any:
		out := make([]map[string]any, 0, len(signals))
		for _, signal := range signals {
			if typed, ok := signal.(map[string]any); ok {
				out = append(out, typed)
			}
		}
		return out
	default:
		return nil
	}
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
		Target: testWorkflowPluginTargetWithInput(
			"roadmap",
			"sync",
			"analytics",
			"tenant-a",
			map[string]any{
				"mode":   "full",
				"source": "scheduled",
			},
		),
		Input: map[string]any{
			"source": "event",
			"taskId": "task-456",
		},
		Metadata: map[string]any{
			"attempt": 2,
		},
		Signals: []coreworkflow.Signal{{
			ID:       "signal-1",
			Name:     "github.webhook",
			Sequence: 7,
			Payload: map[string]any{
				"delivery_id":                   "delivery-123",
				"github_event":                  "issue_comment",
				"github_action":                 "created",
				"_gestalt_payload_preview_json": strings.Repeat("preview", 2000),
				"payload": map[string]any{
					"raw": strings.Repeat("raw-webhook", 2000),
				},
				"agent_request": map[string]any{
					"user_prompt": strings.Repeat("please inspect this issue comment ", 500),
					"subject": map[string]any{
						"repository": "valon-technologies/gestalt",
						"number":     123,
					},
				},
				"payload_sha256": "abc123",
			},
		}},
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
		t.Fatalf("instance = %q, want empty instance for non-user invocations", gotInstance)
	}
	if gotConnection != "" {
		t.Fatalf("connection = %q, want empty connection for non-user invocations", gotConnection)
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
	if !ok || target["kind"] != "plugin" {
		t.Fatalf("workflow target = %#v", roundTripProvider.workflowContext["target"])
	}
	plugin, ok := target["plugin"].(map[string]any)
	if !ok || plugin["pluginName"] != "roadmap" || plugin["operation"] != "sync" {
		t.Fatalf("workflow target plugin = %#v", target["plugin"])
	}
	if got := plugin["connection"]; got != "analytics" {
		t.Fatalf("workflow target connection = %#v, want %q", got, "analytics")
	}
	if got := plugin["instance"]; got != "tenant-a" {
		t.Fatalf("workflow target instance = %#v, want %q", got, "tenant-a")
	}
	pluginInput, ok := plugin["input"].(map[string]any)
	if !ok || pluginInput["mode"] != "full" || pluginInput["source"] != "scheduled" {
		t.Fatalf("workflow target input = %#v", plugin["input"])
	}
	trigger, ok := roundTripProvider.workflowContext["trigger"].(map[string]any)
	if !ok || trigger["kind"] != "schedule" || trigger["scheduleId"] != "sched-1" {
		t.Fatalf("workflow trigger = %#v", roundTripProvider.workflowContext["trigger"])
	}
	if got := trigger["scheduledFor"]; got != scheduledFor.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("scheduledFor = %#v, want %q", got, scheduledFor.UTC().Format(time.RFC3339Nano))
	}
	signals := workflowSignalsFromTestContext(roundTripProvider.workflowContext["signals"])
	if len(signals) != 1 {
		t.Fatalf("workflow signals = %#v", roundTripProvider.workflowContext["signals"])
	}
	signalPayload, ok := signals[0]["payload"].(map[string]any)
	if !ok {
		t.Fatalf("workflow signal payload = %#v", signals[0]["payload"])
	}
	if _, ok := signalPayload["_gestalt_payload_preview_json"]; ok {
		t.Fatalf("workflow signal payload retained preview: %#v", signalPayload)
	}
	if _, ok := signalPayload["payload"]; ok {
		t.Fatalf("workflow signal payload retained raw payload: %#v", signalPayload)
	}
	if signalPayload["payload_sha256"] != "abc123" {
		t.Fatalf("workflow signal payload digest = %#v", signalPayload["payload_sha256"])
	}
	agentRequest, ok := signalPayload["agent_request"].(map[string]any)
	if !ok {
		t.Fatalf("workflow signal agent_request = %#v", signalPayload["agent_request"])
	}
	if prompt, _ := agentRequest["user_prompt"].(string); !strings.Contains(prompt, "please inspect") || len(prompt) > workflowSignalContextMaxStringBytes {
		t.Fatalf("workflow signal user_prompt = %q", prompt)
	}
}

func TestWorkflowRuntimeInvokeAgentTargetCreatesAndSupervisesTurn(t *testing.T) {
	t.Parallel()

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
	toolGrants := newTestAgentToolGrants(t)
	agentRuntime.SetToolGrants(toolGrants)
	manager := agentmanager.New(agentmanager.Config{
		Providers:  &reg.Providers,
		Agent:      agentRuntime,
		ToolGrants: toolGrants,
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
			ToolRefs:       []coreagent.ToolRef{{Plugin: "roadmap", Operation: "sync"}},
			TimeoutSeconds: 5,
		}},
		Trigger: coreworkflow.RunTrigger{Manual: true},
		Signals: []coreworkflow.Signal{{
			ID:       "signal-agent-1",
			Name:     "github.webhook",
			Sequence: 11,
			Payload: map[string]any{
				"delivery_id":                   "delivery-agent-123",
				"github_event":                  "pull_request",
				"github_action":                 "opened",
				"_gestalt_payload_preview_json": strings.Repeat("preview", 2000),
				"payload": map[string]any{
					"raw": strings.Repeat("raw-webhook", 2000),
				},
				"agent_request": map[string]any{
					"user_prompt": "Review the opened pull request",
				},
				"payload_sha256": "def456",
			},
		}},
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
	if got := turnReq.IdempotencyKey; !strings.HasPrefix(got, "workflow:temporal:run-agent-123:turn:signal-batch-") {
		t.Fatalf("turn idempotency key = %q, want workflow signal batch prefix", got)
	}
	if len(turnReq.Messages) != 2 || turnReq.Messages[0].Text != "Send the status summary" {
		t.Fatalf("turn messages = %#v", turnReq.Messages)
	}
	if !strings.Contains(turnReq.Messages[1].Text, "Review the opened pull request") {
		t.Fatalf("signal message text = %q", turnReq.Messages[1].Text)
	}
	if strings.Contains(turnReq.Messages[1].Text, "_gestalt_payload_preview_json") || strings.Contains(turnReq.Messages[1].Text, "raw-webhook") {
		t.Fatalf("signal message retained raw payload: %q", turnReq.Messages[1].Text)
	}
	workflowMetadata, ok := turnReq.Metadata["workflow"].(map[string]any)
	if !ok {
		t.Fatalf("turn workflow metadata = %#v", turnReq.Metadata["workflow"])
	}
	metadataSignals := workflowSignalsFromTestContext(workflowMetadata["signals"])
	if len(metadataSignals) != 1 {
		t.Fatalf("turn workflow signals = %#v", workflowMetadata["signals"])
	}
	metadataPayload, ok := metadataSignals[0]["payload"].(map[string]any)
	if !ok {
		t.Fatalf("turn workflow signal payload = %#v", metadataSignals[0]["payload"])
	}
	if _, ok := metadataPayload["_gestalt_payload_preview_json"]; ok {
		t.Fatalf("turn metadata retained preview: %#v", metadataPayload)
	}
	if _, ok := metadataPayload["payload"]; ok {
		t.Fatalf("turn metadata retained raw payload: %#v", metadataPayload)
	}
	if len(turnReq.Tools) != 1 || turnReq.Tools[0].Target.Plugin != "roadmap" || turnReq.Tools[0].Target.Operation != "sync" {
		t.Fatalf("turn tools = %#v, want preloaded roadmap.sync", turnReq.Tools)
	}
	if turnReq.ToolSource != coreagent.ToolSourceModeNativeSearch {
		t.Fatalf("turn tool source = %q, want native search", turnReq.ToolSource)
	}
	if len(turnReq.ToolRefs) != 1 || turnReq.ToolRefs[0].Plugin != "roadmap" || turnReq.ToolRefs[0].Operation != "sync" {
		t.Fatalf("turn tool refs = %#v", turnReq.ToolRefs)
	}
}

func TestWorkflowRuntimeInvokeAgentTargetWithExecutionRefAcceptsCanonicalTarget(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{
		Agent: &coreworkflow.AgentTarget{
			ProviderName:   "managed",
			Model:          "deep",
			Prompt:         "Send the status summary",
			TimeoutSeconds: 5,
		},
	}
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:               "agent-ref",
		ProviderName:     "temporal",
		Target:           target,
		CallerPluginName: "slack",
		SubjectID:        "service_account:scheduler",
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
		Signals: []coreworkflow.Signal{{
			ID:             "sig-1",
			Name:           "slack.message",
			Payload:        map[string]any{"text": "new message"},
			IdempotencyKey: "evt-1",
			Sequence:       42,
		}},
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
	turnReq := agentManager.createTurnRequests[0]
	if turnReq.CallerPluginName != "slack" {
		t.Fatalf("caller plugin = %q, want slack", turnReq.CallerPluginName)
	}
	if !strings.HasPrefix(turnReq.IdempotencyKey, "workflow:temporal:run-agent-123:turn:signal-batch-") {
		t.Fatalf("turn idempotency key = %q", turnReq.IdempotencyKey)
	}
	if len(turnReq.Messages) != 2 || turnReq.Messages[0].Text != "Send the status summary" || !strings.Contains(turnReq.Messages[1].Text, `"name": "slack.message"`) {
		t.Fatalf("turn messages = %#v", turnReq.Messages)
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

func TestWorkflowRuntimeRejectsMixedAgentPluginTargetWithExecutionRef(t *testing.T) {
	t.Parallel()

	target := testWorkflowPluginTarget("roadmap", "sync")
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "plugin-ref",
		ProviderName: "temporal",
		Target:       target,
		SubjectID:    "service_account:scheduler",
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	_, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "plugin-ref",
		RunID:        "run-123",
		Target: coreworkflow.Target{
			Plugin: target.Plugin,
			Agent: &coreworkflow.AgentTarget{
				ProviderName:   "managed",
				Prompt:         "send reminder",
				TimeoutSeconds: 5,
			},
		},
	})
	if err == nil {
		t.Fatal("Invoke mixed agent/plugin target succeeded, want error")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefUsesStoredHumanPrincipalAndSelectors(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user, err := services.Users.FindOrCreateUser(context.Background(), "ada@example.test")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	target := testWorkflowPluginTargetWithInput("roadmap", "sync", "analytics", "tenant-a", map[string]any{"mode": "full"})
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                  "exec-ref-123",
		ProviderName:        "temporal",
		Target:              target,
		SubjectID:           principal.UserSubjectID(user.ID),
		SubjectKind:         string(principal.KindUser),
		DisplayName:         "Ada Lovelace",
		AuthSource:          "github_app_webhook",
		CredentialSubjectID: "service_account:workflow-credential",
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
		Target:       target,
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
	if gotPrincipal.CredentialSubjectID != "service_account:workflow-credential" {
		t.Fatalf("credential subject = %q, want %q", gotPrincipal.CredentialSubjectID, "service_account:workflow-credential")
	}
	if gotPrincipal.DisplayName != "Ada Lovelace" || gotPrincipal.AuthSource() != "github_app_webhook" {
		t.Fatalf("principal display/auth = (%q, %q), want stored execution ref metadata", gotPrincipal.DisplayName, gotPrincipal.AuthSource())
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

func TestWorkflowRuntimeInvokeExecutionRefUsesStoredSubjectPrincipal(t *testing.T) {
	t.Parallel()

	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "exec-ref-service-account",
		ProviderName: "temporal",
		Target:       testWorkflowPluginTarget("roadmap", "sync"),
		SubjectID:    "service_account:scheduler",
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
		ExecutionRef: "exec-ref-service-account",
		Target:       testWorkflowPluginTarget("roadmap", "sync"),
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if gotPrincipal == nil {
		t.Fatal("principal = nil")
	}
	if gotPrincipal.Kind != principal.Kind("service_account") {
		t.Fatalf("principal kind = %q, want %q", gotPrincipal.Kind, principal.Kind("service_account"))
	}
	if gotPrincipal.SubjectID != "service_account:scheduler" {
		t.Fatalf("subjectID = %q, want %q", gotPrincipal.SubjectID, "service_account:scheduler")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefRechecksAuthorizationThroughBroker(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user, err := services.Users.FindOrCreateUser(context.Background(), "ada@example.test")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	target := testWorkflowPluginTargetWithInput("roadmap", "sync", "analytics", "tenant-a", nil)
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "exec-ref-denied",
		ProviderName: "temporal",
		Target:       target,
		SubjectID:    principal.UserSubjectID(user.ID),
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
		Target:       target,
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

	target := testWorkflowPluginTargetWithInput("roadmap", "export", "analytics", "tenant-a", nil)
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(ctx, &coreworkflow.ExecutionReference{
		ID:           "exec-ref-123",
		ProviderName: "basic",
		Target:       target,
		SubjectID:    principal.UserSubjectID("user-123"),
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
		Target:       target,
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
		Target:       testWorkflowPluginTarget("roadmap", "sync"),
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

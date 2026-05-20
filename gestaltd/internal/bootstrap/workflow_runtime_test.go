package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type funcInvoker struct {
	invoke func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
}

func testWorkflowPluginTarget(pluginName, operation string) coreworkflow.Target {
	return testWorkflowPluginTargetWithPayload(pluginName, operation, "", "", nil)
}

func testWorkflowPluginTargetWithPayload(pluginName, operation, connection, instance string, input map[string]any) coreworkflow.Target {
	return coreworkflow.Target{
		Steps: []coreworkflow.Step{{ID: operation, Plugin: &coreworkflow.PluginCall{
			Name:       pluginName,
			Operation:  operation,
			Connection: connection,
			Instance:   instance,
			Input:      testWorkflowValueObject(input),
		}}},
	}
}

func testWorkflowValueObject(input map[string]any) coreworkflow.Value {
	if input == nil {
		return coreworkflow.Value{}
	}
	out := make(map[string]coreworkflow.Value, len(input))
	for key, value := range input {
		out[key] = coreworkflow.Value{Literal: value, LiteralSet: true}
	}
	return coreworkflow.Value{Object: out}
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
	if ref.RunAs != nil {
		runAs := *core.NormalizeRunAsSubject(ref.RunAs)
		clone.RunAs = &runAs
	}
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
	clone := coreworkflow.Target{Steps: slices.Clone(target.Steps)}
	for i := range clone.Steps {
		clone.Steps[i].Inputs = cloneWorkflowRuntimeValueMap(target.Steps[i].Inputs)
		clone.Steps[i].Metadata = cloneMapAny(target.Steps[i].Metadata)
		if target.Steps[i].Plugin != nil {
			plugin := *target.Steps[i].Plugin
			plugin.Input = cloneWorkflowRuntimeValue(target.Steps[i].Plugin.Input)
			clone.Steps[i].Plugin = &plugin
		}
		if target.Steps[i].Agent != nil {
			agent := *target.Steps[i].Agent
			agent.Messages = slices.Clone(target.Steps[i].Agent.Messages)
			for j := range agent.Messages {
				agent.Messages[j].Metadata = cloneMapAny(target.Steps[i].Agent.Messages[j].Metadata)
			}
			agent.ToolRefs = slices.Clone(target.Steps[i].Agent.ToolRefs)
			agent.ResponseSchema = cloneMapAny(target.Steps[i].Agent.ResponseSchema)
			agent.ModelOptions = cloneMapAny(target.Steps[i].Agent.ModelOptions)
			clone.Steps[i].Agent = &agent
		}
		if target.Steps[i].When != nil {
			when := *target.Steps[i].When
			when.Value = cloneWorkflowRuntimeValue(target.Steps[i].When.Value)
			clone.Steps[i].When = &when
		}
		if target.Steps[i].OutputDelivery != nil {
			delivery := *target.Steps[i].OutputDelivery
			if target.Steps[i].OutputDelivery.Plugin != nil {
				plugin := *target.Steps[i].OutputDelivery.Plugin
				plugin.Input = cloneWorkflowRuntimeValue(target.Steps[i].OutputDelivery.Plugin.Input)
				delivery.Plugin = &plugin
			}
			clone.Steps[i].OutputDelivery = &delivery
		}
	}
	return clone
}

func cloneWorkflowRuntimeValueMap(values map[string]coreworkflow.Value) map[string]coreworkflow.Value {
	if values == nil {
		return nil
	}
	out := make(map[string]coreworkflow.Value, len(values))
	for key := range values {
		out[key] = cloneWorkflowRuntimeValue(values[key])
	}
	return out
}

func cloneWorkflowRuntimeValue(value coreworkflow.Value) coreworkflow.Value {
	out := value
	out.Object = cloneWorkflowRuntimeValueMap(value.Object)
	if value.Array != nil {
		out.Array = make([]coreworkflow.Value, len(value.Array))
		for i := range value.Array {
			out.Array[i] = cloneWorkflowRuntimeValue(value.Array[i])
		}
	}
	if value.Template != nil {
		template := *value.Template
		out.Template = &template
	}
	if value.StepOutput != nil {
		stepOutput := *value.StepOutput
		out.StepOutput = &stepOutput
	}
	return out
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

func TestCloneRuntimeTargetClonesStepAgentMessages(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID: "diagnose",
		Agent: &coreworkflow.AgentTurn{
			ProviderName: "managed",
			Messages: []coreworkflow.AgentMessage{{
				Role: "system",
				Text: coreworkflow.Text{Template: "Use concise replies."},
			}},
		},
	}}}

	clone := cloneRuntimeTarget(target)
	target.Steps[0].Agent.Messages[0].Role = "user"

	if got := clone.Steps[0].Agent.Messages[0].Role; got != "system" {
		t.Fatalf("cloned step agent message role = %q, want system", got)
	}
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
	events                          []string
	createSessionRequests           []coreagent.ManagerCreateSessionRequest
	createSessionDeadlines          []time.Duration
	createTurnRequests              []coreagent.ManagerCreateTurnRequest
	createTurnInheritedDeliveries   []*coreworkflow.StepDelivery
	createTurnDeadlines             []time.Duration
	createTurnProviderCallDeadlines []time.Duration
	getTurnDeadlines                []time.Duration
	getTurnProviderCallDeadlines    []time.Duration
	cancelTurnIDs                   []string
	createTurnResult                func(callIndex int, req coreagent.ManagerCreateTurnRequest) *coreagent.Turn
	returnNilTurn                   bool
	completeTurnViaGet              bool
}

func (m *workflowRuntimeAgentManagerStub) CreateSession(ctx context.Context, _ *principal.Principal, req coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error) {
	m.events = append(m.events, "create-session")
	m.createSessionRequests = append(m.createSessionRequests, req)
	if deadline, ok := ctx.Deadline(); ok {
		m.createSessionDeadlines = append(m.createSessionDeadlines, time.Until(deadline))
	} else {
		m.createSessionDeadlines = append(m.createSessionDeadlines, 0)
	}
	return &coreagent.Session{
		ID:           "session-1",
		ProviderName: req.ProviderName,
		Model:        req.Model,
		State:        coreagent.SessionStateActive,
	}, nil
}

func (m *workflowRuntimeAgentManagerStub) CreateTurn(ctx context.Context, _ *principal.Principal, req coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error) {
	m.events = append(m.events, "create-turn")
	m.createTurnRequests = append(m.createTurnRequests, req)
	callIndex := len(m.createTurnRequests)
	m.createTurnInheritedDeliveries = append(m.createTurnInheritedDeliveries, agentmanager.InheritedOutputDeliveryFromContext(ctx))
	m.createTurnDeadlines = append(m.createTurnDeadlines, remainingDeadline(ctx))
	callCtx, cancel := runtimehost.ProviderWorkflowAgentCallContext(ctx)
	m.createTurnProviderCallDeadlines = append(m.createTurnProviderCallDeadlines, remainingDeadline(callCtx))
	cancel()
	if m.returnNilTurn {
		return nil, nil
	}
	if m.createTurnResult != nil {
		return m.createTurnResult(callIndex, req), nil
	}
	status := coreagent.ExecutionStatusSucceeded
	outputText := "turn completed"
	if m.completeTurnViaGet {
		status = coreagent.ExecutionStatusRunning
		outputText = ""
	}
	return &coreagent.Turn{
		ID:           "turn-" + strconv.Itoa(callIndex),
		SessionID:    req.SessionID,
		ProviderName: "managed",
		Model:        req.Model,
		Status:       status,
		OutputText:   outputText,
	}, nil
}

func (m *workflowRuntimeAgentManagerStub) GetTurn(ctx context.Context, _ *principal.Principal, turnID string) (*coreagent.Turn, error) {
	m.getTurnDeadlines = append(m.getTurnDeadlines, remainingDeadline(ctx))
	callCtx, cancel := runtimehost.ProviderWorkflowAgentCallContext(ctx)
	m.getTurnProviderCallDeadlines = append(m.getTurnProviderCallDeadlines, remainingDeadline(callCtx))
	cancel()
	return &coreagent.Turn{
		ID:         turnID,
		Status:     coreagent.ExecutionStatusSucceeded,
		OutputText: "turn completed",
	}, nil
}

func (m *workflowRuntimeAgentManagerStub) CancelTurn(_ context.Context, _ *principal.Principal, turnID, _ string) (*coreagent.Turn, error) {
	m.cancelTurnIDs = append(m.cancelTurnIDs, turnID)
	return &coreagent.Turn{ID: turnID, Status: coreagent.ExecutionStatusCanceled}, nil
}

func remainingDeadline(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		return time.Until(deadline)
	}
	return 0
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
	roundTripClient := newWorkflowRoundTripClient(t, pluginservice.NewServer(roundTripProvider))
	roundTripRemote, err := pluginservice.NewRemote(context.Background(), roundTripClient, pluginservice.StaticProviderSpec{
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
		Target: testWorkflowPluginTargetWithPayload(
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
				"webhook_policy": map[string]any{
					"id": "github-review",
					"trigger": map[string]any{
						"manual_commands": []any{"@gestalt review"},
					},
				},
				"review_check_run": map[string]any{
					"id":          456,
					"name":        "Gestalt Review",
					"status":      "in_progress",
					"external_id": "gestalt-review:abc123",
				},
				"check_run": map[string]any{
					"id":         789,
					"name":       "CI",
					"status":     "completed",
					"conclusion": "success",
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
	if resp.Status != http.StatusOK || !strings.Contains(resp.Body, `"finalStepId":"sync"`) {
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
	if gotInstance != "tenant-a" {
		t.Fatalf("instance = %q, want tenant-a", gotInstance)
	}
	if gotConnection != "analytics" {
		t.Fatalf("connection = %q, want analytics", gotConnection)
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
	if !ok || target["kind"] != "steps" {
		t.Fatalf("workflow target = %#v", roundTripProvider.workflowContext["target"])
	}
	steps, ok := target["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("workflow target steps = %#v", target["steps"])
	}
	step, ok := steps[0].(map[string]any)
	if !ok || step["kind"] != "plugin" || step["plugin"] != "roadmap" || step["operation"] != "sync" {
		t.Fatalf("workflow target step = %#v", steps[0])
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
	if prompt, _ := agentRequest["user_prompt"].(string); !strings.Contains(prompt, "please inspect") {
		t.Fatalf("workflow signal user_prompt = %q", prompt)
	}
	webhookPolicy, ok := signalPayload["webhook_policy"].(map[string]any)
	if !ok {
		t.Fatalf("workflow signal webhook_policy = %#v", signalPayload["webhook_policy"])
	}
	if webhookPolicy["id"] != "github-review" {
		t.Fatalf("workflow signal webhook_policy.id = %#v", webhookPolicy["id"])
	}
	reviewCheckRun, ok := signalPayload["review_check_run"].(map[string]any)
	if !ok {
		t.Fatalf("workflow signal review_check_run = %#v", signalPayload["review_check_run"])
	}
	if got := reviewCheckRun["id"]; got != 456 && got != float64(456) {
		t.Fatalf("workflow signal review_check_run.id = %#v", got)
	}
	checkRun, ok := signalPayload["check_run"].(map[string]any)
	if !ok {
		t.Fatalf("workflow signal check_run = %#v", signalPayload["check_run"])
	}
	if checkRun["name"] != "CI" {
		t.Fatalf("workflow signal check_run.name = %#v", checkRun["name"])
	}
}

func TestWorkflowRuntimeInvokeRejectsUnresolvedPluginInput(t *testing.T) {
	t.Parallel()

	runtime := &workflowRuntime{}
	invoked := false
	runtime.SetInvoker(funcInvoker{
		invoke: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
			invoked = true
			return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
		},
	})
	p := principal.Canonicalize(&principal.Principal{SubjectID: principal.UserSubjectID("ada")})

	resp, err := runtime.Invoke(principal.WithPrincipal(context.Background(), p), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-missing-plugin-input",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID: "reply",
			Plugin: &coreworkflow.PluginCall{
				Name:      "notification",
				Operation: "reply",
				Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
					"reply_ref": {SignalPayload: "reply_ref"},
				}},
			},
		}}},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if invoked {
		t.Fatal("plugin was invoked with unresolved input")
	}
	if resp.Status != http.StatusInternalServerError || !strings.Contains(resp.Body, "workflow step plugin input did not resolve") {
		t.Fatalf("response = %#v, want unresolved plugin input failure", resp)
	}
}

func TestWorkflowStepIdempotencyKeyIncludesSignalIdentity(t *testing.T) {
	t.Parallel()

	first := coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-1",
		Signals: []coreworkflow.Signal{{
			ID: "signal-1",
		}},
	}
	second := first
	second.Signals = []coreworkflow.Signal{{ID: "signal-2"}}

	firstKey := workflowStepIdempotencyKey(first, workflowStepInvocationScope(first), "diagnosis", "agent-turn")
	secondKey := workflowStepIdempotencyKey(second, workflowStepInvocationScope(second), "diagnosis", "agent-turn")
	if firstKey == secondKey {
		t.Fatalf("signal-scoped idempotency keys matched: %q", firstKey)
	}
	want := "workflow:temporal:run-1:invocation:signal-id:signal-1:step:diagnosis:agent-turn"
	if firstKey != want {
		t.Fatalf("first key = %q, want %q", firstKey, want)
	}
}

func TestWorkflowExecutionRefPermissionsForTargetIncludesAgentProvider(t *testing.T) {
	t.Parallel()

	perms := workflowExecutionRefPermissionsForTarget(coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID: "agent",
		Agent: &coreworkflow.AgentTurn{
			ProviderName: "managed",
			ToolRefs: []coreagent.ToolRef{{
				Plugin:    "roadmap",
				Operation: "sync",
			}},
		},
		OutputDelivery: &coreworkflow.StepDelivery{Plugin: &coreworkflow.PluginCall{
			Name:      "notification",
			Operation: "reply",
		}},
	}}})

	if !slices.ContainsFunc(perms, func(perm core.AccessPermission) bool {
		return perm.Plugin == "managed" && len(perm.Operations) == 0
	}) {
		t.Fatalf("permissions = %#v, want managed agent provider permission", perms)
	}
	if !slices.ContainsFunc(perms, func(perm core.AccessPermission) bool {
		return perm.Plugin == "roadmap" && slices.Equal(perm.Operations, []string{"sync"})
	}) {
		t.Fatalf("permissions = %#v, want roadmap.sync", perms)
	}
	if !slices.ContainsFunc(perms, func(perm core.AccessPermission) bool {
		return perm.Plugin == "notification" && slices.Equal(perm.Operations, []string{"reply"})
	}) {
		t.Fatalf("permissions = %#v, want notification.reply", perms)
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
	runGrants := newTestAgentRunGrants(t)
	agentRuntime.SetRunGrants(runGrants)
	manager := agentmanager.New(agentmanager.Config{
		Providers: &reg.Providers,
		Agent:     agentRuntime,
		RunGrants: runGrants,
	})
	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{},
	}
	runtime.SetAgentManager(manager)

	req := coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-agent-123",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID:             "agent",
			TimeoutSeconds: 60,
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "managed",
				Model:        "deep",
				Prompt:       coreworkflow.Text{Template: "Send the status summary"},
				ToolRefs:     []coreagent.ToolRef{{Plugin: "roadmap", Operation: "sync"}},
			},
		}}},
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
		}, {
			Plugin: "managed",
		}}),
	})

	resp, err := runtime.Invoke(principal.WithPrincipal(context.Background(), p), req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if len(agentProvider.createSessionRequests) != 1 {
		t.Fatalf("session requests = %d, want 1", len(agentProvider.createSessionRequests))
	}
	if got := agentProvider.createSessionRequests[0].IdempotencyKey; got != "workflow:temporal:run-agent-123:invocation:signal-id:signal-agent-1:step:agent:agent-session:agent" {
		t.Fatalf("session idempotency key = %q", got)
	}
	if len(agentProvider.createTurnRequests) != 1 {
		t.Fatalf("turn requests = %d, want 1", len(agentProvider.createTurnRequests))
	}
	turnReq := agentProvider.createTurnRequests[0]
	if got := turnReq.IdempotencyKey; got != "workflow:temporal:run-agent-123:invocation:signal-id:signal-agent-1:step:agent:agent-turn" {
		t.Fatalf("turn idempotency key = %q", got)
	}
	if len(turnReq.Messages) != 1 || turnReq.Messages[0].Text != "Send the status summary" {
		t.Fatalf("turn messages = %#v", turnReq.Messages)
	}
	sessionMetadata, ok := agentProvider.createSessionRequests[0].Metadata["workflow"].(map[string]any)
	if !ok {
		t.Fatalf("session workflow metadata = %#v", agentProvider.createSessionRequests[0].Metadata["workflow"])
	}
	metadataSignals := workflowSignalsFromTestContext(sessionMetadata["signals"])
	if len(metadataSignals) != 1 {
		t.Fatalf("session workflow signals = %#v", sessionMetadata["signals"])
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
	if len(turnReq.Tools) != 0 {
		t.Fatalf("turn tools = %#v, want no preloaded tools", turnReq.Tools)
	}
	if turnReq.ToolSource != coreagent.ToolSourceModeMCPCatalog {
		t.Fatalf("turn tool source = %q, want mcp_catalog", turnReq.ToolSource)
	}
	if len(turnReq.ToolRefs) != 1 || turnReq.ToolRefs[0].Plugin != "roadmap" || turnReq.ToolRefs[0].Operation != "sync" {
		t.Fatalf("turn tool refs = %#v", turnReq.ToolRefs)
	}
}

func TestWorkflowRuntimeInvokeAgentTargetUsesWorkflowKeyForSessionIdempotency(t *testing.T) {
	t.Parallel()

	agentManager := &workflowRuntimeAgentManagerStub{}
	runtime := &workflowRuntime{}
	runtime.SetAgentManager(agentManager)
	p := principal.Canonicalize(&principal.Principal{
		SubjectID:           principal.UserSubjectID("ada"),
		CredentialSubjectID: principal.UserSubjectID("ada"),
	})

	resp, err := runtime.Invoke(principal.WithPrincipal(context.Background(), p), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-agent-thread-reply-2",
		Metadata: map[string]any{
			"workflow_key": "slack:T123:C123:1778255568.567059",
		},
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID: "agent",
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "managed",
				Model:        "deep",
				Prompt:       coreworkflow.Text{Template: "Continue the Slack thread"},
			},
		}}},
		Signals: []coreworkflow.Signal{{
			ID:             "signal-agent-2",
			Name:           "slack.event",
			IdempotencyKey: "evt-2",
		}},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if len(agentManager.createSessionRequests) != 1 {
		t.Fatalf("session requests = %d, want 1", len(agentManager.createSessionRequests))
	}
	if got, want := agentManager.createSessionRequests[0].IdempotencyKey, "workflow:temporal:run-agent-thread-reply-2:invocation:signal-id:signal-agent-2:step:agent:agent-session:agent"; got != want {
		t.Fatalf("session idempotency key = %q, want %q", got, want)
	}
	if len(agentManager.createTurnRequests) != 1 {
		t.Fatalf("turn requests = %d, want 1", len(agentManager.createTurnRequests))
	}
	if got := agentManager.createTurnRequests[0].IdempotencyKey; got != "workflow:temporal:run-agent-thread-reply-2:invocation:signal-id:signal-agent-2:step:agent:agent-turn" {
		t.Fatalf("turn idempotency key = %q", got)
	}
}

func TestWorkflowRuntimeInvokeAgentTargetMarksProviderCallsWithWorkflowDeadline(t *testing.T) {
	t.Parallel()

	agentManager := &workflowRuntimeAgentManagerStub{completeTurnViaGet: true}
	runtime := &workflowRuntime{}
	runtime.SetAgentManager(agentManager)
	p := principal.Canonicalize(&principal.Principal{
		SubjectID:           principal.UserSubjectID("ada"),
		CredentialSubjectID: principal.UserSubjectID("ada"),
	})

	resp, err := runtime.Invoke(principal.WithPrincipal(context.Background(), p), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-agent-default-timeout",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID:             "agent",
			TimeoutSeconds: 60,
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "managed",
				Model:        "deep",
				Prompt:       coreworkflow.Text{Template: "Send the status summary"},
			},
		}}},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if len(agentManager.createSessionDeadlines) != 1 {
		t.Fatalf("create session deadlines = %#v, want one", agentManager.createSessionDeadlines)
	}
	if got := agentManager.createSessionDeadlines[0]; got <= runtimehost.ProviderRPCTimeout {
		t.Fatalf("CreateSession remaining deadline = %s, want workflow deadline above provider RPC timeout %s", got, runtimehost.ProviderRPCTimeout)
	}
	if got := agentManager.createSessionDeadlines[0]; got > 60*time.Second {
		t.Fatalf("CreateSession remaining deadline = %s, want at most workflow step timeout", got)
	}
	if len(agentManager.createTurnDeadlines) != 1 {
		t.Fatalf("create turn deadlines = %#v, want one", agentManager.createTurnDeadlines)
	}
	if got := agentManager.createTurnDeadlines[0]; got <= runtimehost.ProviderRPCTimeout {
		t.Fatalf("CreateTurn remaining deadline = %s, want workflow deadline above provider RPC timeout %s", got, runtimehost.ProviderRPCTimeout)
	}
	if got := agentManager.createTurnProviderCallDeadlines[0]; got <= runtimehost.ProviderRPCTimeout {
		t.Fatalf("CreateTurn provider call deadline = %s, want workflow deadline above provider RPC timeout %s", got, runtimehost.ProviderRPCTimeout)
	}
	if len(agentManager.getTurnDeadlines) != 1 {
		t.Fatalf("get turn deadlines = %#v, want one", agentManager.getTurnDeadlines)
	}
	if got := agentManager.getTurnDeadlines[0]; got <= runtimehost.ProviderRPCTimeout {
		t.Fatalf("GetTurn remaining deadline = %s, want workflow deadline above provider RPC timeout %s", got, runtimehost.ProviderRPCTimeout)
	}
	if got := agentManager.getTurnProviderCallDeadlines[0]; got <= runtimehost.ProviderRPCTimeout {
		t.Fatalf("GetTurn provider call deadline = %s, want workflow deadline above provider RPC timeout %s", got, runtimehost.ProviderRPCTimeout)
	}
}

func TestWorkflowRuntimeInvokeAgentTargetDeliversFinalOutput(t *testing.T) {
	t.Parallel()

	agentManager := &workflowRuntimeAgentManagerStub{}
	runtime := &workflowRuntime{}
	runtime.SetAgentManager(agentManager)

	var gotProvider string
	var gotOperation string
	var gotParams map[string]any
	var gotIdempotencyKey string
	var gotCredentialMode core.ConnectionMode
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, _ *principal.Principal, providerName, _ string, operation string, params map[string]any) (*core.OperationResult, error) {
			gotProvider = providerName
			gotOperation = operation
			gotParams = maps.Clone(params)
			gotIdempotencyKey = invocation.IdempotencyKeyFromContext(ctx)
			gotCredentialMode = invocation.CredentialModeOverrideFromContext(ctx)
			return &core.OperationResult{Status: http.StatusOK, Body: `{"delivered":true}`}, nil
		},
	})

	req := coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-agent-delivery-123",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID: "agent",
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "managed",
				Prompt:       coreworkflow.Text{Template: "Summarize the request"},
			},
			OutputDelivery: &coreworkflow.StepDelivery{
				Plugin: &coreworkflow.PluginCall{
					Name:           "notification",
					Operation:      "reply",
					CredentialMode: core.ConnectionModeNone,
					Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
						"format":     {Literal: "plain", LiteralSet: true},
						"text":       {StepOutput: &coreworkflow.StepOutputSource{StepID: "agent", Path: "agent.text"}},
						"reply_ref":  {SignalPayload: "reply_ref"},
						"event_type": {SignalPayload: "event_type"},
						"source":     {Literal: "workflow", LiteralSet: true},
					}},
				},
			},
		}}},
		Signals: []coreworkflow.Signal{
			{
				ID:             "signal-1",
				IdempotencyKey: "evt-1",
				Payload:        map[string]any{"reply_ref": "older-ref", "event_type": "message"},
				Metadata:       map[string]any{"event": map[string]any{"type": "message"}},
			},
			{
				ID:             "signal-2",
				IdempotencyKey: "evt-2",
				Payload:        map[string]any{"reply_ref": "newer-ref", "event_type": "app_mention"},
				Metadata:       map[string]any{"event": map[string]any{"type": "app_mention"}},
			},
		},
	}
	p := principal.Canonicalize(&principal.Principal{
		SubjectID:           principal.UserSubjectID("ada"),
		CredentialSubjectID: principal.UserSubjectID("ada"),
		TokenPermissions: principal.CompilePermissions([]core.AccessPermission{{
			Plugin:     "notification",
			Operations: []string{"reply"},
		}}),
	})

	resp, err := runtime.Invoke(principal.WithPrincipal(context.Background(), p), req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if gotProvider != "notification" || gotOperation != "reply" {
		t.Fatalf("delivery target = %s.%s, want notification.reply", gotProvider, gotOperation)
	}
	if gotParams["format"] != "plain" || gotParams["text"] != "turn completed" || gotParams["reply_ref"] != "newer-ref" || gotParams["event_type"] != "app_mention" || gotParams["source"] != "workflow" {
		t.Fatalf("delivery params = %#v", gotParams)
	}
	if gotIdempotencyKey != "workflow:temporal:run-agent-delivery-123:invocation:signal-id:signal-2:step:agent:output_delivery" {
		t.Fatalf("delivery idempotency key = %q", gotIdempotencyKey)
	}
	if gotCredentialMode != core.ConnectionModeNone {
		t.Fatalf("delivery credential mode = %q, want %q", gotCredentialMode, core.ConnectionModeNone)
	}

	req.Target.Steps[0].OutputDelivery.Plugin.CredentialMode = ""
	gotCredentialMode = core.ConnectionMode("unexpected")
	resp, err = runtime.Invoke(principal.WithPrincipal(context.Background(), p), req)
	if err != nil {
		t.Fatalf("Invoke without delivery credential mode: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("response without delivery credential mode = %#v", resp)
	}
	if gotCredentialMode != "" {
		t.Fatalf("delivery credential mode without override = %q, want empty", gotCredentialMode)
	}
}

func TestWorkflowRuntimeInvokeAgentTargetRunsStepsInOneSession(t *testing.T) {
	t.Parallel()

	agentManager := &workflowRuntimeAgentManagerStub{
		createTurnResult: func(callIndex int, req coreagent.ManagerCreateTurnRequest) *coreagent.Turn {
			switch callIndex {
			case 1:
				return &coreagent.Turn{
					ID:               "turn-diagnosis",
					SessionID:        req.SessionID,
					ProviderName:     "managed",
					Model:            req.Model,
					Status:           coreagent.ExecutionStatusSucceeded,
					OutputText:       "diagnosis ready",
					StructuredOutput: map[string]any{"actionable_for_pr": true, "root_cause": "missing credential"},
				}
			case 2:
				return &coreagent.Turn{
					ID:           "turn-pr-fix",
					SessionID:    req.SessionID,
					ProviderName: "managed",
					Model:        req.Model,
					Status:       coreagent.ExecutionStatusSucceeded,
					OutputText:   "opened PR",
				}
			default:
				t.Fatalf("unexpected CreateTurn call %d", callIndex)
				return nil
			}
		},
	}
	runtime := &workflowRuntime{}
	runtime.SetAgentManager(agentManager)

	var deliveryOperations []string
	var deliveryIdempotencyKeys []string
	var deliveryParams []map[string]any
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, _ *principal.Principal, providerName, _ string, operation string, params map[string]any) (*core.OperationResult, error) {
			if providerName != "notification" {
				t.Fatalf("delivery provider = %q, want notification", providerName)
			}
			agentManager.events = append(agentManager.events, "delivery:"+operation)
			deliveryOperations = append(deliveryOperations, operation)
			deliveryIdempotencyKeys = append(deliveryIdempotencyKeys, invocation.IdempotencyKeyFromContext(ctx))
			deliveryParams = append(deliveryParams, maps.Clone(params))
			return &core.OperationResult{Status: http.StatusOK, Body: `{"delivered":true}`}, nil
		},
	})

	req := coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-agent-steps-123",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{
			{
				ID:             "diagnosis",
				TimeoutSeconds: 30,
				Metadata:       map[string]any{"phase": "diagnosis"},
				Agent: &coreworkflow.AgentTurn{
					ProviderName:   "managed",
					Model:          "deep",
					SessionKey:     "shared",
					Prompt:         coreworkflow.Text{Template: "Diagnose the Slack request."},
					ToolRefs:       []coreagent.ToolRef{{Plugin: "datadog", Operation: "queryLogs"}},
					ResponseSchema: map[string]any{"type": "object"},
				},
				OutputDelivery: &coreworkflow.StepDelivery{
					Plugin: &coreworkflow.PluginCall{
						Name:      "notification",
						Operation: "diagnosis_reply",
						Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
							"text": {StepOutput: &coreworkflow.StepOutputSource{StepID: "diagnosis", Path: "agent.text"}},
						}},
					},
				},
			},
			{
				ID:             "pr_fix",
				TimeoutSeconds: 120,
				Inputs: map[string]coreworkflow.Value{
					"root_cause": {StepOutput: &coreworkflow.StepOutputSource{StepID: "diagnosis", Path: "agent.structuredOutput.root_cause"}},
				},
				When: &coreworkflow.StepWhen{
					Value:     coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{StepID: "diagnosis", Path: "agent.structuredOutput.actionable_for_pr"}},
					Equals:    true,
					EqualsSet: true,
				},
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "managed",
					Model:        "deep",
					SessionKey:   "shared",
					Prompt:       coreworkflow.Text{Template: "Use the diagnosis (${inputs.root_cause}) to open a PR."},
					Messages: []coreworkflow.AgentMessage{{
						Role: "system",
						Text: coreworkflow.Text{Template: "Keep route system instructions first."},
					}},
					ToolRefs: []coreagent.ToolRef{{Plugin: "github", Operation: "createPullRequest"}},
				},
				OutputDelivery: &coreworkflow.StepDelivery{
					Plugin: &coreworkflow.PluginCall{
						Name:      "notification",
						Operation: "pr_reply",
						Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
							"text": {StepOutput: &coreworkflow.StepOutputSource{StepID: "pr_fix", Path: "agent.text"}},
						}},
					},
				},
			},
			{
				ID: "not_needed",
				When: &coreworkflow.StepWhen{
					Value:     coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{StepID: "diagnosis", Path: "agent.structuredOutput.actionable_for_pr"}},
					Equals:    false,
					EqualsSet: true,
				},
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "managed",
					Model:        "deep",
					Prompt:       coreworkflow.Text{Template: "This should not run."},
				},
			},
		}},
		Signals: []coreworkflow.Signal{{
			ID:             "signal-1",
			IdempotencyKey: "evt-1",
			Payload:        map[string]any{"text": "please investigate"},
		}},
	}
	p := principal.Canonicalize(&principal.Principal{SubjectID: principal.UserSubjectID("ada")})

	resp, err := runtime.Invoke(principal.WithPrincipal(context.Background(), p), req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("response status = %d, want 200", resp.Status)
	}
	var result workflowStepsResult
	if err := json.Unmarshal([]byte(resp.Body), &result); err != nil {
		t.Fatalf("response body json: %v\n%s", err, resp.Body)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("steps = %#v, want three entries", result.Steps)
	}
	if result.Steps[0].ID != "diagnosis" || result.Steps[0].Status != "succeeded" || result.Steps[0].TurnID != "turn-diagnosis" {
		t.Fatalf("diagnosis step result = %#v", result.Steps[0])
	}
	if result.Steps[1].ID != "pr_fix" || result.Steps[1].Status != "succeeded" || result.Steps[1].TurnID != "turn-pr-fix" {
		t.Fatalf("pr_fix step result = %#v", result.Steps[1])
	}
	if result.Steps[2].ID != "not_needed" || result.Steps[2].Status != "skipped" || result.Steps[2].SkippedReason != "when_false" {
		t.Fatalf("skipped step result = %#v", result.Steps[2])
	}
	finalOutput, _ := result.FinalOutput.(map[string]any)
	finalAgent, _ := finalOutput["agent"].(map[string]any)
	if finalAgent["text"] != "opened PR" {
		t.Fatalf("final output = %#v, want opened PR", result.FinalOutput)
	}
	diagnosisOutput, _ := result.Outputs["diagnosis"].(map[string]any)
	diagnosisAgent, _ := diagnosisOutput["agent"].(map[string]any)
	diagnosisStructured, _ := diagnosisAgent["structuredOutput"].(map[string]any)
	if got := diagnosisStructured["root_cause"]; got != "missing credential" {
		t.Fatalf("diagnosis structured output = %#v", diagnosisStructured)
	}
	if len(agentManager.createTurnRequests) != 2 {
		t.Fatalf("turn requests = %d, want 2", len(agentManager.createTurnRequests))
	}
	firstTurn := agentManager.createTurnRequests[0]
	secondTurn := agentManager.createTurnRequests[1]
	if firstTurn.SessionID != "session-1" || secondTurn.SessionID != "session-1" {
		t.Fatalf("turn sessions = %q/%q, want shared session-1", firstTurn.SessionID, secondTurn.SessionID)
	}
	if firstTurn.TimeoutSeconds != 30 || secondTurn.TimeoutSeconds != 120 {
		t.Fatalf("turn timeouts = %d/%d, want 30/120", firstTurn.TimeoutSeconds, secondTurn.TimeoutSeconds)
	}
	if firstTurn.Metadata["phase"] != "diagnosis" {
		t.Fatalf("diagnosis turn metadata = %#v", firstTurn.Metadata)
	}
	if !firstTurn.ToolRefsSet || !secondTurn.ToolRefsSet {
		t.Fatalf("tool refs set = %v/%v, want both true", firstTurn.ToolRefsSet, secondTurn.ToolRefsSet)
	}
	if got := firstTurn.IdempotencyKey; got != "workflow:temporal:run-agent-steps-123:invocation:signal-id:signal-1:step:diagnosis:agent-turn" {
		t.Fatalf("diagnosis turn idempotency key = %q", got)
	}
	if got := secondTurn.IdempotencyKey; got != "workflow:temporal:run-agent-steps-123:invocation:signal-id:signal-1:step:pr_fix:agent-turn" {
		t.Fatalf("pr_fix turn idempotency key = %q", got)
	}
	if len(secondTurn.Messages) != 2 || secondTurn.Messages[0].Role != "system" || secondTurn.Messages[0].Text != "Keep route system instructions first." {
		t.Fatalf("pr_fix messages = %#v", secondTurn.Messages)
	}
	if !strings.Contains(secondTurn.Messages[1].Text, "missing credential") {
		t.Fatalf("pr_fix messages = %#v", secondTurn.Messages)
	}
	if !slices.Equal(deliveryOperations, []string{"diagnosis_reply", "pr_reply"}) {
		t.Fatalf("delivery operations = %#v", deliveryOperations)
	}
	if deliveryIdempotencyKeys[0] != "workflow:temporal:run-agent-steps-123:invocation:signal-id:signal-1:step:diagnosis:output_delivery" ||
		deliveryIdempotencyKeys[1] != "workflow:temporal:run-agent-steps-123:invocation:signal-id:signal-1:step:pr_fix:output_delivery" {
		t.Fatalf("delivery idempotency keys = %#v", deliveryIdempotencyKeys)
	}
	if deliveryParams[0]["text"] != "diagnosis ready" || deliveryParams[1]["text"] != "opened PR" {
		t.Fatalf("delivery params = %#v", deliveryParams)
	}
	if !slices.Equal(agentManager.events, []string{"create-session", "create-turn", "delivery:diagnosis_reply", "create-turn", "delivery:pr_reply"}) {
		t.Fatalf("events = %#v", agentManager.events)
	}
}

func TestWorkflowRuntimeInvokeSkipsNestedStepOutputWhenDependencySkipped(t *testing.T) {
	t.Parallel()

	runtime := &workflowRuntime{}
	var invoked bool
	runtime.SetInvoker(funcInvoker{
		invoke: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
			invoked = true
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	})

	req := coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{
			{
				ID: "optional",
				When: &coreworkflow.StepWhen{
					Value:     coreworkflow.Value{Literal: false, LiteralSet: true},
					Equals:    true,
					EqualsSet: true,
				},
				Plugin: &coreworkflow.PluginCall{Name: "roadmap", Operation: "sync"},
			},
			{
				ID: "dependent",
				When: &coreworkflow.StepWhen{
					Value: coreworkflow.Value{Object: map[string]coreworkflow.Value{
						"ok": {StepOutput: &coreworkflow.StepOutputSource{StepID: "optional", Path: "plugin.body.json.ok"}},
					}},
					Equals:    map[string]any{"ok": true},
					EqualsSet: true,
				},
				Plugin: &coreworkflow.PluginCall{Name: "roadmap", Operation: "followup"},
			},
		}},
	}

	ctx := principal.WithPrincipal(context.Background(), principal.Canonicalize(&principal.Principal{SubjectID: principal.UserSubjectID("ada")}))
	resp, err := runtime.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("response status = %d, want 200: %s", resp.Status, resp.Body)
	}
	var result workflowStepsResult
	if err := json.Unmarshal([]byte(resp.Body), &result); err != nil {
		t.Fatalf("response body json: %v\n%s", err, resp.Body)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps = %#v, want two skipped steps", result.Steps)
	}
	if result.Steps[0].Status != "skipped" || result.Steps[0].SkippedReason != "when_false" {
		t.Fatalf("optional step = %#v, want when_false skip", result.Steps[0])
	}
	if result.Steps[1].Status != "skipped" || result.Steps[1].SkippedReason != "missing_dependency" {
		t.Fatalf("dependent step = %#v, want missing_dependency skip", result.Steps[1])
	}
	if invoked {
		t.Fatal("invoker was called for skipped workflow steps")
	}
}

func TestWorkflowRuntimeInvokeAgentTargetFinalOutputDeliveryFailureFailsRun(t *testing.T) {
	t.Parallel()

	agentManager := &workflowRuntimeAgentManagerStub{}
	runtime := &workflowRuntime{}
	runtime.SetAgentManager(agentManager)
	runtime.SetInvoker(funcInvoker{
		invoke: func(_ context.Context, _ *principal.Principal, _, _, operation string, _ map[string]any) (*core.OperationResult, error) {
			if operation != "reply" {
				t.Fatalf("delivery operation = %q", operation)
			}
			return &core.OperationResult{Status: http.StatusBadGateway, Body: `{"error":"downstream"}`}, nil
		},
	})

	req := coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-agent-output-failure",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID: "agent",
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "managed",
				Prompt:       coreworkflow.Text{Template: "Summarize the request"},
			},
			OutputDelivery: &coreworkflow.StepDelivery{
				Plugin: &coreworkflow.PluginCall{
					Name:      "notification",
					Operation: "reply",
					Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
						"text": {StepOutput: &coreworkflow.StepOutputSource{StepID: "agent", Path: "agent.text"}},
					}},
				},
			},
		}}},
	}
	p := principal.Canonicalize(&principal.Principal{SubjectID: principal.UserSubjectID("ada")})

	resp, err := runtime.Invoke(principal.WithPrincipal(context.Background(), p), req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusInternalServerError || !strings.Contains(resp.Body, "workflow output_delivery plugin notification.reply returned status 502") {
		t.Fatalf("response = %#v, want output delivery failure", resp)
	}
}

func TestWorkflowRuntimeInvokeAgentTargetWithExecutionRefAcceptsCanonicalTarget(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{
		Steps: []coreworkflow.Step{{
			ID:             "agent",
			TimeoutSeconds: 5,
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "managed",
				Model:        "deep",
				Prompt:       coreworkflow.Text{Template: "Send the status summary"},
			},
		}},
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
	if resp.Status != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if len(agentManager.createTurnRequests) != 1 {
		t.Fatalf("turn requests = %d, want 1", len(agentManager.createTurnRequests))
	}
	turnReq := agentManager.createTurnRequests[0]
	if turnReq.CallerPluginName != "slack" {
		t.Fatalf("caller plugin = %q, want slack", turnReq.CallerPluginName)
	}
	if turnReq.IdempotencyKey != "workflow:temporal:run-agent-123:invocation:signal-id:sig-1:step:agent:agent-turn" {
		t.Fatalf("turn idempotency key = %q", turnReq.IdempotencyKey)
	}
	if len(turnReq.Messages) != 1 || turnReq.Messages[0].Text != "Send the status summary" {
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

	resp, err := runtime.Invoke(principal.WithPrincipal(context.Background(), p), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-agent-123",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID:             "agent",
			TimeoutSeconds: 5,
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "managed",
				Model:        "deep",
				Prompt:       coreworkflow.Text{Template: "Send the status summary"},
			},
		}}},
	})
	if err != nil {
		t.Fatalf("Invoke error = %v", err)
	}
	if resp.Status != http.StatusInternalServerError || !strings.Contains(resp.Body, "workflow agent turn is missing") {
		t.Fatalf("Invoke response = %#v, want missing turn failure", resp)
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
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID:     "mixed",
			Plugin: target.Steps[0].Plugin,
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "managed",
				Prompt:       coreworkflow.Text{Template: "send reminder"},
			},
			TimeoutSeconds: 5,
		}}},
	})
	if err == nil {
		t.Fatal("Invoke mixed agent/plugin target succeeded, want error")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefUsesStoredHumanPrincipalAndSelectors(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user, err := services.Users.FindOrCreateUser(context.Background(), "ada@example.test")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	target := testWorkflowPluginTargetWithPayload("roadmap", "sync", "analytics", "tenant-a", map[string]any{"mode": "full"})
	target.Steps[0].Plugin.CredentialMode = core.ConnectionModeNone
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
	var gotCredentialMode core.ConnectionMode
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
			gotPrincipal = p
			gotProvider = providerName
			gotInstance = instance
			gotConnection = invocation.ConnectionFromContext(ctx)
			gotCredentialMode = invocation.CredentialModeOverrideFromContext(ctx)
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
	if resp.Status != http.StatusOK || !strings.Contains(resp.Body, `"finalStepId":"sync"`) {
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
	if gotCredentialMode != core.ConnectionModeNone {
		t.Fatalf("credential mode override = %q, want %q", gotCredentialMode, core.ConnectionModeNone)
	}
}

func TestWorkflowRuntimeInvokeWorkflowActionDerivesActionFromExecutionRef(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID: "diagnose",
		Plugin: &coreworkflow.PluginCall{
			Name:           "github",
			Operation:      "issues.triage",
			Connection:     "analytics",
			Instance:       "tenant-a",
			CredentialMode: core.ConnectionModeNone,
		},
	}}}
	targetDigest, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		t.Fatalf("TargetFingerprint: %v", err)
	}
	actionTableDigest, err := coreworkflow.TargetActionTableDigest(target)
	if err != nil {
		t.Fatalf("TargetActionTableDigest: %v", err)
	}
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                  "exec-ref-steps",
		ProviderName:        "temporal",
		Target:              target,
		SubjectID:           principal.UserSubjectID("ada"),
		SubjectKind:         string(principal.KindUser),
		CredentialSubjectID: principal.UserSubjectID("ada"),
		Permissions: []core.AccessPermission{
			{Plugin: coreworkflow.StepActionPermissionPlugin, Actions: []string{"step/diagnose/plugin"}},
			{Plugin: "github", Operations: []string{"issues.triage"}},
		},
		TargetDigest:      targetDigest,
		ActionTableDigest: actionTableDigest,
		Generation:        1,
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	var gotProvider, gotInstance, gotOperation, gotConnection string
	var gotIdempotencyKey string
	var gotParams map[string]any
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, _ *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
			gotProvider = providerName
			gotInstance = instance
			gotOperation = operation
			gotConnection = invocation.ConnectionFromContext(ctx)
			gotIdempotencyKey = invocation.IdempotencyKeyFromContext(ctx)
			gotParams = maps.Clone(params)
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	})

	resp, err := runtime.InvokeWorkflowAction(context.Background(), coreworkflow.InvokeActionRequest{
		ProviderName: "temporal",
		Selector: coreworkflow.HostActionSelector{
			ExecutionRef:           "exec-ref-steps",
			ExecutionRefGeneration: 1,
			RunID:                  "run-1",
			StepID:                 "diagnose",
			ActionID:               "step/diagnose/plugin",
			AttemptNumber:          1,
			IdempotencyKey:         "run-1:diagnose:plugin:1",
		},
		Plugin: &coreworkflow.PluginActionPayload{Input: map[string]any{"title": "bug"}},
	})
	if err != nil {
		t.Fatalf("InvokeWorkflowAction: %v", err)
	}
	if resp == nil || resp.Status != http.StatusOK || resp.Body != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}
	if gotProvider != "github" || gotOperation != "issues.triage" || gotInstance != "tenant-a" || gotConnection != "analytics" {
		t.Fatalf("invoked (%q, %q, %q, %q), want stored step action", gotProvider, gotOperation, gotInstance, gotConnection)
	}
	if gotParams["title"] != "bug" {
		t.Fatalf("params = %#v, want provider-evaluated input", gotParams)
	}
	if gotIdempotencyKey != "run-1:diagnose:plugin:1" {
		t.Fatalf("idempotency key = %q, want selector idempotency key", gotIdempotencyKey)
	}
}

func TestWorkflowRuntimeInvokeWorkflowActionRejectsInvalidExecutionRefTargetDigest(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{
		Steps: []coreworkflow.Step{{
			ID: "diagnose",
			Plugin: &coreworkflow.PluginCall{
				Name:      "github",
				Operation: "issues.triage",
			},
		}},
	}
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                  "exec-ref-invalid-target",
		ProviderName:        "temporal",
		Target:              target,
		SubjectID:           principal.UserSubjectID("ada"),
		SubjectKind:         string(principal.KindUser),
		CredentialSubjectID: principal.UserSubjectID("ada"),
		Permissions: []core.AccessPermission{
			{Plugin: coreworkflow.StepActionPermissionPlugin, Actions: []string{"step/diagnose/plugin"}},
			{Plugin: "github", Operations: []string{"issues.triage"}},
		},
		TargetDigest: "wrong-target-digest",
		Generation:   1,
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	runtime.SetInvoker(funcInvoker{
		invoke: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
			t.Fatal("invoker should not be called with an invalid execution ref target digest")
			return nil, nil
		},
	})

	_, err := runtime.InvokeWorkflowAction(context.Background(), coreworkflow.InvokeActionRequest{
		ProviderName: "temporal",
		Selector: coreworkflow.HostActionSelector{
			ExecutionRef:           "exec-ref-invalid-target",
			ExecutionRefGeneration: 1,
			RunID:                  "run-1",
			StepID:                 "diagnose",
			ActionID:               "step/diagnose/plugin",
			AttemptNumber:          1,
			IdempotencyKey:         "run-1:diagnose:plugin:1",
		},
		Plugin: &coreworkflow.PluginActionPayload{Input: map[string]any{"title": "bug"}},
	})
	if err == nil || !strings.Contains(err.Error(), "target digest mismatch") {
		t.Fatalf("error = %v, want invalid target digest rejection", err)
	}
}

func TestWorkflowRuntimeInvokeWorkflowActionValidatesDefinitionSelector(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID: "diagnose",
		Plugin: &coreworkflow.PluginCall{
			Name:      "github",
			Operation: "issues.triage",
		},
	}}}
	targetDigest, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		t.Fatalf("TargetFingerprint: %v", err)
	}
	actionTableDigest, err := coreworkflow.TargetActionTableDigest(target)
	if err != nil {
		t.Fatalf("TargetActionTableDigest: %v", err)
	}
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                  "workflow_definition:triage:3",
		ProviderName:        "temporal",
		Target:              target,
		SubjectID:           principal.UserSubjectID("ada"),
		SubjectKind:         string(principal.KindUser),
		CredentialSubjectID: principal.UserSubjectID("ada"),
		Permissions: []core.AccessPermission{
			{Plugin: coreworkflow.StepActionPermissionPlugin, Actions: []string{"step/diagnose/plugin"}},
			{Plugin: "github", Operations: []string{"issues.triage"}},
		},
		TargetDigest:      targetDigest,
		ActionTableDigest: actionTableDigest,
		Generation:        1,
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	runtime.SetInvoker(funcInvoker{
		invoke: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
			t.Fatal("invoker should not be called with mismatched deployment selector")
			return nil, nil
		},
	})

	baseSelector := coreworkflow.HostActionSelector{
		ExecutionRef:           "workflow_definition:triage:3",
		ExecutionRefGeneration: 1,
		RunID:                  "run-1",
		StepID:                 "diagnose",
		ActionID:               "step/diagnose/plugin",
		AttemptNumber:          1,
		IdempotencyKey:         "run-1:diagnose:plugin:1",
	}

	for _, tc := range []struct {
		name     string
		mutate   func(*coreworkflow.HostActionSelector)
		wantText string
	}{
		{
			name: "deployment id",
			mutate: func(selector *coreworkflow.HostActionSelector) {
				selector.DefinitionID = "other"
			},
			wantText: "definition_id mismatch",
		},
		{
			name: "deployment generation",
			mutate: func(selector *coreworkflow.HostActionSelector) {
				selector.DefinitionID = "triage"
				selector.DefinitionGeneration = 4
			},
			wantText: "definition_generation mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selector := baseSelector
			tc.mutate(&selector)
			_, err := runtime.InvokeWorkflowAction(context.Background(), coreworkflow.InvokeActionRequest{
				ProviderName: "temporal",
				Selector:     selector,
				Plugin:       &coreworkflow.PluginActionPayload{Input: map[string]any{"title": "bug"}},
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("error = %v, want %s", err, tc.wantText)
			}
		})
	}
}

func TestWorkflowRuntimeInvokeWorkflowActionUsesSourceDefinitionGeneration(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID: "diagnose",
		Plugin: &coreworkflow.PluginCall{
			Name:      "github",
			Operation: "issues.triage",
		},
	}}}
	targetDigest, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		t.Fatalf("TargetFingerprint: %v", err)
	}
	actionTableDigest, err := coreworkflow.TargetActionTableDigest(target)
	if err != nil {
		t.Fatalf("TargetActionTableDigest: %v", err)
	}
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                         "workflow_schedule:schedule-1:ref-1",
		ProviderName:               "temporal",
		Target:                     target,
		SourceDefinitionID:         "schedule-1",
		SourceDefinitionGeneration: 1,
		SubjectID:                  principal.UserSubjectID("ada"),
		SubjectKind:                string(principal.KindUser),
		CredentialSubjectID:        principal.UserSubjectID("ada"),
		Permissions: []core.AccessPermission{
			{Plugin: coreworkflow.StepActionPermissionPlugin, Actions: []string{"step/diagnose/plugin"}},
			{Plugin: "github", Operations: []string{"issues.triage"}},
		},
		TargetDigest:      targetDigest,
		ActionTableDigest: actionTableDigest,
		Generation:        1,
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	invoked := false
	runtime.SetInvoker(funcInvoker{
		invoke: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
			invoked = true
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	})

	_, err = runtime.InvokeWorkflowAction(context.Background(), coreworkflow.InvokeActionRequest{
		ProviderName: "temporal",
		Selector: coreworkflow.HostActionSelector{
			ExecutionRef:           "workflow_schedule:schedule-1:ref-1",
			ExecutionRefGeneration: 1,
			RunID:                  "run-1",
			DefinitionID:           "schedule-1",
			DefinitionGeneration:   1,
			StepID:                 "diagnose",
			ActionID:               "step/diagnose/plugin",
			AttemptNumber:          1,
			IdempotencyKey:         "run-1:diagnose:plugin:1",
		},
		Plugin: &coreworkflow.PluginActionPayload{Input: map[string]any{"title": "bug"}},
	})
	if err != nil {
		t.Fatalf("InvokeWorkflowAction: %v", err)
	}
	if !invoked {
		t.Fatal("invoker was not called")
	}
}

func TestWorkflowRuntimeInvokeWorkflowActionRoutesDeliveryAction(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{Steps: []coreworkflow.Step{
		{
			ID: "plugin_step",
			Plugin: &coreworkflow.PluginCall{
				Name:      "github",
				Operation: "issues.triage",
			},
			OutputDelivery: &coreworkflow.StepDelivery{Plugin: &coreworkflow.PluginCall{
				Name:           "slack",
				Operation:      "chat.postMessage",
				Connection:     "alerts",
				Instance:       "engineering",
				CredentialMode: core.ConnectionModeNone,
			}},
		},
		{
			ID: "agent_step",
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "managed",
				Model:        "fast",
			},
			OutputDelivery: &coreworkflow.StepDelivery{Plugin: &coreworkflow.PluginCall{
				Name:      "notification",
				Operation: "reply",
			}},
		},
	}}
	targetDigest, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		t.Fatalf("TargetFingerprint: %v", err)
	}
	actionTableDigest, err := coreworkflow.TargetActionTableDigest(target)
	if err != nil {
		t.Fatalf("TargetActionTableDigest: %v", err)
	}
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                  "exec-ref-delivery",
		ProviderName:        "temporal",
		Target:              target,
		SubjectID:           principal.UserSubjectID("ada"),
		SubjectKind:         string(principal.KindUser),
		CredentialSubjectID: principal.UserSubjectID("ada"),
		Permissions: []core.AccessPermission{
			{Plugin: coreworkflow.StepActionPermissionPlugin, Actions: []string{"step/plugin_step/delivery", "step/agent_step/delivery"}},
			{Plugin: "slack", Operations: []string{"chat.postMessage"}},
			{Plugin: "notification", Operations: []string{"reply"}},
		},
		TargetDigest:      targetDigest,
		ActionTableDigest: actionTableDigest,
		Generation:        1,
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	type pluginCall struct {
		provider       string
		instance       string
		operation      string
		connection     string
		credentialMode core.ConnectionMode
		idempotencyKey string
		params         map[string]any
	}
	var calls []pluginCall
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, _ *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
			calls = append(calls, pluginCall{
				provider:       providerName,
				instance:       instance,
				operation:      operation,
				connection:     invocation.ConnectionFromContext(ctx),
				credentialMode: invocation.CredentialModeOverrideFromContext(ctx),
				idempotencyKey: invocation.IdempotencyKeyFromContext(ctx),
				params:         maps.Clone(params),
			})
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	})

	for _, tc := range []struct {
		stepID         string
		idempotencyKey string
		input          map[string]any
	}{
		{stepID: "plugin_step", idempotencyKey: "run-1:plugin-step:delivery:1", input: map[string]any{"text": "plugin done"}},
		{stepID: "agent_step", idempotencyKey: "run-1:agent-step:delivery:1", input: map[string]any{"text": "agent done"}},
	} {
		_, err := runtime.InvokeWorkflowAction(context.Background(), coreworkflow.InvokeActionRequest{
			ProviderName: "temporal",
			Selector: coreworkflow.HostActionSelector{
				ExecutionRef:           "exec-ref-delivery",
				ExecutionRefGeneration: 1,
				RunID:                  "run-1",
				StepID:                 tc.stepID,
				ActionID:               "step/" + tc.stepID + "/delivery",
				AttemptNumber:          1,
				IdempotencyKey:         tc.idempotencyKey,
			},
			Plugin: &coreworkflow.PluginActionPayload{Input: tc.input},
		})
		if err != nil {
			t.Fatalf("InvokeWorkflowAction delivery %s: %v", tc.stepID, err)
		}
	}

	if len(calls) != 2 {
		t.Fatalf("delivery calls = %#v, want 2 calls", calls)
	}
	if calls[0].provider != "slack" || calls[0].operation != "chat.postMessage" || calls[0].instance != "engineering" || calls[0].connection != "alerts" {
		t.Fatalf("plugin-step delivery target = %#v, want slack.chat.postMessage on engineering/alerts", calls[0])
	}
	if calls[0].credentialMode != core.ConnectionModeNone {
		t.Fatalf("plugin-step delivery credential mode = %q, want none", calls[0].credentialMode)
	}
	if calls[0].idempotencyKey != "run-1:plugin-step:delivery:1" {
		t.Fatalf("plugin-step delivery idempotency key = %q", calls[0].idempotencyKey)
	}
	if calls[0].params["text"] != "plugin done" {
		t.Fatalf("plugin-step delivery params = %#v, want provider evaluated input", calls[0].params)
	}
	if calls[1].provider != "notification" || calls[1].operation != "reply" {
		t.Fatalf("agent-step delivery target = %#v, want notification.reply", calls[1])
	}
	if calls[1].idempotencyKey != "run-1:agent-step:delivery:1" {
		t.Fatalf("agent-step delivery idempotency key = %q", calls[1].idempotencyKey)
	}
	if calls[1].params["text"] != "agent done" {
		t.Fatalf("agent-step delivery params = %#v, want provider evaluated input", calls[1].params)
	}
}

func TestWorkflowRuntimeInvokeWorkflowActionRejectsAgentTurnAction(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{Steps: []coreworkflow.Step{{
		ID: "diagnose",
		Agent: &coreworkflow.AgentTurn{
			ProviderName: "managed",
			Model:        "fast",
		},
	}}}
	targetDigest, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		t.Fatalf("TargetFingerprint: %v", err)
	}
	actionTableDigest, err := coreworkflow.TargetActionTableDigest(target)
	if err != nil {
		t.Fatalf("TargetActionTableDigest: %v", err)
	}
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                  "exec-ref-agent-action",
		ProviderName:        "temporal",
		Target:              target,
		SubjectID:           principal.UserSubjectID("ada"),
		SubjectKind:         string(principal.KindUser),
		CredentialSubjectID: principal.UserSubjectID("ada"),
		Permissions: []core.AccessPermission{
			{Plugin: coreworkflow.StepActionPermissionPlugin, Actions: []string{"step/diagnose/agent-turn"}},
		},
		TargetDigest:      targetDigest,
		ActionTableDigest: actionTableDigest,
		Generation:        1,
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	runtime.SetInvoker(funcInvoker{
		invoke: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
			t.Fatal("invoker should not be called for agent-turn action through plugin callback")
			return nil, nil
		},
	})

	_, err = runtime.InvokeWorkflowAction(context.Background(), coreworkflow.InvokeActionRequest{
		ProviderName: "temporal",
		Selector: coreworkflow.HostActionSelector{
			ExecutionRef:           "exec-ref-agent-action",
			ExecutionRefGeneration: 1,
			RunID:                  "run-1",
			StepID:                 "diagnose",
			ActionID:               "step/diagnose/agent-turn",
			AttemptNumber:          1,
			IdempotencyKey:         "run-1:diagnose:agent-turn:1",
		},
		Plugin: &coreworkflow.PluginActionPayload{},
	})
	if err == nil || !strings.Contains(err.Error(), "not a plugin callback action") {
		t.Fatalf("error = %v, want plugin callback action rejection", err)
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

func TestWorkflowRuntimeInvokeExecutionRefAuthorizesInternalConnections(t *testing.T) {
	t.Parallel()

	target := testWorkflowPluginTarget("brain", "sources.sync")
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                  "exec-ref-config-source-sync",
		ProviderName:        "temporal",
		Target:              target,
		SubjectID:           "system:config",
		SubjectKind:         string(principal.Kind("system")),
		AuthSource:          "config",
		CredentialSubjectID: "system:config",
		Permissions: []core.AccessPermission{{
			Plugin:     "brain",
			Operations: []string{"sources.sync"},
		}},
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}

	var gotInternalConnectionAccess bool
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, _ *principal.Principal, providerName, _ string, operation string, _ map[string]any) (*core.OperationResult, error) {
			gotInternalConnectionAccess = invocation.InternalConnectionAccessFromContext(ctx)
			if providerName != "brain" || operation != "sources.sync" {
				t.Fatalf("target = %s.%s, want brain.sources.sync", providerName, operation)
			}
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	})

	if _, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "exec-ref-config-source-sync",
		Target:       target,
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !gotInternalConnectionAccess {
		t.Fatal("workflow execution ref did not authorize internal connection access")
	}
}

func TestWorkflowRuntimeInvokeConfigExecutionRefRunAsUsesServiceAccountPrincipal(t *testing.T) {
	t.Parallel()

	target := testWorkflowPluginTarget("brain", "sources.sync")
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                  "exec-ref-config-runas-source-sync",
		ProviderName:        "temporal",
		Target:              target,
		SubjectID:           "system:config",
		SubjectKind:         "system",
		DisplayName:         "Gestalt config",
		AuthSource:          "config",
		CredentialSubjectID: "system:config",
		RunAs: &core.RunAsSubject{
			SubjectID:   "service_account:brain-sync",
			SubjectKind: "service_account",
			DisplayName: "Brain sync",
			AuthSource:  "config",
		},
		Permissions: []core.AccessPermission{{
			Plugin:     "brain",
			Operations: []string{"sources.sync"},
		}},
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}

	var gotPrincipal *principal.Principal
	var gotAudit invocation.RunAsAuditContext
	var gotInternalConnectionAccess bool
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, p *principal.Principal, providerName, _ string, operation string, _ map[string]any) (*core.OperationResult, error) {
			gotPrincipal = p
			gotAudit = invocation.RunAsAuditFromContext(ctx)
			gotInternalConnectionAccess = invocation.InternalConnectionAccessFromContext(ctx)
			if providerName != "brain" || operation != "sources.sync" {
				t.Fatalf("target = %s.%s, want brain.sources.sync", providerName, operation)
			}
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	})

	if _, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "exec-ref-config-runas-source-sync",
		Target:       target,
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if gotPrincipal == nil || gotPrincipal.SubjectID != "service_account:brain-sync" || gotPrincipal.Kind != principal.Kind("service_account") {
		t.Fatalf("principal = %#v, want brain sync service account", gotPrincipal)
	}
	if gotPrincipal.CredentialSubjectID != "service_account:brain-sync" {
		t.Fatalf("credential subject = %q, want runAs subject", gotPrincipal.CredentialSubjectID)
	}
	if gotPrincipal.DisplayName != "Brain sync" || gotPrincipal.AuthSource() != "config" {
		t.Fatalf("principal display/auth = (%q, %q)", gotPrincipal.DisplayName, gotPrincipal.AuthSource())
	}
	if gotAudit.AgentSubject == nil || gotAudit.AgentSubject.SubjectID != "system:config" || gotAudit.AgentSubject.CredentialSubjectID != "system:config" {
		t.Fatalf("audit agent subject = %#v, want config owner", gotAudit.AgentSubject)
	}
	if gotAudit.RunAsSubject == nil || gotAudit.RunAsSubject.SubjectID != "service_account:brain-sync" {
		t.Fatalf("audit runAs subject = %#v, want brain sync service account", gotAudit.RunAsSubject)
	}
	if !gotInternalConnectionAccess {
		t.Fatal("config-owned runAs execution ref did not authorize internal connection access")
	}
}

func TestWorkflowRuntimeInvokeWorkflowActionRejectsMissingStepActionPermissions(t *testing.T) {
	t.Parallel()

	target := testWorkflowPluginTarget("roadmap", "sync")
	actionID, ok := coreworkflow.StepPluginActionID(target.Steps[0].ID)
	if !ok {
		t.Fatal("step action id was not generated")
	}
	targetDigest, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		t.Fatalf("TargetFingerprint: %v", err)
	}
	actionTableDigest, err := coreworkflow.TargetActionTableDigest(target)
	if err != nil {
		t.Fatalf("TargetActionTableDigest: %v", err)
	}

	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:                "exec-ref-no-step-actions",
		ProviderName:      "temporal",
		Target:            target,
		SubjectID:         "user:ada",
		SubjectKind:       string(principal.KindUser),
		Generation:        1,
		TargetDigest:      targetDigest,
		ActionTableDigest: actionTableDigest,
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	var invoked bool
	runtime.SetInvoker(funcInvoker{
		invoke: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
			invoked = true
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	})

	_, err = runtime.InvokeWorkflowAction(context.Background(), coreworkflow.InvokeActionRequest{
		ProviderName: "temporal",
		Selector: coreworkflow.HostActionSelector{
			ExecutionRef:           "exec-ref-no-step-actions",
			ExecutionRefGeneration: 1,
			RunID:                  "run-1",
			StepID:                 target.Steps[0].ID,
			ActionID:               actionID,
			AttemptNumber:          1,
			IdempotencyKey:         "action-1",
		},
		Plugin: &coreworkflow.PluginActionPayload{Input: map[string]any{}},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("InvokeWorkflowAction error = %v, want authorization denied", err)
	}
	if invoked {
		t.Fatal("invoker was called without step-action permissions")
	}
}

func TestWorkflowRuntimeInvokeUserExecutionRefDoesNotAuthorizeInternalConnections(t *testing.T) {
	t.Parallel()

	target := testWorkflowPluginTarget("brain", "sources.sync")
	refProvider := newWorkflowRuntimeExecutionRefProvider()
	if _, err := refProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "exec-ref-user-source-sync",
		ProviderName: "temporal",
		Target:       target,
		SubjectID:    "user:ada",
		SubjectKind:  string(principal.KindUser),
		AuthSource:   "session",
		Permissions: []core.AccessPermission{{
			Plugin:     "brain",
			Operations: []string{"sources.sync"},
		}},
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}

	var gotInternalConnectionAccess bool
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, _ *principal.Principal, _, _, _ string, _ map[string]any) (*core.OperationResult, error) {
			gotInternalConnectionAccess = invocation.InternalConnectionAccessFromContext(ctx)
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	})

	if _, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "exec-ref-user-source-sync",
		Target:       target,
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if gotInternalConnectionAccess {
		t.Fatal("user-owned workflow execution ref unexpectedly authorized internal connection access")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefRechecksAuthorizationThroughBroker(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user, err := services.Users.FindOrCreateUser(context.Background(), "ada@example.test")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	target := testWorkflowPluginTargetWithPayload("roadmap", "sync", "analytics", "tenant-a", nil)
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

	authz, err := authorization.New(config.AuthorizationStaticConfig(config.AuthorizationConfig{
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
	}))

	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	runtime := &workflowRuntime{
		providers: map[string]coreworkflow.Provider{"temporal": refProvider},
	}
	runtime.SetInvoker(invocation.NewBroker(&providers.Providers, services.Users, services.ExternalCredentials, invocation.WithAuthorizer(authz)))

	resp, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "exec-ref-denied",
		Target:       target,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusInternalServerError || !strings.Contains(resp.Body, "authorization denied") {
		t.Fatalf("Invoke response = %#v, want authorization failure", resp)
	}
	if executed {
		t.Fatal("expected provider execution to be skipped")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefPreservesTokenPermissionCeiling(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	ctx := context.Background()

	target := testWorkflowPluginTargetWithPayload("roadmap", "export", "analytics", "tenant-a", nil)
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

	resp, err := runtime.Invoke(ctx, coreworkflow.InvokeOperationRequest{
		ProviderName: "basic",
		RunID:        "run-123",
		Target:       target,
		ExecutionRef: "exec-ref-123",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusInternalServerError || !strings.Contains(resp.Body, "scope denied") {
		t.Fatalf("Invoke response = %#v, want scope denied failure", resp)
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

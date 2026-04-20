package bootstrap

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
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
	runtime := &workflowRuntime{
		bindings: map[string]workflowBinding{
			"roadmap": {
				providerName: "temporal",
				operations: map[string]struct{}{
					"sync": {},
				},
			},
		},
	}

	var gotPrincipal *principal.Principal
	var gotProvider string
	var gotOperation string
	var gotParams map[string]any
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, p *principal.Principal, providerName, _ string, operation string, params map[string]any) (*core.OperationResult, error) {
			gotPrincipal = p
			gotProvider = providerName
			gotOperation = operation
			gotParams = params
			ctx = principal.WithPrincipal(ctx, p)
			return roundTripRemote.Execute(ctx, operation, params, "")
		},
	})

	resp, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		PluginName:   "roadmap",
		RunID:        "run-123",
		ExecutionRef: "exec-ref-123",
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
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusAccepted || resp.Body != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}
	if gotPrincipal == nil || gotPrincipal.SubjectID != principal.WorkloadSubjectID("workflow.roadmap") {
		t.Fatalf("principal = %#v", gotPrincipal)
	}
	if gotProvider != "roadmap" {
		t.Fatalf("provider = %q, want %q", gotProvider, "roadmap")
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
	if got := roundTripProvider.workflowContext["executionRef"]; got != "exec-ref-123" {
		t.Fatalf("workflow executionRef = %#v, want %q", got, "exec-ref-123")
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

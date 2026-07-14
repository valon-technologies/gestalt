package workflows

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workflowTelemetryProviderServer struct {
	proto.UnimplementedWorkflowServer
	proto.UnimplementedProviderLifecycleServer
}

func (workflowTelemetryProviderServer) GetProviderIdentity(context.Context, *emptypb.Empty) (*proto.ProviderIdentity, error) {
	return &proto.ProviderIdentity{
		Kind:               proto.ProviderKind_PROVIDER_KIND_WORKFLOW,
		Name:               "workflow-metrics",
		MinProtocolVersion: proto.CurrentProtocolVersion,
		MaxProtocolVersion: proto.CurrentProtocolVersion,
	}, nil
}

func (workflowTelemetryProviderServer) ConfigureProvider(context.Context, *proto.ConfigureProviderRequest) (*proto.ConfigureProviderResponse, error) {
	return &proto.ConfigureProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (workflowTelemetryProviderServer) HealthCheck(context.Context, *emptypb.Empty) (*proto.HealthCheckResponse, error) {
	return &proto.HealthCheckResponse{Ready: true}, nil
}

func (workflowTelemetryProviderServer) ApplyDefinition(_ context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	return telemetryDefinition(req.GetSpec().GetId(), req.GetSpec().GetTarget()), nil
}

func (workflowTelemetryProviderServer) GetDefinition(_ context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	return telemetryDefinition(req.GetDefinitionId(), telemetryProtoAppStepTarget("github", "issues.triage")), nil
}

func (workflowTelemetryProviderServer) ListDefinitions(context.Context, *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	return &proto.ListWorkflowProviderDefinitionsResponse{
		Definitions: []*proto.WorkflowDefinition{telemetryDefinition("definition-1", telemetryProtoAppStepTarget("github", "issues.triage"))},
	}, nil
}

func (workflowTelemetryProviderServer) SetDefinitionPaused(_ context.Context, req *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	definition := telemetryDefinition(req.GetDefinitionId(), telemetryProtoAppStepTarget("github", "issues.triage"))
	definition.Paused = req.GetPaused()
	return definition, nil
}

func (workflowTelemetryProviderServer) SetActivationPaused(_ context.Context, req *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	definition := telemetryDefinition(req.GetDefinitionId(), telemetryProtoAppStepTarget("github", "issues.triage"))
	definition.Activations = []*proto.WorkflowActivation{{Id: req.GetActivationId(), Paused: req.GetPaused()}}
	return definition, nil
}

func (workflowTelemetryProviderServer) DeleteDefinition(context.Context, *proto.DeleteWorkflowProviderDefinitionRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (workflowTelemetryProviderServer) StartRun(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return telemetryRun("run-start", proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING), nil
}

func (workflowTelemetryProviderServer) GetRun(_ context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	if req.GetRunId() == "fail" {
		return nil, status.Error(codes.InvalidArgument, "bad run")
	}
	return telemetryRun(req.GetRunId(), proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_SUCCEEDED), nil
}

func (workflowTelemetryProviderServer) ListRuns(context.Context, *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	return &proto.ListWorkflowProviderRunsResponse{Runs: []*proto.WorkflowRun{telemetryRun("run-1", proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_SUCCEEDED)}}, nil
}

func (workflowTelemetryProviderServer) GetRunEvents(context.Context, *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	return &proto.GetWorkflowProviderRunEventsResponse{Events: []*proto.WorkflowRunEvent{{Id: "event-1", RunId: "run-1", Type: "step.completed"}}}, nil
}

func (workflowTelemetryProviderServer) GetRunOutput(context.Context, *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	return &proto.GetWorkflowProviderRunOutputResponse{Output: structpb.NewStringValue("ok")}, nil
}

func (workflowTelemetryProviderServer) CancelRun(context.Context, *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return telemetryRun("run-canceled", proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_CANCELED), nil
}

func (workflowTelemetryProviderServer) SignalRun(context.Context, *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return &proto.SignalWorkflowRunResponse{Run: telemetryRun("run-signaled", proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING)}, nil
}

func (workflowTelemetryProviderServer) SignalOrStartRun(context.Context, *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return &proto.SignalWorkflowRunResponse{
		Run:        telemetryRun("run-signal-started", proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING),
		StartedRun: true,
	}, nil
}

func (workflowTelemetryProviderServer) DeliverEvent(context.Context, *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	return &proto.WorkflowEvent{Type: "ignored"}, nil
}

func TestRemoteWorkflowRecordsProviderOperationMetricsAcrossTransport(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	workflow := newTelemetryRemoteWorkflow(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)

	definitionTarget := telemetryProtoAppStepTarget("github", "issues.triage")
	type workflowProviderMetricCall struct {
		name      string
		operation string
		attrs     map[string]string
		call      func(context.Context, coreworkflow.Provider) error
	}
	calls := []workflowProviderMetricCall{
		{"apply definition", observability.WorkflowOperationApplyDefinition, workflowMetricAttrsWith(observability.WorkflowOperationApplyDefinition, observability.WorkflowTriggerKindNone, observability.WorkflowTargetKindSteps, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ApplyDefinition(ctx, &proto.ApplyWorkflowProviderDefinitionRequest{Spec: &proto.WorkflowDefinitionSpec{Id: "definition-1", Target: definitionTarget}})
			return err
		}},
		{"get definition", observability.WorkflowOperationGetDefinition, workflowMetricAttrs(observability.WorkflowOperationGetDefinition), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.GetDefinition(ctx, &proto.GetWorkflowProviderDefinitionRequest{Provider: "local", DefinitionId: "definition-1"})
			return err
		}},
		{"list definitions", observability.WorkflowOperationListDefinitions, workflowMetricAttrs(observability.WorkflowOperationListDefinitions), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ListDefinitions(ctx, &proto.ListWorkflowProviderDefinitionsRequest{Provider: "local"})
			return err
		}},
		{"set definition paused", observability.WorkflowOperationSetDefinitionPaused, workflowMetricAttrs(observability.WorkflowOperationSetDefinitionPaused), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.SetDefinitionPaused(ctx, &proto.SetWorkflowProviderDefinitionPausedRequest{Provider: "local", DefinitionId: "definition-1", Paused: true})
			return err
		}},
		{"set activation paused", observability.WorkflowOperationSetActivationPaused, workflowMetricAttrs(observability.WorkflowOperationSetActivationPaused), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.SetActivationPaused(ctx, &proto.SetWorkflowProviderActivationPausedRequest{Provider: "local", DefinitionId: "definition-1", ActivationId: "github_pr", Paused: true})
			return err
		}},
		{"delete definition", observability.WorkflowOperationDeleteDefinition, workflowMetricAttrs(observability.WorkflowOperationDeleteDefinition), func(ctx context.Context, p coreworkflow.Provider) error {
			return p.DeleteDefinition(ctx, &proto.DeleteWorkflowProviderDefinitionRequest{Provider: "local", DefinitionId: "definition-1"})
		}},
		{"start run", observability.WorkflowOperationStartRun, workflowMetricAttrsWith(observability.WorkflowOperationStartRun, observability.WorkflowTriggerKindManual, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.StartRun(ctx, &proto.StartWorkflowProviderRunRequest{DefinitionId: "definition-1"})
			return err
		}},
		{"get run", observability.WorkflowOperationGetRun, workflowMetricAttrs(observability.WorkflowOperationGetRun), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.GetRun(ctx, &proto.GetWorkflowProviderRunRequest{Provider: "local", RunId: "run-1"})
			return err
		}},
		{"list runs", observability.WorkflowOperationListRuns, workflowMetricAttrs(observability.WorkflowOperationListRuns), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ListRuns(ctx, &proto.ListWorkflowProviderRunsRequest{Provider: "local"})
			return err
		}},
		{"get run events", observability.WorkflowOperationGetRunEvents, workflowMetricAttrs(observability.WorkflowOperationGetRunEvents), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.GetRunEvents(ctx, &proto.GetWorkflowProviderRunEventsRequest{Provider: "local", RunId: "run-1"})
			return err
		}},
		{"get run output", observability.WorkflowOperationGetRunOutput, workflowMetricAttrs(observability.WorkflowOperationGetRunOutput), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.GetRunOutput(ctx, &proto.GetWorkflowProviderRunOutputRequest{Provider: "local", RunId: "run-1"})
			return err
		}},
		{"cancel run", observability.WorkflowOperationCancelRun, workflowMetricAttrs(observability.WorkflowOperationCancelRun), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.CancelRun(ctx, &proto.CancelWorkflowProviderRunRequest{Provider: "local", RunId: "run-1"})
			return err
		}},
		{"signal run", observability.WorkflowOperationSignalRun, workflowMetricAttrsWith(observability.WorkflowOperationSignalRun, observability.WorkflowTriggerKindSignal, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.SignalRun(ctx, &proto.SignalWorkflowProviderRunRequest{RunId: "run-1", Signal: &proto.WorkflowSignal{Name: "poke"}})
			return err
		}},
		{"signal or start run", observability.WorkflowOperationSignalOrStartRun, workflowMetricAttrsWith(observability.WorkflowOperationSignalOrStartRun, observability.WorkflowTriggerKindSignal, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.SignalOrStartRun(ctx, &proto.SignalOrStartWorkflowProviderRunRequest{DefinitionId: "definition-1", Signal: &proto.WorkflowSignal{Name: "poke"}})
			return err
		}},
		{"deliver event", observability.WorkflowOperationDeliverEvent, workflowMetricAttrsWith(observability.WorkflowOperationDeliverEvent, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.DeliverEvent(ctx, &proto.DeliverWorkflowProviderEventRequest{Event: &proto.WorkflowEvent{Type: "ignored"}})
			return err
		}},
		{"ping", observability.WorkflowOperationPing, workflowMetricAttrs(observability.WorkflowOperationPing), func(ctx context.Context, p coreworkflow.Provider) error {
			return p.Ping(ctx)
		}},
	}
	for _, tc := range calls {
		if err := tc.call(ctx, workflow); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	if _, err := workflow.GetRun(ctx, &proto.GetWorkflowProviderRunRequest{Provider: "local", RunId: "fail"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("failing GetRun error = %v, want InvalidArgument", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	for _, tc := range calls {
		metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.provider.operation.count", 1, tc.attrs)
		metrictest.RequireFloat64Histogram(t, rm, "gestaltd.workflows.provider.operation.duration", tc.attrs)
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.provider.operation.error_count", 1, map[string]string{
		"gestaltd.workflow.provider.name":    "workflow-metrics",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationGetRun,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindNone,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindUnknown,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusUnknown,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
		"error.type":                         "grpc.invalid_argument",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.events.delivered.count", 1, workflowMetricAttrsWith(observability.WorkflowOperationDeliverEvent, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown))
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.runs.started.count", 1, map[string]string{
		"gestaltd.workflow.provider.name":    "workflow-metrics",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationStartRun,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindManual,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindSteps,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusPending,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.runs.started.count", 1, map[string]string{
		"gestaltd.workflow.provider.name":    "workflow-metrics",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationSignalOrStartRun,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindSignal,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindSteps,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusPending,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
	})
}

func TestWorkflowProviderRecordsSignalOrStartMetricsAcrossTransport(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	authz := &managerServerAuthorizationProvider{allowed: true}
	provider := newWorkflowManagerTelemetryProvider()
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
		N:        "github",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "github",
			Operations: []catalog.CatalogOperation{
				{ID: "issues.triage", Method: "POST"},
			},
		},
	})
	invoker := invocation.NewBroker(providers, nil, nil,
		invocation.WithAuthorizationProvider(authz),
		invocation.WithProviderKinds(map[string]invocation.ProviderKind{"github": invocation.ProviderKindApp}),
	)
	manager := workflowmanager.New(workflowmanager.Config{
		Providers: providers,
		Workflow:  workflowManagerTelemetryControl{provider: provider},
		Invoker:   invoker,
	})
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer(grpc.UnaryInterceptor(func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		return handler(metricutil.WithMeterProvider(ctx, metrics.Provider), req)
	}))
	proto.RegisterWorkflowServer(srv, NewProviderServer("slack", manager, authz))
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})
	conn, err := grpc.NewClient("passthrough:///workflow-manager",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := proto.NewWorkflowClient(conn)

	reqCtx := workflowManagerTelemetryRequestContext()
	successRequest := workflowManagerTelemetrySignalOrStartRequest(reqCtx, "slack:T123:C123:1712161829.000300", "idem-success")
	successRequest.Provider = "local"
	_, err = client.SignalOrStartRun(context.Background(), successRequest)
	if err != nil {
		t.Fatalf("SignalOrStartRun success: %v", err)
	}
	provider.signalOrStartErr = status.Error(codes.FailedPrecondition, "provider rejected run")
	failureRequest := workflowManagerTelemetrySignalOrStartRequest(reqCtx, "slack:T123:C123:1712161830.000400", "idem-failure")
	failureRequest.Provider = "local"
	_, err = client.SignalOrStartRun(context.Background(), failureRequest)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("SignalOrStartRun failure = %v, want FailedPrecondition", err)
	}
	if got := provider.signalOrStartCalls.Load(); got != 2 {
		t.Fatalf("provider SignalOrStartRun calls = %d, want 2", got)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	successAttrs := map[string]string{
		"gestaltd.workflow.provider.name":    "local",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationSignalOrStartRun,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindSignal,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindSteps,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusRunning,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
	}
	failureAttrs := map[string]string{
		"gestaltd.workflow.provider.name":    "local",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationSignalOrStartRun,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindSignal,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindUnknown,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusUnknown,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
		"error.type":                         "grpc.failed_precondition",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.manager.operation.count", 1, successAttrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.workflows.manager.operation.duration", successAttrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.manager.operation.error_count", 1, failureAttrs)
}

func newTelemetryRemoteWorkflow(t *testing.T) coreworkflow.Provider {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	provider := workflowTelemetryProviderServer{}
	proto.RegisterWorkflowServer(srv, provider)
	proto.RegisterProviderLifecycleServer(srv, provider)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///workflow-provider",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	workflow, err := NewRemote(context.Background(), RemoteConfig{
		Client:  proto.NewWorkflowClient(conn),
		Runtime: proto.NewProviderLifecycleClient(conn),
		Closer:  noopCloser{},
		Name:    "workflow-metrics",
	})
	if err != nil {
		t.Fatalf("NewRemote: %v", err)
	}
	return workflow
}

func workflowManagerTelemetrySignalOrStartRequest(reqCtx *proto.RequestContext, workflowKey, idempotencyKey string) *proto.SignalOrStartWorkflowProviderRunRequest {
	return &proto.SignalOrStartWorkflowProviderRunRequest{
		WorkflowKey:    workflowKey,
		IdempotencyKey: idempotencyKey,
		Context:        reqCtx,
		DefinitionId:   "definition-1",
		Signal:         &proto.WorkflowSignal{Name: "slack.message"},
	}
}

func workflowManagerTelemetryRequestContext() *proto.RequestContext {
	return &proto.RequestContext{
		Caller: &proto.ProviderContext{
			Kind: string(invocation.ProviderKindApp),
			Name: "slack",
		},
		Subject: &proto.SubjectContext{
			Id: "user:user-123",
		},
	}
}

type workflowManagerTelemetryControl struct {
	provider coreworkflow.Provider
}

func (c workflowManagerTelemetryControl) ResolveProvider(_ context.Context, name string) (string, coreworkflow.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "local"
	}
	return name, c.provider, nil
}

func (workflowManagerTelemetryControl) ProviderNames() []string {
	return []string{"local"}
}

type workflowManagerTelemetryProvider struct {
	coreworkflow.Provider
	signalOrStartCalls atomic.Int64
	signalOrStartErr   error
}

func newWorkflowManagerTelemetryProvider() *workflowManagerTelemetryProvider {
	return &workflowManagerTelemetryProvider{}
}

func (p *workflowManagerTelemetryProvider) GetDefinition(context.Context, *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	return &proto.WorkflowDefinition{
		Id:         "definition-1",
		Generation: 7,
		RunAs:      "service_account:workflow-runner",
		Target:     telemetryProtoAppStepTarget("github", "issues.triage"),
	}, nil
}

func (p *workflowManagerTelemetryProvider) SignalOrStartRun(_ context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	p.signalOrStartCalls.Add(1)
	if p.signalOrStartErr != nil {
		return nil, p.signalOrStartErr
	}
	signal := workflowwire.SignalFromProto(req.GetSignal())
	if signal.ID == "" {
		signal.ID = "signal-1"
	}
	run, err := workflowwire.RunToProto(&coreworkflow.Run{
		ID:                   "run-signal-started",
		Status:               coreworkflow.RunStatusRunning,
		WorkflowKey:          req.GetWorkflowKey(),
		DefinitionID:         req.GetDefinitionId(),
		DefinitionGeneration: req.GetExpectedDefinitionGeneration(),
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID:  "run",
			App: &coreworkflow.AppCall{Name: "github", Operation: "issues.triage"},
		}}},
		CreatedBy: appaccessservice.SubjectIDFromRequestContext(req.GetContext()),
	})
	if err != nil {
		return nil, err
	}
	signalProto, err := workflowwire.SignalToProto(signal)
	if err != nil {
		return nil, err
	}
	return &proto.SignalWorkflowRunResponse{
		Run:         run,
		Signal:      signalProto,
		StartedRun:  true,
		WorkflowKey: req.GetWorkflowKey(),
	}, nil
}

func telemetryDefinition(id string, target *proto.BoundWorkflowTarget) *proto.WorkflowDefinition {
	return &proto.WorkflowDefinition{
		Id:         id,
		Generation: 1,
		Target:     target,
		CreatedAt:  timestamppb.Now(),
		UpdatedAt:  timestamppb.Now(),
	}
}

func telemetryRun(id string, status proto.WorkflowRunStatus) *proto.WorkflowRun {
	return &proto.WorkflowRun{
		Id:        id,
		Status:    status,
		CreatedAt: timestamppb.Now(),
		Target:    telemetryProtoAppStepTarget("github", "issues.triage"),
	}
}

func telemetryProtoAppStepTarget(appName, operation string) *proto.BoundWorkflowTarget {
	return &proto.BoundWorkflowTarget{
		Steps: []*proto.WorkflowStep{{
			Id:     "run",
			Action: &proto.WorkflowStep_App{App: &proto.WorkflowStepAppCall{Name: appName, Operation: operation}},
		}},
	}
}

func workflowMetricAttrs(operation string) map[string]string {
	return workflowMetricAttrsWith(operation, observability.WorkflowTriggerKindNone, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown)
}

func workflowMetricAttrsWith(operation, triggerKind, targetKind, runStatus string) map[string]string {
	return map[string]string{
		"gestaltd.workflow.provider.name":    "workflow-metrics",
		"gestaltd.workflow.operation.name":   operation,
		"gestaltd.workflow.trigger.kind":     triggerKind,
		"gestaltd.workflow.target.kind":      targetKind,
		"gestaltd.workflow.run.status":       runStatus,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
	}
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

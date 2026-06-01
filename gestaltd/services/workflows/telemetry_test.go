package workflows

import (
	"context"
	"net"
	"testing"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workflowTelemetryProviderServer struct {
	proto.UnimplementedWorkflowProviderServer
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

func (workflowTelemetryProviderServer) StartRun(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	return telemetryRun("run-start", proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING), nil
}

func (workflowTelemetryProviderServer) GetRun(_ context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	if req.GetRunId() == "fail" {
		return nil, status.Error(codes.InvalidArgument, "bad run")
	}
	return telemetryRun(req.GetRunId(), proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_SUCCEEDED), nil
}

func (workflowTelemetryProviderServer) ListRuns(context.Context, *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	return &proto.ListWorkflowProviderRunsResponse{Runs: []*proto.BoundWorkflowRun{telemetryRun("run-1", proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_SUCCEEDED)}}, nil
}

func (workflowTelemetryProviderServer) CancelRun(context.Context, *proto.CancelWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
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

func (workflowTelemetryProviderServer) UpsertSchedule(context.Context, *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return telemetrySchedule("sched-1"), nil
}

func (workflowTelemetryProviderServer) GetSchedule(context.Context, *proto.GetWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return telemetrySchedule("sched-1"), nil
}

func (workflowTelemetryProviderServer) ListSchedules(context.Context, *proto.ListWorkflowProviderSchedulesRequest) (*proto.ListWorkflowProviderSchedulesResponse, error) {
	return &proto.ListWorkflowProviderSchedulesResponse{Schedules: []*proto.BoundWorkflowSchedule{telemetrySchedule("sched-1")}}, nil
}

func (workflowTelemetryProviderServer) DeleteSchedule(context.Context, *proto.DeleteWorkflowProviderScheduleRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (workflowTelemetryProviderServer) PauseSchedule(context.Context, *proto.PauseWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return telemetrySchedule("sched-1"), nil
}

func (workflowTelemetryProviderServer) ResumeSchedule(context.Context, *proto.ResumeWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return telemetrySchedule("sched-1"), nil
}

func (workflowTelemetryProviderServer) UpsertEventTrigger(context.Context, *proto.UpsertWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return telemetryEventTrigger("trigger-1"), nil
}

func (workflowTelemetryProviderServer) GetEventTrigger(context.Context, *proto.GetWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return telemetryEventTrigger("trigger-1"), nil
}

func (workflowTelemetryProviderServer) ListEventTriggers(context.Context, *proto.ListWorkflowProviderEventTriggersRequest) (*proto.ListWorkflowProviderEventTriggersResponse, error) {
	return &proto.ListWorkflowProviderEventTriggersResponse{Triggers: []*proto.BoundWorkflowEventTrigger{telemetryEventTrigger("trigger-1")}}, nil
}

func (workflowTelemetryProviderServer) DeleteEventTrigger(context.Context, *proto.DeleteWorkflowProviderEventTriggerRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (workflowTelemetryProviderServer) PauseEventTrigger(context.Context, *proto.PauseWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return telemetryEventTrigger("trigger-1"), nil
}

func (workflowTelemetryProviderServer) ResumeEventTrigger(context.Context, *proto.ResumeWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return telemetryEventTrigger("trigger-1"), nil
}

func (workflowTelemetryProviderServer) PublishEvent(context.Context, *proto.PublishWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	return &proto.WorkflowEvent{Type: "ignored"}, nil
}

func TestRemoteWorkflowRecordsProviderOperationMetricsAcrossTransport(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	workflow := newTelemetryRemoteWorkflow(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)

	type workflowProviderMetricCall struct {
		name      string
		operation string
		attrs     map[string]string
		call      func(context.Context, coreworkflow.Provider) error
	}
	calls := []workflowProviderMetricCall{
		{"start run", observability.WorkflowOperationStartRun, workflowMetricAttrsWith(observability.WorkflowOperationStartRun, observability.WorkflowTriggerKindManual, observability.WorkflowTargetKindSteps, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.StartRun(ctx, &proto.StartWorkflowProviderRunRequest{Target: mustWorkflowTelemetryTarget(t, telemetryCoreAppStepTarget())})
			return err
		}},
		{"get run", observability.WorkflowOperationGetRun, workflowMetricAttrs(observability.WorkflowOperationGetRun), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.GetRun(ctx, &proto.GetWorkflowProviderRunRequest{RunId: "run-1"})
			return err
		}},
		{"list runs", observability.WorkflowOperationListRuns, workflowMetricAttrs(observability.WorkflowOperationListRuns), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ListRuns(ctx, &proto.ListWorkflowProviderRunsRequest{})
			return err
		}},
		{"cancel run", observability.WorkflowOperationCancelRun, workflowMetricAttrs(observability.WorkflowOperationCancelRun), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.CancelRun(ctx, &proto.CancelWorkflowProviderRunRequest{RunId: "run-1"})
			return err
		}},
		{"signal run", observability.WorkflowOperationSignalRun, workflowMetricAttrsWith(observability.WorkflowOperationSignalRun, observability.WorkflowTriggerKindSignal, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.SignalRun(ctx, &proto.SignalWorkflowProviderRunRequest{RunId: "run-1", Signal: &proto.WorkflowSignal{Name: "poke"}})
			return err
		}},
		{"signal or start run", observability.WorkflowOperationSignalOrStartRun, workflowMetricAttrsWith(observability.WorkflowOperationSignalOrStartRun, observability.WorkflowTriggerKindSignal, observability.WorkflowTargetKindSteps, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.SignalOrStartRun(ctx, &proto.SignalOrStartWorkflowProviderRunRequest{Target: mustWorkflowTelemetryTarget(t, telemetryCoreAgentStepTarget(nil)), Signal: &proto.WorkflowSignal{Name: "poke"}})
			return err
		}},
		{"upsert schedule", observability.WorkflowOperationUpsertSchedule, workflowMetricAttrsWith(observability.WorkflowOperationUpsertSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindSteps, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.UpsertSchedule(ctx, &proto.UpsertWorkflowProviderScheduleRequest{Target: mustWorkflowTelemetryTarget(t, telemetryCoreAppStepTarget())})
			return err
		}},
		{"get schedule", observability.WorkflowOperationGetSchedule, workflowMetricAttrsWith(observability.WorkflowOperationGetSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.GetSchedule(ctx, &proto.GetWorkflowProviderScheduleRequest{ScheduleId: "sched-1"})
			return err
		}},
		{"list schedules", observability.WorkflowOperationListSchedules, workflowMetricAttrsWith(observability.WorkflowOperationListSchedules, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ListSchedules(ctx, &proto.ListWorkflowProviderSchedulesRequest{})
			return err
		}},
		{"delete schedule", observability.WorkflowOperationDeleteSchedule, workflowMetricAttrsWith(observability.WorkflowOperationDeleteSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			return p.DeleteSchedule(ctx, &proto.DeleteWorkflowProviderScheduleRequest{ScheduleId: "sched-1"})
		}},
		{"pause schedule", observability.WorkflowOperationPauseSchedule, workflowMetricAttrsWith(observability.WorkflowOperationPauseSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.PauseSchedule(ctx, &proto.PauseWorkflowProviderScheduleRequest{ScheduleId: "sched-1"})
			return err
		}},
		{"resume schedule", observability.WorkflowOperationResumeSchedule, workflowMetricAttrsWith(observability.WorkflowOperationResumeSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ResumeSchedule(ctx, &proto.ResumeWorkflowProviderScheduleRequest{ScheduleId: "sched-1"})
			return err
		}},
		{"upsert trigger", observability.WorkflowOperationUpsertEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationUpsertEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindSteps, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.UpsertEventTrigger(ctx, &proto.UpsertWorkflowProviderEventTriggerRequest{Target: mustWorkflowTelemetryTarget(t, telemetryCoreAppStepTarget())})
			return err
		}},
		{"get trigger", observability.WorkflowOperationGetEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationGetEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.GetEventTrigger(ctx, &proto.GetWorkflowProviderEventTriggerRequest{TriggerId: "trigger-1"})
			return err
		}},
		{"list triggers", observability.WorkflowOperationListEventTriggers, workflowMetricAttrsWith(observability.WorkflowOperationListEventTriggers, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ListEventTriggers(ctx, &proto.ListWorkflowProviderEventTriggersRequest{})
			return err
		}},
		{"delete trigger", observability.WorkflowOperationDeleteEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationDeleteEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			return p.DeleteEventTrigger(ctx, &proto.DeleteWorkflowProviderEventTriggerRequest{TriggerId: "trigger-1"})
		}},
		{"pause trigger", observability.WorkflowOperationPauseEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationPauseEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.PauseEventTrigger(ctx, &proto.PauseWorkflowProviderEventTriggerRequest{TriggerId: "trigger-1"})
			return err
		}},
		{"resume trigger", observability.WorkflowOperationResumeEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationResumeEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ResumeEventTrigger(ctx, &proto.ResumeWorkflowProviderEventTriggerRequest{TriggerId: "trigger-1"})
			return err
		}},
		{"publish event", observability.WorkflowOperationPublishEvent, workflowMetricAttrsWith(observability.WorkflowOperationPublishEvent, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.PublishEvent(ctx, &proto.PublishWorkflowProviderEventRequest{Event: &proto.WorkflowEvent{Type: "ignored"}})
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
	if _, err := workflow.GetRun(ctx, &proto.GetWorkflowProviderRunRequest{RunId: "fail"}); status.Code(err) != codes.InvalidArgument {
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
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.events.published.count", 1, workflowMetricAttrsWith(observability.WorkflowOperationPublishEvent, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown))
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
	startRunAttrs := workflowMetricAttrsWith(observability.WorkflowOperationStartRun, observability.WorkflowTriggerKindManual, observability.WorkflowTargetKindSteps, observability.WorkflowRunStatusUnknown)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.provider.operation.duration", startRunAttrs, "gestaltd.workflow.run.id")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.provider.operation.duration", startRunAttrs, "gestaltd.workflow.app.operation")
}

func newTelemetryRemoteWorkflow(t *testing.T) coreworkflow.Provider {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	provider := workflowTelemetryProviderServer{}
	proto.RegisterWorkflowProviderServer(srv, provider)
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
		Client:  proto.NewWorkflowProviderClient(conn),
		Runtime: proto.NewProviderLifecycleClient(conn),
		Closer:  noopCloser{},
		Name:    "workflow-metrics",
	})
	if err != nil {
		t.Fatalf("NewRemote: %v", err)
	}
	return workflow
}

func telemetryRun(id string, status proto.WorkflowRunStatus) *proto.BoundWorkflowRun {
	return &proto.BoundWorkflowRun{
		Id:        id,
		Status:    status,
		CreatedAt: timestamppb.Now(),
		Target:    telemetryProtoAppStepTarget("ignored", "ignored"),
	}
}

func telemetrySchedule(id string) *proto.BoundWorkflowSchedule {
	return &proto.BoundWorkflowSchedule{
		Id:        id,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
		Target:    telemetryProtoAppStepTarget("ignored", "ignored"),
	}
}

func telemetryEventTrigger(id string) *proto.BoundWorkflowEventTrigger {
	return &proto.BoundWorkflowEventTrigger{
		Id:        id,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
		Match:     &proto.WorkflowEventMatch{Type: "ignored"},
		Target:    telemetryProtoAppStepTarget("ignored", "ignored"),
	}
}

func telemetryCoreAppStepTarget() coreworkflow.Target {
	return coreworkflow.Target{
		Steps: []coreworkflow.Step{{ID: "run", App: &coreworkflow.AppCall{Name: "ignored", Operation: "ignored"}}},
	}
}

func telemetryCoreAgentStepTarget(tools []coreagent.ToolRef) coreworkflow.Target {
	return coreworkflow.Target{
		Steps: []coreworkflow.Step{{ID: "run", Agent: &coreworkflow.AgentTurn{
			ProviderName: "simple",
			Prompt:       coreworkflow.Text{Template: "handle the Slack message"},
			ToolRefs:     tools,
			Output:       coreagent.Output{Text: &coreagent.TextOutput{}},
		}}},
	}
}

func mustWorkflowTelemetryTarget(t *testing.T, target coreworkflow.Target) *proto.BoundWorkflowTarget {
	t.Helper()
	out, err := workflowwire.TargetToProto(target)
	if err != nil {
		t.Fatalf("workflowTargetToProto: %v", err)
	}
	return out
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

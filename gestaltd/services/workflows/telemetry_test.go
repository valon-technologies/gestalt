package workflows

import (
	"context"
	"net"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel"
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

func (workflowTelemetryProviderServer) PutExecutionReference(_ context.Context, req *proto.PutWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	return req.GetReference(), nil
}

func (workflowTelemetryProviderServer) GetExecutionReference(context.Context, *proto.GetWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	return telemetryExecutionReference(), nil
}

func (workflowTelemetryProviderServer) ListExecutionReferences(context.Context, *proto.ListWorkflowExecutionReferencesRequest) (*proto.ListWorkflowExecutionReferencesResponse, error) {
	return &proto.ListWorkflowExecutionReferencesResponse{References: []*proto.WorkflowExecutionReference{telemetryExecutionReference()}}, nil
}

func (workflowTelemetryProviderServer) PublishEvent(context.Context, *proto.PublishWorkflowProviderEventRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
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
		{"start run", observability.WorkflowOperationStartRun, workflowMetricAttrsWith(observability.WorkflowOperationStartRun, observability.WorkflowTriggerKindManual, observability.WorkflowTargetKindPlugin, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.StartRun(ctx, coreworkflow.StartRunRequest{Target: coreworkflow.Target{Plugin: &coreworkflow.PluginTarget{PluginName: "ignored", Operation: "ignored"}}})
			return err
		}},
		{"get run", observability.WorkflowOperationGetRun, workflowMetricAttrs(observability.WorkflowOperationGetRun), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.GetRun(ctx, coreworkflow.GetRunRequest{RunID: "run-1"})
			return err
		}},
		{"list runs", observability.WorkflowOperationListRuns, workflowMetricAttrs(observability.WorkflowOperationListRuns), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ListRuns(ctx, coreworkflow.ListRunsRequest{})
			return err
		}},
		{"cancel run", observability.WorkflowOperationCancelRun, workflowMetricAttrs(observability.WorkflowOperationCancelRun), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.CancelRun(ctx, coreworkflow.CancelRunRequest{RunID: "run-1"})
			return err
		}},
		{"signal run", observability.WorkflowOperationSignalRun, workflowMetricAttrsWith(observability.WorkflowOperationSignalRun, observability.WorkflowTriggerKindSignal, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.SignalRun(ctx, coreworkflow.SignalRunRequest{RunID: "run-1", Signal: coreworkflow.Signal{Name: "poke"}})
			return err
		}},
		{"signal or start run", observability.WorkflowOperationSignalOrStartRun, workflowMetricAttrsWith(observability.WorkflowOperationSignalOrStartRun, observability.WorkflowTriggerKindSignal, observability.WorkflowTargetKindAgent, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.SignalOrStartRun(ctx, coreworkflow.SignalOrStartRunRequest{Target: coreworkflow.Target{Agent: &coreworkflow.AgentTarget{ProviderName: "ignored"}}, Signal: coreworkflow.Signal{Name: "poke"}})
			return err
		}},
		{"upsert schedule", observability.WorkflowOperationUpsertSchedule, workflowMetricAttrsWith(observability.WorkflowOperationUpsertSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindPlugin, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{Target: coreworkflow.Target{Plugin: &coreworkflow.PluginTarget{PluginName: "ignored", Operation: "ignored"}}})
			return err
		}},
		{"get schedule", observability.WorkflowOperationGetSchedule, workflowMetricAttrsWith(observability.WorkflowOperationGetSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.GetSchedule(ctx, coreworkflow.GetScheduleRequest{ScheduleID: "sched-1"})
			return err
		}},
		{"list schedules", observability.WorkflowOperationListSchedules, workflowMetricAttrsWith(observability.WorkflowOperationListSchedules, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ListSchedules(ctx, coreworkflow.ListSchedulesRequest{})
			return err
		}},
		{"delete schedule", observability.WorkflowOperationDeleteSchedule, workflowMetricAttrsWith(observability.WorkflowOperationDeleteSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			return p.DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{ScheduleID: "sched-1"})
		}},
		{"pause schedule", observability.WorkflowOperationPauseSchedule, workflowMetricAttrsWith(observability.WorkflowOperationPauseSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.PauseSchedule(ctx, coreworkflow.PauseScheduleRequest{ScheduleID: "sched-1"})
			return err
		}},
		{"resume schedule", observability.WorkflowOperationResumeSchedule, workflowMetricAttrsWith(observability.WorkflowOperationResumeSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ResumeSchedule(ctx, coreworkflow.ResumeScheduleRequest{ScheduleID: "sched-1"})
			return err
		}},
		{"upsert trigger", observability.WorkflowOperationUpsertEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationUpsertEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindPlugin, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.UpsertEventTrigger(ctx, coreworkflow.UpsertEventTriggerRequest{Target: coreworkflow.Target{Plugin: &coreworkflow.PluginTarget{PluginName: "ignored", Operation: "ignored"}}})
			return err
		}},
		{"get trigger", observability.WorkflowOperationGetEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationGetEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.GetEventTrigger(ctx, coreworkflow.GetEventTriggerRequest{TriggerID: "trigger-1"})
			return err
		}},
		{"list triggers", observability.WorkflowOperationListEventTriggers, workflowMetricAttrsWith(observability.WorkflowOperationListEventTriggers, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ListEventTriggers(ctx, coreworkflow.ListEventTriggersRequest{})
			return err
		}},
		{"delete trigger", observability.WorkflowOperationDeleteEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationDeleteEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			return p.DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{TriggerID: "trigger-1"})
		}},
		{"pause trigger", observability.WorkflowOperationPauseEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationPauseEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.PauseEventTrigger(ctx, coreworkflow.PauseEventTriggerRequest{TriggerID: "trigger-1"})
			return err
		}},
		{"resume trigger", observability.WorkflowOperationResumeEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationResumeEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.ResumeEventTrigger(ctx, coreworkflow.ResumeEventTriggerRequest{TriggerID: "trigger-1"})
			return err
		}},
		{"publish event", observability.WorkflowOperationPublishEvent, workflowMetricAttrsWith(observability.WorkflowOperationPublishEvent, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindUnknown, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			return p.PublishEvent(ctx, coreworkflow.PublishEventRequest{Event: coreworkflow.Event{Type: "ignored"}})
		}},
		{"ping", observability.WorkflowOperationPing, workflowMetricAttrs(observability.WorkflowOperationPing), func(ctx context.Context, p coreworkflow.Provider) error {
			return p.Ping(ctx)
		}},
	}
	store := workflow.(coreworkflow.ExecutionReferenceStore)
	calls = append(calls,
		workflowProviderMetricCall{"put execution ref", observability.WorkflowOperationPutExecutionReference, workflowMetricAttrsWith(observability.WorkflowOperationPutExecutionReference, observability.WorkflowTriggerKindNone, observability.WorkflowTargetKindPlugin, observability.WorkflowRunStatusUnknown), func(ctx context.Context, _ coreworkflow.Provider) error {
			_, err := store.PutExecutionReference(ctx, &coreworkflow.ExecutionReference{ID: "ref-1", Target: coreworkflow.Target{Plugin: &coreworkflow.PluginTarget{PluginName: "ignored", Operation: "ignored"}}})
			return err
		}},
		workflowProviderMetricCall{"get execution ref", observability.WorkflowOperationGetExecutionReference, workflowMetricAttrs(observability.WorkflowOperationGetExecutionReference), func(ctx context.Context, _ coreworkflow.Provider) error {
			_, err := store.GetExecutionReference(ctx, "ref-1")
			return err
		}},
		workflowProviderMetricCall{"list execution refs", observability.WorkflowOperationListExecutionReferences, workflowMetricAttrs(observability.WorkflowOperationListExecutionReferences), func(ctx context.Context, _ coreworkflow.Provider) error {
			_, err := store.ListExecutionReferences(ctx, "subject-1")
			return err
		}},
	)

	for _, tc := range calls {
		if err := tc.call(ctx, workflow); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	if _, err := workflow.GetRun(ctx, coreworkflow.GetRunRequest{RunID: "fail"}); status.Code(err) != codes.InvalidArgument {
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
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindPlugin,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusPending,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.runs.started.count", 1, map[string]string{
		"gestaltd.workflow.provider.name":    "workflow-metrics",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationSignalOrStartRun,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindSignal,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindAgent,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusPending,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
	})
	startRunAttrs := workflowMetricAttrsWith(observability.WorkflowOperationStartRun, observability.WorkflowTriggerKindManual, observability.WorkflowTargetKindPlugin, observability.WorkflowRunStatusUnknown)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.provider.operation.duration", startRunAttrs, "gestaltd.workflow.run.id")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.provider.operation.duration", startRunAttrs, "gestaltd.workflow.execution_ref")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.provider.operation.duration", startRunAttrs, "gestaltd.workflow.plugin.operation")
}

func TestWorkflowHostRecordsOperationMetricsAcrossTransport(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(metrics.Provider)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	proto.RegisterWorkflowHostServer(srv, NewHostServer("workflow-host-metrics", func(context.Context, coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error) {
		return &coreworkflow.InvokeOperationResponse{Status: 200, Body: "ok"}, nil
	}))
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})
	conn, err := grpc.NewClient("passthrough:///workflow-host",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := proto.NewWorkflowHostClient(conn)

	_, err = client.InvokeOperation(context.Background(), &proto.InvokeWorkflowOperationRequest{
		RunId:        "run-1",
		ExecutionRef: "exec-ref-1",
		Trigger: &proto.WorkflowRunTrigger{Kind: &proto.WorkflowRunTrigger_Schedule{
			Schedule: &proto.WorkflowScheduleTrigger{ScheduleId: "sched-1"},
		}},
		Target: &proto.BoundWorkflowTarget{Kind: &proto.BoundWorkflowTarget_Plugin{
			Plugin: &proto.BoundWorkflowPluginTarget{PluginName: "ignored", Operation: "ignored"},
		}},
	})
	if err != nil {
		t.Fatalf("InvokeOperation success: %v", err)
	}
	_, err = client.InvokeOperation(context.Background(), &proto.InvokeWorkflowOperationRequest{
		ExecutionRef: "exec-ref-1",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("InvokeOperation validation error = %v, want InvalidArgument", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	successAttrs := map[string]string{
		"gestaltd.workflow.provider.name":    "workflow-host-metrics",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationInvokeOperation,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindSchedule,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindPlugin,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusUnknown,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.host.operation.count", 1, successAttrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.workflows.host.operation.duration", successAttrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.host.operation.error_count", 1, map[string]string{
		"gestaltd.workflow.provider.name":    "workflow-host-metrics",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationInvokeOperation,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindNone,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindUnknown,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusUnknown,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
		"error.type":                         "grpc.invalid_argument",
	})
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.host.operation.duration", successAttrs, "gestalt.provider")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.host.operation.duration", successAttrs, "gestaltd.workflow.run.id")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.host.operation.duration", successAttrs, "gestaltd.workflow.execution_ref")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.host.operation.duration", successAttrs, "gestaltd.workflow.plugin.operation")
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
		Target: &proto.BoundWorkflowTarget{Kind: &proto.BoundWorkflowTarget_Plugin{
			Plugin: &proto.BoundWorkflowPluginTarget{PluginName: "ignored", Operation: "ignored"},
		}},
	}
}

func telemetrySchedule(id string) *proto.BoundWorkflowSchedule {
	return &proto.BoundWorkflowSchedule{
		Id:        id,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
		Target: &proto.BoundWorkflowTarget{Kind: &proto.BoundWorkflowTarget_Plugin{
			Plugin: &proto.BoundWorkflowPluginTarget{PluginName: "ignored", Operation: "ignored"},
		}},
	}
}

func telemetryEventTrigger(id string) *proto.BoundWorkflowEventTrigger {
	return &proto.BoundWorkflowEventTrigger{
		Id:        id,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
		Match:     &proto.WorkflowEventMatch{Type: "ignored"},
		Target: &proto.BoundWorkflowTarget{Kind: &proto.BoundWorkflowTarget_Plugin{
			Plugin: &proto.BoundWorkflowPluginTarget{PluginName: "ignored", Operation: "ignored"},
		}},
	}
}

func telemetryExecutionReference() *proto.WorkflowExecutionReference {
	return &proto.WorkflowExecutionReference{
		Id:           "ref-1",
		ProviderName: "workflow-metrics",
		Target: &proto.BoundWorkflowTarget{Kind: &proto.BoundWorkflowTarget_Plugin{
			Plugin: &proto.BoundWorkflowPluginTarget{PluginName: "ignored", Operation: "ignored"},
		}},
		CreatedAt: timestamppb.Now(),
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

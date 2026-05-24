package workflows

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
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
			_, err := p.StartRun(ctx, coreworkflow.StartRunRequest{Target: telemetryCoreAppStepTarget()})
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
		{"signal or start run", observability.WorkflowOperationSignalOrStartRun, workflowMetricAttrsWith(observability.WorkflowOperationSignalOrStartRun, observability.WorkflowTriggerKindSignal, observability.WorkflowTargetKindSteps, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.SignalOrStartRun(ctx, coreworkflow.SignalOrStartRunRequest{Target: telemetryCoreAgentStepTarget(nil), Signal: coreworkflow.Signal{Name: "poke"}})
			return err
		}},
		{"upsert schedule", observability.WorkflowOperationUpsertSchedule, workflowMetricAttrsWith(observability.WorkflowOperationUpsertSchedule, observability.WorkflowTriggerKindSchedule, observability.WorkflowTargetKindSteps, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{Target: telemetryCoreAppStepTarget()})
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
		{"upsert trigger", observability.WorkflowOperationUpsertEventTrigger, workflowMetricAttrsWith(observability.WorkflowOperationUpsertEventTrigger, observability.WorkflowTriggerKindEvent, observability.WorkflowTargetKindSteps, observability.WorkflowRunStatusUnknown), func(ctx context.Context, p coreworkflow.Provider) error {
			_, err := p.UpsertEventTrigger(ctx, coreworkflow.UpsertEventTriggerRequest{Target: telemetryCoreAppStepTarget()})
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
			_, err := p.PublishEvent(ctx, coreworkflow.PublishEventRequest{Event: coreworkflow.Event{Type: "ignored"}})
			return err
		}},
		{"ping", observability.WorkflowOperationPing, workflowMetricAttrs(observability.WorkflowOperationPing), func(ctx context.Context, p coreworkflow.Provider) error {
			return p.Ping(ctx)
		}},
	}
	store := workflow.(coreworkflow.ExecutionReferenceStore)
	calls = append(calls,
		workflowProviderMetricCall{"put execution ref", observability.WorkflowOperationPutExecutionReference, workflowMetricAttrsWith(observability.WorkflowOperationPutExecutionReference, observability.WorkflowTriggerKindNone, observability.WorkflowTargetKindSteps, observability.WorkflowRunStatusUnknown), func(ctx context.Context, _ coreworkflow.Provider) error {
			_, err := store.PutExecutionReference(ctx, &coreworkflow.ExecutionReference{ID: "ref-1", Target: telemetryCoreAppStepTarget()})
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
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.provider.operation.duration", startRunAttrs, "gestaltd.workflow.execution_ref")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.provider.operation.duration", startRunAttrs, "gestaltd.workflow.app.operation")
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
		Target: telemetryProtoAppStepTarget("ignored", "ignored"),
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
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindSteps,
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
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.host.operation.duration", successAttrs, "gestaltd.workflow.app.operation")
}

func TestWorkflowProviderRecordsSignalOrStartMetricsAcrossTransport(t *testing.T) { //nolint:paralleltest // mutates global slog and OTel providers.
	metrics := metrictest.NewManualMeterProvider(t)
	prevMeter := otel.GetMeterProvider()
	otel.SetMeterProvider(metrics.Provider)
	t.Cleanup(func() { otel.SetMeterProvider(prevMeter) })

	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	provider := newWorkflowManagerTelemetryProvider()
	var auditBuf bytes.Buffer
	authz, err := authorization.New(authorization.StaticConfig{
		Policies: map[string]authorization.StaticSubjectPolicy{
			"deny": {Default: "deny"},
		},
		ProviderPolicies: map[string]string{"datadog": "deny"},
	})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	manager := workflowmanager.New(workflowmanager.Config{
		Workflow:     workflowManagerTelemetryControl{provider: provider},
		Agent:        workflowManagerTelemetryAgentControl{},
		AgentManager: workflowManagerTelemetryAgentManager{},
		Audit:        invocation.NewSlogAuditSink(&auditBuf),
		Authorizer:   authz,
	})
	tokens, err := NewInvocationTokenManager([]byte("workflow-manager-telemetry-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	token, err := tokens.MintRootTokenWithWorkflowGrants(
		principal.WithPrincipal(context.Background(), &principal.Principal{
			SubjectID: "user:user-123",
			UserID:    "user-123",
			Kind:      principal.KindUser,
			Source:    principal.SourceSession,
		}),
		"slack",
		nil,
		workflowgrants.Grants{workflowgrants.OperationRunsSignalOrStart: {}},
	)
	if err != nil {
		t.Fatalf("MintRootTokenWithWorkflowGrants: %v", err)
	}
	principalDeniedToken, err := tokens.MintRootTokenWithWorkflowGrants(
		principal.WithPrincipal(context.Background(), &principal.Principal{
			SubjectID: "user:restricted",
			UserID:    "restricted",
			Kind:      principal.KindUser,
			Source:    principal.SourceSession,
			TokenPermissions: principal.CompilePermissions([]core.AccessPermission{
				{App: "simple"},
			}),
		}),
		"slack",
		nil,
		workflowgrants.Grants{workflowgrants.OperationRunsSignalOrStart: {}},
	)
	if err != nil {
		t.Fatalf("MintRootTokenWithWorkflowGrants(principal denied): %v", err)
	}
	authorizerDeniedToken, err := tokens.MintRootTokenWithWorkflowGrants(
		principal.WithPrincipal(context.Background(), &principal.Principal{
			SubjectID: "user:authorizer-denied",
			UserID:    "authorizer-denied",
			Kind:      principal.KindUser,
			Source:    principal.SourceSession,
			TokenPermissions: principal.CompilePermissions([]core.AccessPermission{
				{App: "simple"},
				{App: "datadog", Operations: []string{"listAlerts"}},
			}),
		}),
		"slack",
		nil,
		workflowgrants.Grants{workflowgrants.OperationRunsSignalOrStart: {}},
	)
	if err != nil {
		t.Fatalf("MintRootTokenWithWorkflowGrants(authorizer denied): %v", err)
	}

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	proto.RegisterWorkflowProviderServer(srv, NewProviderServer("slack", manager, tokens))
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
	client := proto.NewWorkflowProviderClient(conn)

	successKey := "slack:T123:C123:1712161829.000300"
	_, err = client.SignalOrStartRun(context.Background(), workflowManagerTelemetrySignalOrStartRequest(token, successKey, "idem-success"))
	if err != nil {
		t.Fatalf("SignalOrStartRun success: %v", err)
	}
	failureKey := "slack:T123:C123:1712161830.000400"
	provider.signalOrStartErr = status.Errorf(codes.FailedPrecondition, "provider echoed %s idem-failure", failureKey)
	_, err = client.SignalOrStartRun(context.Background(), workflowManagerTelemetrySignalOrStartRequest(token, failureKey, "idem-failure"))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("SignalOrStartRun failure = %v, want FailedPrecondition", err)
	}
	untrustedProviderName := "caller-controlled-provider"
	_, err = client.SignalOrStartRun(context.Background(), &proto.SignalOrStartWorkflowProviderRunRequest{
		ProviderName:    untrustedProviderName,
		WorkflowKey:     "slack:T123:C123:1712161831.000500",
		IdempotencyKey:  "idem-invalid-target",
		InvocationToken: token,
		Signal:          &proto.WorkflowSignal{Name: "slack.message"},
		Target:          telemetryProtoAppStepTarget("", "run"),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SignalOrStartRun invalid target = %v, want InvalidArgument", err)
	}
	principalDeniedKey := "slack:T123:C123:1712161832.000600"
	_, err = client.SignalOrStartRun(context.Background(), &proto.SignalOrStartWorkflowProviderRunRequest{
		ProviderName:    "local",
		WorkflowKey:     principalDeniedKey,
		IdempotencyKey:  "idem-principal-denied",
		InvocationToken: principalDeniedToken,
		Signal:          &proto.WorkflowSignal{Name: "slack.message"},
		Target: telemetryProtoAgentStepTarget([]*proto.AgentToolRef{
			{App: "github", Operation: "reviewPullRequest"},
		}),
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("SignalOrStartRun principal denied = %v, want NotFound", err)
	}
	authorizerDeniedKey := "slack:T123:C123:1712161833.000700"
	_, err = client.SignalOrStartRun(context.Background(), &proto.SignalOrStartWorkflowProviderRunRequest{
		ProviderName:    "local",
		WorkflowKey:     authorizerDeniedKey,
		IdempotencyKey:  "idem-authorizer-denied",
		InvocationToken: authorizerDeniedToken,
		Signal:          &proto.WorkflowSignal{Name: "slack.message"},
		Target: telemetryProtoAgentStepTarget([]*proto.AgentToolRef{
			{App: "datadog", Operation: "listAlerts"},
		}),
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("SignalOrStartRun authorizer denied = %v, want NotFound", err)
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
		"gestaltd.workflow.provider.name":    "unknown",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationSignalOrStartRun,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindSignal,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindSteps,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusUnknown,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
		"error.type":                         "grpc.failed_precondition",
	}
	untrustedFailureAttrs := map[string]string{
		"gestaltd.workflow.provider.name":    "unknown",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationSignalOrStartRun,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindSignal,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindSteps,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusUnknown,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
		"error.type":                         "grpc.invalid_argument",
	}
	notFoundFailureAttrs := map[string]string{
		"gestaltd.workflow.provider.name":    "unknown",
		"gestaltd.workflow.operation.name":   observability.WorkflowOperationSignalOrStartRun,
		"gestaltd.workflow.trigger.kind":     observability.WorkflowTriggerKindSignal,
		"gestaltd.workflow.target.kind":      observability.WorkflowTargetKindSteps,
		"gestaltd.workflow.run.status":       observability.WorkflowRunStatusUnknown,
		"gestaltd.workflow.telemetry.source": observability.WorkflowTelemetrySourceCore,
		"error.type":                         "grpc.not_found",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.manager.operation.count", 1, successAttrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.workflows.manager.operation.duration", successAttrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.manager.operation.error_count", 1, failureAttrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.manager.operation.error_count", 1, untrustedFailureAttrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.workflows.manager.operation.error_count", 2, notFoundFailureAttrs)
	untrustedProviderMetricAttrs := map[string]string{}
	for key, value := range untrustedFailureAttrs {
		untrustedProviderMetricAttrs[key] = value
	}
	untrustedProviderMetricAttrs["gestaltd.workflow.provider.name"] = untrustedProviderName
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.workflows.manager.operation.error_count", untrustedProviderMetricAttrs)
	for _, forbidden := range []string{
		"subject_id",
		"caller_app",
		"workflow_key_sha256",
		"gestaltd.workflow.run.id",
		"gestaltd.workflow.execution_ref",
		"gestaltd.workflow.app.operation",
	} {
		metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gestaltd.workflows.manager.operation.duration", successAttrs, forbidden)
	}

	logOutput := logBuf.String()
	for _, forbidden := range []string{failureKey, principalDeniedKey, authorizerDeniedKey} {
		if strings.Contains(logOutput, forbidden) {
			t.Fatalf("manager log contains raw workflow key %q", forbidden)
		}
	}
	for _, forbidden := range []string{"idem-failure", "idem-principal-denied", "idem-authorizer-denied"} {
		if strings.Contains(logOutput, forbidden) {
			t.Fatalf("manager log contains raw idempotency key %q", forbidden)
		}
	}
	assertWorkflowManagerFailureLog(t, logOutput, failureKey, "provider_signal_or_start", map[string]any{
		"level":          "WARN",
		"request_id_set": true,
		"error_type":     "grpc_status",
		"error_code":     "failed_precondition",
	})
	assertWorkflowManagerFailureLog(t, logOutput, principalDeniedKey, "authorize_target", map[string]any{
		"level":                     "WARN",
		"request_id_set":            true,
		"error_type":                "not_found",
		"workflow_target_component": "agent_tool_ref",
		"authorization_decision":    "workflow_target_principal_operation_permission_denied",
	})
	assertWorkflowManagerFailureLog(t, logOutput, authorizerDeniedKey, "authorize_target", map[string]any{
		"level":                     "WARN",
		"request_id_set":            true,
		"error_type":                "not_found",
		"workflow_target_component": "agent_tool_ref",
		"authorization_decision":    "workflow_target_authorizer_provider_denied",
	})
	assertWorkflowManagerFailureLogsOmitFields(t, logOutput,
		"provider_selection",
		"workflow_provider",
		"target_kind",
		"caller_app",
		"subject_id",
		"subject_kind",
		"execution_ref_id",
		"target_authorization_provider",
		"target_authorization_operation",
		"target_authorization_tool_ref_index",
	)

	auditOutput := auditBuf.String()
	for _, forbidden := range []string{failureKey, principalDeniedKey, authorizerDeniedKey, "idem-failure", "idem-principal-denied", "idem-authorizer-denied", "provider echoed"} {
		if strings.Contains(auditOutput, forbidden) {
			t.Fatalf("workflow audit contains raw sensitive value %q", forbidden)
		}
	}
	assertWorkflowAuditLog(t, auditOutput, successKey, map[string]any{
		"level":                "INFO",
		"request_id_set":       true,
		"source":               "workflow_manager",
		"provider":             "local",
		"operation":            "workflow.run.signal_or_start",
		"target_kind":          "workflow_run",
		"target_id":            "run-signal-started",
		"allowed":              true,
		"caller_app":           "slack",
		"subject_id":           "user:user-123",
		"subject_kind":         "user",
		"workflow_target_kind": "steps",
	})
	assertWorkflowAuditLog(t, auditOutput, failureKey, map[string]any{
		"level":                "WARN",
		"request_id_set":       true,
		"source":               "workflow_manager",
		"provider":             "local",
		"operation":            "workflow.run.signal_or_start",
		"target_kind":          "workflow_run",
		"allowed":              false,
		"error":                "grpc_status",
		"caller_app":           "slack",
		"subject_id":           "user:user-123",
		"subject_kind":         "user",
		"workflow_target_kind": "steps",
	})
	assertWorkflowAuditLog(t, auditOutput, principalDeniedKey, map[string]any{
		"level":                     "WARN",
		"request_id_set":            true,
		"source":                    "workflow_manager",
		"provider":                  "local",
		"operation":                 "workflow.run.signal_or_start",
		"target_kind":               "workflow_run",
		"allowed":                   false,
		"error":                     "not_found",
		"authorization_decision":    "workflow_target_principal_operation_permission_denied",
		"caller_app":                "slack",
		"subject_id":                "user:restricted",
		"subject_kind":              "user",
		"workflow_target_kind":      "steps",
		"workflow_target_component": "agent_tool_ref",
		"workflow_target_provider":  "github",
		"workflow_target_operation": "reviewPullRequest",
	})
	assertWorkflowAuditLog(t, auditOutput, authorizerDeniedKey, map[string]any{
		"level":                     "WARN",
		"request_id_set":            true,
		"source":                    "workflow_manager",
		"provider":                  "local",
		"operation":                 "workflow.run.signal_or_start",
		"target_kind":               "workflow_run",
		"allowed":                   false,
		"error":                     "not_found",
		"authorization_decision":    "workflow_target_authorizer_provider_denied",
		"caller_app":                "slack",
		"subject_id":                "user:authorizer-denied",
		"subject_kind":              "user",
		"workflow_target_kind":      "steps",
		"workflow_target_component": "agent_tool_ref",
		"workflow_target_provider":  "datadog",
		"workflow_target_operation": "listAlerts",
	})
	assertWorkflowAuditLogsOmitFields(t, auditOutput,
		"idempotency_key",
		"result_body",
		"result_body_bytes",
		"result_body_truncated",
		"result_body_sha256",
		"workflow_target_tool_ref_index",
		"target_authorization_tool_ref_index",
	)
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

func workflowManagerTelemetrySignalOrStartRequest(token, workflowKey, idempotencyKey string) *proto.SignalOrStartWorkflowProviderRunRequest {
	return &proto.SignalOrStartWorkflowProviderRunRequest{
		ProviderName:    "local",
		WorkflowKey:     workflowKey,
		IdempotencyKey:  idempotencyKey,
		InvocationToken: token,
		Signal:          &proto.WorkflowSignal{Name: "slack.message"},
		Target:          telemetryProtoAgentStepTarget(nil),
	}
}

func assertWorkflowManagerFailureLog(t *testing.T, output, workflowKey, phase string, want map[string]any) {
	t.Helper()

	expectedHash := workflowManagerTelemetrySHA256(workflowKey)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not valid JSON: %q: %v", line, err)
		}
		if record["msg"] != "workflow manager signal-or-start failed" {
			continue
		}
		if record["workflow_key_sha256"] != expectedHash || record["phase"] != phase {
			continue
		}
		if _, ok := record["error"]; ok {
			t.Fatalf("log record includes raw error field %#v", record)
		}
		assertWorkflowManagerLogField(t, record, "workflow_key_sha256", expectedHash)
		assertWorkflowManagerLogField(t, record, "phase", phase)
		for key, value := range want {
			if key == "execution_ref_id_set" {
				if value == true && !workflowManagerLogStringPresent(record, "execution_ref_id") {
					t.Fatalf("execution_ref_id missing from record %#v", record)
				}
				continue
			}
			if key == "request_id_set" {
				if value == true && !workflowManagerLogStringPresent(record, "request_id") {
					t.Fatalf("request_id missing from record %#v", record)
				}
				continue
			}
			assertWorkflowManagerLogField(t, record, key, value)
		}
		return
	}
	t.Fatalf("workflow manager failure log for key %q phase %q not found in %q", workflowKey, phase, output)
}

func assertWorkflowManagerFailureLogsOmitFields(t *testing.T, output string, fields ...string) {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not valid JSON: %q: %v", line, err)
		}
		if record["msg"] != "workflow manager signal-or-start failed" {
			continue
		}
		for _, field := range fields {
			if _, ok := record[field]; ok {
				t.Fatalf("workflow manager failure log includes %s in record %#v", field, record)
			}
		}
	}
}

func assertWorkflowAuditLog(t *testing.T, output, workflowKey string, want map[string]any) {
	t.Helper()

	expectedHash := workflowManagerTelemetrySHA256(workflowKey)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("audit line is not valid JSON: %q: %v", line, err)
		}
		if record["log.type"] != "audit" || record["workflow_key_sha256"] != expectedHash {
			continue
		}
		assertWorkflowManagerLogField(t, record, "workflow_key_sha256", expectedHash)
		for key, value := range want {
			if key == "request_id_set" {
				if value == true && !workflowManagerLogStringPresent(record, "request_id") {
					t.Fatalf("request_id missing from audit record %#v", record)
				}
				continue
			}
			assertWorkflowManagerLogField(t, record, key, value)
		}
		return
	}
	t.Fatalf("workflow audit for key %q not found in %q", workflowKey, output)
}

func assertWorkflowAuditLogsOmitFields(t *testing.T, output string, fields ...string) {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("audit line is not valid JSON: %q: %v", line, err)
		}
		if record["log.type"] != "audit" {
			continue
		}
		for _, field := range fields {
			if _, ok := record[field]; ok {
				t.Fatalf("workflow audit includes %s in record %#v", field, record)
			}
		}
	}
}

func workflowManagerLogStringPresent(record map[string]any, key string) bool {
	value, ok := record[key].(string)
	return ok && value != ""
}

func assertWorkflowManagerLogField(t *testing.T, record map[string]any, key string, want any) {
	t.Helper()

	got := record[key]
	switch want := want.(type) {
	case string:
		if got != want {
			t.Fatalf("log field %s = %#v, want %q in record %#v", key, got, want, record)
		}
	case int:
		number, ok := got.(float64)
		if !ok || number != float64(want) {
			t.Fatalf("log field %s = %#v, want %d in record %#v", key, got, want, record)
		}
	default:
		if got != want {
			t.Fatalf("log field %s = %#v, want %#v in record %#v", key, got, want, record)
		}
	}
}

func workflowManagerTelemetrySHA256(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

type workflowManagerTelemetryControl struct {
	provider coreworkflow.Provider
}

func (c workflowManagerTelemetryControl) ResolveProvider(string) (coreworkflow.Provider, error) {
	return c.provider, nil
}

func (c workflowManagerTelemetryControl) ResolveProviderSelection(name string) (string, coreworkflow.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "local"
	}
	return name, c.provider, nil
}

func (workflowManagerTelemetryControl) ProviderNames() []string {
	return []string{"local"}
}

type workflowManagerTelemetryAgentControl struct{}

func (workflowManagerTelemetryAgentControl) ResolveProviderSelection(name string) (string, coreagent.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "simple"
	}
	return name, nil, nil
}

type workflowManagerTelemetryAgentManager struct {
	agentmanager.Service
}

type workflowManagerTelemetryProvider struct {
	coreworkflow.Provider
	refs               map[string]*coreworkflow.ExecutionReference
	signalOrStartCalls atomic.Int64
	signalOrStartErr   error
}

func newWorkflowManagerTelemetryProvider() *workflowManagerTelemetryProvider {
	return &workflowManagerTelemetryProvider{refs: map[string]*coreworkflow.ExecutionReference{}}
}

func (p *workflowManagerTelemetryProvider) SignalOrStartRun(_ context.Context, req coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	p.signalOrStartCalls.Add(1)
	if p.signalOrStartErr != nil {
		return nil, p.signalOrStartErr
	}
	signal := req.Signal
	if signal.ID == "" {
		signal.ID = "signal-1"
	}
	return &coreworkflow.SignalRunResponse{
		Run: &coreworkflow.Run{
			ID:           "run-signal-started",
			Status:       coreworkflow.RunStatusRunning,
			WorkflowKey:  req.WorkflowKey,
			Target:       req.Target,
			ExecutionRef: req.ExecutionRef,
			CreatedBy:    req.CreatedBy,
		},
		Signal:      signal,
		StartedRun:  true,
		WorkflowKey: req.WorkflowKey,
	}, nil
}

func (p *workflowManagerTelemetryProvider) PutExecutionReference(_ context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	copied := *ref
	p.refs[copied.ID] = &copied
	return &copied, nil
}

func (p *workflowManagerTelemetryProvider) GetExecutionReference(_ context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	ref := p.refs[strings.TrimSpace(id)]
	if ref == nil {
		return nil, core.ErrNotFound
	}
	copied := *ref
	return &copied, nil
}

func (p *workflowManagerTelemetryProvider) ListExecutionReferences(_ context.Context, subjectID string) ([]*coreworkflow.ExecutionReference, error) {
	var out []*coreworkflow.ExecutionReference
	for _, ref := range p.refs {
		if strings.TrimSpace(ref.SubjectID) != strings.TrimSpace(subjectID) {
			continue
		}
		copied := *ref
		out = append(out, &copied)
	}
	return out, nil
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

func telemetryExecutionReference() *proto.WorkflowExecutionReference {
	return &proto.WorkflowExecutionReference{
		Id:           "ref-1",
		ProviderName: "workflow-metrics",
		Target:       telemetryProtoAppStepTarget("ignored", "ignored"),
		CreatedAt:    timestamppb.Now(),
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
		}}},
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

func telemetryProtoAgentStepTarget(tools []*proto.AgentToolRef) *proto.BoundWorkflowTarget {
	return &proto.BoundWorkflowTarget{
		Steps: []*proto.WorkflowStep{{
			Id: "run",
			Action: &proto.WorkflowStep_Agent{Agent: &proto.WorkflowStepAgentTurn{
				Provider: "simple",
				Prompt:   &proto.WorkflowText{Template: "handle the Slack message"},
				Tools:    tools,
			}},
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

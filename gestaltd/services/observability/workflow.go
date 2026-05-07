package observability

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	WorkflowTelemetrySourceCore     = "core"
	WorkflowTelemetrySourceProvider = "provider"

	WorkflowTriggerKindManual   = "manual"
	WorkflowTriggerKindSchedule = "schedule"
	WorkflowTriggerKindEvent    = "event"
	WorkflowTriggerKindSignal   = "signal"
	WorkflowTriggerKindNone     = "none"
	WorkflowTriggerKindUnknown  = "unknown"

	WorkflowTargetKindPlugin  = "plugin"
	WorkflowTargetKindAgent   = "agent"
	WorkflowTargetKindUnknown = "unknown"

	WorkflowRunStatusPending   = "pending"
	WorkflowRunStatusRunning   = "running"
	WorkflowRunStatusSucceeded = "succeeded"
	WorkflowRunStatusFailed    = "failed"
	WorkflowRunStatusCanceled  = "canceled"
	WorkflowRunStatusUnknown   = "unknown"

	WorkflowOperationStartRun                = "start_run"
	WorkflowOperationGetRun                  = "get_run"
	WorkflowOperationListRuns                = "list_runs"
	WorkflowOperationCancelRun               = "cancel_run"
	WorkflowOperationSignalRun               = "signal_run"
	WorkflowOperationSignalOrStartRun        = "signal_or_start_run"
	WorkflowOperationUpsertSchedule          = "upsert_schedule"
	WorkflowOperationGetSchedule             = "get_schedule"
	WorkflowOperationListSchedules           = "list_schedules"
	WorkflowOperationDeleteSchedule          = "delete_schedule"
	WorkflowOperationPauseSchedule           = "pause_schedule"
	WorkflowOperationResumeSchedule          = "resume_schedule"
	WorkflowOperationUpsertEventTrigger      = "upsert_event_trigger"
	WorkflowOperationGetEventTrigger         = "get_event_trigger"
	WorkflowOperationListEventTriggers       = "list_event_triggers"
	WorkflowOperationDeleteEventTrigger      = "delete_event_trigger"
	WorkflowOperationPauseEventTrigger       = "pause_event_trigger"
	WorkflowOperationResumeEventTrigger      = "resume_event_trigger"
	WorkflowOperationPublishEvent            = "publish_event"
	WorkflowOperationPing                    = "ping"
	WorkflowOperationPutExecutionReference   = "put_execution_reference"
	WorkflowOperationGetExecutionReference   = "get_execution_reference"
	WorkflowOperationListExecutionReferences = "list_execution_references"
	WorkflowOperationInvokeOperation         = "invoke_operation"
)

var (
	AttrWorkflowProviderName    = attribute.Key("gestaltd.workflow.provider.name")
	AttrWorkflowOperationName   = attribute.Key("gestaltd.workflow.operation.name")
	AttrWorkflowTriggerKind     = attribute.Key("gestaltd.workflow.trigger.kind")
	AttrWorkflowTargetKind      = attribute.Key("gestaltd.workflow.target.kind")
	AttrWorkflowRunStatus       = attribute.Key("gestaltd.workflow.run.status")
	AttrWorkflowTelemetrySource = attribute.Key("gestaltd.workflow.telemetry.source")
)

type WorkflowMetricDims struct {
	ProviderName    string
	OperationName   string
	TriggerKind     string
	TargetKind      string
	RunStatus       string
	TelemetrySource string
}

type workflowRunCompletedMetricSet struct {
	count    metric.Int64Counter
	duration metric.Float64Histogram
}

type workflowMatchedTriggersMetricSet struct {
	count metric.Int64Counter
}

var (
	workflowProviderOperationMetrics metricutil.MeterCache[metricSet]
	workflowHostOperationMetrics     metricutil.MeterCache[metricSet]
	workflowRunStartedMetrics        metricutil.MeterCache[countMetricSet]
	workflowRunCompletedMetrics      metricutil.MeterCache[workflowRunCompletedMetricSet]
	workflowEventPublishedMetrics    metricutil.MeterCache[countMetricSet]
	workflowEventMatchedMetrics      metricutil.MeterCache[workflowMatchedTriggersMetricSet]
	workflowScheduleFiredMetrics     metricutil.MeterCache[countMetricSet]
)

func RecordWorkflowProviderOperation(ctx context.Context, startedAt time.Time, err error, dims WorkflowMetricDims) {
	record(ctx, &workflowProviderOperationMetrics, "gestaltd.workflows.provider.operation", "gestaltd workflow provider operations", startedAt, err != nil, workflowMetricAttrs(dims, err)...)
}

func RecordWorkflowHostOperation(ctx context.Context, startedAt time.Time, err error, dims WorkflowMetricDims) {
	record(ctx, &workflowHostOperationMetrics, "gestaltd.workflows.host.operation", "gestaltd workflow host operations", startedAt, err != nil, workflowMetricAttrs(dims, err)...)
}

func RecordWorkflowRunStarted(ctx context.Context, dims WorkflowMetricDims) {
	recordCount(ctx, &workflowRunStartedMetrics, "gestaltd.workflows.runs.started", "gestaltd workflow run starts", false, workflowMetricAttrs(dims, nil)...)
}

func RecordWorkflowRunCompleted(ctx context.Context, startedAt time.Time, dims WorkflowMetricDims) {
	if ctx == nil {
		ctx = context.Background()
	}
	metrics := workflowRunCompletedMetrics.Load(ctx, tracerName, func(meter metric.Meter) workflowRunCompletedMetricSet {
		return workflowRunCompletedMetricSet{
			count: metricutil.NewInt64Counter(
				meter,
				"gestaltd.workflows.runs.completed.count",
				"Counts completed gestaltd workflow runs.",
			),
			duration: metricutil.NewFloat64Histogram(
				meter,
				"gestaltd.workflows.runs.duration",
				"Measures gestaltd workflow run duration.",
				"s",
			),
		}
	})
	attrs := workflowMetricAttrs(dims, nil)
	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	metrics.duration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
}

func RecordWorkflowEventPublished(ctx context.Context, err error, dims WorkflowMetricDims) {
	recordCount(ctx, &workflowEventPublishedMetrics, "gestaltd.workflows.events.published", "gestaltd workflow published events", err != nil, workflowMetricAttrs(dims, err)...)
}

func RecordWorkflowEventMatchedTriggers(ctx context.Context, count int64, dims WorkflowMetricDims) {
	if count <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	metrics := workflowEventMatchedMetrics.Load(ctx, tracerName, func(meter metric.Meter) workflowMatchedTriggersMetricSet {
		return workflowMatchedTriggersMetricSet{
			count: metricutil.NewInt64Counter(
				meter,
				"gestaltd.workflows.events.matched_triggers.count",
				"Counts gestaltd workflow event trigger matches.",
			),
		}
	})
	metrics.count.Add(ctx, count, metric.WithAttributes(workflowMetricAttrs(dims, nil)...))
}

func RecordWorkflowScheduleFired(ctx context.Context, dims WorkflowMetricDims) {
	recordCount(ctx, &workflowScheduleFiredMetrics, "gestaltd.workflows.schedules.fired", "gestaltd workflow schedule firings", false, workflowMetricAttrs(dims, nil)...)
}

func WorkflowMetricAttributes(dims WorkflowMetricDims) []attribute.KeyValue {
	return workflowMetricAttrs(dims, nil)
}

func workflowMetricAttrs(dims WorkflowMetricDims, err error) []attribute.KeyValue {
	dims = normalizeWorkflowMetricDims(dims)
	attrs := []attribute.KeyValue{
		AttrWorkflowProviderName.String(metricutil.AttrValue(dims.ProviderName)),
		AttrWorkflowOperationName.String(metricutil.AttrValue(dims.OperationName)),
		AttrWorkflowTriggerKind.String(metricutil.AttrValue(dims.TriggerKind)),
		AttrWorkflowTargetKind.String(metricutil.AttrValue(dims.TargetKind)),
		AttrWorkflowRunStatus.String(metricutil.AttrValue(dims.RunStatus)),
		AttrWorkflowTelemetrySource.String(metricutil.AttrValue(dims.TelemetrySource)),
	}
	if typ := workflowErrorType(err); typ != "" {
		attrs = append(attrs, AttrErrorType.String(typ))
	}
	return attrs
}

func normalizeWorkflowMetricDims(dims WorkflowMetricDims) WorkflowMetricDims {
	dims.ProviderName = strings.TrimSpace(dims.ProviderName)
	dims.OperationName = strings.TrimSpace(dims.OperationName)
	dims.TriggerKind = workflowAttrValue(dims.TriggerKind, WorkflowTriggerKindNone)
	dims.TargetKind = workflowAttrValue(dims.TargetKind, WorkflowTargetKindUnknown)
	dims.RunStatus = workflowAttrValue(dims.RunStatus, WorkflowRunStatusUnknown)
	dims.TelemetrySource = workflowAttrValue(dims.TelemetrySource, WorkflowTelemetrySourceCore)
	return dims
}

func workflowAttrValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func workflowErrorType(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "context.canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context.deadline_exceeded"
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.OK {
		return "grpc." + workflowGRPCCodeName(st.Code())
	}
	return "unknown"
}

func workflowGRPCCodeName(code codes.Code) string {
	switch code {
	case codes.Canceled:
		return "canceled"
	case codes.Unknown:
		return "unknown"
	case codes.InvalidArgument:
		return "invalid_argument"
	case codes.DeadlineExceeded:
		return "deadline_exceeded"
	case codes.NotFound:
		return "not_found"
	case codes.AlreadyExists:
		return "already_exists"
	case codes.PermissionDenied:
		return "permission_denied"
	case codes.ResourceExhausted:
		return "resource_exhausted"
	case codes.FailedPrecondition:
		return "failed_precondition"
	case codes.Aborted:
		return "aborted"
	case codes.OutOfRange:
		return "out_of_range"
	case codes.Unimplemented:
		return "unimplemented"
	case codes.Internal:
		return "internal"
	case codes.Unavailable:
		return "unavailable"
	case codes.DataLoss:
		return "data_loss"
	case codes.Unauthenticated:
		return "unauthenticated"
	default:
		return strings.ToLower(code.String())
	}
}

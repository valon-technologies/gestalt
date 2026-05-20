package gestalt

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
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
	WorkflowOperationGetExecutionReference   = "get_execution_reference"
	WorkflowOperationListExecutionReferences = "list_execution_references"
	WorkflowOperationInvokeOperation         = "invoke_operation"

	workflowTelemetrySourceProvider = "provider"
)

type WorkflowOperationOptions struct {
	ProviderName  string
	OperationName string
	TriggerKind   string
	TargetKind    string
	RunStatus     string
}

type WorkflowTelemetryOperation struct {
	ctx       context.Context
	span      trace.Span
	startedAt time.Time
	opts      WorkflowOperationOptions
	ended     bool
	mu        sync.Mutex
}

type workflowTelemetryMetrics struct {
	providerOperationCount      metric.Int64Counter
	providerOperationErrorCount metric.Int64Counter
	providerOperationDuration   metric.Float64Histogram
	runStartedCount             metric.Int64Counter
	runCompletedCount           metric.Int64Counter
	runDuration                 metric.Float64Histogram
	eventPublishedCount         metric.Int64Counter
	eventPublishedErrorCount    metric.Int64Counter
	eventMatchedTriggersCount   metric.Int64Counter
	scheduleFiredCount          metric.Int64Counter
}

var (
	workflowTelemetryOnce    sync.Once
	workflowTelemetryRecords workflowTelemetryMetrics
)

func WorkflowOperation(ctx context.Context, opts WorkflowOperationOptions) (context.Context, *WorkflowTelemetryOperation) {
	if ctx == nil {
		ctx = context.Background()
	}
	opts = normalizeWorkflowOperationOptions(opts)
	ctx, span := otel.Tracer(TelemetryInstrumentationName).Start(
		ctx,
		workflowSpanName(opts.OperationName),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(workflowTelemetryAttrs(opts, nil)...),
	)
	return ctx, &WorkflowTelemetryOperation{
		ctx:       ctx,
		span:      span,
		startedAt: time.Now(),
		opts:      opts,
	}
}

func (op *WorkflowTelemetryOperation) End(err error) {
	if op == nil {
		return
	}
	op.mu.Lock()
	if op.ended {
		op.mu.Unlock()
		return
	}
	op.ended = true
	ctx := op.ctx
	startedAt := op.startedAt
	opts := op.opts
	op.mu.Unlock()

	if err != nil {
		op.span.RecordError(err)
		op.span.SetAttributes(attribute.String("error.type", workflowTelemetryErrorType(err)))
		op.span.SetStatus(codes.Error, err.Error())
	}
	attrs := workflowTelemetryAttrs(opts, err)
	metrics := workflowMetrics()
	metrics.providerOperationCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	metrics.providerOperationDuration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
	if err != nil {
		metrics.providerOperationErrorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	op.span.End()
}

func RecordWorkflowRunStarted(ctx context.Context, opts WorkflowOperationOptions) {
	if ctx == nil {
		ctx = context.Background()
	}
	workflowMetrics().runStartedCount.Add(ctx, 1, metric.WithAttributes(workflowTelemetryAttrs(opts, nil)...))
}

func RecordWorkflowRunCompleted(ctx context.Context, startedAt time.Time, opts WorkflowOperationOptions) {
	if ctx == nil {
		ctx = context.Background()
	}
	attrs := workflowTelemetryAttrs(opts, nil)
	metrics := workflowMetrics()
	metrics.runCompletedCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	metrics.runDuration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
}

func RecordWorkflowEventPublished(ctx context.Context, err error, opts WorkflowOperationOptions) {
	if ctx == nil {
		ctx = context.Background()
	}
	attrs := workflowTelemetryAttrs(opts, err)
	metrics := workflowMetrics()
	metrics.eventPublishedCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	if err != nil {
		metrics.eventPublishedErrorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func RecordWorkflowEventMatchedTriggers(ctx context.Context, count int64, opts WorkflowOperationOptions) {
	if count <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	workflowMetrics().eventMatchedTriggersCount.Add(ctx, count, metric.WithAttributes(workflowTelemetryAttrs(opts, nil)...))
}

func RecordWorkflowScheduleFired(ctx context.Context, opts WorkflowOperationOptions) {
	if ctx == nil {
		ctx = context.Background()
	}
	workflowMetrics().scheduleFiredCount.Add(ctx, 1, metric.WithAttributes(workflowTelemetryAttrs(opts, nil)...))
}

func workflowMetrics() workflowTelemetryMetrics {
	workflowTelemetryOnce.Do(func() {
		meter := otel.Meter(TelemetryInstrumentationName)
		workflowTelemetryRecords.providerOperationCount, _ = meter.Int64Counter(
			"gestaltd.workflows.provider.operation.count",
			metric.WithDescription("Counts gestaltd workflow provider operations."),
		)
		workflowTelemetryRecords.providerOperationErrorCount, _ = meter.Int64Counter(
			"gestaltd.workflows.provider.operation.error_count",
			metric.WithDescription("Counts failed gestaltd workflow provider operations."),
		)
		workflowTelemetryRecords.providerOperationDuration, _ = meter.Float64Histogram(
			"gestaltd.workflows.provider.operation.duration",
			metric.WithDescription("Measures gestaltd workflow provider operation duration."),
			metric.WithUnit("s"),
		)
		workflowTelemetryRecords.runStartedCount, _ = meter.Int64Counter(
			"gestaltd.workflows.runs.started.count",
			metric.WithDescription("Counts gestaltd workflow run starts."),
		)
		workflowTelemetryRecords.runCompletedCount, _ = meter.Int64Counter(
			"gestaltd.workflows.runs.completed.count",
			metric.WithDescription("Counts completed gestaltd workflow runs."),
		)
		workflowTelemetryRecords.runDuration, _ = meter.Float64Histogram(
			"gestaltd.workflows.runs.duration",
			metric.WithDescription("Measures gestaltd workflow run duration."),
			metric.WithUnit("s"),
		)
		workflowTelemetryRecords.eventPublishedCount, _ = meter.Int64Counter(
			"gestaltd.workflows.events.published.count",
			metric.WithDescription("Counts gestaltd workflow published events."),
		)
		workflowTelemetryRecords.eventPublishedErrorCount, _ = meter.Int64Counter(
			"gestaltd.workflows.events.published.error_count",
			metric.WithDescription("Counts failed gestaltd workflow published events."),
		)
		workflowTelemetryRecords.eventMatchedTriggersCount, _ = meter.Int64Counter(
			"gestaltd.workflows.events.matched_triggers.count",
			metric.WithDescription("Counts gestaltd workflow event trigger matches."),
		)
		workflowTelemetryRecords.scheduleFiredCount, _ = meter.Int64Counter(
			"gestaltd.workflows.schedules.fired.count",
			metric.WithDescription("Counts gestaltd workflow schedule firings."),
		)
	})
	return workflowTelemetryRecords
}

func workflowTelemetryAttrs(opts WorkflowOperationOptions, err error) []attribute.KeyValue {
	opts = normalizeWorkflowOperationOptions(opts)
	attrs := []attribute.KeyValue{
		attribute.String("gestaltd.workflow.provider.name", workflowRequiredAttr(opts.ProviderName, "unknown")),
		attribute.String("gestaltd.workflow.operation.name", workflowRequiredAttr(opts.OperationName, "unknown")),
		attribute.String("gestaltd.workflow.trigger.kind", workflowRequiredAttr(opts.TriggerKind, WorkflowTriggerKindNone)),
		attribute.String("gestaltd.workflow.target.kind", workflowRequiredAttr(opts.TargetKind, WorkflowTargetKindUnknown)),
		attribute.String("gestaltd.workflow.run.status", workflowRequiredAttr(opts.RunStatus, WorkflowRunStatusUnknown)),
		attribute.String("gestaltd.workflow.telemetry.source", workflowTelemetrySourceProvider),
	}
	if typ := workflowTelemetryErrorType(err); typ != "" {
		attrs = append(attrs, attribute.String("error.type", typ))
	}
	return attrs
}

func normalizeWorkflowOperationOptions(opts WorkflowOperationOptions) WorkflowOperationOptions {
	opts.ProviderName = cleanTelemetryString(opts.ProviderName)
	opts.OperationName = cleanTelemetryString(opts.OperationName)
	opts.TriggerKind = cleanTelemetryString(opts.TriggerKind)
	opts.TargetKind = cleanTelemetryString(opts.TargetKind)
	opts.RunStatus = cleanTelemetryString(opts.RunStatus)
	return opts
}

func workflowRequiredAttr(value, fallback string) string {
	value = cleanTelemetryString(value)
	if value == "" {
		return fallback
	}
	return value
}

func workflowTelemetryErrorType(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "context.canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context.deadline_exceeded"
	}
	if st, ok := status.FromError(err); ok && st.Code() != grpccodes.OK {
		return "grpc." + workflowTelemetryGRPCCodeName(st.Code())
	}
	return "unknown"
}

func workflowTelemetryGRPCCodeName(code grpccodes.Code) string {
	switch code {
	case grpccodes.Canceled:
		return "canceled"
	case grpccodes.Unknown:
		return "unknown"
	case grpccodes.InvalidArgument:
		return "invalid_argument"
	case grpccodes.DeadlineExceeded:
		return "deadline_exceeded"
	case grpccodes.NotFound:
		return "not_found"
	case grpccodes.AlreadyExists:
		return "already_exists"
	case grpccodes.PermissionDenied:
		return "permission_denied"
	case grpccodes.ResourceExhausted:
		return "resource_exhausted"
	case grpccodes.FailedPrecondition:
		return "failed_precondition"
	case grpccodes.Aborted:
		return "aborted"
	case grpccodes.OutOfRange:
		return "out_of_range"
	case grpccodes.Unimplemented:
		return "unimplemented"
	case grpccodes.Internal:
		return "internal"
	case grpccodes.Unavailable:
		return "unavailable"
	case grpccodes.DataLoss:
		return "data_loss"
	case grpccodes.Unauthenticated:
		return "unauthenticated"
	default:
		return strings.ToLower(code.String())
	}
}

func workflowSpanName(operation string) string {
	operation = cleanTelemetryString(operation)
	if operation == "" {
		return "workflow.provider.operation"
	}
	return "workflow.provider.operation " + operation
}

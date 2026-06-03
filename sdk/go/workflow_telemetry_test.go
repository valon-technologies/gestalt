package gestalt

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWorkflowTelemetryRecordsMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(prev)
		_ = provider.Shutdown(context.Background())
	})

	opts := WorkflowOperationOptions{
		ProviderName:  "temporal",
		OperationName: WorkflowOperationDeliverEvent,
		TriggerKind:   WorkflowTriggerKindEvent,
		TargetKind:    WorkflowTargetKindSteps,
	}
	ctx, op := WorkflowOperation(context.Background(), opts)
	op.End(status.Error(codes.InvalidArgument, "bad event"))
	op.End(nil)

	RecordWorkflowEventDelivered(ctx, nil, opts)
	RecordWorkflowEventMatchedActivations(ctx, 2, opts)
	RecordWorkflowEventMatchedActivations(ctx, 0, opts)
	RecordWorkflowRunStarted(ctx, WorkflowOperationOptions{
		ProviderName:  "temporal",
		OperationName: WorkflowOperationDeliverEvent,
		TriggerKind:   WorkflowTriggerKindEvent,
		TargetKind:    WorkflowTargetKindSteps,
		RunStatus:     WorkflowRunStatusPending,
	})
	RecordWorkflowRunCompleted(ctx, time.Now().Add(-time.Second), WorkflowOperationOptions{
		ProviderName:  "temporal",
		OperationName: WorkflowOperationStartRun,
		TriggerKind:   WorkflowTriggerKindManual,
		TargetKind:    WorkflowTargetKindSteps,
		RunStatus:     WorkflowRunStatusSucceeded,
	})
	RecordWorkflowActivationFired(ctx, WorkflowOperationOptions{
		ProviderName:  "temporal",
		OperationName: WorkflowOperationStartRun,
		TriggerKind:   WorkflowTriggerKindSchedule,
		TargetKind:    WorkflowTargetKindSteps,
	})

	rm := collectWorkflowTelemetryMetrics(t, reader)
	operationErrorAttrs := workflowTelemetryTestAttrs(
		WorkflowOperationDeliverEvent,
		WorkflowTriggerKindEvent,
		WorkflowTargetKindSteps,
		WorkflowRunStatusUnknown,
		map[string]string{"error.type": "grpc.invalid_argument"},
	)
	requireWorkflowInt64Sum(t, rm, "gestaltd.workflows.provider.operation.count", 1, operationErrorAttrs)
	requireWorkflowInt64Sum(t, rm, "gestaltd.workflows.provider.operation.error_count", 1, operationErrorAttrs)
	requireWorkflowFloat64Histogram(t, rm, "gestaltd.workflows.provider.operation.duration", operationErrorAttrs)
	requireWorkflowInt64Sum(t, rm, "gestaltd.workflows.events.delivered.count", 1, workflowTelemetryTestAttrs(WorkflowOperationDeliverEvent, WorkflowTriggerKindEvent, WorkflowTargetKindSteps, WorkflowRunStatusUnknown, nil))
	requireWorkflowInt64Sum(t, rm, "gestaltd.workflows.events.matched_activations.count", 2, workflowTelemetryTestAttrs(WorkflowOperationDeliverEvent, WorkflowTriggerKindEvent, WorkflowTargetKindSteps, WorkflowRunStatusUnknown, nil))
	requireWorkflowInt64Sum(t, rm, "gestaltd.workflows.runs.started.count", 1, workflowTelemetryTestAttrs(WorkflowOperationDeliverEvent, WorkflowTriggerKindEvent, WorkflowTargetKindSteps, WorkflowRunStatusPending, nil))
	requireWorkflowInt64Sum(t, rm, "gestaltd.workflows.runs.completed.count", 1, workflowTelemetryTestAttrs(WorkflowOperationStartRun, WorkflowTriggerKindManual, WorkflowTargetKindSteps, WorkflowRunStatusSucceeded, nil))
	requireWorkflowFloat64Histogram(t, rm, "gestaltd.workflows.runs.duration", workflowTelemetryTestAttrs(WorkflowOperationStartRun, WorkflowTriggerKindManual, WorkflowTargetKindSteps, WorkflowRunStatusSucceeded, nil))
	requireWorkflowInt64Sum(t, rm, "gestaltd.workflows.activations.fired.count", 1, workflowTelemetryTestAttrs(WorkflowOperationStartRun, WorkflowTriggerKindSchedule, WorkflowTargetKindSteps, WorkflowRunStatusUnknown, nil))
}

func collectWorkflowTelemetryMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	return rm
}

func workflowTelemetryTestAttrs(operation, triggerKind, targetKind, runStatus string, extra map[string]string) map[string]string {
	attrs := map[string]string{
		"gestaltd.workflow.provider.name":    "temporal",
		"gestaltd.workflow.operation.name":   operation,
		"gestaltd.workflow.trigger.kind":     triggerKind,
		"gestaltd.workflow.target.kind":      targetKind,
		"gestaltd.workflow.run.status":       runStatus,
		"gestaltd.workflow.telemetry.source": "provider",
	}
	for key, value := range extra {
		attrs[key] = value
	}
	return attrs
}

func requireWorkflowInt64Sum(t *testing.T, rm metricdata.ResourceMetrics, name string, want int64, attrs map[string]string) {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q is %T, want Sum[int64]", name, metric.Data)
			}
			for _, point := range sum.DataPoints {
				if workflowTelemetryAttrsMatch(point.Attributes, attrs) {
					if point.Value != want {
						t.Fatalf("metric %q attrs %v = %d, want %d", name, attrs, point.Value, want)
					}
					return
				}
			}
		}
	}

	t.Fatalf("metric %q with attrs %v not found", name, attrs)
}

func requireWorkflowFloat64Histogram(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %q is %T, want Histogram[float64]", name, metric.Data)
			}
			for _, point := range histogram.DataPoints {
				if workflowTelemetryAttrsMatch(point.Attributes, attrs) {
					return
				}
			}
		}
	}

	t.Fatalf("metric %q with attrs %v not found", name, attrs)
}

func workflowTelemetryAttrsMatch(set attribute.Set, want map[string]string) bool {
	for key, expected := range want {
		value, ok := set.Value(attribute.Key(key))
		if !ok || value.AsString() != expected {
			return false
		}
	}
	return true
}

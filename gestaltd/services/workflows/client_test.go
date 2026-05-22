package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestNewExecutableWorkflowForwardsProcessRuntimeOptions(t *testing.T) {
	t.Parallel()

	originalStart := startWorkflowProviderProcess
	defer func() { startWorkflowProviderProcess = originalStart }()

	wantErr := errors.New("boom")
	wantTelemetry := workflowTestTelemetry{}
	var got runtimehost.ProcessConfig
	startWorkflowProviderProcess = func(_ context.Context, cfg runtimehost.ProcessConfig) (*runtimehost.AppProcess, error) {
		got = cfg
		return nil, wantErr
	}

	_, err := NewExecutable(context.Background(), ExecConfig{
		Command:   "/bin/true",
		Egress:    egress.Policy{DefaultAction: egress.PolicyDeny},
		Name:      "workflow-provider",
		Telemetry: wantTelemetry,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("NewExecutable error = %v, want %v", err, wantErr)
	}
	if got.Egress.DefaultAction != egress.PolicyDeny {
		t.Fatalf("default action = %q, want %q", got.Egress.DefaultAction, egress.PolicyDeny)
	}
	if got.ProviderName != "workflow-provider" {
		t.Fatalf("provider name = %q, want workflow-provider", got.ProviderName)
	}
	if got.Telemetry != wantTelemetry {
		t.Fatalf("telemetry provider was not forwarded")
	}
}

type workflowTestTelemetry struct{}

func (workflowTestTelemetry) MeterProvider() metric.MeterProvider {
	return metricnoop.NewMeterProvider()
}

func (workflowTestTelemetry) TracerProvider() trace.TracerProvider {
	return tracenoop.NewTracerProvider()
}

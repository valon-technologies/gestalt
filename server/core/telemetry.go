package core

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type TelemetryProvider interface {
	Logger() *slog.Logger
	TracerProvider() trace.TracerProvider
	MeterProvider() metric.MeterProvider
	Shutdown(ctx context.Context) error
}

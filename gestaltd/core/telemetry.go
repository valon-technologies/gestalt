package core

import (
	"context"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type TelemetryProvider interface {
	Logger() *slog.Logger
	TracerProvider() trace.TracerProvider
	MeterProvider() metric.MeterProvider
	PrometheusHandler() http.Handler
	OperationMetrics() OperationMetrics
	Shutdown(ctx context.Context) error
}

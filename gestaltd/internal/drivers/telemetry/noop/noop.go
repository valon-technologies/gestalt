package noop

import (
	"context"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

var _ core.TelemetryProvider = (*Provider)(nil)

type Provider struct {
	logger *slog.Logger
	tp     trace.TracerProvider
	mp     metric.MeterProvider
}

func New() *Provider {
	return &Provider{
		logger: slog.New(slog.DiscardHandler),
		tp:     nooptrace.NewTracerProvider(),
		mp:     noopmetric.NewMeterProvider(),
	}
}

func (p *Provider) Logger() *slog.Logger                 { return p.logger }
func (p *Provider) TracerProvider() trace.TracerProvider { return p.tp }
func (p *Provider) MeterProvider() metric.MeterProvider  { return p.mp }
func (p *Provider) PrometheusHandler() http.Handler      { return nil }
func (p *Provider) Shutdown(context.Context) error       { return nil }

var Factory bootstrap.TelemetryFactory = func(yaml.Node) (core.TelemetryProvider, error) {
	return New(), nil
}

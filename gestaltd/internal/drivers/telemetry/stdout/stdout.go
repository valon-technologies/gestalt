package stdout

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/telemetryutil"
	"gopkg.in/yaml.v3"
)

var _ core.TelemetryProvider = (*Provider)(nil)

type yamlConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type Provider struct {
	logger *slog.Logger
	tp     trace.TracerProvider
	mp     metric.MeterProvider
}

func NewLogger(levelName, format string) *slog.Logger {
	level := telemetryutil.ParseLevel(levelName)
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

func New(cfg yamlConfig) (*Provider, error) {
	tp := nooptrace.NewTracerProvider()
	mp := noopmetric.NewMeterProvider()

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	return &Provider{
		logger: NewLogger(cfg.Level, cfg.Format),
		tp:     tp,
		mp:     mp,
	}, nil
}

func (p *Provider) Logger() *slog.Logger                 { return p.logger }
func (p *Provider) TracerProvider() trace.TracerProvider { return p.tp }
func (p *Provider) MeterProvider() metric.MeterProvider  { return p.mp }

func (p *Provider) Shutdown(context.Context) error { return nil }

var Factory bootstrap.TelemetryFactory = func(node yaml.Node) (core.TelemetryProvider, error) {
	var cfg yamlConfig
	if node.Kind != 0 {
		if err := node.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("stdout telemetry: parsing config: %w", err)
		}
	}
	return New(cfg)
}

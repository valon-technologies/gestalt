package otlp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/services/observability/drivers/metricspipeline"
	telemetrystdout "github.com/valon-technologies/gestalt/server/services/observability/drivers/stdout"
	"github.com/valon-technologies/gestalt/server/services/observability/drivers/telemetryutil"
	"gopkg.in/yaml.v3"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

var _ core.TelemetryProvider = (*Provider)(nil)

type yamlConfig struct {
	Endpoint           string            `yaml:"endpoint"`
	Protocol           string            `yaml:"protocol"`
	ServiceName        string            `yaml:"serviceName"`
	Insecure           bool              `yaml:"insecure"`
	Headers            map[string]string `yaml:"headers"`
	ResourceAttributes map[string]string `yaml:"resourceAttributes"`
	Traces             tracesConfig      `yaml:"traces"`
	Metrics            metricsConfig     `yaml:"metrics"`
	Logs               logsConfig        `yaml:"logs"`
}

type tracesConfig struct {
	SamplingRatio *float64 `yaml:"samplingRatio"`
}

type metricsConfig struct {
	Interval   string                           `yaml:"interval"`
	Prometheus metricspipeline.PrometheusConfig `yaml:"prometheus"`
}

type logsConfig struct {
	Level    string `yaml:"level"`
	Exporter string `yaml:"exporter"`
	Format   string `yaml:"format"`
}

const (
	defaultProtocol    = "grpc"
	defaultServiceName = "gestaltd"
	defaultInterval    = 60 * time.Second
	defaultLogLevel    = "info"
	defaultLogExporter = "otlp"
	defaultLogFormat   = "text"
)

type Provider struct {
	logger                *slog.Logger
	tp                    *sdktrace.TracerProvider
	mp                    *sdkmetric.MeterProvider
	lp                    *sdklog.LoggerProvider
	prometheus            http.Handler
	providerEnv           map[string]string
	providerResourceAttrs map[string]string
}

func New(ctx context.Context, cfg yamlConfig) (*Provider, error) {
	applyConfigDefaults(&cfg)

	switch strings.ToLower(cfg.Protocol) {
	case "grpc", "http":
	default:
		return nil, fmt.Errorf("otlp telemetry: unknown protocol %q (expected \"grpc\" or \"http\")", cfg.Protocol)
	}

	res := telemetryutil.BuildResource(cfg.ServiceName, cfg.ResourceAttributes)

	tp, err := buildTracerProvider(ctx, cfg, res)
	if err != nil {
		return nil, fmt.Errorf("otlp telemetry: building tracer provider: %w", err)
	}

	metrics, err := buildMeterProvider(ctx, cfg, res)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, fmt.Errorf("otlp telemetry: building meter provider: %w", err)
	}

	logger, lp, err := buildLogger(ctx, cfg, res)
	if err != nil {
		_ = tp.Shutdown(ctx)
		_ = metrics.MeterProvider.Shutdown(ctx)
		return nil, fmt.Errorf("otlp telemetry: building logger: %w", err)
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(metrics.MeterProvider)

	return &Provider{
		logger:                logger,
		tp:                    tp,
		mp:                    metrics.MeterProvider,
		lp:                    lp,
		prometheus:            metrics.Prometheus,
		providerEnv:           buildProviderTelemetryEnv(cfg),
		providerResourceAttrs: buildProviderResourceAttributes(cfg),
	}, nil
}

func (p *Provider) Logger() *slog.Logger                 { return p.logger }
func (p *Provider) TracerProvider() trace.TracerProvider { return p.tp }
func (p *Provider) MeterProvider() metric.MeterProvider  { return p.mp }
func (p *Provider) PrometheusHandler() http.Handler      { return p.prometheus }

func (p *Provider) ProviderTelemetryEnv(providerName string) map[string]string {
	if p == nil || len(p.providerEnv) == 0 {
		return nil
	}
	env := make(map[string]string, len(p.providerEnv)+1)
	for key, value := range p.providerEnv {
		env[key] = value
	}
	env["OTEL_SERVICE_NAME"] = providerServiceName(providerName)

	resourceAttrs := make(map[string]string, len(p.providerResourceAttrs)+2)
	for key, value := range p.providerResourceAttrs {
		resourceAttrs[key] = value
	}
	if strings.TrimSpace(resourceAttrs["service.namespace"]) == "" {
		resourceAttrs["service.namespace"] = "gestalt-providers"
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" {
		resourceAttrs["gestaltd.provider.name"] = providerName
	}
	if encoded := encodeOTELKeyValueEnv(resourceAttrs); encoded != "" {
		env["OTEL_RESOURCE_ATTRIBUTES"] = encoded
	}
	return env
}

func (p *Provider) Shutdown(ctx context.Context) error {
	tpErr := p.tp.Shutdown(ctx)
	mpErr := p.mp.Shutdown(ctx)

	var lpErr error
	if p.lp != nil {
		lpErr = p.lp.Shutdown(ctx)
	}

	return errors.Join(tpErr, mpErr, lpErr)
}

func applyConfigDefaults(cfg *yamlConfig) {
	if cfg.Protocol == "" {
		cfg.Protocol = defaultProtocol
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultServiceName
	}
	if cfg.Traces.SamplingRatio == nil {
		ratio := 1.0
		cfg.Traces.SamplingRatio = &ratio
	}
	if cfg.Metrics.Interval == "" {
		cfg.Metrics.Interval = defaultInterval.String()
	}
	if cfg.Logs.Level == "" {
		cfg.Logs.Level = defaultLogLevel
	}
	if cfg.Logs.Exporter == "" {
		cfg.Logs.Exporter = defaultLogExporter
	}
	if cfg.Logs.Format == "" {
		cfg.Logs.Format = defaultLogFormat
	}
}

func buildProviderTelemetryEnv(cfg yamlConfig) map[string]string {
	env := map[string]string{
		"OTEL_EXPORTER_OTLP_PROTOCOL": otelEnvProtocol(cfg.Protocol),
	}
	if cfg.Endpoint != "" {
		env["OTEL_EXPORTER_OTLP_ENDPOINT"] = cfg.Endpoint
	}
	if cfg.Insecure {
		env["OTEL_EXPORTER_OTLP_INSECURE"] = "true"
	}
	if encodedHeaders := encodeOTELKeyValueEnv(cfg.Headers); encodedHeaders != "" {
		env["OTEL_EXPORTER_OTLP_HEADERS"] = encodedHeaders
	}
	if cfg.Traces.SamplingRatio != nil {
		env["OTEL_TRACES_SAMPLER"] = "parentbased_traceidratio"
		env["OTEL_TRACES_SAMPLER_ARG"] = strconv.FormatFloat(*cfg.Traces.SamplingRatio, 'f', -1, 64)
	}
	if interval, err := time.ParseDuration(cfg.Metrics.Interval); err == nil && interval > 0 {
		env["OTEL_METRIC_EXPORT_INTERVAL"] = strconv.FormatInt(interval.Milliseconds(), 10)
	}
	return env
}

func buildProviderResourceAttributes(cfg yamlConfig) map[string]string {
	attrs := make(map[string]string)
	for _, attr := range telemetryutil.BuildResource("", cfg.ResourceAttributes).Attributes() {
		key := string(attr.Key)
		if key == "service.name" {
			continue
		}
		if value := strings.TrimSpace(attr.Value.AsString()); value != "" {
			attrs[key] = value
		}
	}
	return attrs
}

func otelEnvProtocol(protocol string) string {
	if strings.EqualFold(protocol, "http") {
		return "http/protobuf"
	}
	return "grpc"
}

func providerServiceName(providerName string) string {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return "gestalt-provider"
	}
	return "gestalt-provider-" + providerName
}

func encodeOTELKeyValueEnv(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, encodeOTELKeyValuePart(key)+"="+encodeOTELKeyValuePart(values[key]))
	}
	return strings.Join(parts, ",")
}

func encodeOTELKeyValuePart(value string) string {
	return strings.ReplaceAll(url.QueryEscape(strings.TrimSpace(value)), "+", "%20")
}

func buildTracerProvider(ctx context.Context, cfg yamlConfig, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	var exporter sdktrace.SpanExporter
	var err error

	switch strings.ToLower(cfg.Protocol) {
	case "http":
		opts := []otlptracehttp.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
	default:
		opts := []otlptracegrpc.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		exporter, err = otlptracegrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, err
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(*cfg.Traces.SamplingRatio))
	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	), nil
}

func buildMeterProvider(ctx context.Context, cfg yamlConfig, res *resource.Resource) (*metricspipeline.Result, error) {
	interval, err := time.ParseDuration(cfg.Metrics.Interval)
	if err != nil {
		return nil, fmt.Errorf("parsing metrics interval: %w", err)
	}

	var exporter sdkmetric.Exporter
	switch strings.ToLower(cfg.Protocol) {
	case "http":
		opts := []otlpmetrichttp.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlpmetrichttp.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(cfg.Headers))
		}
		exporter, err = otlpmetrichttp.New(ctx, opts...)
	default:
		opts := []otlpmetricgrpc.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlpmetricgrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlpmetricgrpc.WithHeaders(cfg.Headers))
		}
		exporter, err = otlpmetricgrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, err
	}

	reader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(interval))
	return metricspipeline.Build(res, metricspipeline.Config{
		Prometheus: cfg.Metrics.Prometheus,
	}, reader)
}

func buildLogger(ctx context.Context, cfg yamlConfig, res *resource.Resource) (*slog.Logger, *sdklog.LoggerProvider, error) {
	switch strings.ToLower(cfg.Logs.Exporter) {
	case "otlp":
		lp, err := buildLoggerProvider(ctx, cfg, res)
		if err != nil {
			return nil, nil, err
		}

		level := telemetryutil.ParseLevel(cfg.Logs.Level)
		logger := slog.New(otelslog.NewHandler("gestaltd",
			otelslog.WithLoggerProvider(lp),
		))
		logger = slog.New(levelFilterHandler{level: level, inner: logger.Handler()})
		return logger, lp, nil

	case "stdout":
		return telemetrystdout.NewLogger(cfg.Logs.Level, cfg.Logs.Format), nil, nil

	default:
		return nil, nil, fmt.Errorf(
			"unknown logs exporter %q (expected %q or %q)",
			cfg.Logs.Exporter,
			"otlp",
			"stdout",
		)
	}
}

func buildLoggerProvider(ctx context.Context, cfg yamlConfig, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	var exporter sdklog.Exporter
	var err error

	switch strings.ToLower(cfg.Protocol) {
	case "http":
		opts := []otlploghttp.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlploghttp.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlploghttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlploghttp.WithHeaders(cfg.Headers))
		}
		exporter, err = otlploghttp.New(ctx, opts...)
	default:
		opts := []otlploggrpc.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlploggrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlploggrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlploggrpc.WithHeaders(cfg.Headers))
		}
		exporter, err = otlploggrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, err
	}

	return sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	), nil
}

type levelFilterHandler struct {
	level slog.Level
	inner slog.Handler
}

func (h levelFilterHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level && h.inner.Enabled(ctx, level)
}

func (h levelFilterHandler) Handle(ctx context.Context, record slog.Record) error {
	return h.inner.Handle(ctx, record)
}

func (h levelFilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelFilterHandler{level: h.level, inner: h.inner.WithAttrs(attrs)}
}

func (h levelFilterHandler) WithGroup(name string) slog.Handler {
	return levelFilterHandler{level: h.level, inner: h.inner.WithGroup(name)}
}

func decodeConfig(node yaml.Node, subject string) (yamlConfig, error) {
	var cfg yamlConfig
	if node.Kind != 0 {
		if err := node.Decode(&cfg); err != nil {
			return yamlConfig{}, fmt.Errorf("%s: parsing config: %w", subject, err)
		}
	}
	return cfg, nil
}

var Factory bootstrap.TelemetryFactory = func(node yaml.Node) (core.TelemetryProvider, error) {
	cfg, err := decodeConfig(node, "otlp telemetry")
	if err != nil {
		return nil, err
	}
	return New(context.Background(), cfg)
}

func NewAuditLogger(ctx context.Context, node yaml.Node) (*slog.Logger, func(context.Context) error, error) {
	cfg, err := decodeConfig(node, "otlp audit")
	if err != nil {
		return nil, nil, err
	}
	applyConfigDefaults(&cfg)
	if !strings.EqualFold(cfg.Logs.Exporter, "otlp") {
		return nil, nil, fmt.Errorf("otlp audit: logs.exporter must be %q", "otlp")
	}

	logger, lp, err := buildLogger(ctx, cfg, telemetryutil.BuildResource(cfg.ServiceName, cfg.ResourceAttributes))
	if err != nil {
		return nil, nil, err
	}

	var closeFn func(context.Context) error
	if lp != nil {
		closeFn = lp.Shutdown
	}

	return logger, closeFn, nil
}

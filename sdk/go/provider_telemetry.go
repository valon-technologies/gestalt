package gestalt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const providerTelemetryOTEL = "otel"

type providerTelemetryShutdown func(context.Context) error

func setupProviderTelemetryFromEnv(ctx context.Context) (providerTelemetryShutdown, error) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(proto.EnvProviderTelemetry)), providerTelemetryOTEL) {
		return func(context.Context) error { return nil }, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	res, err := providerTelemetryResource(ctx)
	if err != nil {
		return nil, err
	}

	prevTracerProvider := otel.GetTracerProvider()
	prevMeterProvider := otel.GetMeterProvider()
	var shutdowns []providerTelemetryShutdown

	if providerTelemetryExporterEnabled("TRACES") {
		exporter, err := providerTraceExporter(ctx)
		if err != nil {
			return nil, fmt.Errorf("provider telemetry: build trace exporter: %w", err)
		}
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(providerTraceSamplerFromEnv()),
			sdktrace.WithBatcher(exporter),
		)
		otel.SetTracerProvider(tp)
		shutdowns = append(shutdowns, func(ctx context.Context) error {
			return tp.Shutdown(ctx)
		})
	}

	if providerTelemetryExporterEnabled("METRICS") {
		exporter, err := providerMetricExporter(ctx)
		if err != nil {
			shutdownProviderTelemetry(ctx, shutdowns)
			otel.SetTracerProvider(prevTracerProvider)
			return nil, fmt.Errorf("provider telemetry: build metric exporter: %w", err)
		}
		readerOpts := []sdkmetric.PeriodicReaderOption{}
		if interval := providerMetricExportIntervalFromEnv(); interval > 0 {
			readerOpts = append(readerOpts, sdkmetric.WithInterval(interval))
		}
		mp := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, readerOpts...)),
		)
		otel.SetMeterProvider(mp)
		shutdowns = append(shutdowns, func(ctx context.Context) error {
			return mp.Shutdown(ctx)
		})
	}

	return func(ctx context.Context) error {
		err := shutdownProviderTelemetry(ctx, shutdowns)
		otel.SetTracerProvider(prevTracerProvider)
		otel.SetMeterProvider(prevMeterProvider)
		return err
	}, nil
}

func shutdownProviderTelemetry(ctx context.Context, shutdowns []providerTelemetryShutdown) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var errs []error
	for i := len(shutdowns) - 1; i >= 0; i-- {
		errs = append(errs, shutdowns[i](ctx))
	}
	return errors.Join(errs...)
}

func providerTelemetryResource(ctx context.Context) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{}
	if serviceName := cleanTelemetryString(os.Getenv("OTEL_SERVICE_NAME")); serviceName != "" {
		attrs = append(attrs, attribute.String("service.name", serviceName))
	}
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("provider telemetry: build resource: %w", err)
	}
	return res, nil
}

func providerTraceExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	switch providerOTLPProtocol("TRACES") {
	case "http/protobuf":
		return otlptracehttp.New(ctx)
	case "grpc":
		return otlptracegrpc.New(ctx)
	default:
		return nil, fmt.Errorf("unsupported OTLP trace protocol %q", providerOTLPProtocol("TRACES"))
	}
}

func providerMetricExporter(ctx context.Context) (sdkmetric.Exporter, error) {
	switch providerOTLPProtocol("METRICS") {
	case "http/protobuf":
		return otlpmetrichttp.New(ctx)
	case "grpc":
		return otlpmetricgrpc.New(ctx)
	default:
		return nil, fmt.Errorf("unsupported OTLP metric protocol %q", providerOTLPProtocol("METRICS"))
	}
}

func providerOTLPProtocol(signal string) string {
	signal = strings.ToUpper(strings.TrimSpace(signal))
	for _, key := range []string{"OTEL_EXPORTER_OTLP_" + signal + "_PROTOCOL", "OTEL_EXPORTER_OTLP_PROTOCOL"} {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
		case "http/protobuf":
			return "http/protobuf"
		case "grpc":
			return "grpc"
		}
	}
	return "grpc"
}

func providerTelemetryExporterEnabled(signal string) bool {
	raw := strings.TrimSpace(os.Getenv("OTEL_" + strings.ToUpper(signal) + "_EXPORTER"))
	if raw == "" {
		return true
	}
	for _, part := range strings.Split(raw, ",") {
		if strings.EqualFold(strings.TrimSpace(part), "otlp") {
			return true
		}
	}
	return false
}

func providerTraceSamplerFromEnv() sdktrace.Sampler {
	ratio := 1.0
	if raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); raw != "" {
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			switch {
			case value < 0:
				ratio = 0
			case value > 1:
				ratio = 1
			default:
				ratio = value
			}
		}
	}

	switch strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER"))) {
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(ratio)
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "always_on":
		return sdktrace.AlwaysSample()
	default:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}

func providerMetricExportIntervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("OTEL_METRIC_EXPORT_INTERVAL"))
	if raw == "" {
		return 0
	}
	milliseconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || milliseconds <= 0 {
		return 0
	}
	return time.Duration(milliseconds) * time.Millisecond
}

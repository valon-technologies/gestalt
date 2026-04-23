package metricutil

import (
	"context"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	AttrProvider          = attribute.Key("gestalt.provider")
	AttrOperation         = attribute.Key("gestalt.operation")
	AttrTransport         = attribute.Key("gestalt.transport")
	AttrConnectionMode    = attribute.Key("gestalt.connection_mode")
	AttrInvocationSurface = attribute.Key("gestalt.invocation_surface")
	AttrHTTPBinding       = attribute.Key("gestalt.http_binding")
	AttrUI                = attribute.Key("gestalt.ui")
	AttrRPCRole           = attribute.Key("gestalt.rpc.role")
	AttrHostService       = attribute.Key("gestalt.host_service")
	AttrHTTPRoute         = attribute.Key("http.route")
)

type TelemetryProviders interface {
	MeterProvider() metric.MeterProvider
	TracerProvider() trace.TracerProvider
}

func AddHTTPAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		return
	}
	if labeler, ok := otelhttp.LabelerFromContext(ctx); ok {
		missing := make([]attribute.KeyValue, 0, len(attrs))
		existing := make(map[attribute.Key]struct{}, len(labeler.Get()))
		for _, attr := range labeler.Get() {
			existing[attr.Key] = struct{}{}
		}
		for _, attr := range attrs {
			if _, ok := existing[attr.Key]; ok {
				continue
			}
			missing = append(missing, attr)
		}
		if len(missing) > 0 {
			labeler.Add(missing...)
		}
	}
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

func GRPCOptions(telemetry TelemetryProviders, attrs ...attribute.KeyValue) []otelgrpc.Option {
	opts := make([]otelgrpc.Option, 0, 4)
	if telemetry != nil {
		if mp := telemetry.MeterProvider(); mp != nil {
			opts = append(opts, otelgrpc.WithMeterProvider(mp))
		}
		if tp := telemetry.TracerProvider(); tp != nil {
			opts = append(opts, otelgrpc.WithTracerProvider(tp))
		}
	}
	if len(attrs) > 0 {
		opts = append(opts,
			otelgrpc.WithMetricAttributes(attrs...),
			otelgrpc.WithSpanAttributes(attrs...),
		)
	}
	return opts
}

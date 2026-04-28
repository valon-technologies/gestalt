package metricutil

import (
	"context"
	"strings"

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
	AttrResultStatus      = attribute.Key("gestalt.result_status")
	AttrResultStatusClass = attribute.Key("gestalt.result_status_class")
	AttrHTTPRoute         = attribute.Key("http.route")

	attrGestaltdProviderName       = attribute.Key("gestaltd.provider.name")
	attrGestaltdOperationName      = attribute.Key("gestaltd.operation.name")
	attrGestaltdOperationTransport = attribute.Key("gestaltd.operation.transport")
	attrGestaltdConnectionMode     = attribute.Key("gestaltd.connection.mode")
	attrGestaltdInvocationSurface  = attribute.Key("gestaltd.invocation.surface")
	attrGestaltdHTTPBindingName    = attribute.Key("gestaltd.http.binding.name")
	attrGestaltdUIName             = attribute.Key("gestaltd.ui.name")
	attrGestaltdRPCRole            = attribute.Key("gestaltd.rpc.role")
	attrGestaltdHostServiceName    = attribute.Key("gestaltd.host_service.name")
	attrGestaltdIndexedDBIndexName = attribute.Key("gestaltd.indexeddb.index.name")

	attrDBSystemName     = attribute.Key("db.system.name")
	attrDBNamespace      = attribute.Key("db.namespace")
	attrDBCollectionName = attribute.Key("db.collection.name")
	attrDBOperationName  = attribute.Key("db.operation.name")
	attrErrorType        = attribute.Key("error.type")
)

const (
	InvocationSurfaceHTTP        = "http"
	InvocationSurfaceHTTPBinding = "http_binding"
	InvocationSurfaceUI          = "ui"

	RPCRoleProviderClient     = "provider_client"
	RPCRoleProviderServer     = "provider_server"
	RPCRoleHostedPluginClient = "hosted_plugin_client"
	RPCRoleHostServiceServer  = "host_service_server"

	DBSystemNameGestaltdIndexedDB = "gestaltd.indexeddb"
)

type TelemetryProviders interface {
	MeterProvider() metric.MeterProvider
	TracerProvider() trace.TracerProvider
}

type HTTPMetricDims struct {
	ProviderName    string
	OperationName   string
	Transport       string
	ConnectionMode  string
	Surface         string
	HTTPBindingName string
	UIName          string
}

type RPCMetricDims struct {
	Role            string
	ProviderName    string
	HostServiceName string
}

type DBMetricDims struct {
	SystemName     string
	Namespace      string
	CollectionName string
	OperationName  string
	ProviderName   string
	IndexName      string
	ErrorType      string
}

func HTTPServerAttrs(dims HTTPMetricDims) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 7)
	attrs = appendStringAttr(attrs, attrGestaltdProviderName, dims.ProviderName)
	attrs = appendStringAttr(attrs, attrGestaltdOperationName, dims.OperationName)
	attrs = appendStringAttr(attrs, attrGestaltdOperationTransport, dims.Transport)
	attrs = appendStringAttr(attrs, attrGestaltdConnectionMode, dims.ConnectionMode)
	attrs = appendStringAttr(attrs, attrGestaltdInvocationSurface, dims.Surface)
	attrs = appendStringAttr(attrs, attrGestaltdHTTPBindingName, dims.HTTPBindingName)
	attrs = appendStringAttr(attrs, attrGestaltdUIName, dims.UIName)
	return attrs
}

func AddHTTPServerMetricDims(ctx context.Context, dims HTTPMetricDims) {
	AddHTTPAttributes(ctx, HTTPServerAttrs(dims)...)
}

func RPCAttrs(dims RPCMetricDims) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 3)
	attrs = appendStringAttr(attrs, attrGestaltdRPCRole, dims.Role)
	attrs = appendStringAttr(attrs, attrGestaltdProviderName, dims.ProviderName)
	attrs = appendStringAttr(attrs, attrGestaltdHostServiceName, dims.HostServiceName)
	return attrs
}

func GRPCMetricOptions(telemetry TelemetryProviders, dims RPCMetricDims) []otelgrpc.Option {
	attrs := RPCAttrs(dims)
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

func DBClientAttrs(dims DBMetricDims) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 7)
	attrs = appendStringAttr(attrs, attrDBSystemName, dims.SystemName)
	attrs = appendStringAttr(attrs, attrDBNamespace, dims.Namespace)
	attrs = appendStringAttr(attrs, attrDBCollectionName, dims.CollectionName)
	attrs = appendStringAttr(attrs, attrDBOperationName, dims.OperationName)
	attrs = appendStringAttr(attrs, attrGestaltdProviderName, dims.ProviderName)
	attrs = appendStringAttr(attrs, attrGestaltdIndexedDBIndexName, dims.IndexName)
	attrs = appendStringAttr(attrs, attrErrorType, dims.ErrorType)
	return attrs
}

func appendStringAttr(attrs []attribute.KeyValue, key attribute.Key, value string) []attribute.KeyValue {
	value = strings.TrimSpace(value)
	if value == "" {
		return attrs
	}
	return append(attrs, key.String(value))
}

func AddHTTPAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		return
	}
	if labeler, ok := otelhttp.LabelerFromContext(ctx); ok {
		addMissingHTTPAttributes(labeler, attrs...)
	}
	addSpanAttributes(ctx, attrs...)
}

func addMissingHTTPAttributes(labeler *otelhttp.Labeler, attrs ...attribute.KeyValue) {
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

func addSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		return
	}
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

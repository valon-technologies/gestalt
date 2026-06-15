package providergateway

import (
	"context"
	"time"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	attrProviderGatewayProviderID   = attribute.Key("gestaltd.provider_gateway.provider.id")
	attrProviderGatewayProviderKind = attribute.Key("gestaltd.provider_gateway.provider.kind")
	attrProviderGatewayServiceName  = attribute.Key("gestaltd.provider_gateway.service.name")
	attrProviderGatewayOperation    = attribute.Key("gestaltd.provider_gateway.operation.name")
	attrProviderGatewaySource        = attribute.Key("gestaltd.provider_gateway.source")
	attrProviderGatewayTransportPath = attribute.Key("gestaltd.provider_gateway.transport.path")

	providerGatewayOperationMetrics metricutil.MeterCache[providerGatewayMetrics]
)

type providerGatewayMetrics struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

func newProviderGatewayMetrics(meter metric.Meter) providerGatewayMetrics {
	return providerGatewayMetrics{
		count: metricutil.NewInt64Counter(
			meter,
			"gestaltd.provider_gateway.operation.count",
			"Counts provider gateway operations.",
		),
		errorCount: metricutil.NewInt64Counter(
			meter,
			"gestaltd.provider_gateway.operation.error_count",
			"Counts failed provider gateway operations.",
		),
		duration: metricutil.NewFloat64Histogram(
			meter,
			"gestaltd.provider_gateway.operation.duration",
			"Measures provider gateway operation duration.",
			"s",
		),
	}
}

func recordProviderGatewayOperation(ctx context.Context, startedAt time.Time, err error, req ProviderGatewayRequest, transportPath TransportPath) {
	if ctx == nil {
		ctx = context.Background()
	}
	metrics := providerGatewayOperationMetrics.Load(ctx, "gestaltd", newProviderGatewayMetrics)
	attrs := []attribute.KeyValue{
		attrProviderGatewayProviderID.String(metricutil.AttrValue(req.ProviderID)),
		attrProviderGatewayProviderKind.String(metricutil.AttrValue(string(req.ProviderKind))),
		attrProviderGatewayServiceName.String(metricutil.AttrValue(req.ServiceName)),
		attrProviderGatewayOperation.String(metricutil.AttrValue(req.Operation)),
		attrProviderGatewaySource.String(metricutil.AttrValue(string(req.Source))),
		attrProviderGatewayTransportPath.String(metricutil.AttrValue(string(transportPath))),
	}
	metricAttrs := metric.WithAttributes(attrs...)

	metrics.count.Add(ctx, 1, metricAttrs)
	metrics.duration.Record(ctx, time.Since(startedAt).Seconds(), metricAttrs)
	if err != nil {
		metrics.errorCount.Add(ctx, 1, metricAttrs)
	}
}

package providergateway

import (
	"context"
	"strconv"
	"time"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	attrProviderGatewayProviderID              = attribute.Key("gd.provider_id")
	attrProviderGatewayProviderKind            = attribute.Key("gd.provider_kind")
	attrProviderGatewayServiceName             = attribute.Key("gd.service")
	attrProviderGatewayOperation               = attribute.Key("gd.operation")
	attrProviderGatewayTransportPath           = attribute.Key("gd.transport")
	attrProviderGatewayEntry                   = attribute.Key("gd.entry")
	attrProviderGatewayCallerPrincipalProvided = attribute.Key("gd.caller_token_provided")

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
		attrProviderGatewayTransportPath.String(metricutil.AttrValue(string(transportPath))),
		attrProviderGatewayEntry.String(string(invocation.EntryFromContext(ctx))),
		attrProviderGatewayCallerPrincipalProvided.String(strconv.FormatBool(principal.FromContext(ctx) != nil)),
	}
	metricAttrs := metric.WithAttributes(attrs...)
	metrics.count.Add(ctx, 1, metricAttrs)
	metrics.duration.Record(ctx, time.Since(startedAt).Seconds(), metricAttrs)
	if err != nil {
		metrics.errorCount.Add(ctx, 1, metricAttrs)
	}
}

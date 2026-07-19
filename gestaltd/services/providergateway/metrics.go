package providergateway

import (
	"context"
	"strconv"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
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
	attrProviderGatewayAuthorizationAllowed    = attribute.Key("gd.allowed")
	attrProviderGatewayAuthorizationSubject    = attribute.Key("gd.subject")
	attrProviderGatewayAuthorizationResource   = attribute.Key("gd.resource")
	attrProviderGatewayAuthorizationAction     = attribute.Key("gd.action")

	providerGatewayOperationMetrics    metricutil.MeterCache[providerGatewayMetrics]
	providerGatewayAuthorizationChecks metricutil.MeterCache[providerGatewayAuthorizationMetrics]
)

type providerGatewayMetrics struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

type providerGatewayAuthorizationMetrics struct {
	count metric.Int64Counter
}

func newProviderGatewayAuthorizationMetrics(meter metric.Meter) providerGatewayAuthorizationMetrics {
	return providerGatewayAuthorizationMetrics{
		count: metricutil.NewInt64Counter(
			meter,
			"gestaltd.provider_gateway.authorization.count",
			"Counts provider gateway authorization checks.",
		),
	}
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

func recordProviderGatewayAuthorizationCheck(ctx context.Context, allowed bool, callerPrincipalProvided bool, req *proto.CheckAccessRequest) {
	if ctx == nil {
		ctx = context.Background()
	}
	metrics := providerGatewayAuthorizationChecks.Load(ctx, "gestaltd", newProviderGatewayAuthorizationMetrics)
	attrs := []attribute.KeyValue{
		attrProviderGatewayEntry.String(string(invocation.EntryFromContext(ctx))),
		attrProviderGatewayCallerPrincipalProvided.String(strconv.FormatBool(callerPrincipalProvided)),
		attrProviderGatewayAuthorizationAllowed.String(strconv.FormatBool(allowed)),
		attrProviderGatewayAuthorizationSubject.String(metricutil.AttrValue(authorizationCheckSubjectValue(req))),
		attrProviderGatewayAuthorizationResource.String(metricutil.AttrValue(authorizationCheckResourceValue(req))),
		attrProviderGatewayAuthorizationAction.String(metricutil.AttrValue(authorizationCheckActionValue(req))),
	}
	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func authorizationCheckSubjectValue(req *proto.CheckAccessRequest) string {
	if req == nil || req.GetSubject() == nil {
		return ""
	}
	subject := req.GetSubject()
	return strings.TrimSpace(subject.GetType()) + "/" + strings.TrimSpace(subject.GetId())
}

func authorizationCheckResourceValue(req *proto.CheckAccessRequest) string {
	if req == nil || req.GetResource() == nil {
		return ""
	}
	resource := req.GetResource()
	return strings.TrimSpace(resource.GetType()) + "/" + strings.TrimSpace(resource.GetId())
}

func authorizationCheckActionValue(req *proto.CheckAccessRequest) string {
	if req == nil || req.GetAction() == nil {
		return ""
	}
	return strings.TrimSpace(req.GetAction().GetName())
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

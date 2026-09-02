package invocation

import (
	"context"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Invoke authorization gate entrypoints (P2 operation-invoke audit, step 1).
//
// Every in-scope operation dispatch must reach Broker.authorizeOperation or
// Broker.checkAuthorizationAccess before provider execution. Remote-delegated
// providers skip the local gate because the upstream app owns authorization.
//
// | Surface        | Dispatch entrypoint                                      | Gate path                          |
// | -------------- | -------------------------------------------------------- | ---------------------------------- |
// | v1 HTTP UI/CLI | server.Server.handleOperationInvocation (handlers.go)    | invoker.Invoke → authorizeOperation |
// | HTTP bindings  | server.Server.httpBindingInvocation (http_binding_dispatch.go) | invoker.Invoke / InvokeMaybeStream → authorizeOperation |
// | Cross-app      | appaccess.AppServer.Invoke (app_server.go, gRPC)         | invoker.Invoke → authorizeOperation |
// | Workflow       | appaccess.AppServer.Invoke with workflow caller context  | invoker.Invoke → authorizeOperation |
// | GraphQL        | Broker.InvokeGraphQL (broker.go)                         | checkAuthorizationAccess           |
//
// Out of scope for this gate: MCP invokes (InvocationSurfaceMCP, P5 follow-up),
// remote-delegated providers, and provider-handler checkAccessRaw checks.
const (
	attrInvokeAuthorizationDecision   = attribute.Key("gestaltd.invoke.authorization.decision")
	attrInvokeAuthorizationDenyReason = attribute.Key("gestaltd.invoke.authorization.deny_reason")
	attrGestaltdInvocationSurface     = attribute.Key("gestaltd.invocation.surface")
	attrInvokeSubjectKind             = attribute.Key("gestaltd.subject.kind")
	attrInvokeSubjectID               = attribute.Key("gestaltd.subject.id")

	invokeAuthorizationDecisionAllow = "allow"
	invokeAuthorizationDecisionDeny  = "deny"

	invokeAuthorizationDenyReasonAuthorizationError = "authorization_error"
	invokeAuthorizationDenyReasonRelationDenied     = "relation_denied"
	invokeAuthorizationDenyReasonRoleDenied         = "role_denied"
)

type invokeAuthorizationMetrics struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

var invokeAuthorizationMetricsCache metricutil.MeterCache[invokeAuthorizationMetrics]

func recordInvokeAuthorizationDecision(
	ctx context.Context,
	startedAt time.Time,
	p *principal.Principal,
	providerName string,
	operationID string,
	allowed bool,
	denyReason string,
) {
	if ctx == nil {
		ctx = context.Background()
	}
	metrics := invokeAuthorizationMetricsCache.Load(ctx, tracerName, newInvokeAuthorizationMetrics)
	attrs := invokeAuthorizationMetricAttrs(ctx, p, providerName, operationID, allowed, denyReason)
	opts := metric.WithAttributes(attrs...)
	metrics.count.Add(ctx, 1, opts)
	metrics.duration.Record(ctx, time.Since(startedAt).Seconds(), opts)
	if !allowed {
		metrics.errorCount.Add(ctx, 1, opts)
	}
}

func newInvokeAuthorizationMetrics(meter metric.Meter) invokeAuthorizationMetrics {
	return invokeAuthorizationMetrics{
		count: metricutil.NewInt64Counter(
			meter,
			"gestaltd.invoke.authorization.count",
			"Counts gestaltd operation invoke authorization decisions.",
		),
		errorCount: metricutil.NewInt64Counter(
			meter,
			"gestaltd.invoke.authorization.error_count",
			"Counts denied gestaltd operation invoke authorization decisions.",
		),
		duration: metricutil.NewFloat64Histogram(
			meter,
			"gestaltd.invoke.authorization.duration",
			"Measures gestaltd operation invoke authorization decision duration.",
			"s",
		),
	}
}

func invokeAuthorizationMetricAttrs(
	ctx context.Context,
	p *principal.Principal,
	providerName string,
	operationID string,
	allowed bool,
	denyReason string,
) []attribute.KeyValue {
	decision := invokeAuthorizationDecisionAllow
	if !allowed {
		decision = invokeAuthorizationDecisionDeny
	}
	attrs := []attribute.KeyValue{
		metricutil.AttrProvider.String(metricutil.AttrValue(providerName)),
		metricutil.AttrOperation.String(metricutil.AttrValue(operationID)),
		attrGestaltdInvocationSurface.String(metricutil.AttrValue(invokeAuthorizationSurface(ctx))),
		attrInvokeAuthorizationDecision.String(metricutil.AttrValue(decision)),
	}
	if !allowed {
		attrs = append(attrs, attrInvokeAuthorizationDenyReason.String(metricutil.AttrValue(denyReason)))
	}
	subjectKind, subjectID := invokeAuthorizationSubject(p)
	attrs = append(attrs,
		attrInvokeSubjectKind.String(metricutil.AttrValue(subjectKind)),
		attrInvokeSubjectID.String(metricutil.AttrValue(subjectID)),
	)
	return attrs
}

func invokeAuthorizationSurface(ctx context.Context) string {
	if surface := InvocationSurfaceFromContext(ctx); surface != "" {
		return string(surface)
	}
	caller := CallerProviderFromContext(ctx)
	switch caller.Kind {
	case ProviderKindWorkflow:
		return string(InvocationSurfaceWorkflow)
	case ProviderKindApp:
		if EntryFromContext(ctx) == EntryGRPC {
			return string(InvocationSurfaceCrossApp)
		}
	}
	if EntryFromContext(ctx) == EntryHTTP {
		return string(InvocationSurfaceHTTP)
	}
	return metricutil.UnknownAttrValue
}

func invokeAuthorizationSubject(p *principal.Principal) (kind string, id string) {
	p = principal.Canonicalized(p)
	if p == nil {
		return metricutil.UnknownAttrValue, metricutil.UnknownAttrValue
	}
	if subjectKind, subjectID, ok := core.ParseSubjectID(p.SubjectID); ok {
		switch subjectKind {
		case string(principal.KindUser):
			if p.Identity != nil {
				if email := strings.ToLower(strings.TrimSpace(p.Identity.Email)); email != "" {
					return subjectKind, email
				}
			}
			return subjectKind, metricutil.UnknownAttrValue
		case "service_account", "system":
			if subjectID != "" {
				return subjectKind, subjectID
			}
		default:
			if subjectID != "" {
				return subjectKind, subjectID
			}
		}
	}
	if p.Kind == principal.KindUser && p.Identity != nil {
		if email := strings.ToLower(strings.TrimSpace(p.Identity.Email)); email != "" {
			return string(principal.KindUser), email
		}
	}
	return metricutil.UnknownAttrValue, metricutil.UnknownAttrValue
}

package invocation

import (
	"context"
	"fmt"
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/attribute"
)

const (
	invokeAuthorizationDecisionAllow = "allow"
	invokeAuthorizationDecisionDeny  = "deny"

	invokeAuthorizationDenyReasonAuthorizationError = "authorization_error"
	invokeAuthorizationDenyReasonRelationDenied     = "relation_denied"
	invokeAuthorizationDenyReasonRoleDenied         = "role_denied"
)

func (b *Broker) checkAuthorizationAccess(ctx context.Context, p *principal.Principal, providerName, operationID string) error {
	_, err := b.evaluateInvokeAuthorization(ctx, p, providerName, operationID, nil)
	return err
}

func (b *Broker) authorizeOperation(
	ctx context.Context,
	p *principal.Principal,
	providerName string,
	operation catalog.CatalogOperation,
) (context.Context, error) {
	role, err := b.evaluateInvokeAuthorization(ctx, p, providerName, operation.ID, operation.AllowedRoles)
	if err != nil {
		return ctx, err
	}
	if role == "" {
		return ctx, nil
	}
	return WithAccessContext(ctx, AccessContext{
		Policy: b.authorizationPolicy(providerName),
		Role:   role,
	}), nil
}

func (b *Broker) evaluateInvokeAuthorization(
	ctx context.Context,
	p *principal.Principal,
	providerName, operationID string,
	allowedRoles []string,
) (role string, err error) {
	if b == nil || b.authorization == nil {
		return "", nil
	}

	startedAt := time.Now()
	allowed := false
	denyReason := ""
	defer func() {
		observability.RecordInvokeAuthorization(
			ctx,
			startedAt,
			!allowed,
			invokeAuthorizationMetricAttrs(ctx, p, providerName, operationID, allowed, denyReason)...,
		)
	}()

	decision, err := b.authorizationDecision(ctx, p, providerName, operationID)
	if err != nil {
		denyReason = invokeAuthorizationDenyReasonAuthorizationError
		return "", fmt.Errorf("%w: %s.%s: %v", ErrAuthorizationDenied, providerName, operationID, err)
	}
	if decision == nil || !decision.GetAllowed() {
		denyReason = invokeAuthorizationDenyReasonRelationDenied
		return "", fmt.Errorf("%w: %s.%s", ErrAuthorizationDenied, providerName, operationID)
	}
	if len(allowedRoles) > 0 {
		role = matchedAllowedRole(decision.GetMatchedRelations(), allowedRoles)
		if role == "" {
			denyReason = invokeAuthorizationDenyReasonRoleDenied
			return "", fmt.Errorf("%w: %s.%s", ErrAuthorizationDenied, providerName, operationID)
		}
	}
	allowed = true
	return role, nil
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
		observability.AttrInvocationSurface.String(metricutil.AttrValue(authorizationSurface(ctx))),
		observability.AttrInvokeAuthorizationDecision.String(metricutil.AttrValue(decision)),
	}
	if !allowed {
		attrs = append(attrs, observability.AttrInvokeAuthorizationDenyReason.String(metricutil.AttrValue(denyReason)))
	}
	subjectKind, subjectID := principal.MetricAuthorizationSubject(p)
	attrs = append(attrs,
		observability.AttrSubjectKind.String(metricutil.AttrValue(subjectKind)),
		observability.AttrSubjectID.String(metricutil.AttrValue(subjectID)),
	)
	return attrs
}

func authorizationSurface(ctx context.Context) string {
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

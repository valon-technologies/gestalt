package providergateway

import (
	"context"
	"log/slog"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

const (
	authorizationResourceType = string(ProviderKindAuthorization)
	authorizationResourceID   = "authorization"
	legacyAuthorizationAction = "authorization"
)

func authorizationMethodAccessClass(fullMethod string) (read bool, write bool, ok bool) {
	_, method := splitFullMethod(fullMethod)
	switch method {
	case "GetActiveModelRef",
		"ListActiveModelResourceTypes",
		"ListRelationships",
		"CheckAccess",
		"CheckAccessMany":
		return true, false, true
	case "SetActiveModel",
		"SetAuthorizationState",
		"AddRelationship",
		"DeleteRelationship",
		"WriteRelationships":
		return false, true, true
	default:
		return false, false, false
	}
}

func (t *ProviderGatewayTransport) enforceAuthorizationPublicAccess(
	ctx context.Context,
	subjectID, fullMethod string,
	req gproto.Message,
) (context.Context, error) {
	read, write, ok := authorizationMethodAccessClass(fullMethod)
	if !ok {
		return ctx, status.Error(codes.Internal, "provider gateway: unsupported authorization method")
	}

	resource := &proto.Resource{Type: authorizationResourceType, Id: authorizationResourceID}
	actions := authorizationPublicActions(read, write)
	for _, action := range actions {
		checkReq := invocation.SubjectAccessRequest(subjectID, action, resource)
		allowed, err := invocation.CheckSubjectAccess(ctx, t.authorization, checkReq)
		if err != nil {
			return ctx, status.Error(codes.Unavailable, "authorization provider unavailable")
		}
		if allowed {
			return ctx, nil
		}
	}
	if write {
		if tuple, ok := relationshipTupleFromAuthorizationRequest(fullMethod, req); ok {
			allowed, err := allowsAppScopedRelationshipMutation(ctx, t.authorization, subjectID, tuple)
			if err != nil {
				return ctx, status.Error(codes.Unavailable, "authorization provider unavailable")
			}
			if allowed {
				appID := strings.TrimSpace(tuple.GetResource().GetId())
				action := appScopedRelationshipActionFromMethod(fullMethod)
				return WithAppScopedRelationshipMutationAuth(ctx, appID, action), nil
			}
			if deniedErr := recordAppScopedRelationshipAuthFailure(ctx, tuple, fullMethod, status.Error(codes.PermissionDenied, "access denied")); deniedErr != nil {
				return ctx, deniedErr
			}
		}
	}
	return ctx, status.Error(codes.PermissionDenied, "access denied")
}

func authorizationPublicActions(read, write bool) []string {
	switch {
	case write:
		return []string{"admin", legacyAuthorizationAction}
	case read:
		return []string{"viewer", "admin", legacyAuthorizationAction}
	default:
		return nil
	}
}

func isAuthorizationServiceMethod(fullMethod string) bool {
	service, _ := splitFullMethod(fullMethod)
	return service == proto.Authorization_ServiceDesc.ServiceName
}

const (
	appAdminUISurfaceMembers = "members"

	appAdminUIOutcomeFailure = "failure"

	appAdminUIFailureAuth = "auth_failure"
)

func recordAppScopedRelationshipAuthFailure(ctx context.Context, tuple *proto.RelationshipTuple, fullMethod string, err error) error {
	if tuple == nil || strings.TrimSpace(tuple.GetResource().GetType()) != appAuthorizationResourceType {
		return err
	}
	appID := strings.TrimSpace(tuple.GetResource().GetId())
	action := appScopedRelationshipActionFromMethod(fullMethod)
	if appID == "" || action == "" {
		return err
	}
	startedAt := time.Now()
	attrs := []attribute.KeyValue{
		observability.AttrAppAdminUIApp.String(appID),
		observability.AttrAppAdminUISurface.String(appAdminUISurfaceMembers),
		observability.AttrAppAdminUIAction.String(action),
		observability.AttrAppAdminUIOutcome.String(appAdminUIOutcomeFailure),
		observability.AttrAppAdminUIFailureCategory.String(appAdminUIFailureAuth),
	}
	observability.RecordAppAdminUI(ctx, startedAt, true, attrs...)
	logAppScopedRelationshipFailure(ctx, appID, action, appAdminUIFailureAuth, err)
	return err
}

func logAppScopedRelationshipFailure(ctx context.Context, appID, action, failureCategory string, err error) {
	attrs := []any{
		slog.String("event", "app_admin.ui"),
		slog.String("app", appID),
		slog.String("surface", appAdminUISurfaceMembers),
		slog.String("action", action),
		slog.String("failure_category", failureCategory),
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	if meta := invocation.MetaFromContext(ctx); meta != nil {
		if requestID := strings.TrimSpace(meta.RequestID); requestID != "" {
			attrs = append(attrs, slog.String("request_id", requestID))
		}
	}
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		attrs = append(attrs, slog.String("trace_id", spanCtx.TraceID().String()))
	}
	slog.WarnContext(ctx, "app admin ui interaction failed", attrs...)
}

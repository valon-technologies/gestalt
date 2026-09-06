package providergateway

import (
	"context"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

const (
	authorizationResourceType = string(ProviderKindAuthorization)
	authorizationResourceID   = "authorization"
	legacyAuthorizationAction = "authorization"
)

type authorizationEnforcementPolicy int

const (
	authorizationPolicyUnsupported authorizationEnforcementPolicy = iota
	authorizationPolicyGlobalRead
	authorizationPolicyGlobalAdminWrite
	authorizationPolicyRelationshipWrite
)

func authorizationMethodEnforcementPolicy(fullMethod string) (authorizationEnforcementPolicy, bool) {
	_, method := splitFullMethod(fullMethod)
	switch method {
	case "GetActiveModelRef",
		"ListActiveModelResourceTypes",
		"ListRelationships",
		"CheckAccess",
		"CheckAccessMany":
		return authorizationPolicyGlobalRead, true
	case "SetActiveModel", "SetAuthorizationState":
		return authorizationPolicyGlobalAdminWrite, true
	case "AddRelationship", "DeleteRelationship", "WriteRelationships":
		return authorizationPolicyRelationshipWrite, true
	default:
		return authorizationPolicyUnsupported, false
	}
}

func authorizationMethodAccessClass(fullMethod string) (read bool, write bool, ok bool) {
	policy, ok := authorizationMethodEnforcementPolicy(fullMethod)
	if !ok {
		return false, false, false
	}
	switch policy {
	case authorizationPolicyGlobalRead:
		return true, false, true
	case authorizationPolicyGlobalAdminWrite, authorizationPolicyRelationshipWrite:
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
	policy, ok := authorizationMethodEnforcementPolicy(fullMethod)
	if !ok {
		return ctx, status.Error(codes.Internal, "provider gateway: unsupported authorization method")
	}

	read := policy == authorizationPolicyGlobalRead
	write := policy == authorizationPolicyGlobalAdminWrite || policy == authorizationPolicyRelationshipWrite
	globalAdmin, err := t.hasAuthorizationPublicAdminAccess(ctx, subjectID, read, write)
	if err != nil {
		return ctx, status.Error(codes.Unavailable, "authorization provider unavailable")
	}

	switch policy {
	case authorizationPolicyGlobalRead, authorizationPolicyGlobalAdminWrite:
		if !globalAdmin {
			return ctx, status.Error(codes.PermissionDenied, "access denied")
		}
		return ctx, nil
	case authorizationPolicyRelationshipWrite:
		allowed, err := t.allowsRelationshipWriteAccess(ctx, subjectID, fullMethod, req, globalAdmin)
		if err != nil {
			return ctx, status.Error(codes.Unavailable, "authorization provider unavailable")
		}
		if !allowed {
			return ctx, status.Error(codes.PermissionDenied, "access denied")
		}
		ctx = withAppScopedRelationshipMutationAuthFromRequest(ctx, fullMethod, req)
		return ctx, nil
	default:
		return ctx, status.Error(codes.Internal, "provider gateway: unsupported authorization method")
	}
}

func (t *ProviderGatewayTransport) hasAuthorizationPublicAdminAccess(
	ctx context.Context,
	subjectID string,
	read, write bool,
) (bool, error) {
	resource := &proto.Resource{Type: authorizationResourceType, Id: authorizationResourceID}
	for _, action := range authorizationPublicActions(read, write) {
		checkReq := invocation.SubjectAccessRequest(subjectID, action, resource)
		accessAllowed, err := invocation.CheckSubjectAccess(ctx, t.authorization, checkReq)
		if err != nil {
			return false, err
		}
		if accessAllowed {
			return true, nil
		}
	}
	return false, nil
}

func (t *ProviderGatewayTransport) allowsRelationshipWriteAccess(
	ctx context.Context,
	subjectID, fullMethod string,
	req gproto.Message,
	globalAdmin bool,
) (bool, error) {
	tuples := relationshipTuplesFromAuthorizationRequest(fullMethod, req)
	if len(tuples) == 0 {
		return globalAdmin, nil
	}
	for _, tuple := range tuples {
		allowed, err := t.allowsRelationshipTupleWrite(ctx, subjectID, tuple, globalAdmin)
		if err != nil {
			return false, err
		}
		if !allowed {
			recordAppScopedRelationshipAuthFailure(ctx, subjectID, tuple, fullMethod, status.Error(codes.PermissionDenied, "access denied"))
			return false, nil
		}
	}
	return true, nil
}

func (t *ProviderGatewayTransport) allowsRelationshipTupleWrite(
	ctx context.Context,
	subjectID string,
	tuple *proto.RelationshipTuple,
	globalAdmin bool,
) (bool, error) {
	if isGroupMemberRelationshipTuple(tuple) {
		return allowsGroupScopedRelationshipMutation(ctx, t.authorization, subjectID, tuple)
	}
	if !isAppScopedRelationshipTuple(tuple) {
		return globalAdmin, nil
	}
	if globalAdmin && isAppBootstrapRelationshipTuple(tuple) {
		return true, nil
	}
	return allowsAppScopedRelationshipMutation(ctx, t.authorization, subjectID, tuple)
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

func recordAppScopedRelationshipAuthFailure(ctx context.Context, subjectID string, tuple *proto.RelationshipTuple, fullMethod string, err error) {
	if tuple == nil || strings.TrimSpace(tuple.GetResource().GetType()) != appAuthorizationResourceType {
		return
	}
	appID := strings.TrimSpace(tuple.GetResource().GetId())
	action := appScopedRelationshipActionFromMethod(fullMethod)
	if appID == "" || action == "" {
		return
	}
	targetSubjectKind, targetSubjectID := relationshipTupleTargetSubjectMetric(tuple)
	principalKind, principalID := principal.MetricAuthorizationSubject(&principal.Principal{SubjectID: subjectID})
	observability.RecordAppAdminUIInteraction(ctx, time.Now(), observability.AppAdminUIInteraction{
		App:                  appID,
		Surface:              observability.AppAdminUISurfaceMembers,
		Action:               action,
		Failed:               true,
		FailureCategory:      observability.AppAdminUIFailureAuth,
		Err:                  err,
		PrincipalSubjectKind: principalKind,
		PrincipalSubjectID:   principalID,
		TargetSubjectKind:    targetSubjectKind,
		TargetSubjectID:      targetSubjectID,
	})
}

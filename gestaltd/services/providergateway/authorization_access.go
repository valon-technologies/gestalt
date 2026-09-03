package providergateway

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
) error {
	read, write, ok := authorizationMethodAccessClass(fullMethod)
	if !ok {
		return status.Error(codes.Internal, "provider gateway: unsupported authorization method")
	}

	resource := &proto.Resource{Type: authorizationResourceType, Id: authorizationResourceID}
	actions := authorizationPublicActions(read, write)
	for _, action := range actions {
		req := invocation.SubjectAccessRequest(subjectID, action, resource)
		allowed, err := invocation.CheckSubjectAccess(ctx, t.authorization, req)
		if err != nil {
			return status.Error(codes.Unavailable, "authorization provider unavailable")
		}
		if allowed {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "access denied")
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

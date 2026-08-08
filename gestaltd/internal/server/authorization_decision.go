package server

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// authorizationMapper returns the shared app key -> policy alias -> resource
// mapping. It is the same mapping the invocation broker uses, so an HTTP
// surface and an operation invocation always ask about the same resource.
func (s *Server) authorizationMapper() invocation.AuthorizationResourceMapper {
	if s == nil {
		return invocation.AuthorizationResourceMapper{}
	}
	return invocation.NewAuthorizationResourceMapper(s.providerKinds, s.authorizationPolicies)
}

// authorizationResource resolves the authorization resource for an app key or
// policy alias.
func (s *Server) authorizationResource(appKey string) *proto.Resource {
	return s.authorizationMapper().Resource(appKey)
}

// checkResourceAccess routes an HTTP-surface authorization question through the
// provider-owned evaluator. Server code never traverses relationships to reach
// a decision, so group and subject-set derived access is honored everywhere.
func (s *Server) checkResourceAccess(
	ctx context.Context,
	req invocation.ResourceAccessRequest,
) (invocation.ResourceAccessDecision, error) {
	if s == nil || s.authorization == nil {
		return invocation.ResourceAccessDecision{}, invocation.ErrAuthorizationUnavailable
	}
	return invocation.CheckResourceAccess(ctx, s.authorization, req)
}

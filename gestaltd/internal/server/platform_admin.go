package server

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/invocation"
)

const (
	defaultAdminAuthorizationResource = "gestalt"
	legacyAdminAuthorizationResource  = "gestaltAdmin"
)

func platformAdminResourceNames(primary string) []string {
	primary = strings.TrimSpace(primary)
	if primary == "" {
		primary = defaultAdminAuthorizationResource
	}
	if primary == defaultAdminAuthorizationResource {
		return []string{defaultAdminAuthorizationResource, legacyAdminAuthorizationResource}
	}
	return []string{primary}
}

func (s *Server) authorizeMountedResourceRolesWithLegacy(
	ctx context.Context,
	access mountedResourceAccess,
) (invocation.AccessContext, bool, error) {
	names := platformAdminResourceNames(access.resourceName)
	if len(names) == 1 {
		return s.authorizeMountedResourceRoles(ctx, access)
	}

	var last invocation.AccessContext
	for _, name := range names {
		candidate := access
		candidate.resourceName = name
		decision, allowed, err := s.authorizeMountedResourceRoles(ctx, candidate)
		if err != nil {
			return invocation.AccessContext{}, false, err
		}
		if allowed {
			return decision, true, nil
		}
		last = decision
	}
	return last, false, nil
}

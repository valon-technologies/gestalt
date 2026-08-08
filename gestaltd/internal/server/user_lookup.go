package server

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// defaultUserLookupAuthorizationResource names the dedicated authorization
// resource that gates user lookup. It is deliberately not any app's resource:
// administering an app must not, on its own, let the administrator enumerate
// the people in the directory.
const defaultUserLookupAuthorizationResource = "gestaltUserLookup"

// userLookupLegacyDecisionLogMessage marks a user-lookup decision that fell
// back to the direct-grant path because the active model declares no matching
// or wildcard action. Its absence in production is the evidence the shim can
// be deleted.
const userLookupLegacyDecisionLogMessage = "auth: user lookup resource type declares no matching action; using legacy direct-grant path"

// defaultUserLookupOperatorRole is the explicit employee operator relation that
// permits user lookup. It is a distinct relation from "admin" so an app-scoped
// admin grant can never satisfy it.
const defaultUserLookupOperatorRole = "operator"

// UserLookupRouteConfig gates user lookup - resolving another person's identity
// (their email) from a subject ID - on an explicit employee operator role.
//
// AuthorizationPolicy empty means the gate is inactive, which is the case when
// auth or the authorization provider is not configured; behavior then matches
// what it was before the gate existed. Otherwise the caller must hold one of
// AllowedRoles on the policy resource.
type UserLookupRouteConfig struct {
	AuthorizationPolicy string
	AllowedRoles        []string
}

func normalizeUserLookupRouteConfig(cfg UserLookupRouteConfig, defaultAuthorizationResource string) (UserLookupRouteConfig, error) {
	cfg.AuthorizationPolicy = strings.TrimSpace(cfg.AuthorizationPolicy)
	if cfg.AuthorizationPolicy == "" {
		cfg.AuthorizationPolicy = strings.TrimSpace(defaultAuthorizationResource)
	}
	if cfg.AuthorizationPolicy == "" {
		if len(cfg.AllowedRoles) > 0 {
			return UserLookupRouteConfig{}, fmt.Errorf("user lookup allowedRoles requires authorizationPolicy")
		}
		cfg.AllowedRoles = nil
		return cfg, nil
	}
	if len(cfg.AllowedRoles) == 0 {
		cfg.AllowedRoles = []string{defaultUserLookupOperatorRole}
		return cfg, nil
	}
	roles, err := packageio.NormalizeUIAllowedRoles("user lookup allowedRoles", cfg.AllowedRoles)
	if err != nil {
		return UserLookupRouteConfig{}, err
	}
	cfg.AllowedRoles = roles
	return cfg, nil
}

// userLookupAllowed reports whether the caller holds the employee operator role
// that permits resolving other people's identities.
//
// The decision goes through the shared evaluator like every other server-side
// decision, so the role may be held directly or through a group. Any evaluator
// error denies: user enumeration fails closed. Callers resolve this once per
// request and pass the answer down, so gating a roster costs one decision, not
// one per row.
func (s *Server) userLookupAllowed(ctx context.Context) bool {
	if s == nil {
		return false
	}
	policy := strings.TrimSpace(s.userLookupRoute.AuthorizationPolicy)
	if policy == "" {
		// The gate is not configured (no auth / no authorization provider), so
		// lookup behaves as it did before the gate existed.
		return true
	}
	p := PrincipalFromContext(ctx)
	if p == nil || principal.IsNonUserPrincipal(p) {
		return false
	}
	subjectID, err := principal.ResolveAuthorizationSubjectID(ctx, s.credentialUserResolver(), p)
	if err != nil {
		return false
	}
	if subjectID = strings.TrimSpace(subjectID); subjectID == "" {
		return false
	}
	decision, err := s.checkResourceAccess(ctx, invocation.ResourceAccessRequest{
		SubjectID:    subjectID,
		Action:       policy,
		Resource:     s.authorizationResource(policy),
		AllowedRoles: s.userLookupRoute.AllowedRoles,
	})
	if err != nil {
		return false
	}
	if decision.Allowed {
		return true
	}
	return s.userLookupAllowedByDirectGrant(ctx, subjectID, policy)
}

// userLookupAllowedByDirectGrant is the user-lookup half of the TRANSITION
// SHIM documented on legacyDirectGrantMountedAccess. The user-lookup resource
// type is policy-shaped like gestaltAdmin: it declares the operator relation
// but typically no actions. CheckAccessRequest is action-shaped, so an
// evaluator asked about an undeclared action answers "denied" to a question it
// cannot represent, and an operator grant would never restore lookup. Delete
// this with the mounted-UI shim once T1 confirms the user-lookup resource type
// declares a matching or wildcard action.
//
// Only a successfully read model that proves the evaluator cannot answer
// reaches the direct-grant scan; a model or provider error denies.
func (s *Server) userLookupAllowedByDirectGrant(ctx context.Context, subjectID, policy string) bool {
	model, err := s.mountedUIResourceModel(ctx, policy)
	if err != nil || model.answersAction(policy) {
		return false
	}

	slog.WarnContext(ctx, userLookupLegacyDecisionLogMessage,
		"resourceType", model.typeName,
		"resource", policy,
		"action", policy,
		"modelKnowsResourceType", model.found,
	)

	roles, err := s.legacyDirectGrantRoles(ctx, subjectID, policy)
	if err != nil {
		return false
	}
	allowed := s.userLookupRoute.AllowedRoles
	if len(allowed) == 0 {
		return len(roles) > 0
	}
	for _, allowedRole := range allowed {
		if allowedRole = strings.TrimSpace(allowedRole); allowedRole != "" && slices.Contains(roles, allowedRole) {
			return true
		}
	}
	return false
}

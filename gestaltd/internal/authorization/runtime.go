package authorization

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

// RuntimeAuthorizer is the internal authorization interface used by gestaltd
// request paths. Authorization decisions are evaluated against canonical subject
// IDs, regardless of how the caller authenticated.
type RuntimeAuthorizer interface {
	Start(ctx context.Context) error
	Close() error

	ReloadAuthorizationState(ctx context.Context) error

	AllowProvider(ctx context.Context, p *principal.Principal, provider string) bool
	AllowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool

	ResolveAccess(ctx context.Context, p *principal.Principal, provider string) (AccessContext, bool)
	ResolvePolicyAccess(ctx context.Context, p *principal.Principal, policyName string) (AccessContext, bool)
	ResolveAdminAccess(ctx context.Context, p *principal.Principal, policyName string) (AccessContext, bool)
	AllowCatalogOperation(ctx context.Context, p *principal.Principal, provider string, op catalog.CatalogOperation) bool

	PolicyNameForProvider(provider string) string
	StaticRoleForPolicyIdentity(policyName, subjectID string) (AccessContext, bool)
	StaticRoleForProviderIdentity(provider, subjectID string) (AccessContext, bool)
	StaticMembersForPolicy(policyName string) ([]StaticSubjectMember, bool)
	StaticMembersForProvider(provider string) (string, []StaticSubjectMember, bool)
}

// ProviderActionAuthorizer optionally grants provider-scoped actions that are
// not operation invocations, such as provider-dev remote attach.
type ProviderActionAuthorizer interface {
	AllowProviderAction(ctx context.Context, p *principal.Principal, provider, action string) bool
}

// ManagedAuthorizationModelResolver exposes the authorization model managed by
// the current runtime authorizer when one exists.
type ManagedAuthorizationModelResolver interface {
	ManagedModelID(ctx context.Context) (string, error)
}

var _ RuntimeAuthorizer = (*Authorizer)(nil)

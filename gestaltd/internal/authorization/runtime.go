package authorization

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

// RuntimeAuthorizer is the internal authorization interface used by gestaltd
// request paths. It keeps identity-token execution binding concerns local while
// allowing user-context authorization decisions to come from a backing provider.
type RuntimeAuthorizer interface {
	principal.IdentityTokenResolver

	Start(ctx context.Context) error
	Close() error

	ReloadAuthorizationState(ctx context.Context) error

	AllowProvider(ctx context.Context, p *principal.Principal, provider string) bool
	AllowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool
	Binding(p *principal.Principal, provider string) (CredentialBinding, bool)

	ResolveAccess(ctx context.Context, p *principal.Principal, provider string) (AccessContext, bool)
	ResolvePolicyAccess(ctx context.Context, p *principal.Principal, policyName string) (AccessContext, bool)
	ResolveAdminAccess(ctx context.Context, p *principal.Principal, policyName string) (AccessContext, bool)
	AllowCatalogOperation(ctx context.Context, p *principal.Principal, provider string, op catalog.CatalogOperation) bool

	PolicyNameForProvider(provider string) string
	StaticRoleForPolicyIdentity(policyName, subjectID, userID, email string) (AccessContext, bool)
	StaticRoleForProviderIdentity(provider, subjectID, userID, email string) (AccessContext, bool)
	StaticMembersForPolicy(policyName string) ([]StaticHumanMember, bool)
	StaticMembersForProvider(provider string) (string, []StaticHumanMember, bool)
}

// ManagedAuthorizationModelResolver exposes the authorization model managed by
// the current runtime authorizer when one exists.
type ManagedAuthorizationModelResolver interface {
	ManagedModelID(ctx context.Context) (string, error)
}

var _ RuntimeAuthorizer = (*Authorizer)(nil)

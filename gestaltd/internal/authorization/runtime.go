package authorization

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

// RuntimeAuthorizer is the internal authorization interface used by gestaltd
// request paths. Workload tokens authenticate non-human subjects; authorization
// decisions are evaluated against the subject ID just like user subjects.
type RuntimeAuthorizer interface {
	principal.WorkloadTokenResolver

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

// ManagedAuthorizationModelResolver exposes the authorization model managed by
// the current runtime authorizer when one exists.
type ManagedAuthorizationModelResolver interface {
	ManagedModelID(ctx context.Context) (string, error)
}

var _ RuntimeAuthorizer = (*Authorizer)(nil)

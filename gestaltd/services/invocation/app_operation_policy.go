package invocation

import (
	"context"
	"fmt"
	"slices"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func (b *Broker) appOperationPolicy(ctx context.Context, app string) (core.AppOperationPolicy, error) {
	if b == nil || b.appOperationPolicies == nil {
		return nil, nil
	}
	policy, err := b.appOperationPolicies.GetAppOperationPolicy(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("%w: operation permissions for %s: %v", ErrAuthorizationUnavailable, app, err)
	}
	return policy, nil
}

// authorizeInvocation is shared by unary, streaming, and raw GraphQL calls.
// Remote providers still own their role evaluation, but local app-wide removal
// and caller capabilities are enforced before delegation.
func (b *Broker) authorizeInvocation(ctx context.Context, p *principal.Principal, prov core.Provider, app string, operation catalog.CatalogOperation) (context.Context, catalog.CatalogOperation, error) {
	if !principal.AllowsOperationPermission(p, app, operation.ID) {
		return ctx, operation, fmt.Errorf("%w: %s.%s", ErrScopeDenied, app, operation.ID)
	}
	if err := b.checkAppAccess(ctx, p, app, operation.ID); err != nil {
		return ctx, operation, err
	}
	policy, err := b.appOperationPolicy(ctx, app)
	if err != nil {
		return ctx, operation, err
	}
	roles, allowed := policy.Resolve(operation.ID, operation.AllowedRoles)
	if !allowed {
		return ctx, operation, fmt.Errorf("%w: %s.%s", ErrAuthorizationDenied, app, operation.ID)
	}
	operation.AllowedRoles = slices.Clone(roles)
	if providerDelegatesRemoteAuthorization(prov) {
		return ctx, operation, nil
	}
	ctx, err = b.authorizeOperation(ctx, p, app, operation)
	return ctx, operation, err
}

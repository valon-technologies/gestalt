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
	private := privateOperation(operation)
	if private {
		// Private operations require the shared evaluator for both the original
		// user and a verified provider caller. Remote providers normally evaluate
		// users themselves, so force the host-side user decision here as well.
		if b == nil || b.authorization == nil {
			return ctx, operation, ErrAuthorizationUnavailable
		}
		if !verifiedInternalCaller(ctx) {
			return ctx, operation, fmt.Errorf("%w: private operation requires a verified internal caller", ErrAuthorizationDenied)
		}
	}
	if !providerDelegatesRemoteAuthorization(prov) || private {
		// Remote providers evaluate their user grants themselves for public
		// operations. Private operations are the exception: the host must check
		// the original user as well as the verified internal caller.
		ctx, err = b.authorizeOperation(ctx, p, app, operation)
		if err != nil {
			return ctx, operation, err
		}
	}
	if private {
		if err := b.authorizeInternalCaller(ctx, app, operation); err != nil {
			return ctx, operation, err
		}
	}
	return ctx, operation, nil
}

func privateOperation(operation catalog.CatalogOperation) bool {
	return operation.API != nil && !*operation.API && operation.MCP != nil && !*operation.MCP
}

func verifiedInternalCaller(ctx context.Context) bool {
	caller := CallerProviderFromContext(ctx)
	if caller.Name == "" || isPublicIngress(ctx) {
		return false
	}
	switch caller.Kind {
	case ProviderKindApp, ProviderKindWorkflow, ProviderKindAgent:
		return true
	default:
		return false
	}
}

func (b *Broker) authorizeInternalCaller(ctx context.Context, app string, operation catalog.CatalogOperation) error {
	caller := CallerProviderFromContext(ctx)
	subjectID := string(caller.Kind) + ":" + caller.Name
	decision, err := CheckResourceAccess(ctx, b.authorization, ResourceAccessRequest{
		SubjectID:    subjectID,
		Action:       operation.ID,
		Resource:     b.authorizationResource(ctx, app),
		AllowedRoles: operation.AllowedRoles,
	})
	if err != nil {
		return fmt.Errorf("%w: %s.%s caller %s: %v", ErrAuthorizationDenied, app, operation.ID, subjectID, err)
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s.%s caller %s", ErrAuthorizationDenied, app, operation.ID, subjectID)
	}
	return nil
}

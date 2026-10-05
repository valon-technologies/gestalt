package invocation

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

// appAccessScope is what an app contributes to judging a user's access
// profile: its defaults, its static operations by id, and whether its real
// operation list only exists per session.
type appAccessScope struct {
	defaults       core.AppAccessDefaults
	operations     map[string]catalog.CatalogOperation
	sessionCatalog bool
}

func (b *Broker) appAccessScope(ctx context.Context, providerName string) appAccessScope {
	if b == nil || b.providers == nil {
		return appAccessScope{}
	}
	prov, err := b.providers.GetWithContext(ctx, providerName)
	if err != nil || prov == nil {
		return appAccessScope{}
	}
	scope := appAccessScope{defaults: core.AppAccessDefaultsFor(prov), sessionCatalog: core.SupportsSessionCatalog(prov)}
	if cat := prov.Catalog(); cat != nil {
		scope.operations = make(map[string]catalog.CatalogOperation, len(cat.Operations))
		for i := range cat.Operations {
			scope.operations[cat.Operations[i].ID] = cat.Operations[i]
		}
	}
	return scope
}

// unlistedSession reports whether an operation belongs to a session-catalog
// app but is absent from its static catalog, so its default cannot be proven
// without the caller's session catalog. The graphql capability is judged as
// default-on and is never unlisted.
func (s appAccessScope) unlistedSession(operationID string) bool {
	if !s.sessionCatalog || operationID == core.GraphQLCapabilityID {
		return false
	}
	_, listed := s.operations[operationID]
	return !listed
}

// allows is the one decision for whether a profile permits an operation.
// Caller metadata is used only when it describes the operation being asked
// about; otherwise the profile could be judged on a different operation.
func (s appAccessScope) allows(profile *core.AppAccessProfile, operationID string, metadata *catalog.CatalogOperation) bool {
	if metadata != nil && strings.TrimSpace(metadata.ID) == operationID {
		return profile.Allows(*metadata, s.defaults)
	}
	if operation, listed := s.operations[operationID]; listed {
		return profile.Allows(operation, s.defaults)
	}
	if s.unlistedSession(operationID) {
		return profile.AllowsUnlisted(operationID)
	}
	return profile.Allows(catalog.CatalogOperation{ID: operationID}, s.defaults)
}

// checkUnlistedSessionOperation denies an operation of a session-catalog app
// that the static catalog does not list unless the profile explicitly allows it.
func (b *Broker) checkUnlistedSessionOperation(ctx context.Context, p *principal.Principal, providerName, operationID string, scope appAccessScope) error {
	if !scope.unlistedSession(operationID) {
		return nil
	}
	profile, err := b.appAccessProfile(ctx, p, providerName)
	if err != nil {
		return fmt.Errorf("%w: %s.%s: %v", ErrAuthorizationDenied, providerName, operationID, err)
	}
	if !scope.allows(profile, operationID, nil) {
		return fmt.Errorf("%w: %s.%s", ErrAuthorizationDenied, providerName, operationID)
	}
	return nil
}

package invocation

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func (b *Broker) providerSupportsSessionCatalog(ctx context.Context, providerName string) bool {
	if b == nil || b.providers == nil {
		return false
	}
	prov, err := b.providers.GetWithContext(ctx, providerName)
	if err != nil || prov == nil {
		return false
	}
	return core.SupportsSessionCatalog(prov)
}

// sessionOperationUnprovable reports whether an operation of a session-catalog
// app is absent from the static catalog, so its default-on status cannot be
// proven without the caller's session catalog.
func sessionOperationUnprovable(sessionCatalog bool, static map[string]catalog.CatalogOperation, operationID string) bool {
	if !sessionCatalog || operationID == core.GraphQLCapabilityID {
		return false
	}
	_, listed := static[operationID]
	return !listed
}

// profileAllowsUnprovableOperation allows an operation of unknown default only
// when the user explicitly switched it on.
func profileAllowsUnprovableOperation(profile *core.AppAccessProfile, operationID string) bool {
	if profile == nil {
		return true
	}
	id := strings.TrimSpace(operationID)
	if profile.Legacy {
		return slices.Contains(profile.LegacyEnabledOperations, id)
	}
	return slices.Contains(profile.ExtraOperations, id)
}

// checkUnlistedSessionOperation denies an operation of a session-catalog app
// that the static catalog does not list unless the profile explicitly allows it.
func (b *Broker) checkUnlistedSessionOperation(ctx context.Context, p *principal.Principal, prov core.Provider, providerName, operationID string) error {
	if prov == nil || !core.SupportsSessionCatalog(prov) || operationID == core.GraphQLCapabilityID {
		return nil
	}
	if _, listed := catalog.OperationByID(prov.Catalog(), operationID); listed {
		return nil
	}
	profile, err := b.appAccessProfile(ctx, p, providerName)
	if err != nil {
		return fmt.Errorf("%w: %s.%s: %v", ErrAuthorizationDenied, providerName, operationID, err)
	}
	if profileAllowsUnprovableOperation(profile, operationID) {
		return nil
	}
	return fmt.Errorf("%w: %s.%s", ErrAuthorizationDenied, providerName, operationID)
}

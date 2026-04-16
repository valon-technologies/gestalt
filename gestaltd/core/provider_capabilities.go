package core

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func SupportsManualAuth(prov Provider) bool {
	mp, ok := prov.(ManualProvider)
	return ok && mp.SupportsManualAuth()
}

func SupportsOAuth(prov Provider) bool {
	_, ok := prov.(OAuthProvider)
	return ok
}

func SupportsSessionCatalog(prov Provider) bool {
	_, ok := prov.(SessionCatalogProvider)
	return ok
}

func CatalogForRequest(ctx context.Context, prov Provider, token string) (*catalog.Catalog, bool, error) {
	scp, ok := prov.(SessionCatalogProvider)
	if !ok {
		return nil, false, nil
	}
	cat, err := scp.CatalogForRequest(ctx, token)
	return cat, true, err
}

func OperationConnection(prov Provider, operation string) string {
	ocp, ok := prov.(OperationConnectionProvider)
	if !ok {
		return ""
	}
	return ocp.ConnectionForOperation(operation)
}

func ConnectionParamDefs(prov Provider) map[string]ConnectionParamDef {
	cpp, ok := prov.(ConnectionParamProvider)
	if !ok {
		return nil
	}
	return cpp.ConnectionParamDefs()
}

func HasConnectionParamDefs(prov Provider) bool {
	_, ok := prov.(ConnectionParamProvider)
	return ok
}

func CredentialFields(prov Provider) []CredentialFieldDef {
	cfp, ok := prov.(CredentialFieldsProvider)
	if !ok {
		return nil
	}
	return cfp.CredentialFields()
}

func AuthTypes(prov Provider) []string {
	atl, ok := prov.(AuthTypeLister)
	if !ok {
		return nil
	}
	return atl.AuthTypes()
}

func HasAuthTypes(prov Provider) bool {
	_, ok := prov.(AuthTypeLister)
	return ok
}

func DiscoveryConfigFor(prov Provider) *DiscoveryConfig {
	dcp, ok := prov.(DiscoveryConfigProvider)
	if !ok {
		return nil
	}
	return dcp.DiscoveryConfig()
}

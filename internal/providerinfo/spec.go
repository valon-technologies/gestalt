package providerinfo

import (
	"slices"

	"github.com/valon-technologies/gestalt/core"
)

func ResolveConnectionSpec(prov core.Provider) core.ConnectionSpec {
	if prov == nil {
		return core.ConnectionSpec{}
	}
	if csp, ok := prov.(core.ConnectionSpecProvider); ok {
		return csp.ConnectionSpec().Clone()
	}

	spec := core.ConnectionSpec{
		AuthTypes: authTypesForProvider(prov),
	}

	if cpp, ok := prov.(core.ConnectionParamProvider); ok {
		spec.ConnectionParams = cpp.ConnectionParamDefs()
	}
	if dcp, ok := prov.(core.DiscoveryConfigProvider); ok {
		spec.Discovery = dcp.DiscoveryConfig()
	}

	return spec.Clone()
}

func UserConnectionParams(spec core.ConnectionSpec) map[string]core.ConnectionParamDef {
	if len(spec.ConnectionParams) == 0 {
		return nil
	}
	out := make(map[string]core.ConnectionParamDef)
	for name, def := range spec.ConnectionParams {
		if def.From == "" {
			out[name] = def
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func authTypesForProvider(prov core.Provider) []string {
	if atl, ok := prov.(core.AuthTypeLister); ok {
		return slices.Clone(atl.AuthTypes())
	}

	hasOAuth := false
	if _, ok := prov.(core.OAuthProvider); ok {
		hasOAuth = true
	}

	hasManual := false
	if mp, ok := prov.(core.ManualProvider); ok {
		hasManual = mp.SupportsManualAuth()
	}

	switch {
	case hasOAuth && hasManual:
		return []string{"oauth", "manual"}
	case hasManual:
		return []string{"manual"}
	default:
		return []string{"oauth"}
	}
}

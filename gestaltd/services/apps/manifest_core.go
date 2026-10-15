package plugins

import (
	"maps"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/maputil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func ConnectionParamDefsFromManifest(defs map[string]providermanifestv1.ProviderConnectionParam) map[string]core.ConnectionParamDef {
	return maputil.ConvertNonEmpty(defs, func(value providermanifestv1.ProviderConnectionParam) core.ConnectionParamDef {
		return core.ConnectionParamDef(value)
	})
}

func CredentialFieldsFromManifest(fields []providermanifestv1.CredentialField) []core.CredentialFieldDef {
	if len(fields) == 0 {
		return nil
	}
	out := make([]core.CredentialFieldDef, len(fields))
	for i, field := range fields {
		out[i] = core.CredentialFieldDef(field)
	}
	return out
}

func DiscoveryConfigFromManifest(discovery *providermanifestv1.ProviderDiscovery) *core.DiscoveryConfig {
	if discovery == nil {
		return nil
	}
	return &core.DiscoveryConfig{
		URL:      discovery.URL,
		IDPath:   discovery.IDPath,
		NamePath: discovery.NamePath,
		Metadata: maps.Clone(discovery.Metadata),
	}
}

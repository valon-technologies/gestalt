package providerhost

import (
	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
)

func ConnectionParamDefsFromManifest(defs map[string]providermanifestv1.ProviderConnectionParam) map[string]core.ConnectionParamDef {
	return pluginservice.ConnectionParamDefsFromManifest(defs)
}

func CredentialFieldsFromManifest(fields []providermanifestv1.CredentialField) []core.CredentialFieldDef {
	return pluginservice.CredentialFieldsFromManifest(fields)
}

func DiscoveryConfigFromManifest(discovery *providermanifestv1.ProviderDiscovery) *core.DiscoveryConfig {
	return pluginservice.DiscoveryConfigFromManifest(discovery)
}

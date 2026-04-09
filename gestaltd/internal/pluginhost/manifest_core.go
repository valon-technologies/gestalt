package pluginhost

import (
	"maps"

	"github.com/valon-technologies/gestalt/server/core"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func ConnectionParamDefsFromManifest(defs map[string]pluginmanifestv1.ProviderConnectionParam) map[string]core.ConnectionParamDef {
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]core.ConnectionParamDef, len(defs))
	for name, def := range defs {
		out[name] = core.ConnectionParamDef{
			Required:    def.Required,
			Description: def.Description,
			From:        def.From,
		}
	}
	return out
}

func CredentialFieldsFromManifest(fields []pluginmanifestv1.CredentialField) []core.CredentialFieldDef {
	if len(fields) == 0 {
		return nil
	}
	out := make([]core.CredentialFieldDef, len(fields))
	for i, field := range fields {
		out[i] = core.CredentialFieldDef{
			Name:        field.Name,
			Label:       field.Label,
			Description: field.Description,
		}
	}
	return out
}

func DiscoveryConfigFromManifest(discovery *pluginmanifestv1.ProviderDiscovery) *core.DiscoveryConfig {
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

package plugins

import (
	"maps"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func ConnectionParamDefsFromManifest(defs map[string]providermanifestv1.ProviderConnectionParam) map[string]core.ConnectionParamDef {
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]core.ConnectionParamDef, len(defs))
	for name, def := range defs {
		out[name] = core.ConnectionParamDef{
			Required:    def.Required,
			Description: def.Description,
			Default:     def.Default,
			From:        def.From,
			Field:       def.Field,
		}
	}
	return out
}

func CredentialFieldsFromManifest(fields []providermanifestv1.CredentialField) []core.CredentialFieldDef {
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

func PostConnectConfigsFromManifestConnections(connections map[string]*providermanifestv1.ManifestConnectionDef) map[string]*core.PostConnectConfig {
	if len(connections) == 0 {
		return nil
	}
	out := make(map[string]*core.PostConnectConfig)
	for name, conn := range connections {
		if conn == nil || conn.PostConnect == nil {
			continue
		}
		out[name] = PostConnectConfigFromManifest(conn.PostConnect)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func PostConnectConfigFromManifest(postConnect *providermanifestv1.ProviderPostConnect) *core.PostConnectConfig {
	if postConnect == nil {
		return nil
	}
	cfg := &core.PostConnectConfig{
		Request: core.PostConnectRequestConfig{
			Method:  postConnect.Request.Method,
			URL:     postConnect.Request.URL,
			Headers: maps.Clone(postConnect.Request.Headers),
		},
		SourcePath: postConnect.SourcePath,
		Metadata:   maps.Clone(postConnect.Metadata),
	}
	if postConnect.Success != nil {
		cfg.Success = &core.PostConnectSuccessCheck{
			Path:   postConnect.Success.Path,
			Equals: postConnect.Success.Equals,
		}
	}
	if postConnect.ExternalIdentity != nil {
		cfg.ExternalIdentity = &core.PostConnectExternalIdentityConfig{
			Type: postConnect.ExternalIdentity.Type,
			ID:   postConnect.ExternalIdentity.ID,
		}
	}
	return cfg
}

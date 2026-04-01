package config

import "fmt"

// ValidateExecution rejects config fields that parse successfully but have no
// effect in gestaltd's resolved execution model.
func ValidateExecution(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	for name, intg := range cfg.Integrations {
		if err := validateExecutionPluginFields(name, intg.Plugin); err != nil {
			return err
		}
	}
	return nil
}

func validateExecutionPluginFields(name string, plugin *PluginDef) error {
	if plugin == nil {
		return nil
	}
	if plugin.Connection != "" {
		return fmt.Errorf("config validation: integration %q plugin.connection is not supported; use default_connection or surface-specific *_connection fields", name)
	}

	hasOpenAPI := effectivePluginSurfaceURL(plugin, SpecSurfaceOpenAPI) != ""
	hasGraphQL := effectivePluginSurfaceURL(plugin, SpecSurfaceGraphQL) != ""
	hasMCP := effectivePluginSurfaceURL(plugin, SpecSurfaceMCP) != ""
	hasAPISurface := hasOpenAPI || hasGraphQL

	if plugin.OpenAPIConnection != "" && !hasOpenAPI {
		return fmt.Errorf("config validation: integration %q plugin.openapi_connection is only valid when openapi is configured", name)
	}
	if plugin.GraphQLConnection != "" && !hasGraphQL {
		return fmt.Errorf("config validation: integration %q plugin.graphql_connection is only valid when graphql_url is configured", name)
	}
	if plugin.MCPConnection != "" && !hasMCP {
		return fmt.Errorf("config validation: integration %q plugin.mcp_connection is only valid when mcp_url is configured", name)
	}
	if plugin.ResponseMapping != nil && !(plugin.IsInline() && hasAPISurface) {
		return fmt.Errorf("config validation: integration %q plugin.response_mapping is only valid for inline openapi/graphql integrations", name)
	}
	if plugin.BaseURL != "" && !(hasAPISurface || (plugin.IsInline() && len(plugin.Operations) > 0)) {
		return fmt.Errorf("config validation: integration %q plugin.base_url is only valid with inline operations or openapi/graphql surfaces", name)
	}
	if len(plugin.ManagedParameters) > 0 && !hasAPISurface {
		return fmt.Errorf("config validation: integration %q plugin.managed_parameters are only valid with openapi/graphql surfaces", name)
	}

	return nil
}

func effectivePluginSurfaceURL(plugin *PluginDef, surface SpecSurface) string {
	if plugin == nil {
		return ""
	}
	if url := plugin.SurfaceURL(surface); url != "" {
		return url
	}
	return ManifestProviderSurfaceURL(plugin.ManifestProvider(), surface)
}

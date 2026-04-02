package config

import (
	"fmt"
	"maps"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

// EffectiveManifestBackedInputs returns the manifest-backed provider inputs that
// runtime validation and bootstrap both consume.
func EffectiveManifestBackedInputs(name string, plugin *PluginDef) (*pluginmanifestv1.Manifest, map[string]*OperationOverride, error) {
	if plugin == nil {
		return nil, nil, nil
	}

	if plugin.IsInline() {
		manifest, err := InlineToManifest(name, plugin)
		if err != nil {
			return nil, nil, fmt.Errorf("convert inline plugin %q to manifest: %w", name, err)
		}
		if manifest == nil || manifest.Provider == nil {
			return nil, nil, fmt.Errorf("manifest-backed provider %q is missing provider definition", name)
		}
		return manifest, plugin.AllowedOperations, nil
	}

	if !plugin.HasResolvedManifest() {
		return nil, nil, nil
	}

	manifest := cloneManifest(plugin.ResolvedManifest)
	if manifest == nil || manifest.Provider == nil {
		return nil, nil, fmt.Errorf("manifest-backed provider %q is missing provider definition", name)
	}
	manifest.Provider = MergedManifestProvider(plugin.ResolvedManifest.Provider, plugin)
	if manifest.Provider == nil {
		return nil, nil, fmt.Errorf("manifest-backed provider %q is missing provider definition", name)
	}
	return manifest, OperationOverridesFromManifest(manifest.Provider.AllowedOperations), nil
}

// MergedManifestProvider applies local plugin overrides to a resolved manifest
// provider in a single place so validation, bootstrap, and UI metadata can all
// consume the same effective view.
func MergedManifestProvider(manifestProvider *pluginmanifestv1.Provider, plugin *PluginDef) *pluginmanifestv1.Provider {
	if manifestProvider == nil {
		return nil
	}

	merged := cloneManifestProvider(manifestProvider)
	if plugin == nil {
		return merged
	}

	merged.Auth = mergedManifestProviderAuth(manifestProvider.Auth, plugin.Auth)
	if plugin.MCP {
		merged.MCP = true
	}
	merged.BaseURL = MergedProviderBaseURL(manifestProvider, plugin)
	merged.Headers = MergedProviderHeaders(manifestProvider, plugin)
	merged.ManagedParameters = MergedProviderManagedParameters(manifestProvider, plugin)

	if len(plugin.Operations) > 0 {
		merged.Operations = inlineOperationsToManifest(plugin.Operations)
	}
	merged.Connection = MergedProviderConnectionParams(manifestProvider, plugin)

	if plugin.OpenAPI != "" {
		merged.OpenAPI = plugin.OpenAPI
	}
	if plugin.GraphQLURL != "" {
		merged.GraphQLURL = plugin.GraphQLURL
	}
	if plugin.MCPURL != "" {
		merged.MCPURL = plugin.MCPURL
	}

	merged.AllowedOperations = MergedProviderAllowedOperations(manifestProvider, plugin)

	if plugin.OpenAPIConnection != "" {
		merged.OpenAPIConnection = plugin.OpenAPIConnection
	}
	if plugin.GraphQLConnection != "" {
		merged.GraphQLConnection = plugin.GraphQLConnection
	}
	if plugin.MCPConnection != "" {
		merged.MCPConnection = plugin.MCPConnection
	}
	if plugin.DefaultConnection != "" {
		merged.DefaultConnection = plugin.DefaultConnection
	}

	merged.Connections = MergedProviderConnections(manifestProvider, plugin)
	merged.ResponseMapping = MergedProviderResponseMapping(manifestProvider, plugin)

	return merged
}

func MergedProviderBaseURL(manifestProvider *pluginmanifestv1.Provider, plugin *PluginDef) string {
	if plugin != nil && plugin.BaseURL != "" {
		return plugin.BaseURL
	}
	if manifestProvider != nil {
		return manifestProvider.BaseURL
	}
	return ""
}

func MergedProviderHeaders(manifestProvider *pluginmanifestv1.Provider, plugin *PluginDef) map[string]string {
	var manifestHeaders map[string]string
	if manifestProvider != nil {
		manifestHeaders = manifestProvider.Headers
	}

	var pluginHeaders map[string]string
	if plugin != nil {
		pluginHeaders = plugin.Headers
	}

	return MergeHeaders(manifestHeaders, pluginHeaders)
}

func MergedProviderManagedParameters(manifestProvider *pluginmanifestv1.Provider, plugin *PluginDef) []pluginmanifestv1.ManagedParameter {
	var manifestParams []pluginmanifestv1.ManagedParameter
	if manifestProvider != nil {
		manifestParams = manifestProvider.ManagedParameters
	}

	var pluginParams []pluginmanifestv1.ManagedParameter
	if plugin != nil {
		pluginParams = plugin.ManagedParameters
	}

	return MergeManagedParameters(manifestParams, pluginParams)
}

func MergedProviderConnectionParams(manifestProvider *pluginmanifestv1.Provider, plugin *PluginDef) map[string]pluginmanifestv1.ProviderConnectionParam {
	if manifestProvider == nil && (plugin == nil || plugin.ConnectionParams == nil) {
		return nil
	}

	merged := cloneManifestConnectionParams(nil)
	if manifestProvider != nil {
		merged = cloneManifestConnectionParams(manifestProvider.Connection)
	}
	if plugin == nil || plugin.ConnectionParams == nil {
		return merged
	}
	if merged == nil {
		merged = make(map[string]pluginmanifestv1.ProviderConnectionParam, len(plugin.ConnectionParams))
	}
	for name, def := range plugin.ConnectionParams {
		param := merged[name]
		param.Required = def.Required
		merged[name] = param
	}
	return merged
}

func MergedProviderAllowedOperations(manifestProvider *pluginmanifestv1.Provider, plugin *PluginDef) map[string]*pluginmanifestv1.ManifestOperationOverride {
	if plugin != nil && plugin.AllowedOperations != nil {
		return manifestOperationOverridesFromConfig(plugin.AllowedOperations)
	}
	if manifestProvider == nil {
		return nil
	}
	return cloneManifestOperationOverrides(manifestProvider.AllowedOperations)
}

func MergedProviderConnections(manifestProvider *pluginmanifestv1.Provider, plugin *PluginDef) map[string]*pluginmanifestv1.ManifestConnectionDef {
	if manifestProvider == nil && (plugin == nil || plugin.Connections == nil) {
		return nil
	}

	merged := cloneManifestConnections(nil)
	if manifestProvider != nil {
		merged = cloneManifestConnections(manifestProvider.Connections)
	}
	if plugin == nil || plugin.Connections == nil {
		return merged
	}
	if merged == nil {
		merged = make(map[string]*pluginmanifestv1.ManifestConnectionDef, len(plugin.Connections))
	}
	for name, override := range plugin.Connections {
		if override == nil {
			merged[name] = nil
			continue
		}
		current := merged[name]
		if current == nil {
			current = &pluginmanifestv1.ManifestConnectionDef{}
		} else {
			current = cloneManifestConnectionDef(current)
		}
		if override.Mode != "" {
			current.Mode = override.Mode
		}
		if !isZeroConnectionAuthDef(override.Auth) {
			current.Auth = mergedManifestProviderAuth(current.Auth, &override.Auth)
		}
		merged[name] = current
	}
	return merged
}

func MergedProviderResponseMapping(manifestProvider *pluginmanifestv1.Provider, plugin *PluginDef) *pluginmanifestv1.ManifestResponseMapping {
	if plugin != nil && plugin.ResponseMapping != nil {
		return responseMappingToManifest(plugin.ResponseMapping)
	}
	if manifestProvider == nil {
		return nil
	}
	return cloneManifestResponseMapping(manifestProvider.ResponseMapping)
}

func inlineOperationsToManifest(ops []InlineOperationDef) []pluginmanifestv1.ProviderOperation {
	if len(ops) == 0 {
		return nil
	}
	manifestOps := make([]pluginmanifestv1.ProviderOperation, len(ops))
	for i, op := range ops {
		manifestOps[i] = pluginmanifestv1.ProviderOperation{
			Name:        op.Name,
			Description: op.Description,
			Method:      op.Method,
			Path:        op.Path,
		}
		if len(op.Parameters) == 0 {
			continue
		}
		manifestOps[i].Parameters = make([]pluginmanifestv1.ProviderParameter, len(op.Parameters))
		for j, param := range op.Parameters {
			manifestOps[i].Parameters[j] = pluginmanifestv1.ProviderParameter{
				Name:        param.Name,
				Type:        param.Type,
				In:          param.In,
				Description: param.Description,
				Required:    param.Required,
			}
		}
	}
	return manifestOps
}

func responseMappingToManifest(mapping *ResponseMappingDef) *pluginmanifestv1.ManifestResponseMapping {
	if mapping == nil {
		return nil
	}
	out := &pluginmanifestv1.ManifestResponseMapping{
		DataPath: mapping.DataPath,
	}
	if mapping.Pagination != nil {
		out.Pagination = &pluginmanifestv1.ManifestPaginationMapping{
			HasMorePath: mapping.Pagination.HasMorePath,
			CursorPath:  mapping.Pagination.CursorPath,
		}
	}
	return out
}

func manifestOperationOverridesFromConfig(overrides map[string]*OperationOverride) map[string]*pluginmanifestv1.ManifestOperationOverride {
	if overrides == nil {
		return nil
	}
	out := make(map[string]*pluginmanifestv1.ManifestOperationOverride, len(overrides))
	for name, override := range overrides {
		if override == nil {
			out[name] = nil
			continue
		}
		out[name] = &pluginmanifestv1.ManifestOperationOverride{
			Alias:       override.Alias,
			Description: override.Description,
		}
	}
	return out
}

func mergedManifestProviderAuth(base *pluginmanifestv1.ProviderAuth, override *ConnectionAuthDef) *pluginmanifestv1.ProviderAuth {
	if base == nil && override == nil {
		return nil
	}

	auth := ManifestAuthToConnectionAuthDef(base)
	if override != nil {
		MergeConnectionAuth(&auth, *override)
	}
	if isZeroConnectionAuthDef(auth) {
		return nil
	}
	return connectionAuthToManifest(&auth)
}

func isZeroConnectionAuthDef(auth ConnectionAuthDef) bool {
	return auth.Type == "" &&
		auth.AuthorizationURL == "" &&
		auth.TokenURL == "" &&
		auth.ClientID == "" &&
		auth.ClientSecret == "" &&
		auth.RedirectURL == "" &&
		auth.ClientAuth == "" &&
		auth.TokenExchange == "" &&
		auth.Scopes == nil &&
		auth.ScopeParam == "" &&
		auth.ScopeSeparator == "" &&
		!auth.PKCE &&
		auth.AuthorizationParams == nil &&
		auth.TokenParams == nil &&
		auth.RefreshParams == nil &&
		auth.AcceptHeader == "" &&
		auth.AccessTokenPath == "" &&
		auth.TokenMetadata == nil &&
		len(auth.Credentials) == 0 &&
		auth.AuthMapping == nil
}

func cloneManifest(manifest *pluginmanifestv1.Manifest) *pluginmanifestv1.Manifest {
	if manifest == nil {
		return nil
	}
	cloned := *manifest
	cloned.Kinds = append([]string(nil), manifest.Kinds...)
	cloned.Artifacts = append([]pluginmanifestv1.Artifact(nil), manifest.Artifacts...)
	cloned.Provider = cloneManifestProvider(manifest.Provider)
	if manifest.WebUI != nil {
		webUI := *manifest.WebUI
		cloned.WebUI = &webUI
	}
	if manifest.Entrypoints.Provider != nil {
		entrypoint := *manifest.Entrypoints.Provider
		entrypoint.Args = append([]string(nil), manifest.Entrypoints.Provider.Args...)
		cloned.Entrypoints.Provider = &entrypoint
	}
	return &cloned
}

func cloneManifestProvider(provider *pluginmanifestv1.Provider) *pluginmanifestv1.Provider {
	if provider == nil {
		return nil
	}
	cloned := *provider
	cloned.Auth = cloneManifestProviderAuth(provider.Auth)
	cloned.Headers = maps.Clone(provider.Headers)
	cloned.ManagedParameters = append([]pluginmanifestv1.ManagedParameter(nil), provider.ManagedParameters...)
	cloned.Operations = cloneManifestOperations(provider.Operations)
	cloned.PostConnectDiscovery = cloneManifestPostConnectDiscovery(provider.PostConnectDiscovery)
	cloned.Connection = cloneManifestConnectionParams(provider.Connection)
	cloned.AllowedOperations = cloneManifestOperationOverrides(provider.AllowedOperations)
	cloned.Connections = cloneManifestConnections(provider.Connections)
	cloned.ResponseMapping = cloneManifestResponseMapping(provider.ResponseMapping)
	return &cloned
}

func cloneManifestProviderAuth(auth *pluginmanifestv1.ProviderAuth) *pluginmanifestv1.ProviderAuth {
	if auth == nil {
		return nil
	}
	cloned := *auth
	cloned.Scopes = append([]string(nil), auth.Scopes...)
	cloned.AuthorizationParams = maps.Clone(auth.AuthorizationParams)
	cloned.TokenParams = maps.Clone(auth.TokenParams)
	cloned.RefreshParams = maps.Clone(auth.RefreshParams)
	cloned.TokenMetadata = append([]string(nil), auth.TokenMetadata...)
	if len(auth.Credentials) > 0 {
		cloned.Credentials = append([]pluginmanifestv1.CredentialField(nil), auth.Credentials...)
	}
	return &cloned
}

func cloneManifestOperations(ops []pluginmanifestv1.ProviderOperation) []pluginmanifestv1.ProviderOperation {
	if len(ops) == 0 {
		return nil
	}
	cloned := make([]pluginmanifestv1.ProviderOperation, len(ops))
	for i, op := range ops {
		cloned[i] = op
		cloned[i].Parameters = append([]pluginmanifestv1.ProviderParameter(nil), op.Parameters...)
	}
	return cloned
}

func cloneManifestPostConnectDiscovery(discovery *pluginmanifestv1.ProviderPostConnectDiscovery) *pluginmanifestv1.ProviderPostConnectDiscovery {
	if discovery == nil {
		return nil
	}
	cloned := *discovery
	cloned.MetadataMapping = maps.Clone(discovery.MetadataMapping)
	return &cloned
}

func cloneManifestConnectionParams(params map[string]pluginmanifestv1.ProviderConnectionParam) map[string]pluginmanifestv1.ProviderConnectionParam {
	if params == nil {
		return nil
	}
	return maps.Clone(params)
}

func cloneManifestOperationOverrides(overrides map[string]*pluginmanifestv1.ManifestOperationOverride) map[string]*pluginmanifestv1.ManifestOperationOverride {
	if overrides == nil {
		return nil
	}
	cloned := make(map[string]*pluginmanifestv1.ManifestOperationOverride, len(overrides))
	for name, override := range overrides {
		if override == nil {
			cloned[name] = nil
			continue
		}
		copy := *override
		cloned[name] = &copy
	}
	return cloned
}

func cloneManifestConnections(connections map[string]*pluginmanifestv1.ManifestConnectionDef) map[string]*pluginmanifestv1.ManifestConnectionDef {
	if connections == nil {
		return nil
	}
	cloned := make(map[string]*pluginmanifestv1.ManifestConnectionDef, len(connections))
	for name, connection := range connections {
		cloned[name] = cloneManifestConnectionDef(connection)
	}
	return cloned
}

func cloneManifestConnectionDef(connection *pluginmanifestv1.ManifestConnectionDef) *pluginmanifestv1.ManifestConnectionDef {
	if connection == nil {
		return nil
	}
	cloned := *connection
	cloned.Auth = cloneManifestProviderAuth(connection.Auth)
	return &cloned
}

func cloneManifestResponseMapping(mapping *pluginmanifestv1.ManifestResponseMapping) *pluginmanifestv1.ManifestResponseMapping {
	if mapping == nil {
		return nil
	}
	cloned := *mapping
	if mapping.Pagination != nil {
		pagination := *mapping.Pagination
		cloned.Pagination = &pagination
	}
	return &cloned
}

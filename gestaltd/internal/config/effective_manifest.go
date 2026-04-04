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

	manifest := mergeManifestProviderRuntimeConfig(plugin.ResolvedManifest, plugin)
	if manifest == nil || manifest.Provider == nil {
		return nil, nil, fmt.Errorf("manifest-backed provider %q is missing provider definition", name)
	}

	allowedOperations := plugin.AllowedOperations
	if allowedOperations == nil {
		allowedOperations = maps.Clone(manifest.Provider.AllowedOperations)
	}

	return manifest, allowedOperations, nil
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

func MergedProviderResponseMapping(manifestProvider *pluginmanifestv1.Provider, plugin *PluginDef) *pluginmanifestv1.ManifestResponseMapping {
	if plugin != nil && plugin.ResponseMapping != nil {
		return plugin.ResponseMapping
	}
	if manifestProvider == nil {
		return nil
	}
	return manifestProvider.ResponseMapping
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

func mergeManifestProviderRuntimeConfig(manifest *pluginmanifestv1.Manifest, plugin *PluginDef) *pluginmanifestv1.Manifest {
	if manifest == nil || manifest.Provider == nil || plugin == nil {
		return manifest
	}
	if plugin.BaseURL == "" &&
		len(plugin.Headers) == 0 &&
		len(plugin.ManagedParameters) == 0 &&
		plugin.Auth == nil &&
		plugin.ConnectionParams == nil &&
		plugin.ResponseMapping == nil {
		return manifest
	}

	provider := manifest.Provider
	cloned := *manifest
	providerCopy := *provider
	providerCopy.BaseURL = MergedProviderBaseURL(provider, plugin)
	providerCopy.Headers = MergedProviderHeaders(provider, plugin)
	providerCopy.ManagedParameters = MergedProviderManagedParameters(provider, plugin)
	providerCopy.ConnectionParams = mergedProviderConnectionParams(provider, plugin)
	providerCopy.ResponseMapping = MergedProviderResponseMapping(provider, plugin)
	if plugin.Auth != nil {
		providerCopy.Auth = mergedManifestProviderAuth(provider.Auth, plugin.Auth)
	}
	cloned.Provider = &providerCopy
	return &cloned
}

func mergedProviderConnectionParams(manifestProvider *pluginmanifestv1.Provider, plugin *PluginDef) map[string]pluginmanifestv1.ProviderConnectionParam {
	if manifestProvider == nil {
		return nil
	}
	if plugin == nil || plugin.ConnectionParams == nil {
		return manifestProvider.ConnectionParams
	}

	merged := maps.Clone(manifestProvider.ConnectionParams)
	for name, override := range plugin.ConnectionParams {
		param := merged[name]
		param.Required = override.Required
		merged[name] = param
	}
	return merged
}

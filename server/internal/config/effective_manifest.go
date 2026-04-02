package config

import (
	"fmt"

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
	return manifest, OperationOverridesFromManifest(manifest.Provider.AllowedOperations), nil
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

func mergeManifestProviderRuntimeConfig(manifest *pluginmanifestv1.Manifest, plugin *PluginDef) *pluginmanifestv1.Manifest {
	if manifest == nil || manifest.Provider == nil {
		return manifest
	}

	headers := MergedProviderHeaders(manifest.Provider, plugin)
	managedParameters := MergedProviderManagedParameters(manifest.Provider, plugin)
	baseURL := manifest.Provider.BaseURL
	if plugin != nil && plugin.BaseURL != "" {
		baseURL = plugin.BaseURL
	}
	if len(headers) == 0 && len(managedParameters) == 0 && baseURL == manifest.Provider.BaseURL {
		return manifest
	}

	cloned := *manifest
	providerCopy := *manifest.Provider
	providerCopy.BaseURL = baseURL
	providerCopy.Headers = headers
	providerCopy.ManagedParameters = managedParameters
	cloned.Provider = &providerCopy
	return &cloned
}

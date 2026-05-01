package config

import pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"

func PluginValidationConfig(cfg *Config) *pluginservice.ValidationConfig {
	if cfg == nil {
		return nil
	}
	validation := &pluginservice.ValidationConfig{
		Plugins: make(map[string]*pluginservice.ValidationPlugin, len(cfg.Plugins)),
	}
	for name, entry := range cfg.Plugins {
		validation.Plugins[name] = PluginValidationEntry(entry)
	}
	return validation
}

func PluginValidationEntry(entry *ProviderEntry) *pluginservice.ValidationPlugin {
	if entry == nil {
		return nil
	}
	return &pluginservice.ValidationPlugin{
		Manifest:            entry.ResolvedManifest,
		ManifestPath:        entry.ResolvedManifestPath,
		AllowedOperations:   entry.AllowedOperations,
		Invokes:             pluginInvocationDependencies(entry.Invokes),
		SurfaceURLOverrides: pluginSurfaceURLOverrides(entry),
		ReadStaticCatalog:   pluginservice.StaticCatalogReaderForManifest(entry.ResolvedManifestPath),
		LoadAPICatalog:      pluginservice.DefaultAPICatalogLoader,
	}
}

func pluginInvocationDependencies(dependencies []PluginInvocationDependency) []pluginservice.InvocationDependency {
	if len(dependencies) == 0 {
		return nil
	}
	result := make([]pluginservice.InvocationDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		result = append(result, pluginservice.InvocationDependency{
			Plugin:    dependency.Plugin,
			Operation: dependency.Operation,
			Surface:   pluginservice.SpecSurface(dependency.Surface),
		})
	}
	return result
}

func pluginSurfaceURLOverrides(entry *ProviderEntry) map[pluginservice.SpecSurface]string {
	if entry == nil {
		return nil
	}
	overrides := make(map[pluginservice.SpecSurface]string)
	for _, surface := range []pluginservice.SpecSurface{pluginservice.SpecSurfaceGraphQL, pluginservice.SpecSurfaceMCP} {
		if url := ProviderSurfaceURLOverride(entry, SpecSurface(surface)); url != "" {
			overrides[surface] = url
		}
	}
	if len(overrides) == 0 {
		return nil
	}
	return overrides
}

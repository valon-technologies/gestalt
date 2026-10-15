package config

import appservice "github.com/valon-technologies/gestalt/server/services/apps"

func AppValidationConfig(cfg *Config) *appservice.ValidationConfig {
	return withOptionalConfig(cfg, func(cfg *Config) *appservice.ValidationConfig {
		validation := &appservice.ValidationConfig{
			Apps: make(map[string]*appservice.ValidationApp, len(cfg.Apps)),
		}
		for name, entry := range cfg.Apps {
			if entry != nil && entry.Source.IsRegistry() {
				continue
			}
			validation.Apps[name] = AppValidationEntry(entry)
		}
		return validation
	})
}

func AppValidationEntry(entry *ProviderEntry) *appservice.ValidationApp {
	return withOptionalConfig(entry, func(entry *ProviderEntry) *appservice.ValidationApp {
		return &appservice.ValidationApp{
			Manifest:                    entry.ResolvedManifest,
			ManifestPath:                entry.ResolvedManifestPath,
			AllowedOperations:           entry.AllowedOperations,
			SurfaceURLOverrides:         pluginSurfaceURLOverrides(entry),
			EffectiveCatalog:            entry.ResolvedCatalog,
			EffectiveCatalogAvailable:   entry.ResolvedCatalogAvailable,
			EffectiveCatalogSessionOnly: entry.ResolvedCatalogSessionOnly,
			StaticMetadataUnavailable:   entry.StaticManifestUnavailable,
			ReadStaticCatalog:           appservice.StaticCatalogReaderForManifest(entry.ResolvedManifestPath),
			LoadAPICatalog:              appservice.DefaultAPICatalogLoader,
		}
	})
}

func pluginSurfaceURLOverrides(entry *ProviderEntry) map[appservice.SpecSurface]string {
	overrides := make(map[appservice.SpecSurface]string)
	for _, surface := range []appservice.SpecSurface{appservice.SpecSurfaceGraphQL, appservice.SpecSurfaceMCP} {
		if url := ProviderSurfaceURLOverride(entry, SpecSurface(surface)); url != "" {
			overrides[surface] = url
		}
	}
	if len(overrides) == 0 {
		return nil
	}
	return overrides
}

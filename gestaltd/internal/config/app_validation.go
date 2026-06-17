package config

import appservice "github.com/valon-technologies/gestalt/server/services/apps"

func AppValidationConfig(cfg *Config) *appservice.ValidationConfig {
	if cfg == nil {
		return nil
	}
	validation := &appservice.ValidationConfig{
		Apps: make(map[string]*appservice.ValidationApp, len(cfg.Apps)),
	}
	for name, entry := range cfg.Apps {
		validation.Apps[name] = AppValidationEntry(entry)
	}
	return validation
}

func AppValidationEntry(entry *ProviderEntry) *appservice.ValidationApp {
	if entry == nil {
		return nil
	}
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
}

func pluginSurfaceURLOverrides(entry *ProviderEntry) map[appservice.SpecSurface]string {
	if entry == nil {
		return nil
	}
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

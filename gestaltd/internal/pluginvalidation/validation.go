package pluginvalidation

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
	"github.com/valon-technologies/gestalt/server/services/plugins/openapi"
)

func ValidateEffectiveManifest(ctx context.Context, name, manifestPath string, manifest *providermanifestv1.Manifest) error {
	return pluginservice.ValidateEffectiveCatalog(ctx, name, FromManifest(manifestPath, manifest))
}

func ValidateEffectiveCatalogsAndDependencies(ctx context.Context, cfg *config.Config) error {
	return pluginservice.ValidateEffectiveCatalogsAndDependencies(ctx, FromConfig(cfg))
}

func ValidateDependencies(ctx context.Context, cfg *config.Config) error {
	return pluginservice.ValidateDependencies(ctx, FromConfig(cfg))
}

func FromConfig(cfg *config.Config) *pluginservice.ValidationConfig {
	if cfg == nil {
		return nil
	}
	validation := &pluginservice.ValidationConfig{
		Plugins: make(map[string]*pluginservice.ValidationPlugin, len(cfg.Plugins)),
	}
	for name, entry := range cfg.Plugins {
		validation.Plugins[name] = FromProviderEntry(entry)
	}
	return validation
}

func FromProviderEntry(entry *config.ProviderEntry) *pluginservice.ValidationPlugin {
	if entry == nil {
		return nil
	}
	return &pluginservice.ValidationPlugin{
		Manifest:            entry.ResolvedManifest,
		ManifestPath:        entry.ResolvedManifestPath,
		AllowedOperations:   entry.AllowedOperations,
		Invokes:             invocationDependencies(entry.Invokes),
		SurfaceURLOverrides: surfaceURLOverrides(entry),
		ReadStaticCatalog:   staticCatalogReader(entry),
		LoadAPICatalog:      loadAPICatalog,
	}
}

func FromManifest(manifestPath string, manifest *providermanifestv1.Manifest) *pluginservice.ValidationPlugin {
	return FromProviderEntry(&config.ProviderEntry{
		ResolvedManifestPath: manifestPath,
		ResolvedManifest:     manifest,
	})
}

func invocationDependencies(dependencies []config.PluginInvocationDependency) []pluginservice.InvocationDependency {
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

func surfaceURLOverrides(entry *config.ProviderEntry) map[pluginservice.SpecSurface]string {
	if entry == nil {
		return nil
	}
	overrides := make(map[pluginservice.SpecSurface]string)
	for _, surface := range []pluginservice.SpecSurface{pluginservice.SpecSurfaceGraphQL, pluginservice.SpecSurfaceMCP} {
		if url := config.ProviderSurfaceURLOverride(entry, config.SpecSurface(surface)); url != "" {
			overrides[surface] = url
		}
	}
	if len(overrides) == 0 {
		return nil
	}
	return overrides
}

func staticCatalogReader(entry *config.ProviderEntry) pluginservice.StaticCatalogReader {
	if entry == nil || entry.ResolvedManifestPath == "" {
		return nil
	}
	root := filepath.Dir(entry.ResolvedManifestPath)
	return func(name string) (*catalog.Catalog, error) {
		return providerpkg.ReadStaticCatalog(root, name)
	}
}

func loadAPICatalog(ctx context.Context, name string, surface pluginservice.SpecSurface, specURL string, allowed map[string]*pluginservice.OperationOverride) (*catalog.Catalog, error) {
	switch surface {
	case pluginservice.SpecSurfaceOpenAPI:
		def, err := openapi.LoadDefinition(ctx, name, specURL, allowed)
		if err != nil {
			return nil, err
		}
		return declarative.CatalogFromDefinition(def), nil
	default:
		return nil, fmt.Errorf("unsupported API catalog surface %q", surface)
	}
}

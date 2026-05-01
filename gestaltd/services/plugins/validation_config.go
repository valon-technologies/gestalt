package plugins

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
	"github.com/valon-technologies/gestalt/server/services/plugins/openapi"
)

func ValidateEffectiveManifest(ctx context.Context, name, manifestPath string, manifest *providermanifestv1.Manifest) error {
	return ValidateEffectiveCatalog(ctx, name, ValidationPluginFromManifest(manifestPath, manifest))
}

func ValidationConfigFromConfig(cfg *config.Config) *ValidationConfig {
	if cfg == nil {
		return nil
	}
	validation := &ValidationConfig{
		Plugins: make(map[string]*ValidationPlugin, len(cfg.Plugins)),
	}
	for name, entry := range cfg.Plugins {
		validation.Plugins[name] = ValidationPluginFromProviderEntry(entry)
	}
	return validation
}

func ValidationPluginFromProviderEntry(entry *config.ProviderEntry) *ValidationPlugin {
	if entry == nil {
		return nil
	}
	return &ValidationPlugin{
		Manifest:            entry.ResolvedManifest,
		ManifestPath:        entry.ResolvedManifestPath,
		AllowedOperations:   entry.AllowedOperations,
		Invokes:             invocationDependencies(entry.Invokes),
		SurfaceURLOverrides: surfaceURLOverrides(entry),
		ReadStaticCatalog:   staticCatalogReader(entry),
		LoadAPICatalog:      loadAPICatalog,
	}
}

func ValidationPluginFromManifest(manifestPath string, manifest *providermanifestv1.Manifest) *ValidationPlugin {
	return ValidationPluginFromProviderEntry(&config.ProviderEntry{
		ResolvedManifestPath: manifestPath,
		ResolvedManifest:     manifest,
	})
}

func invocationDependencies(dependencies []config.PluginInvocationDependency) []InvocationDependency {
	if len(dependencies) == 0 {
		return nil
	}
	result := make([]InvocationDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		result = append(result, InvocationDependency{
			Plugin:    dependency.Plugin,
			Operation: dependency.Operation,
			Surface:   SpecSurface(dependency.Surface),
		})
	}
	return result
}

func surfaceURLOverrides(entry *config.ProviderEntry) map[SpecSurface]string {
	if entry == nil {
		return nil
	}
	overrides := make(map[SpecSurface]string)
	for _, surface := range []SpecSurface{SpecSurfaceGraphQL, SpecSurfaceMCP} {
		if url := config.ProviderSurfaceURLOverride(entry, config.SpecSurface(surface)); url != "" {
			overrides[surface] = url
		}
	}
	if len(overrides) == 0 {
		return nil
	}
	return overrides
}

func staticCatalogReader(entry *config.ProviderEntry) StaticCatalogReader {
	if entry == nil || entry.ResolvedManifestPath == "" {
		return nil
	}
	root := filepath.Dir(entry.ResolvedManifestPath)
	return func(name string) (*catalog.Catalog, error) {
		return providerpkg.ReadStaticCatalog(root, name)
	}
}

func loadAPICatalog(ctx context.Context, name string, surface SpecSurface, specURL string, allowed map[string]*OperationOverride) (*catalog.Catalog, error) {
	switch surface {
	case SpecSurfaceOpenAPI:
		def, err := openapi.LoadDefinition(ctx, name, specURL, allowed)
		if err != nil {
			return nil, err
		}
		return declarative.CatalogFromDefinition(def), nil
	default:
		return nil, fmt.Errorf("unsupported API catalog surface %q", surface)
	}
}

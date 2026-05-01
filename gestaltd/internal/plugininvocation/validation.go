package plugininvocation

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/openapi"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/plugins/operationexposure"
)

type resolvedDependencyCatalog struct {
	catalog     *catalog.Catalog
	sessionOnly bool
	err         error
}

type catalogResolutionCache struct {
	entries map[string]resolvedDependencyCatalog
}

func newCatalogResolutionCache(size int) *catalogResolutionCache {
	if size < 0 {
		size = 0
	}
	return &catalogResolutionCache{entries: make(map[string]resolvedDependencyCatalog, size)}
}

func (c *catalogResolutionCache) resolve(ctx context.Context, name string, entry *config.ProviderEntry) resolvedDependencyCatalog {
	if c == nil {
		resolved := resolvedDependencyCatalog{}
		resolved.catalog, resolved.sessionOnly, resolved.err = resolveStaticCatalog(ctx, name, entry)
		return resolved
	}
	if resolved, ok := c.entries[name]; ok {
		return resolved
	}
	resolved := resolvedDependencyCatalog{}
	resolved.catalog, resolved.sessionOnly, resolved.err = resolveStaticCatalog(ctx, name, entry)
	c.entries[name] = resolved
	return resolved
}

func ValidateEffectiveCatalog(ctx context.Context, name string, entry *config.ProviderEntry) error {
	if entry == nil {
		return nil
	}
	_, _, err := resolveStaticCatalog(ctx, name, entry)
	return err
}

func ValidateEffectiveCatalogs(ctx context.Context, cfg *config.Config) error {
	return validateEffectiveCatalogs(ctx, cfg, newCatalogResolutionCache(len(cfg.Plugins)))
}

func ValidateEffectiveCatalogsAndDependencies(ctx context.Context, cfg *config.Config) error {
	cache := newCatalogResolutionCache(len(cfg.Plugins))
	if err := validateEffectiveCatalogs(ctx, cfg, cache); err != nil {
		return err
	}
	return validateDependencies(ctx, cfg, cache)
}

func validateEffectiveCatalogs(ctx context.Context, cfg *config.Config, cache *catalogResolutionCache) error {
	if cfg == nil {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Plugins)) {
		entry := cfg.Plugins[name]
		if entry == nil {
			continue
		}
		resolved := cache.resolve(ctx, name, entry)
		if resolved.err != nil {
			return fmt.Errorf("config validation: plugins.%s: %w", name, resolved.err)
		}
	}
	return nil
}

func ValidateDependencies(ctx context.Context, cfg *config.Config) error {
	return validateDependencies(ctx, cfg, newCatalogResolutionCache(len(cfg.Plugins)))
}

func validateDependencies(ctx context.Context, cfg *config.Config, cache *catalogResolutionCache) error {
	if cfg == nil {
		return nil
	}
	for callerName, callerEntry := range cfg.Plugins {
		if callerEntry == nil || len(callerEntry.Invokes) == 0 {
			continue
		}
		if !isExecutablePlugin(callerEntry) {
			return fmt.Errorf("config validation: plugins.%s.invokes is only supported on executable plugins", callerName)
		}
		for i, dependency := range callerEntry.Invokes {
			targetEntry, ok := cfg.Plugins[dependency.Plugin]
			if !ok || targetEntry == nil {
				return fmt.Errorf("config validation: plugins.%s.invokes[%d] references unknown plugin %q", callerName, i, dependency.Plugin)
			}
			if dependency.Surface != "" {
				if !pluginSupportsSurface(targetEntry, config.SpecSurface(dependency.Surface)) {
					return fmt.Errorf("config validation: plugins.%s.invokes[%d] references plugin %q surface %q, but that surface is not configured", callerName, i, dependency.Plugin, dependency.Surface)
				}
				continue
			}
			resolved := cache.resolve(ctx, dependency.Plugin, targetEntry)
			if resolved.err != nil {
				return fmt.Errorf("config validation: plugins.%s.invokes[%d]: %w", callerName, i, resolved.err)
			}
			if hasCatalogOperation(resolved.catalog, dependency.Operation) {
				continue
			}
			if resolved.sessionOnly {
				return fmt.Errorf("config validation: plugins.%s.invokes[%d] references session-catalog-only operation %q on plugin %q, which is unsupported during init/validate", callerName, i, dependency.Operation, dependency.Plugin)
			}
			return fmt.Errorf("config validation: plugins.%s.invokes[%d] references unknown effective operation %q on plugin %q", callerName, i, dependency.Operation, dependency.Plugin)
		}
	}
	return nil
}

func isExecutablePlugin(entry *config.ProviderEntry) bool {
	if entry == nil {
		return false
	}
	manifest := entry.ResolvedManifest
	spec := entry.ManifestSpec()
	if manifest == nil || spec == nil {
		return false
	}
	if spec.IsSpecLoaded() && manifest.Entrypoint == nil {
		return false
	}
	if spec.IsDeclarative() && manifest.Entrypoint == nil {
		return false
	}
	return true
}

func pluginSupportsSurface(entry *config.ProviderEntry, surface config.SpecSurface) bool {
	if entry == nil {
		return false
	}
	spec := entry.ManifestSpec()
	if spec == nil {
		return false
	}
	_, ok := resolvedSurfaceURL(entry, spec, surface)
	return ok
}

func resolveStaticCatalog(ctx context.Context, name string, entry *config.ProviderEntry) (*catalog.Catalog, bool, error) {
	manifest := entry.ResolvedManifest
	spec := entry.ManifestSpec()
	if manifest == nil || spec == nil {
		return nil, false, fmt.Errorf("plugin %q does not have a resolved manifest", name)
	}
	allowed := effectiveAllowedOperations(entry, spec)
	switch {
	case spec.IsSpecLoaded() && manifest.Entrypoint == nil:
		return resolveSpecLoadedCatalog(ctx, name, entry, spec, allowed)
	case spec.IsDeclarative() && manifest.Entrypoint == nil:
		return resolveDeclarativeCatalog(name, manifest, allowed)
	default:
		return resolveExecutableCatalog(ctx, name, entry, manifest, spec, allowed)
	}
}

func resolveSpecLoadedCatalog(ctx context.Context, name string, entry *config.ProviderEntry, spec *providermanifestv1.Spec, allowed map[string]*config.OperationOverride) (*catalog.Catalog, bool, error) {
	apiCatalog, err := loadConfiguredAPICatalog(ctx, name, entry, spec, allowed)
	if err != nil {
		return nil, false, err
	}
	_, hasMCP := resolvedSurfaceURL(entry, spec, config.SpecSurfaceMCP)
	return apiCatalog, hasMCP && apiCatalog == nil, nil
}

func resolveDeclarativeCatalog(name string, manifest *providermanifestv1.Manifest, allowed map[string]*config.OperationOverride) (*catalog.Catalog, bool, error) {
	rawCatalog, err := loadDeclarativeCatalog(name, manifest)
	if err != nil {
		return nil, false, err
	}
	filtered, err := applyOperationExposure(rawCatalog, allowed)
	if err != nil {
		return nil, false, fmt.Errorf("plugin %q declarative catalog: %w", name, err)
	}
	return filtered, false, nil
}

func loadDeclarativeCatalog(name string, manifest *providermanifestv1.Manifest) (*catalog.Catalog, error) {
	prov, err := pluginservice.NewDeclarativeProvider(manifest, nil)
	if err != nil {
		return nil, fmt.Errorf("plugin %q declarative catalog: %w", name, err)
	}
	return prov.Catalog(), nil
}

func resolveExecutableCatalog(ctx context.Context, name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, spec *providermanifestv1.Spec, allowed map[string]*config.OperationOverride) (*catalog.Catalog, bool, error) {
	pluginCatalog, err := readStaticCatalog(name, entry, manifest)
	if err != nil {
		return nil, false, err
	}

	if !spec.IsManifestBacked() {
		filtered, err := applyOperationExposure(pluginCatalog, allowed)
		if err != nil {
			return nil, false, fmt.Errorf("plugin %q static catalog: %w", name, err)
		}
		return filtered, false, nil
	}

	filteredPluginCatalog, err := applyOperationExposure(pluginCatalog, operationexposure.MatchingAllowedOperations(allowed, pluginCatalog))
	if err != nil {
		return nil, false, fmt.Errorf("plugin %q static catalog: %w", name, err)
	}

	if spec.IsDeclarative() {
		apiCatalog, err := loadDeclarativeCatalog(name, manifest)
		if err != nil {
			return nil, false, err
		}
		if err := validateAllowedOperationCoverage(name, allowed, pluginCatalog, apiCatalog); err != nil {
			return nil, false, err
		}
		filteredAPICatalog, err := applyOperationExposure(apiCatalog, operationexposure.MatchingAllowedOperations(allowed, apiCatalog))
		if err != nil {
			return nil, false, err
		}
		merged, err := mergeCatalogs(name, filteredPluginCatalog, filteredAPICatalog)
		if err != nil {
			return nil, false, err
		}
		return merged, false, nil
	}

	apiCatalog, err := loadConfiguredAPICatalog(ctx, name, entry, spec, nil)
	if err != nil {
		return nil, false, err
	}
	if err := validateAllowedOperationCoverage(name, allowed, pluginCatalog, apiCatalog); err != nil {
		return nil, false, err
	}
	filteredAPICatalog, err := applyOperationExposure(apiCatalog, operationexposure.MatchingAllowedOperations(allowed, apiCatalog))
	if err != nil {
		return nil, false, fmt.Errorf("plugin %q API catalog: %w", name, err)
	}
	_, hasMCP := resolvedSurfaceURL(entry, spec, config.SpecSurfaceMCP)
	merged, err := mergeCatalogs(name, filteredPluginCatalog, filteredAPICatalog)
	if err != nil {
		return nil, false, err
	}
	return merged, hasMCP && merged == nil, nil
}

func readStaticCatalog(name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest) (*catalog.Catalog, error) {
	if entry == nil || entry.ResolvedManifestPath == "" {
		if providerpkg.StaticCatalogRequired(manifest) {
			return nil, fmt.Errorf("plugin %q requires %s", name, providerpkg.StaticCatalogFile)
		}
		return nil, nil
	}
	cat, err := providerpkg.ReadStaticCatalog(filepath.Dir(entry.ResolvedManifestPath), name)
	if err != nil {
		return nil, fmt.Errorf("plugin %q static catalog: %w", name, err)
	}
	if cat == nil && providerpkg.StaticCatalogRequired(manifest) {
		return nil, fmt.Errorf("plugin %q requires %s", name, providerpkg.StaticCatalogFile)
	}
	return cat, nil
}

func loadConfiguredAPICatalog(ctx context.Context, name string, entry *config.ProviderEntry, spec *providermanifestv1.Spec, allowed map[string]*config.OperationOverride) (*catalog.Catalog, error) {
	var merged *catalog.Catalog
	for _, surface := range []config.SpecSurface{config.SpecSurfaceOpenAPI, config.SpecSurfaceGraphQL} {
		url, ok := resolvedSurfaceURL(entry, spec, surface)
		if !ok {
			continue
		}
		if surface == config.SpecSurfaceGraphQL {
			continue
		}
		var (
			def *provider.Definition
			err error
		)
		def, err = openapi.LoadDefinition(ctx, name, url, allowed)
		if err != nil {
			return nil, fmt.Errorf("plugin %q %s catalog: %w", name, surface, err)
		}
		merged, err = mergeCatalogs(name, merged, provider.CatalogFromDefinition(def))
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func resolvedSurfaceURL(entry *config.ProviderEntry, spec *providermanifestv1.Spec, surface config.SpecSurface) (string, bool) {
	if url := config.ProviderSurfaceURLOverride(entry, surface); url != "" {
		return url, true
	}
	url := config.ManifestProviderSurfaceURL(spec, surface)
	if url == "" {
		return "", false
	}
	return config.ResolveManifestRelativeSpecURL(entry, url), true
}

func applyOperationExposure(cat *catalog.Catalog, allowed map[string]*config.OperationOverride) (*catalog.Catalog, error) {
	if cat == nil {
		return nil, nil
	}
	policy, err := operationexposure.New(allowed)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return cat.Clone(), nil
	}
	if err := policy.ValidateCatalog(cat); err != nil {
		return nil, err
	}
	return policy.ApplyCatalog(cat), nil
}

func validateAllowedOperationCoverage(name string, allowed map[string]*config.OperationOverride, catalogs ...*catalog.Catalog) error {
	if len(allowed) == 0 {
		return nil
	}
	available := make(map[string]struct{})
	for _, cat := range catalogs {
		if cat == nil {
			continue
		}
		for i := range cat.Operations {
			available[cat.Operations[i].ID] = struct{}{}
		}
	}
	for operation := range allowed {
		if _, ok := available[operation]; !ok {
			return fmt.Errorf("plugin %q effective catalog: allowedOperations contains unknown operation %q", name, operation)
		}
	}
	return nil
}

func mergeCatalogs(name string, first, second *catalog.Catalog) (*catalog.Catalog, error) {
	switch {
	case first == nil && second == nil:
		return nil, nil
	case first == nil:
		return second.Clone(), nil
	case second == nil:
		return first.Clone(), nil
	}

	merged := first.Clone()
	seen := make(map[string]struct{}, len(merged.Operations))
	for i := range merged.Operations {
		seen[merged.Operations[i].ID] = struct{}{}
	}
	for i := range second.Operations {
		if _, ok := seen[second.Operations[i].ID]; ok {
			return nil, fmt.Errorf("plugin %q exposes duplicate operation %q across merged catalogs", name, second.Operations[i].ID)
		}
		merged.Operations = append(merged.Operations, second.Operations[i])
	}
	return merged, nil
}

func hasCatalogOperation(cat *catalog.Catalog, operation string) bool {
	if cat == nil {
		return false
	}
	for i := range cat.Operations {
		if cat.Operations[i].ID == operation {
			return true
		}
	}
	return false
}

func effectiveAllowedOperations(entry *config.ProviderEntry, spec *providermanifestv1.Spec) map[string]*config.OperationOverride {
	if entry != nil && entry.AllowedOperations != nil {
		return entry.AllowedOperations
	}
	if spec == nil {
		return nil
	}
	return spec.AllowedOperations
}

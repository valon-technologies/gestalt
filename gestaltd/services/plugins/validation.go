package plugins

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/operationexposure"
)

type resolvedDependencyCatalog struct {
	catalog     *catalog.Catalog
	sessionOnly bool
	err         error
}

type EffectiveCatalogResult struct {
	Catalog     *catalog.Catalog
	Available   bool
	SessionOnly bool
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

func (c *catalogResolutionCache) resolve(ctx context.Context, name string, entry *ValidationPlugin) resolvedDependencyCatalog {
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

func ValidateEffectiveCatalog(ctx context.Context, name string, entry *ValidationPlugin) error {
	if entry == nil {
		return nil
	}
	_, _, err := EffectiveCatalog(ctx, name, entry)
	return err
}

func EffectiveCatalog(ctx context.Context, name string, entry *ValidationPlugin) (*catalog.Catalog, bool, error) {
	return resolveStaticCatalog(ctx, name, entry)
}

func ValidateEffectiveCatalogs(ctx context.Context, cfg *ValidationConfig) error {
	if cfg == nil {
		return nil
	}
	return validateEffectiveCatalogs(ctx, cfg, newCatalogResolutionCache(len(cfg.Plugins)))
}

func ValidateEffectiveCatalogsAndDependencies(ctx context.Context, cfg *ValidationConfig) error {
	_, err := EffectiveCatalogsAndDependencies(ctx, cfg)
	return err
}

func EffectiveCatalogsAndDependencies(ctx context.Context, cfg *ValidationConfig) (map[string]EffectiveCatalogResult, error) {
	if cfg == nil {
		return nil, nil
	}
	cache := newCatalogResolutionCache(len(cfg.Plugins))
	if err := validateEffectiveCatalogs(ctx, cfg, cache); err != nil {
		return nil, err
	}
	if err := validateDependencies(ctx, cfg, cache); err != nil {
		return nil, err
	}
	results := make(map[string]EffectiveCatalogResult, len(cache.entries))
	for name, resolved := range cache.entries {
		results[name] = EffectiveCatalogResult{
			Catalog:     resolved.catalog.Clone(),
			Available:   resolved.catalog != nil,
			SessionOnly: resolved.sessionOnly,
		}
	}
	return results, nil
}

func validateEffectiveCatalogs(ctx context.Context, cfg *ValidationConfig, cache *catalogResolutionCache) error {
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

func ValidateDependencies(ctx context.Context, cfg *ValidationConfig) error {
	if cfg == nil {
		return nil
	}
	return validateDependencies(ctx, cfg, newCatalogResolutionCache(len(cfg.Plugins)))
}

func validateDependencies(ctx context.Context, cfg *ValidationConfig, cache *catalogResolutionCache) error {
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
			if targetEntry.StaticMetadataUnavailable {
				continue
			}
			if dependency.Surface != "" {
				if !pluginSupportsSurface(targetEntry, dependency.Surface) {
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

func isExecutablePlugin(entry *ValidationPlugin) bool {
	if entry == nil {
		return false
	}
	manifest := entry.Manifest
	spec := entry.manifestSpec()
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

func pluginSupportsSurface(entry *ValidationPlugin, surface SpecSurface) bool {
	if entry == nil {
		return false
	}
	spec := entry.manifestSpec()
	if spec == nil {
		return false
	}
	_, ok := resolvedSurfaceURL(entry, spec, surface)
	return ok
}

func resolveStaticCatalog(ctx context.Context, name string, entry *ValidationPlugin) (*catalog.Catalog, bool, error) {
	if entry != nil && (entry.EffectiveCatalogAvailable || entry.EffectiveCatalog != nil || entry.EffectiveCatalogSessionOnly) {
		return entry.EffectiveCatalog.Clone(), entry.EffectiveCatalogSessionOnly, nil
	}
	manifest := entry.Manifest
	spec := entry.manifestSpec()
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

func resolveSpecLoadedCatalog(ctx context.Context, name string, entry *ValidationPlugin, spec *providermanifestv1.Spec, allowed map[string]*OperationOverride) (*catalog.Catalog, bool, error) {
	apiCatalog, err := loadConfiguredAPICatalog(ctx, name, entry, spec, allowed)
	if err != nil {
		return nil, false, err
	}
	_, hasMCP := resolvedSurfaceURL(entry, spec, SpecSurfaceMCP)
	return apiCatalog, hasMCP && apiCatalog == nil, nil
}

func resolveDeclarativeCatalog(name string, manifest *providermanifestv1.Manifest, allowed map[string]*OperationOverride) (*catalog.Catalog, bool, error) {
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
	prov, err := NewDeclarativeProvider(manifest, nil)
	if err != nil {
		return nil, fmt.Errorf("plugin %q declarative catalog: %w", name, err)
	}
	return prov.Catalog(), nil
}

func resolveExecutableCatalog(ctx context.Context, name string, entry *ValidationPlugin, manifest *providermanifestv1.Manifest, spec *providermanifestv1.Spec, allowed map[string]*OperationOverride) (*catalog.Catalog, bool, error) {
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
	_, hasMCP := resolvedSurfaceURL(entry, spec, SpecSurfaceMCP)
	merged, err := mergeCatalogs(name, filteredPluginCatalog, filteredAPICatalog)
	if err != nil {
		return nil, false, err
	}
	return merged, hasMCP && merged == nil, nil
}

func readStaticCatalog(name string, entry *ValidationPlugin, manifest *providermanifestv1.Manifest) (*catalog.Catalog, error) {
	if entry == nil || entry.ReadStaticCatalog == nil {
		if staticCatalogRequired(manifest) {
			return nil, fmt.Errorf("plugin %q requires %s", name, StaticCatalogFile)
		}
		return nil, nil
	}
	cat, err := entry.ReadStaticCatalog(name)
	if err != nil {
		return nil, fmt.Errorf("plugin %q static catalog: %w", name, err)
	}
	if cat == nil && staticCatalogRequired(manifest) {
		return nil, fmt.Errorf("plugin %q requires %s", name, StaticCatalogFile)
	}
	return cat, nil
}

func loadConfiguredAPICatalog(ctx context.Context, name string, entry *ValidationPlugin, spec *providermanifestv1.Spec, allowed map[string]*OperationOverride) (*catalog.Catalog, error) {
	var merged *catalog.Catalog
	for _, surface := range []SpecSurface{SpecSurfaceOpenAPI, SpecSurfaceGraphQL} {
		url, ok := resolvedSurfaceURL(entry, spec, surface)
		if !ok {
			continue
		}
		if surface == SpecSurfaceGraphQL {
			continue
		}
		if entry == nil || entry.LoadAPICatalog == nil {
			return nil, fmt.Errorf("plugin %q %s catalog loader is not configured", name, surface)
		}
		apiCatalog, err := entry.LoadAPICatalog(ctx, name, surface, url, allowed)
		if err != nil {
			return nil, fmt.Errorf("plugin %q %s catalog: %w", name, surface, err)
		}
		merged, err = mergeCatalogs(name, merged, apiCatalog)
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func resolvedSurfaceURL(entry *ValidationPlugin, spec *providermanifestv1.Spec, surface SpecSurface) (string, bool) {
	return entry.surfaceURL(spec, surface)
}

func applyOperationExposure(cat *catalog.Catalog, allowed map[string]*OperationOverride) (*catalog.Catalog, error) {
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

func validateAllowedOperationCoverage(name string, allowed map[string]*OperationOverride, catalogs ...*catalog.Catalog) error {
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

func effectiveAllowedOperations(entry *ValidationPlugin, spec *providermanifestv1.Spec) map[string]*OperationOverride {
	if entry != nil && entry.AllowedOperations != nil {
		return entry.AllowedOperations
	}
	if spec == nil {
		return nil
	}
	return spec.AllowedOperations
}

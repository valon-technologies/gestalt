package bootstrap

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/composite"
	"github.com/valon-technologies/gestalt/server/internal/config"
	graphqlupstream "github.com/valon-technologies/gestalt/server/internal/graphql"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/openapi"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func newDeclarativeProvider(manifest *pluginmanifestv1.Manifest, meta providerMetadata) (core.Provider, error) {
	return pluginhost.NewDeclarativeProvider(
		manifest,
		nil,
		pluginhost.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
	)
}

type specProviderConfig struct {
	overlay              provider.DefinitionOverlay
	allowedOperations    map[string]*config.OperationOverride
	providerBuildOptions func(config.ConnectionDef) []provider.BuildOption
}

type specAuthFallback struct {
	definition     *provider.Definition
	connectionName string
}

func buildExternalPluginProvider(ctx context.Context, compiled *compiledIntegration, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	pluginProv, err := buildPluginProvider(ctx, compiled)
	if err != nil {
		return nil, err
	}

	if compiled.manifestProvider != nil && compiled.manifestProvider.IsDeclarative() {
		declarative, err := newDeclarativeProvider(compiled.manifest, compiled.meta)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, fmt.Errorf("create declarative provider %q: %w", compiled.name, err)
		}
		apiProv, err := applyAllowedOperations(compiled.name, compiled.allowedOperations, declarative)
		if err != nil {
			closeIfPossible(declarative, pluginProv)
			return nil, err
		}
		merged, err := composite.NewMergedWithConnections(
			compiled.name,
			compiled.meta.displayNameOr(pluginProv.DisplayName()),
			compiled.meta.descriptionOr(pluginProv.Description()),
			compiled.meta.iconSVGOr(firstProviderIconSVG(pluginProv, apiProv)),
			composite.BoundProvider{Provider: pluginProv, Connection: config.PluginConnectionName},
			composite.BoundProvider{Provider: apiProv, Connection: compiled.connectionPlan.apiConnection()},
		)
		if err != nil {
			closeIfPossible(apiProv, pluginProv)
			return nil, err
		}
		return compiled.newProviderBuildResult(merged, nil, deps, regStore)
	}

	resolved, hasSpecSurface := compiled.connectionPlan.configuredSpecSurface()
	if !hasSpecSurface {
		restricted, err := applyAllowedOperations(compiled.name, compiled.allowedOperations, pluginProv)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, err
		}
		return compiled.newProviderBuildResult(restricted, nil, deps, regStore)
	}

	specProv, _, err := buildConfiguredSpecProvider(ctx, compiled.name, resolved, compiled.executableSpecProviderConfig(deps), deps)
	if err != nil {
		closeIfPossible(pluginProv)
		return nil, fmt.Errorf("build hybrid spec provider %q: %w", compiled.name, err)
	}
	merged, err := composite.NewMergedWithConnections(
		compiled.name,
		compiled.meta.displayNameOr(pluginProv.DisplayName()),
		compiled.meta.descriptionOr(pluginProv.Description()),
		compiled.meta.iconSVGOr(firstProviderIconSVG(pluginProv, specProv)),
		composite.BoundProvider{Provider: pluginProv, Connection: config.PluginConnectionName},
		composite.BoundProvider{Provider: specProv, Connection: resolved.connectionName},
	)
	if err != nil {
		closeIfPossible(specProv, pluginProv)
		return nil, err
	}

	return compiled.newProviderBuildResult(merged, nil, deps, regStore)
}

func loadConfiguredAPIDefinition(ctx context.Context, name string, resolved resolvedSpecSurface, cfg specProviderConfig) (*provider.Definition, error) {
	def, err := loadSpecDefinition(ctx, name, resolved, cfg.allowedOperations)
	if err != nil {
		return nil, fmt.Errorf("load %s definition: %w", resolved.surface, err)
	}
	if err := provider.ApplyDefinitionOverlay(def, cfg.overlay); err != nil {
		return nil, err
	}
	return def, nil
}

func buildConfiguredSpecProvider(ctx context.Context, name string, resolved resolvedSpecSurface, cfg specProviderConfig, deps Deps) (core.Provider, *provider.Definition, error) {
	var buildOpts []provider.BuildOption
	if cfg.providerBuildOptions != nil {
		buildOpts = cfg.providerBuildOptions(resolved.connection)
	}

	switch resolved.surface {
	case config.SpecSurfaceOpenAPI, config.SpecSurfaceGraphQL:
		def, err := loadConfiguredAPIDefinition(ctx, name, resolved, cfg)
		if err != nil {
			return nil, nil, err
		}
		prov, err := provider.Build(def, resolved.connection, buildOpts...)
		if err != nil {
			return nil, nil, err
		}
		return prov, def, nil

	case config.SpecSurfaceMCP:
		connMode := core.ConnectionMode(resolved.connection.Mode)
		if connMode == "" {
			connMode = core.ConnectionModeUser
		}
		up, err := mcpupstream.New(
			ctx,
			name,
			resolved.url,
			connMode,
			cfg.overlay.Headers,
			deps.Egress.Resolver,
			mcpupstream.WithMetadataOverrides(cfg.overlay.DisplayName, cfg.overlay.Description, cfg.overlay.IconSVG),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("create mcp upstream: %w", err)
		}
		if cfg.allowedOperations != nil {
			if err := up.FilterOperations(cfg.allowedOperations); err != nil {
				_ = up.Close()
				return nil, nil, fmt.Errorf("filter mcp operations: %w", err)
			}
		}
		return up, nil, nil

	default:
		return nil, nil, fmt.Errorf("unsupported spec surface %q", resolved.surface)
	}
}

func loadSpecDefinition(ctx context.Context, name string, resolved resolvedSpecSurface, allowedOperations map[string]*config.OperationOverride) (*provider.Definition, error) {
	switch resolved.surface {
	case config.SpecSurfaceOpenAPI:
		return openapi.LoadDefinition(ctx, name, resolved.url, allowedOperations)
	case config.SpecSurfaceGraphQL:
		return graphqlupstream.LoadDefinition(ctx, name, resolved.url, allowedOperations)
	default:
		return nil, fmt.Errorf("unsupported spec definition surface %q", resolved.surface)
	}
}

func mcpOAuthBuildOpts(conn config.ConnectionDef, mcpURL string, regStore *lazyRegStore, deps Deps) []provider.BuildOption {
	if conn.Auth.Type != pluginmanifestv1.AuthTypeMCPOAuth || mcpURL == "" {
		return nil
	}
	handler := buildMCPOAuthHandler(conn, mcpURL, regStore.get(), deps)
	return []provider.BuildOption{provider.WithAuthHandler(handler)}
}

func buildSpecLoadedProvider(ctx context.Context, compiled *compiledIntegration, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	apiResolved, hasAPI := compiled.connectionPlan.configuredAPISurface()
	mcpResolved, hasMCP := compiled.connectionPlan.resolvedSurface(config.SpecSurfaceMCP)
	if !hasAPI && !hasMCP {
		return nil, fmt.Errorf("build spec-loaded provider %q: no spec URL", compiled.name)
	}

	buildSpec := func(resolved resolvedSpecSurface, allowed map[string]*config.OperationOverride) (core.Provider, error) {
		prov, _, err := buildConfiguredSpecProvider(ctx, compiled.name, resolved, compiled.manifestSpecProviderConfig(allowed, regStore, deps), deps)
		return prov, err
	}

	if !hasAPI {
		prov, err := buildSpec(mcpResolved, compiled.allowedOperations)
		if err != nil {
			return nil, fmt.Errorf("build spec-loaded provider %q: %w", compiled.name, err)
		}
		return compiled.newProviderBuildResult(prov, nil, deps, regStore)
	}

	apiProv, apiDef, err := buildConfiguredSpecProvider(ctx, compiled.name, apiResolved, compiled.manifestSpecProviderConfig(compiled.allowedOperations, regStore, deps), deps)
	if err != nil {
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", compiled.name, err)
	}
	authFallback := &specAuthFallback{
		definition:     apiDef,
		connectionName: apiResolved.connectionName,
	}

	if !hasMCP {
		return compiled.newProviderBuildResult(apiProv, authFallback, deps, regStore)
	}

	mcpProv, err := buildSpec(mcpResolved, nil)
	if err != nil {
		closeIfPossible(apiProv)
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", compiled.name, err)
	}
	mcpUp, ok := mcpProv.(composite.MCPUpstream)
	if !ok {
		closeIfPossible(mcpProv, apiProv)
		return nil, fmt.Errorf("build spec-loaded provider %q: unexpected mcp provider type %T", compiled.name, mcpProv)
	}

	filtered := matchingAllowedOperations(compiled.allowedOperations, mcpUp.Catalog())
	if len(filtered) > 0 {
		filterable, ok := any(mcpUp).(interface {
			FilterOperations(map[string]*config.OperationOverride) error
		})
		if !ok {
			closeIfPossible(mcpUp, apiProv)
			return nil, fmt.Errorf("build spec-loaded provider %q: unexpected non-filterable mcp provider type %T", compiled.name, mcpProv)
		}
		if err := filterable.FilterOperations(filtered); err != nil {
			closeIfPossible(mcpUp, apiProv)
			return nil, fmt.Errorf("build spec-loaded provider %q: filter mcp operations: %w", compiled.name, err)
		}
	}

	return compiled.newProviderBuildResult(composite.New(compiled.name, apiProv, mcpUp), authFallback, deps, regStore)
}

func (compiled *compiledIntegration) executableSpecProviderConfig(deps Deps) specProviderConfig {
	return specProviderConfig{
		overlay:           compiled.specOverlay,
		allowedOperations: compiled.allowedOperations,
		providerBuildOptions: func(config.ConnectionDef) []provider.BuildOption {
			return []provider.BuildOption{provider.WithEgressResolver(deps.Egress.Resolver)}
		},
	}
}

func (compiled *compiledIntegration) manifestSpecProviderConfig(allowedOperations map[string]*config.OperationOverride, regStore *lazyRegStore, deps Deps) specProviderConfig {
	return specProviderConfig{
		overlay:           compiled.specOverlay,
		allowedOperations: allowedOperations,
		providerBuildOptions: func(conn config.ConnectionDef) []provider.BuildOption {
			return mcpOAuthBuildOpts(conn, compiled.mcpURL(), regStore, deps)
		},
	}
}

func firstProviderIconSVG(providers ...core.Provider) string {
	for _, prov := range providers {
		cat := prov.Catalog()
		if cat != nil && cat.IconSVG != "" {
			return cat.IconSVG
		}
	}
	return ""
}

func matchingAllowedOperations(allowed map[string]*config.OperationOverride, cat *catalog.Catalog) map[string]*config.OperationOverride {
	if len(allowed) == 0 || cat == nil || len(cat.Operations) == 0 {
		return nil
	}
	available := make(map[string]struct{}, len(cat.Operations))
	for i := range cat.Operations {
		available[cat.Operations[i].ID] = struct{}{}
	}
	filtered := make(map[string]*config.OperationOverride)
	for name, override := range allowed {
		if _, ok := available[name]; ok {
			filtered[name] = override
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

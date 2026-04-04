package bootstrap

import (
	"context"
	"fmt"
	"strings"

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

func buildExternalPluginProvider(ctx context.Context, name string, intg config.IntegrationDef, pluginConfig map[string]any, meta providerMetadata, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	pluginProv, err := buildPluginProvider(ctx, name, intg, pluginConfig, meta)
	if err != nil {
		return nil, err
	}
	manifest := intg.Plugin.ResolvedManifest
	manifestProvider := intg.Plugin.ManifestProvider()
	allowedOperations := intg.Plugin.AllowedOperations
	specPlugin := intg.Plugin
	if intg.Plugin.HasResolvedManifest() {
		resolvedManifest, resolvedAllowedOperations, err := config.EffectiveManifestBackedInputs(name, intg.Plugin)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, err
		}
		manifest = resolvedManifest
		if manifest != nil {
			manifestProvider = manifest.Provider
		}
		allowedOperations = resolvedAllowedOperations
		specPlugin = nil
	}
	plan, err := buildPluginConnectionPlan(intg.Plugin, manifestProvider)
	if err != nil {
		closeIfPossible(pluginProv)
		return nil, fmt.Errorf("build external plugin provider %q: %w", name, err)
	}

	if manifestProvider != nil && manifestProvider.IsDeclarative() {
		declarative, err := newDeclarativeProvider(manifest, meta)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, fmt.Errorf("create declarative provider %q: %w", name, err)
		}
		apiProv, err := applyAllowedOperations(name, allowedOperations, declarative)
		if err != nil {
			closeIfPossible(declarative, pluginProv)
			return nil, err
		}
		merged, err := composite.NewMergedWithConnections(
			name,
			meta.displayNameOr(pluginProv.DisplayName()),
			meta.descriptionOr(pluginProv.Description()),
			meta.iconSVGOr(firstProviderIconSVG(pluginProv, apiProv)),
			composite.BoundProvider{Provider: pluginProv, Connection: config.PluginConnectionName},
			composite.BoundProvider{Provider: apiProv, Connection: plan.apiConnection()},
		)
		if err != nil {
			closeIfPossible(apiProv, pluginProv)
			return nil, err
		}
		return newProviderBuildResult(name, intg, manifest, pluginConfig, merged, nil, deps, regStore)
	}

	resolved, hasSpecSurface := plan.configuredSpecSurface()
	if !hasSpecSurface {
		restricted, err := applyAllowedOperations(name, allowedOperations, pluginProv)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, err
		}
		return newProviderBuildResult(name, intg, manifest, pluginConfig, restricted, nil, deps, regStore)
	}

	baseURL := intg.Plugin.BaseURL
	if manifestProvider != nil {
		baseURL = manifestProvider.BaseURL
	}
	specProv, _, err := buildConfiguredSpecProvider(ctx, name, resolved, meta, specProviderConfig{
		plugin:               specPlugin,
		manifestProvider:     manifestProvider,
		allowedOperations:    allowedOperations,
		baseURL:              baseURL,
		applyResponseMapping: true,
		providerBuildOptions: func(config.ConnectionDef) []provider.BuildOption {
			return []provider.BuildOption{provider.WithEgressResolver(deps.Egress.Resolver)}
		},
	}, deps)
	if err != nil {
		closeIfPossible(pluginProv)
		return nil, fmt.Errorf("build hybrid spec provider %q: %w", name, err)
	}
	merged, err := composite.NewMergedWithConnections(
		name,
		meta.displayNameOr(pluginProv.DisplayName()),
		meta.descriptionOr(pluginProv.Description()),
		meta.iconSVGOr(firstProviderIconSVG(pluginProv, specProv)),
		composite.BoundProvider{Provider: pluginProv, Connection: config.PluginConnectionName},
		composite.BoundProvider{Provider: specProv, Connection: resolved.connectionName},
	)
	if err != nil {
		closeIfPossible(specProv, pluginProv)
		return nil, err
	}

	return newProviderBuildResult(name, intg, manifest, pluginConfig, merged, nil, deps, regStore)
}

type specProviderConfig struct {
	plugin               *config.PluginDef
	manifestProvider     *pluginmanifestv1.Provider
	allowedOperations    map[string]*config.OperationOverride
	baseURL              string
	providerBuildOptions func(config.ConnectionDef) []provider.BuildOption
	applyResponseMapping bool
}

func loadConfiguredAPIDefinition(ctx context.Context, name string, resolved resolvedSpecSurface, meta providerMetadata, cfg specProviderConfig) (*provider.Definition, error) {
	def, err := loadSpecDefinition(ctx, name, resolved, cfg.allowedOperations)
	if err != nil {
		return nil, fmt.Errorf("load %s definition: %w", resolved.surface, err)
	}
	if cfg.baseURL != "" {
		def.BaseURL = cfg.baseURL
	}
	applyPluginHeaders(def, cfg.plugin, cfg.manifestProvider)
	if err := applyManagedParameters(def, cfg.plugin, cfg.manifestProvider); err != nil {
		return nil, err
	}
	if cfg.applyResponseMapping {
		applyProviderResponseMapping(def, cfg.manifestProvider, cfg.plugin)
	}
	meta.applyToDefinition(def)
	return def, nil
}

func buildConfiguredSpecProvider(ctx context.Context, name string, resolved resolvedSpecSurface, meta providerMetadata, cfg specProviderConfig, deps Deps) (core.Provider, *provider.Definition, error) {
	var buildOpts []provider.BuildOption
	if cfg.providerBuildOptions != nil {
		buildOpts = cfg.providerBuildOptions(resolved.connection)
	}

	switch resolved.surface {
	case config.SpecSurfaceOpenAPI, config.SpecSurfaceGraphQL:
		def, err := loadConfiguredAPIDefinition(ctx, name, resolved, meta, cfg)
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
			config.MergedProviderHeaders(cfg.manifestProvider, cfg.plugin),
			deps.Egress.Resolver,
			mcpupstream.WithMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
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

type specAuthFallback struct {
	definition     *provider.Definition
	connectionName string
}

func newProviderBuildResult(name string, intg config.IntegrationDef, manifest *pluginmanifestv1.Manifest, pluginConfig map[string]any, prov core.Provider, authFallback *specAuthFallback, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	result := &ProviderBuildResult{Provider: prov}
	var err error
	result.ConnectionAuth, err = buildConnectionAuthMap(name, intg, manifest, pluginConfig, authFallback, deps, regStore)
	if err != nil {
		closeIfPossible(prov)
		return nil, err
	}
	return result, nil
}

func mcpOAuthBuildOpts(conn config.ConnectionDef, mp *pluginmanifestv1.Provider, regStore *lazyRegStore, deps Deps) []provider.BuildOption {
	if conn.Auth.Type != pluginmanifestv1.AuthTypeMCPOAuth || mp.MCPURL == "" {
		return nil
	}
	handler := buildMCPOAuthHandler(conn, mp.MCPURL, regStore.get(), deps)
	return []provider.BuildOption{provider.WithAuthHandler(handler)}
}

func manifestSpecProviderConfig(mp *pluginmanifestv1.Provider, allowedOperations map[string]*config.OperationOverride, regStore *lazyRegStore, deps Deps) specProviderConfig {
	return specProviderConfig{
		manifestProvider:     mp,
		allowedOperations:    allowedOperations,
		baseURL:              mp.BaseURL,
		applyResponseMapping: true,
		providerBuildOptions: func(conn config.ConnectionDef) []provider.BuildOption {
			return mcpOAuthBuildOpts(conn, mp, regStore, deps)
		},
	}
}

func buildSpecLoadedProvider(ctx context.Context, name string, intg config.IntegrationDef, manifest *pluginmanifestv1.Manifest, pluginConfig map[string]any, meta providerMetadata, deps Deps, regStore *lazyRegStore, allowedOperations map[string]*config.OperationOverride) (*ProviderBuildResult, error) {
	mp := manifest.Provider
	plan, err := buildPluginConnectionPlan(intg.Plugin, mp)
	if err != nil {
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}
	apiResolved, hasAPI := plan.configuredAPISurface()
	mcpResolved, hasMCP := plan.resolvedSurface(config.SpecSurfaceMCP)
	if !hasAPI && !hasMCP {
		return nil, fmt.Errorf("build spec-loaded provider %q: no spec URL", name)
	}

	buildSpec := func(resolved resolvedSpecSurface, allowed map[string]*config.OperationOverride) (core.Provider, error) {
		prov, _, err := buildConfiguredSpecProvider(ctx, name, resolved, meta, manifestSpecProviderConfig(mp, allowed, regStore, deps), deps)
		return prov, err
	}

	if !hasAPI {
		prov, err := buildSpec(mcpResolved, allowedOperations)
		if err != nil {
			return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
		}
		return newProviderBuildResult(name, intg, manifest, pluginConfig, prov, nil, deps, regStore)
	}

	apiProv, apiDef, err := buildConfiguredSpecProvider(ctx, name, apiResolved, meta, manifestSpecProviderConfig(mp, allowedOperations, regStore, deps), deps)
	if err != nil {
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}
	authFallback := &specAuthFallback{
		definition:     apiDef,
		connectionName: apiResolved.connectionName,
	}

	if !hasMCP {
		return newProviderBuildResult(name, intg, manifest, pluginConfig, apiProv, authFallback, deps, regStore)
	}

	mcpProv, err := buildSpec(mcpResolved, nil)
	if err != nil {
		closeIfPossible(apiProv)
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}
	mcpUp, ok := mcpProv.(composite.MCPUpstream)
	if !ok {
		closeIfPossible(mcpProv, apiProv)
		return nil, fmt.Errorf("build spec-loaded provider %q: unexpected mcp provider type %T", name, mcpProv)
	}

	filtered := matchingAllowedOperations(allowedOperations, mcpUp.Catalog())
	if len(filtered) > 0 {
		filterable, ok := any(mcpUp).(interface {
			FilterOperations(map[string]*config.OperationOverride) error
		})
		if !ok {
			closeIfPossible(mcpUp, apiProv)
			return nil, fmt.Errorf("build spec-loaded provider %q: unexpected non-filterable mcp provider type %T", name, mcpProv)
		}
		if err := filterable.FilterOperations(filtered); err != nil {
			closeIfPossible(mcpUp, apiProv)
			return nil, fmt.Errorf("build spec-loaded provider %q: filter mcp operations: %w", name, err)
		}
	}

	return newProviderBuildResult(name, intg, manifest, pluginConfig, composite.New(name, apiProv, mcpUp), authFallback, deps, regStore)
}

func applyPluginHeaders(def *provider.Definition, plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) {
	if def == nil {
		return
	}
	headers := config.MergedProviderHeaders(manifestProvider, plugin)
	if len(headers) == 0 {
		return
	}
	def.Headers = headers
}

func applyManagedParameters(def *provider.Definition, plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) error {
	if def == nil {
		return nil
	}

	params := config.MergedProviderManagedParameters(manifestProvider, plugin)
	if len(params) == 0 {
		return nil
	}

	if def.Headers == nil {
		def.Headers = make(map[string]string, len(params))
	}
	for _, param := range params {
		switch param.In {
		case config.ManagedParameterInHeader:
			if _, exists := def.Headers[param.Name]; exists {
				return fmt.Errorf("managed parameter %q conflicts with configured header", param.Name)
			}
			def.Headers[param.Name] = param.Value
		case config.ManagedParameterInPath:
		default:
			return fmt.Errorf("unsupported managed parameter location %q", param.In)
		}
	}

	for opName := range def.Operations {
		op := def.Operations[opName]
		for _, param := range params {
			if param.In != config.ManagedParameterInPath {
				continue
			}
			op.Path = strings.ReplaceAll(op.Path, "{"+param.Name+"}", param.Value)
		}
		filtered := op.Parameters[:0]
		for _, param := range op.Parameters {
			if isManagedOperationParameter(param, params) {
				continue
			}
			filtered = append(filtered, param)
		}
		op.Parameters = filtered
		def.Operations[opName] = op
	}

	return nil
}

func isManagedOperationParameter(param provider.ParameterDef, managed []pluginmanifestv1.ManagedParameter) bool {
	location := strings.ToLower(param.Location)
	if location == "" {
		return false
	}

	wireName := param.WireName
	if wireName == "" {
		wireName = param.Name
	}
	target := config.NormalizeManagedParameter(pluginmanifestv1.ManagedParameter{In: location, Name: wireName})

	for _, managedParam := range managed {
		if managedParam.In == target.In && managedParam.Name == target.Name {
			return true
		}
	}
	return false
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

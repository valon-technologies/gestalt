package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/composite"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/graphql"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/mcpoauth"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/openapi"
	"github.com/valon-technologies/gestalt/server/internal/operationexposure"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

func buildRegistrationStore(deps Deps) mcpoauth.RegistrationStore {
	if deps.Services == nil {
		return nil
	}
	if store, ok := metricutil.UnwrapIndexedDB(deps.Services.DB).(mcpoauth.RegistrationStore); ok {
		return store
	}
	return nil
}

func buildProviders(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (*registry.ProviderMap[core.Provider], <-chan struct{}, func() map[string]map[string]OAuthHandler, error) {
	providers, ready, connAuthResolver, _, err := buildProvidersAsync(ctx, cfg, factories, deps, buildProvider)
	return providers, ready, connAuthResolver, err
}

func buildProvidersAsync(
	ctx context.Context,
	cfg *config.Config,
	factories *FactoryRegistry,
	deps Deps,
	builder func(context.Context, string, *config.ProviderEntry, Deps) (*ProviderBuildResult, error),
) (*registry.ProviderMap[core.Provider], <-chan struct{}, func() map[string]map[string]OAuthHandler, func() []error, error) {
	reg := registry.New()
	connAuth := make(map[string]map[string]OAuthHandler)
	var buildErrs []error
	var connMu sync.Mutex

	for _, builtin := range factories.Builtins {
		if err := validateProviderConnectionMode(builtin.Name(), builtin.ConnectionMode()); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("bootstrap: builtin provider %q: %w", builtin.Name(), err)
		}
		if err := reg.Providers.Register(builtin.Name(), builtin); errors.Is(err, core.ErrAlreadyRegistered) {
			continue
		} else if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("bootstrap: registering builtin %q: %w", builtin.Name(), err)
		}
		slog.Info("loaded builtin provider", "provider", builtin.Name(), "operations", catalogOperationCount(builtin.Catalog()))
	}

	ready := make(chan struct{})
	if len(cfg.Plugins) == 0 {
		close(ready)
		return &reg.Providers, ready, func() map[string]map[string]OAuthHandler { return connAuth }, func() []error { return nil }, nil
	}

	var wg sync.WaitGroup
	var errMu sync.Mutex
	for name := range cfg.Plugins {
		intgDef := cfg.Plugins[name]
		var proxy *startupProviderProxy
		if deps.WorkflowRuntime != nil {
			spec, operationConnections, err := buildStartupProviderSpec(name, intgDef)
			if err != nil {
				slog.Warn("building startup provider proxy metadata failed", "provider", name, "error", err)
			} else {
				proxy = newStartupProviderProxy(spec, operationConnections, deps.WorkflowRuntime.StartupWaitTracker())
				if err := reg.Providers.Register(name, proxy); err != nil {
					errMu.Lock()
					buildErrs = append(buildErrs, fmt.Errorf("integration %q: %w", name, err))
					errMu.Unlock()
					slog.Warn("registering startup provider proxy failed", "provider", name, "error", err)
					proxy = nil
				}
			}
		}
		wg.Add(1)
		go func(name string, intgDef *config.ProviderEntry, proxy *startupProviderProxy) {
			defer wg.Done()
			result, err := builder(ctx, name, intgDef, deps)
			if err != nil {
				errMu.Lock()
				buildErrs = append(buildErrs, fmt.Errorf("integration %q: %w", name, err))
				errMu.Unlock()
				if proxy != nil {
					proxy.fail(err)
					reg.Providers.Remove(name)
				}
				slog.Warn("skipping provider", "provider", name, "error", err)
				return
			}
			if err := validateProviderConnectionMode(name, result.Provider.ConnectionMode()); err != nil {
				errMu.Lock()
				buildErrs = append(buildErrs, fmt.Errorf("integration %q: %w", name, err))
				errMu.Unlock()
				if proxy != nil {
					proxy.fail(err)
					reg.Providers.Remove(name)
				}
				closeIfPossible(result.Provider)
				slog.Warn("skipping provider", "provider", name, "error", err)
				return
			}
			if proxy != nil {
				if err := reg.Providers.Replace(name, result.Provider); err != nil {
					errMu.Lock()
					buildErrs = append(buildErrs, fmt.Errorf("integration %q: %w", name, err))
					errMu.Unlock()
					proxy.fail(err)
					reg.Providers.Remove(name)
					closeIfPossible(result.Provider)
					slog.Warn("replacing startup provider proxy failed", "provider", name, "error", err)
					return
				}
				proxy.publish(result.Provider)
			} else {
				if err := reg.Providers.Register(name, result.Provider); err != nil {
					errMu.Lock()
					buildErrs = append(buildErrs, fmt.Errorf("integration %q: %w", name, err))
					errMu.Unlock()
					closeIfPossible(result.Provider)
					slog.Warn("registering provider failed", "provider", name, "error", err)
					return
				}
			}
			if len(result.ConnectionAuth) > 0 {
				connMu.Lock()
				connAuth[name] = result.ConnectionAuth
				connMu.Unlock()
			}
			slog.Info("loaded provider", "provider", name, "operations", catalogOperationCount(result.Provider.Catalog()))
		}(name, intgDef, proxy)
	}

	go func() {
		wg.Wait()
		close(ready)
	}()

	resolver := func() map[string]map[string]OAuthHandler {
		<-ready
		return connAuth
	}
	errResolver := func() []error {
		<-ready
		errMu.Lock()
		defer errMu.Unlock()
		return append([]error(nil), buildErrs...)
	}
	return &reg.Providers, ready, resolver, errResolver, nil
}

func validateProviderConnectionMode(provider string, mode core.ConnectionMode) error {
	switch core.NormalizeConnectionMode(mode) {
	case core.ConnectionModeNone, core.ConnectionModeUser:
		return nil
	default:
		return fmt.Errorf("unsupported connection mode %q for provider %q", mode, provider)
	}
}

func buildStartupProviderSpec(name string, entry *config.ProviderEntry) (providerhost.StaticProviderSpec, map[string]string, error) {
	if entry == nil {
		return providerhost.StaticProviderSpec{}, nil, fmt.Errorf("integration %q has no plugin defined", name)
	}
	manifest := entry.ResolvedManifest
	manifestPlugin := entry.ManifestSpec()
	if manifest == nil || manifestPlugin == nil {
		return providerhost.StaticProviderSpec{}, nil, fmt.Errorf("integration %q must resolve to a provider manifest", name)
	}

	meta := resolveProviderMetadata(entry)
	spec, plan, err := buildPluginStaticSpec(name, entry, manifest, meta)
	if err != nil {
		return providerhost.StaticProviderSpec{}, nil, err
	}
	if spec.Catalog != nil {
		return spec, operationConnectionsForCatalog(spec.Catalog, plan), nil
	}
	if !manifestPlugin.IsDeclarative() && !manifestPlugin.IsSpecLoaded() {
		return spec, map[string]string{}, nil
	}
	declarative, err := providerhost.NewDeclarativeProvider(
		manifest,
		nil,
		providerhost.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
		providerhost.WithDeclarativeConnectionMode(plan.ConnectionMode()),
	)
	if err != nil {
		return providerhost.StaticProviderSpec{}, nil, err
	}
	spec.Catalog = declarative.Catalog()
	return spec, operationConnectionsForCatalog(spec.Catalog, plan), nil
}

func operationConnectionsForCatalog(cat *catalog.Catalog, plan config.StaticConnectionPlan) map[string]string {
	if cat == nil {
		return map[string]string{}
	}
	operationConnections := make(map[string]string, len(cat.Operations))
	pluginConnection := hybridPluginOperationConnection(plan, configuredSpecConnection(plan))
	for i := range cat.Operations {
		operation := &cat.Operations[i]
		connection := pluginConnection
		switch operation.Transport {
		case catalog.TransportREST:
			connection = plan.APIConnection()
		case "graphql":
			if resolved, ok := plan.ResolvedSurface(config.SpecSurfaceGraphQL); ok {
				connection = resolved.ConnectionName
			} else {
				connection = plan.APIConnection()
			}
		case catalog.TransportMCPPassthrough:
			connection = plan.MCPConnection()
		}
		if connection != "" {
			operationConnections[operation.ID] = connection
		}
	}
	return operationConnections
}

func configuredSpecConnection(plan config.StaticConnectionPlan) string {
	if resolved, ok := plan.ConfiguredSpecSurface(); ok {
		return resolved.ConnectionName
	}
	return ""
}

func hybridPluginOperationConnection(plan config.StaticConnectionPlan, specConnection string) string {
	if explicitPluginConnection(plan) {
		return config.PluginConnectionName
	}
	if specConnection != "" && specConnection != config.PluginConnectionName {
		return specConnection
	}
	if fallback := plan.AuthDefaultConnection(); fallback != "" {
		return fallback
	}
	return config.PluginConnectionName
}

func explicitPluginConnection(plan config.StaticConnectionPlan) bool {
	if !reflect.DeepEqual(plan.PluginConnection(), config.ConnectionDef{}) {
		return true
	}
	return plan.AuthDefaultConnection() == config.PluginConnectionName && len(plan.NamedConnectionNames()) > 0
}

func buildProvider(ctx context.Context, name string, entry *config.ProviderEntry, deps Deps) (*ProviderBuildResult, error) {
	if entry == nil {
		return nil, fmt.Errorf("integration %q has no plugin defined", name)
	}

	meta := resolveProviderMetadata(entry)
	pluginConfig, err := config.NodeToMap(entry.Config)
	if err != nil {
		return nil, fmt.Errorf("decode plugin config for %q: %w", name, err)
	}

	manifest := entry.ResolvedManifest
	manifestPlugin := entry.ManifestSpec()
	if manifest == nil || manifestPlugin == nil {
		return nil, fmt.Errorf("integration %q must resolve to a provider manifest", name)
	}

	allowedOperations := entry.AllowedOperations
	if allowedOperations == nil {
		allowedOperations = maps.Clone(manifestPlugin.AllowedOperations)
	}

	switch {
	case manifestPlugin.IsSpecLoaded() && manifest.Entrypoint == nil:
		return buildSpecLoadedProvider(ctx, name, entry, manifest, pluginConfig, meta, deps, allowedOperations)
	case manifestPlugin.IsDeclarative() && manifest.Entrypoint == nil:
		plan, err := config.BuildStaticConnectionPlan(entry, manifestPlugin)
		if err != nil {
			return nil, fmt.Errorf("build declarative provider %q: %w", name, err)
		}
		declarative, err := providerhost.NewDeclarativeProvider(
			manifest,
			nil,
			providerhost.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
			providerhost.WithDeclarativeConnectionMode(plan.ConnectionMode()),
		)
		if err != nil {
			return nil, fmt.Errorf("create declarative provider %q: %w", name, err)
		}
		prov, err := applyAllowedOperations(name, allowedOperations, declarative)
		if err != nil {
			closeIfPossible(declarative)
			return nil, err
		}
		return newProviderBuildResult(name, entry, manifest, pluginConfig, prov, nil, deps)
	default:
		return buildExecutablePluginProvider(ctx, name, entry, pluginConfig, meta, deps)
	}
}

func buildExecutablePluginProvider(ctx context.Context, name string, entry *config.ProviderEntry, pluginConfig map[string]any, meta providerMetadata, deps Deps) (*ProviderBuildResult, error) {
	manifest := entry.ResolvedManifest
	manifestPlugin := entry.ManifestSpec()
	if manifest == nil || manifestPlugin == nil {
		return nil, fmt.Errorf("build executable plugin provider %q: resolved manifest is required", name)
	}
	staticSpec, plan, err := buildPluginStaticSpec(name, entry, manifest, meta)
	if err != nil {
		return nil, fmt.Errorf("build executable plugin provider %q: %w", name, err)
	}
	pluginProv, err := buildPluginProvider(ctx, name, entry, pluginConfig, staticSpec, deps)
	if err != nil {
		return nil, err
	}
	allowedOperations := entry.AllowedOperations
	if allowedOperations == nil && manifestPlugin != nil {
		allowedOperations = maps.Clone(manifestPlugin.AllowedOperations)
	}
	staticAllowedOperations := operationexposure.MatchingAllowedOperations(allowedOperations, pluginProv.Catalog())

	if manifestPlugin.IsDeclarative() {
		filteredPluginProv, err := applyAllowedOperations(name, staticAllowedOperations, pluginProv)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, err
		}
		pluginProv = filteredPluginProv
		declarative, err := providerhost.NewDeclarativeProvider(
			manifest,
			nil,
			providerhost.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
			providerhost.WithDeclarativeConnectionMode(plan.ConnectionMode()),
		)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, fmt.Errorf("create declarative provider %q: %w", name, err)
		}
		apiAllowedOperations := operationexposure.MatchingAllowedOperations(allowedOperations, declarative.Catalog())
		apiProv, err := applyAllowedOperations(name, apiAllowedOperations, declarative)
		if err != nil {
			closeIfPossible(apiProv, pluginProv)
			return nil, err
		}
		merged, err := composite.NewMergedWithConnections(
			name,
			pluginProv.DisplayName(),
			pluginProv.Description(),
			firstProviderIconSVG(pluginProv, apiProv),
			composite.BoundProvider{Provider: pluginProv, Connection: hybridPluginOperationConnection(plan, plan.APIConnection())},
			composite.BoundProvider{Provider: apiProv, Connection: plan.APIConnection()},
		)
		if err != nil {
			closeIfPossible(apiProv, pluginProv)
			return nil, err
		}
		return newProviderBuildResult(name, entry, manifest, pluginConfig, merged, nil, deps)
	}

	specProv, authFallback, err := buildConfiguredSpecComposite(ctx, name, entry, plan, manifestPlugin, meta, deps, allowedOperations)
	if err != nil {
		closeIfPossible(pluginProv)
		return nil, fmt.Errorf("build hybrid spec provider %q: %w", name, err)
	}
	if specProv == nil {
		restricted, err := applyAllowedOperations(name, allowedOperations, pluginProv)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, err
		}
		return newProviderBuildResult(name, entry, manifest, pluginConfig, restricted, nil, deps)
	}
	filteredPluginProv, err := applyAllowedOperations(name, staticAllowedOperations, pluginProv)
	if err != nil {
		closeIfPossible(pluginProv)
		return nil, err
	}
	pluginProv = filteredPluginProv
	merged, err := composite.NewMergedWithConnections(
		name,
		pluginProv.DisplayName(),
		pluginProv.Description(),
		firstProviderIconSVG(pluginProv, specProv),
		composite.BoundProvider{Provider: pluginProv, Connection: hybridPluginOperationConnection(plan, configuredSpecConnection(plan))},
		composite.BoundProvider{Provider: specProv},
	)
	if err != nil {
		closeIfPossible(specProv, pluginProv)
		return nil, err
	}
	return newProviderBuildResult(name, entry, manifest, pluginConfig, merged, authFallback, deps)
}

type specProviderConfig struct {
	manifestPlugin       *providermanifestv1.Spec
	allowedOperations    map[string]*config.OperationOverride
	allowedHosts         []string
	baseURL              string
	providerBuildOptions func(config.ConnectionDef) []provider.BuildOption
	applyResponseMapping bool
}

type specAuthFallback struct {
	definitions map[string]*provider.Definition
}

func newSpecAuthFallback() *specAuthFallback {
	return &specAuthFallback{definitions: make(map[string]*provider.Definition)}
}

func (f *specAuthFallback) add(connectionName string, def *provider.Definition) {
	if f == nil || def == nil {
		return
	}
	resolvedName := config.ResolveConnectionAlias(connectionName)
	if resolvedName == "" {
		resolvedName = config.PluginConnectionName
	}
	if _, ok := f.definitions[resolvedName]; ok {
		return
	}
	f.definitions[resolvedName] = def
}

func (f *specAuthFallback) definitionFor(connectionName string) *provider.Definition {
	if f == nil {
		return nil
	}
	resolvedName := config.ResolveConnectionAlias(connectionName)
	if resolvedName == "" {
		resolvedName = config.PluginConnectionName
	}
	return f.definitions[resolvedName]
}

func (f *specAuthFallback) empty() bool {
	return f == nil || len(f.definitions) == 0
}

func newProviderBuildResult(name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, pluginConfig map[string]any, prov core.Provider, authFallback *specAuthFallback, deps Deps) (*ProviderBuildResult, error) {
	result := &ProviderBuildResult{Provider: prov}
	var err error
	result.ConnectionAuth, err = buildConnectionAuthMap(name, entry, manifest, pluginConfig, authFallback, deps)
	if err != nil {
		closeIfPossible(prov)
		return nil, err
	}
	return result, nil
}

type builtSpecSurface struct {
	provider   core.Provider
	resolved   config.ResolvedSpecSurface
	definition *provider.Definition
}

func buildSpecLoadedProvider(ctx context.Context, name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, pluginConfig map[string]any, meta providerMetadata, deps Deps, allowedOperations map[string]*config.OperationOverride) (*ProviderBuildResult, error) {
	mp := manifest.Spec
	plan, err := config.BuildStaticConnectionPlan(entry, mp)
	if err != nil {
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}

	prov, authFallback, err := buildConfiguredSpecComposite(ctx, name, entry, plan, mp, meta, deps, allowedOperations)
	if err != nil {
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}
	if prov == nil {
		return nil, fmt.Errorf("build spec-loaded provider %q: no spec URL", name)
	}
	return newProviderBuildResult(name, entry, manifest, pluginConfig, prov, authFallback, deps)
}

func buildConfiguredSpecComposite(ctx context.Context, name string, entry *config.ProviderEntry, plan config.StaticConnectionPlan, manifestPlugin *providermanifestv1.Spec, meta providerMetadata, deps Deps, allowedOperations map[string]*config.OperationOverride) (core.Provider, *specAuthFallback, error) {
	mcpResolved, hasMCP := plan.ResolvedSurface(config.SpecSurfaceMCP)
	mcpURL := ""
	if hasMCP {
		mcpURL = mcpResolved.URL
	}

	cfg := specProviderConfig{
		manifestPlugin:       manifestPlugin,
		allowedOperations:    allowedOperations,
		allowedHosts:         entry.AllowedHosts,
		baseURL:              config.EffectiveProviderSpecBaseURL(entry, manifestPlugin),
		applyResponseMapping: true,
		providerBuildOptions: func(conn config.ConnectionDef) []provider.BuildOption {
			return mcpOAuthBuildOpts(conn, mcpURL, deps)
		},
	}

	apiProv, authFallback, err := buildConfiguredAPIProvider(ctx, name, plan, meta, cfg, deps)
	if err != nil {
		return nil, nil, err
	}
	if !hasMCP {
		return apiProv, authFallback, nil
	}

	mcpProv, _, err := buildConfiguredSpecProvider(ctx, name, mcpResolved, meta, cfg, deps)
	if err != nil {
		closeIfPossible(apiProv)
		return nil, nil, err
	}
	mcpUp, ok := mcpProv.(composite.MCPUpstream)
	if !ok {
		closeIfPossible(mcpProv, apiProv)
		return nil, nil, fmt.Errorf("unexpected mcp provider type %T", mcpProv)
	}

	filtered := operationexposure.MatchingAllowedOperations(allowedOperations, mcpUp.Catalog())
	if len(filtered) > 0 {
		filterable, ok := any(mcpUp).(interface {
			FilterOperations(map[string]*config.OperationOverride) error
		})
		if !ok {
			closeIfPossible(mcpUp, apiProv)
			return nil, nil, fmt.Errorf("unexpected non-filterable mcp provider type %T", mcpProv)
		}
		if err := filterable.FilterOperations(filtered); err != nil {
			closeIfPossible(mcpUp, apiProv)
			return nil, nil, fmt.Errorf("filter mcp operations: %w", err)
		}
	}

	if apiProv == nil {
		return mcpUp, nil, nil
	}
	return composite.New(name, apiProv, mcpUp), authFallback, nil
}

func buildConfiguredAPIProvider(ctx context.Context, name string, plan config.StaticConnectionPlan, meta providerMetadata, cfg specProviderConfig, deps Deps) (core.Provider, *specAuthFallback, error) {
	resolvedSurfaces := plan.ConfiguredAPISurfaces()
	if len(resolvedSurfaces) == 0 {
		return nil, nil, nil
	}

	built := make([]builtSpecSurface, 0, len(resolvedSurfaces))
	authFallback := newSpecAuthFallback()
	for i := range resolvedSurfaces {
		resolved := resolvedSurfaces[i]
		prov, def, err := buildConfiguredSpecProvider(ctx, name, resolved, meta, cfg, deps)
		if err != nil {
			closeBuiltSpecSurfaces(built)
			return nil, nil, fmt.Errorf("build %s provider: %w", resolved.Surface, err)
		}
		built = append(built, builtSpecSurface{
			provider:   prov,
			resolved:   resolved,
			definition: def,
		})
		authFallback.add(resolved.ConnectionName, def)
	}

	if len(built) == 1 {
		if authFallback.empty() {
			authFallback = nil
		}
		return bindProviderConnection(built[0].provider, built[0].resolved.ConnectionName), authFallback, nil
	}

	boundProviders := make([]composite.BoundProvider, 0, len(built))
	providers := make([]core.Provider, 0, len(built))
	for i := range built {
		specSurface := &built[i]
		boundProviders = append(boundProviders, composite.BoundProvider{
			Provider:   specSurface.provider,
			Connection: specSurface.resolved.ConnectionName,
		})
		providers = append(providers, specSurface.provider)
	}

	merged, err := composite.NewMergedWithConnections(
		name,
		built[0].provider.DisplayName(),
		built[0].provider.Description(),
		firstProviderIconSVG(providers...),
		boundProviders...,
	)
	if err != nil {
		closeBuiltSpecSurfaces(built)
		return nil, nil, err
	}
	if authFallback.empty() {
		authFallback = nil
	}
	return merged, authFallback, nil
}

func closeBuiltSpecSurfaces(surfaces []builtSpecSurface) {
	for i := range surfaces {
		closeIfPossible(surfaces[i].provider)
	}
}

func loadConfiguredAPIDefinition(ctx context.Context, name string, resolved config.ResolvedSpecSurface, meta providerMetadata, cfg specProviderConfig) (*provider.Definition, error) {
	def, err := loadSpecDefinition(ctx, name, resolved, cfg.allowedOperations)
	if err != nil {
		return nil, fmt.Errorf("load %s definition: %w", resolved.Surface, err)
	}
	if cfg.baseURL != "" && resolved.Surface == config.SpecSurfaceOpenAPI {
		def.BaseURL = cfg.baseURL
	}
	applyProviderHeaders(def, cfg.manifestPlugin)
	if err := applyManagedParameters(def, cfg.manifestPlugin); err != nil {
		return nil, err
	}
	if cfg.applyResponseMapping {
		applyProviderResponseMapping(def, cfg.manifestPlugin)
	}
	applyProviderPagination(def, cfg.manifestPlugin, cfg.allowedOperations)
	if meta.displayName != "" {
		def.DisplayName = meta.displayName
	}
	if meta.description != "" {
		def.Description = meta.description
	}
	if meta.iconSVG != "" {
		def.IconSVG = meta.iconSVG
	}
	return def, nil
}

func buildConfiguredSpecProvider(ctx context.Context, name string, resolved config.ResolvedSpecSurface, meta providerMetadata, cfg specProviderConfig, deps Deps) (core.Provider, *provider.Definition, error) {
	var buildOpts []provider.BuildOption
	buildOpts = append(buildOpts, provider.WithEgressCheck(deps.Egress.CheckFunc(cfg.allowedHosts)))
	if cfg.providerBuildOptions != nil {
		buildOpts = append(buildOpts, cfg.providerBuildOptions(resolved.Connection)...)
	}

	switch resolved.Surface {
	case config.SpecSurfaceOpenAPI, config.SpecSurfaceGraphQL:
		def, err := loadConfiguredAPIDefinition(ctx, name, resolved, meta, cfg)
		if err != nil {
			return nil, nil, err
		}
		prov, err := provider.Build(def, resolved.Connection, buildOpts...)
		if err != nil {
			return nil, nil, err
		}
		if resolved.Surface == config.SpecSurfaceGraphQL {
			prov = wrapGraphQLSessionCatalogProvider(prov, name, resolved.URL, cfg.allowedOperations, resolved.GraphQLSelections)
		}
		return prov, def, nil
	case config.SpecSurfaceMCP:
		connMode := core.ConnectionMode(resolved.Connection.Mode)
		if connMode == "" {
			connMode = core.ConnectionModeUser
		}
		up, err := mcpupstream.New(
			ctx,
			name,
			resolved.URL,
			connMode,
			manifestHeaders(cfg.manifestPlugin),
			deps.Egress.CheckFunc(cfg.allowedHosts),
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
		return nil, nil, fmt.Errorf("unsupported spec surface %q", resolved.Surface)
	}
}

func loadSpecDefinition(ctx context.Context, name string, resolved config.ResolvedSpecSurface, allowedOperations map[string]*config.OperationOverride) (*provider.Definition, error) {
	switch resolved.Surface {
	case config.SpecSurfaceOpenAPI:
		return openapi.LoadDefinition(ctx, name, resolved.URL, allowedOperations)
	case config.SpecSurfaceGraphQL:
		return graphql.StaticDefinition(name, resolved.URL), nil
	default:
		return nil, fmt.Errorf("unsupported spec definition surface %q", resolved.Surface)
	}
}

func applyAllowedOperations(name string, allowedOperations map[string]*config.OperationOverride, pluginProv core.Provider) (core.Provider, error) {
	policy, err := operationexposure.New(allowedOperations)
	if err != nil {
		return nil, fmt.Errorf("integration %q plugin: %w", name, err)
	}
	if policy == nil {
		return pluginProv, nil
	}
	if err := policy.ValidateCatalog(pluginProv.Catalog()); err != nil {
		return nil, fmt.Errorf("integration %q plugin: %w", name, err)
	}
	return policy.Wrap(pluginProv), nil
}

func catalogOperationCount(cat *catalog.Catalog) int {
	if cat == nil {
		return 0
	}
	return len(cat.Operations)
}

func buildPluginProvider(ctx context.Context, name string, entry *config.ProviderEntry, pluginConfig map[string]any, spec providerhost.StaticProviderSpec, deps Deps) (core.Provider, error) {
	command := entry.Command
	args := entry.Args
	env := clonePluginEnv(entry.Env)
	var cleanup func()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	runtimeConfig, runtimeProvider, runtimeOwned, err := effectivePluginRuntime(ctx, name, entry, deps)
	if err != nil {
		return nil, err
	}
	runtimeCapabilities, err := runtimeProvider.Capabilities(ctx)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, fmt.Errorf("query %s capabilities: %w", pluginRuntimeLabel(runtimeConfig), err)
	}
	runtimePlan, err := buildPluginRuntimePlan(name, entry, deps, runtimeCapabilities)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	if err := runtimePlan.Validate(pluginRuntimeLabel(runtimeConfig)); err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	if command == "" && runtimePlan.Effective.LaunchMode == RuntimeLaunchModeHostPath {
		if entry.ResolvedManifestPath == "" {
			if runtimeOwned {
				_ = runtimeProvider.Close()
			}
			return nil, fmt.Errorf("resolved manifest path is required for synthesized source provider execution")
		}
		rootDir := filepath.Dir(entry.ResolvedManifestPath)
		command, args, cleanup, err = providerpkg.SourceProviderExecutionCommand(rootDir, runtime.GOOS, runtime.GOARCH)
		if errors.Is(err, providerpkg.ErrNoSourceProviderPackage) {
			if runtimeOwned {
				_ = runtimeProvider.Close()
			}
			return nil, fmt.Errorf("prepare synthesized source provider execution: no Go or Python provider source found")
		}
		if err != nil {
			if runtimeOwned {
				_ = runtimeProvider.Close()
			}
			return nil, fmt.Errorf("prepare synthesized source provider execution: %w", err)
		}
		execEnv, err := providerpkg.SourceProviderExecutionEnv(rootDir, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			if runtimeOwned {
				_ = runtimeProvider.Close()
			}
			return nil, fmt.Errorf("prepare synthesized source provider environment: %w", err)
		}
		if len(execEnv) > 0 {
			if env == nil {
				env = make(map[string]string, len(execEnv))
			}
			maps.Copy(env, execEnv)
		}
	}
	if command == "" && runtimePlan.Effective.LaunchMode != RuntimeLaunchModeHostPath {
		args = nil
	}
	launch, err := preparePluginRuntimeLaunch(name, entry, command, args, cleanup, runtimePlan.Effective)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	command = launch.command
	args = launch.args
	cleanup = launch.cleanup
	session, err := runtimeProvider.StartSession(ctx, buildPluginRuntimeStartSessionRequest(name, runtimeConfig))
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, fmt.Errorf("start plugin runtime session: %w", err)
	}
	sessionID := session.ID
	stopSession := true
	defer func() {
		if !stopSession {
			return
		}
		_ = stopPluginRuntimeSession(runtimeProvider, sessionID)
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
	}()
	if err := waitForPluginRuntimeSessionReady(ctx, runtimeProvider, sessionID); err != nil {
		return nil, fmt.Errorf("wait for plugin runtime session %q ready: %w", sessionID, err)
	}

	hostServices, invTokens, runtimeCleanup, err := buildPluginRuntimeHostServices(name, entry, deps, runtimePlan.Effective.HostServiceMode == RuntimeHostServiceModeDirect)
	if err != nil {
		return nil, err
	}
	cleanup = chainCleanup(cleanup, runtimeCleanup)
	startedHostServices, err := providerhost.StartHostServices(hostServices)
	if err != nil {
		return nil, fmt.Errorf("start runtime host services: %w", err)
	}
	if startedHostServices != nil {
		cleanup = chainCleanup(cleanup, func() {
			_ = startedHostServices.Close()
		})
	}
	startEnv := maps.Clone(env)
	allowedHosts := slices.Clone(entry.AllowedHosts)
	for _, hostService := range startedHostServices.Bindings() {
		bindingReq, bindingEnv, relayHost, err := buildPluginRuntimeHostServiceBinding(name, sessionID, hostService, deps, runtimePlan.Effective.HostServiceMode == RuntimeHostServiceModeDirect)
		if err != nil {
			return nil, err
		}
		if _, err := runtimeProvider.BindHostService(ctx, bindingReq); err != nil {
			return nil, fmt.Errorf("bind host service %q: %w", hostService.EnvVar, err)
		}
		if len(bindingEnv) > 0 {
			if startEnv == nil {
				startEnv = make(map[string]string, len(bindingEnv))
			}
			maps.Copy(startEnv, bindingEnv)
		}
		allowedHosts = appendAllowedHost(allowedHosts, relayHost)
	}
	if runtimePlan.HostnameEgressTransport == RuntimeHostnameEgressTransportPublicProxy {
		proxyEnv, err := buildPluginRuntimePublicEgressProxy(name, sessionID, entry.AllowedHosts, deps.Egress.DefaultAction, deps)
		if err != nil {
			return nil, err
		}
		if len(proxyEnv) > 0 {
			if startEnv == nil {
				startEnv = make(map[string]string, len(proxyEnv))
			}
			maps.Copy(startEnv, proxyEnv)
		}
	}

	hostedPlugin, err := runtimeProvider.StartPlugin(ctx, pluginruntime.StartPluginRequest{
		SessionID:     sessionID,
		PluginName:    name,
		Command:       command,
		Args:          args,
		Env:           startEnv,
		BundleDir:     launch.bundleDir,
		AllowedHosts:  allowedHosts,
		DefaultAction: pluginruntime.PolicyAction(deps.Egress.DefaultAction),
		HostBinary:    entry.HostBinary,
	})
	if err != nil {
		return nil, fmt.Errorf("start hosted plugin: %w", err)
	}
	conn, err := pluginruntime.DialHostedPlugin(ctx, hostedPlugin.DialTarget)
	if err != nil {
		return nil, fmt.Errorf("dial hosted plugin: %w", err)
	}
	opts := []providerhost.RemoteProviderOption{providerhost.WithCloser(&runtimeBackedPluginCloser{
		conn:         conn,
		runtime:      runtimeProvider,
		sessionID:    sessionID,
		closeRuntime: runtimeOwned,
		cleanup:      cleanup,
	})}
	if invTokens != nil {
		opts = append(opts,
			providerhost.WithInvocationTokens(invTokens),
			providerhost.WithInvocationTokenSubject(name, providerhost.InvocationDependencyGrants(entry.Invokes)),
		)
	}
	prov, err := providerhost.NewRemoteProvider(ctx, conn.Integration(), spec, pluginConfig, opts...)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	stopSession = false
	cleanup = nil
	return prov, nil
}

func effectivePluginRuntime(ctx context.Context, name string, entry *config.ProviderEntry, deps Deps) (config.EffectivePluginRuntime, pluginruntime.Provider, bool, error) {
	if deps.PluginRuntime != nil {
		runtimeConfig := config.EffectivePluginRuntime{}
		if entry != nil && entry.Runtime != nil {
			runtimeConfig.Template = strings.TrimSpace(entry.Runtime.Template)
			runtimeConfig.Image = strings.TrimSpace(entry.Runtime.Image)
			runtimeConfig.Metadata = maps.Clone(entry.Runtime.Metadata)
		}
		return runtimeConfig, deps.PluginRuntime, false, nil
	}
	if deps.PluginRuntimeRegistry != nil {
		runtimeConfig, runtimeProvider, err := deps.PluginRuntimeRegistry.Resolve(ctx, name, entry)
		if err != nil {
			return config.EffectivePluginRuntime{}, nil, false, err
		}
		if runtimeProvider != nil {
			return runtimeConfig, runtimeProvider, false, nil
		}
		if runtimeConfig.Enabled {
			return runtimeConfig, pluginruntime.NewLocalProvider(), true, nil
		}
	}
	return config.EffectivePluginRuntime{}, pluginruntime.NewLocalProvider(), true, nil
}

const (
	pluginRuntimeHostServiceRelayTokenTTL = 30 * 24 * time.Hour
	pluginRuntimeEgressProxyTokenTTL      = 30 * 24 * time.Hour
)

func pluginRuntimeLabel(runtimeConfig config.EffectivePluginRuntime) string {
	if name := strings.TrimSpace(runtimeConfig.ProviderName); name != "" {
		return fmt.Sprintf("runtime provider %q", name)
	}
	return "plugin runtime"
}

func buildPluginRuntimeStartSessionRequest(name string, runtimeConfig config.EffectivePluginRuntime) pluginruntime.StartSessionRequest {
	metadata := maps.Clone(runtimeConfig.Metadata)
	if metadata == nil {
		metadata = map[string]string{}
	}
	if name != "" {
		metadata["plugin"] = name
	}
	return pluginruntime.StartSessionRequest{
		PluginName: name,
		Template:   runtimeConfig.Template,
		Image:      runtimeConfig.Image,
		Metadata:   metadata,
	}
}

const pluginRuntimeStopTimeout = 3 * time.Second

type runtimeBackedPluginCloser struct {
	conn         pluginruntime.HostedPluginConn
	runtime      pluginruntime.Provider
	sessionID    string
	closeRuntime bool
	cleanup      func()
}

func (c *runtimeBackedPluginCloser) Close() error {
	if c == nil {
		return nil
	}
	var errs []error
	if c.runtime != nil && c.sessionID != "" {
		errs = append(errs, stopPluginRuntimeSession(c.runtime, c.sessionID))
	}
	if c.conn != nil {
		errs = append(errs, c.conn.Close())
	}
	if c.closeRuntime && c.runtime != nil {
		errs = append(errs, c.runtime.Close())
	}
	if c.cleanup != nil {
		c.cleanup()
	}
	return errors.Join(errs...)
}

func stopPluginRuntimeSession(runtimeProvider pluginruntime.Provider, sessionID string) error {
	if runtimeProvider == nil || sessionID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), pluginRuntimeStopTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runtimeProvider.StopSession(ctx, pluginruntime.StopSessionRequest{SessionID: sessionID})
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("stop plugin runtime session %q: %w", sessionID, ctx.Err())
	}
}

func waitForPluginRuntimeSessionReady(ctx context.Context, runtimeProvider pluginruntime.Provider, sessionID string) error {
	for {
		session, err := runtimeProvider.GetSession(ctx, pluginruntime.GetSessionRequest{SessionID: sessionID})
		if err != nil {
			return err
		}
		switch session.State {
		case pluginruntime.SessionStateReady, pluginruntime.SessionStateRunning:
			return nil
		case pluginruntime.SessionStateFailed, pluginruntime.SessionStateStopped:
			return fmt.Errorf("session entered %q state", session.State)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func buildPluginRuntimeHostServices(name string, entry *config.ProviderEntry, deps Deps, allowDirectBindings bool) ([]providerhost.HostService, *providerhost.InvocationTokenManager, func(), error) {
	var (
		hostServices []providerhost.HostService
		cleanup      func()
		invTokens    *providerhost.InvocationTokenManager
	)
	fail := func(err error) ([]providerhost.HostService, *providerhost.InvocationTokenManager, func(), error) {
		if cleanup != nil {
			cleanup()
			cleanup = nil
		}
		return nil, nil, nil, err
	}

	effectiveIndexedDB, err := config.ResolveEffectivePluginIndexedDB(name, entry, deps.SelectedIndexedDBName, deps.IndexedDBDefs)
	if err != nil {
		return fail(err)
	}
	if effectiveIndexedDB.Enabled {
		services, indexedDBCleanup, err := buildPluginIndexedDBHostServices(name, effectiveIndexedDB, deps)
		if err != nil {
			return fail(err)
		}
		hostServices = append(hostServices, services...)
		cleanup = chainCleanup(cleanup, indexedDBCleanup)
	}
	if len(entry.Cache) > 0 {
		services, cacheCleanup, err := buildPluginCacheHostServices(name, entry, deps)
		if err != nil {
			return fail(err)
		}
		hostServices = append(hostServices, services...)
		cleanup = chainCleanup(cleanup, cacheCleanup)
	}
	if len(entry.S3) > 0 {
		services, err := buildPluginS3HostServices(name, entry, deps)
		if err != nil {
			return fail(err)
		}
		hostServices = append(hostServices, services...)
	}
	includeWorkflowManager := deps.WorkflowManager != nil || (deps.WorkflowRuntime != nil && deps.WorkflowRuntime.HasConfiguredProviders())
	includeAgentManager := deps.AgentManager != nil || deps.AgentRuntime != nil
	needInvocationTokens := allowDirectBindings || len(entry.Invokes) > 0
	if includeWorkflowManager || includeAgentManager {
		needInvocationTokens = true
	}
	if needInvocationTokens {
		invTokens, err = providerhost.NewInvocationTokenManager(deps.EncryptionKey)
		if err != nil {
			return fail(err)
		}
	}
	if includeWorkflowManager {
		hostServices = append(hostServices, buildPluginWorkflowManagerHostService(name, deps, invTokens))
	}
	if includeAgentManager {
		hostServices = append(hostServices, buildPluginAgentManagerHostService(name, deps, invTokens))
	}
	if deps.AuthorizationProvider != nil && len(entry.EffectiveHTTPBindings()) > 0 {
		hostServices = append(hostServices, buildPluginAuthorizationHostService(deps.AuthorizationProvider))
	}
	if !allowDirectBindings {
		if len(entry.Invokes) > 0 {
			hostServices = append(hostServices, buildPluginInvokerHostService(name, entry, deps, invTokens))
		}
		return hostServices, invTokens, cleanup, nil
	}
	if len(entry.Invokes) > 0 {
		hostServices = append(hostServices, buildPluginInvokerHostService(name, entry, deps, invTokens))
	}
	return hostServices, invTokens, cleanup, nil
}

func buildPluginRuntimeHostServiceBinding(pluginName, sessionID string, hostService providerhost.StartedHostService, deps Deps, allowUnixRelay bool) (pluginruntime.BindHostServiceRequest, map[string]string, string, error) {
	if !allowUnixRelay {
		var (
			serviceKey   string
			serviceLabel string
			methodPrefix string
		)
		switch {
		case isIndexedDBHostServiceEnv(hostService.EnvVar):
			serviceKey = "indexeddb"
			serviceLabel = "IndexedDB"
			methodPrefix = "/" + proto.IndexedDB_ServiceDesc.ServiceName + "/"
		case isCacheHostServiceEnv(hostService.EnvVar):
			serviceKey = "cache"
			serviceLabel = "cache"
			methodPrefix = "/" + proto.Cache_ServiceDesc.ServiceName + "/"
		case isS3HostServiceEnv(hostService.EnvVar):
			serviceKey = "s3"
			serviceLabel = "S3"
			methodPrefix = "/" + proto.S3_ServiceDesc.ServiceName + "/"
		case hostService.EnvVar == providerhost.DefaultWorkflowManagerSocketEnv:
			serviceKey = "workflow_manager"
			serviceLabel = "workflow manager"
			methodPrefix = "/" + proto.WorkflowManagerHost_ServiceDesc.ServiceName + "/"
		case hostService.EnvVar == providerhost.DefaultAgentManagerSocketEnv:
			serviceKey = "agent_manager"
			serviceLabel = "agent manager"
			methodPrefix = "/" + proto.AgentManagerHost_ServiceDesc.ServiceName + "/"
		case hostService.EnvVar == providerhost.DefaultAuthorizationSocketEnv:
			serviceKey = "authorization"
			serviceLabel = "authorization"
			methodPrefix = "/" + proto.AuthorizationProvider_ServiceDesc.ServiceName + "/"
		case hostService.EnvVar == providerhost.DefaultPluginInvokerSocketEnv:
			serviceKey = "plugin_invoker"
			serviceLabel = "plugin invoker"
			methodPrefix = "/" + proto.PluginInvoker_ServiceDesc.ServiceName + "/"
		default:
			return pluginruntime.BindHostServiceRequest{}, nil, "", fmt.Errorf("host service %q requires host service tunnels", hostService.EnvVar)
		}
		relay, relayEnv, relayHost, ok, err := buildPluginRuntimePublicHostServiceRelay(pluginName, sessionID, hostService, deps, serviceKey, serviceLabel, methodPrefix)
		if err != nil {
			return pluginruntime.BindHostServiceRequest{}, nil, "", err
		}
		if !ok {
			return pluginruntime.BindHostServiceRequest{}, nil, "", fmt.Errorf("plugin %q requires server.baseURL and server.encryptionKey to relay %s without host service tunnels", pluginName, serviceLabel)
		}
		return pluginruntime.BindHostServiceRequest{
			SessionID: sessionID,
			EnvVar:    hostService.EnvVar,
			Relay:     relay,
		}, relayEnv, relayHost, nil
	}
	return pluginruntime.BindHostServiceRequest{
		SessionID:      sessionID,
		EnvVar:         hostService.EnvVar,
		HostSocketPath: hostService.SocketPath,
		Relay: pluginruntime.HostServiceRelay{
			DialTarget: "unix://" + hostService.SocketPath,
		},
	}, nil, "", nil
}

func buildPluginRuntimePublicEgressProxy(pluginName, sessionID string, allowedHosts []string, defaultAction egress.PolicyAction, deps Deps) (map[string]string, error) {
	if strings.TrimSpace(deps.BaseURL) == "" || len(deps.EncryptionKey) == 0 {
		return nil, fmt.Errorf("plugin %q requires server.baseURL and server.encryptionKey to enforce hostname-based egress on hosted runtimes without host service tunnels", pluginName)
	}
	proxyBaseURL, _, err := pluginRuntimePublicProxyBaseURL(deps.BaseURL)
	if err != nil {
		return nil, err
	}
	tokenManager, err := providerhost.NewEgressProxyTokenManager(deps.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("init egress proxy tokens: %w", err)
	}
	token, err := tokenManager.MintToken(providerhost.EgressProxyTokenRequest{
		PluginName:    pluginName,
		SessionID:     sessionID,
		AllowedHosts:  slices.Clone(allowedHosts),
		DefaultAction: defaultAction,
		TTL:           pluginRuntimeEgressProxyTokenTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("mint public egress proxy token: %w", err)
	}
	proxyURL := *proxyBaseURL
	proxyURL.User = url.UserPassword("gestalt-egress-proxy", token)
	return map[string]string{
		"HTTP_PROXY":  proxyURL.String(),
		"HTTPS_PROXY": proxyURL.String(),
	}, nil
}

func buildPluginRuntimePublicHostServiceRelay(pluginName, sessionID string, hostService providerhost.StartedHostService, deps Deps, serviceKey, serviceLabel, methodPrefix string) (pluginruntime.HostServiceRelay, map[string]string, string, bool, error) {
	if strings.TrimSpace(deps.BaseURL) == "" || len(deps.EncryptionKey) == 0 {
		return pluginruntime.HostServiceRelay{}, nil, "", false, nil
	}
	dialTarget, relayHost, err := pluginRuntimePublicRelayTarget(deps.BaseURL)
	if err != nil {
		return pluginruntime.HostServiceRelay{}, nil, "", false, err
	}
	tokenManager, err := providerhost.NewHostServiceRelayTokenManager(deps.EncryptionKey)
	if err != nil {
		return pluginruntime.HostServiceRelay{}, nil, "", false, fmt.Errorf("init host service relay tokens: %w", err)
	}
	token, err := tokenManager.MintToken(providerhost.HostServiceRelayTokenRequest{
		PluginName:   pluginName,
		SessionID:    sessionID,
		Service:      serviceKey,
		SocketPath:   hostService.SocketPath,
		MethodPrefix: methodPrefix,
		TTL:          pluginRuntimeHostServiceRelayTokenTTL,
	})
	if err != nil {
		return pluginruntime.HostServiceRelay{}, nil, "", false, fmt.Errorf("mint %s host service relay token: %w", serviceLabel, err)
	}
	return pluginruntime.HostServiceRelay{
			DialTarget: dialTarget,
		}, map[string]string{
			hostService.EnvVar + "_TOKEN": token,
		}, relayHost, true, nil
}

func pluginRuntimePublicRelayTarget(baseURL string) (string, string, error) {
	parsed, host, err := pluginRuntimePublicProxyBaseURL(baseURL)
	if err != nil {
		return "", "", err
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	return "tls://" + net.JoinHostPort(host, port), host, nil
}

func pluginRuntimePublicProxyBaseURL(baseURL string) (*url.URL, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, "", fmt.Errorf("parse server.baseURL for public runtime relay: %w", err)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return nil, "", fmt.Errorf("server.baseURL %q is missing a hostname", baseURL)
	}
	if path := strings.TrimSpace(parsed.EscapedPath()); path != "" && path != "/" {
		return nil, "", fmt.Errorf("server.baseURL %q must not include a path for public runtime relay", baseURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, "", fmt.Errorf("server.baseURL %q must not include a query or fragment for public runtime relay", baseURL)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return nil, "", fmt.Errorf("server.baseURL %q must use https for public runtime relay", baseURL)
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, host, nil
}

func isIndexedDBHostServiceEnv(envVar string) bool {
	envVar = strings.TrimSpace(envVar)
	return envVar == providerhost.DefaultIndexedDBSocketEnv || strings.HasPrefix(envVar, providerhost.DefaultIndexedDBSocketEnv+"_")
}

func isCacheHostServiceEnv(envVar string) bool {
	envVar = strings.TrimSpace(envVar)
	return envVar == providerhost.DefaultCacheSocketEnv || strings.HasPrefix(envVar, providerhost.DefaultCacheSocketEnv+"_")
}

func isS3HostServiceEnv(envVar string) bool {
	envVar = strings.TrimSpace(envVar)
	return envVar == providerhost.DefaultS3SocketEnv || strings.HasPrefix(envVar, providerhost.DefaultS3SocketEnv+"_")
}

func appendAllowedHost(allowedHosts []string, host string) []string {
	host = strings.TrimSpace(host)
	if host == "" {
		return allowedHosts
	}
	for _, allowed := range allowedHosts {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return allowedHosts
		}
	}
	return append(allowedHosts, host)
}

func buildPluginIndexedDBHostServices(pluginName string, effective config.EffectivePluginIndexedDB, deps Deps) ([]providerhost.HostService, func(), error) {
	if deps.IndexedDBFactory == nil || len(deps.IndexedDBDefs) == 0 {
		return nil, nil, fmt.Errorf("indexeddb host services are not available")
	}

	ds, err := buildPluginScopedIndexedDB(pluginName, effective, deps)
	if err != nil {
		return nil, nil, err
	}

	hostServices := []providerhost.HostService{{
		EnvVar: providerhost.DefaultIndexedDBSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, providerhost.NewIndexedDBServer(ds, pluginName, providerhost.IndexedDBServerOptions{
				AllowedStores: effective.ObjectStores,
			}))
		},
	}}
	return hostServices, func() {
		_ = closeIndexedDBs(ds)
	}, nil
}

func buildPluginCacheHostServices(pluginName string, entry *config.ProviderEntry, deps Deps) ([]providerhost.HostService, func(), error) {
	if deps.CacheFactory == nil || len(deps.CacheDefs) == 0 {
		return nil, nil, fmt.Errorf("cache host services are not available")
	}

	hostServices := make([]providerhost.HostService, 0, len(entry.Cache)+1)
	boundCaches := make([]corecache.Cache, 0, len(entry.Cache))
	for _, bindingName := range entry.Cache {
		def, ok := deps.CacheDefs[bindingName]
		if !ok || def == nil {
			_ = closeCaches(boundCaches...)
			return nil, nil, fmt.Errorf("cache %q is not available", bindingName)
		}
		value, err := buildCache(def, &FactoryRegistry{Cache: deps.CacheFactory})
		if err != nil {
			_ = closeCaches(boundCaches...)
			return nil, nil, fmt.Errorf("cache %q: %w", bindingName, err)
		}
		boundCaches = append(boundCaches, value)
		hostServices = append(hostServices, providerhost.HostService{
			EnvVar: providerhost.CacheSocketEnv(bindingName),
			Register: func(cacheValue corecache.Cache) func(*grpc.Server) {
				return func(srv *grpc.Server) {
					proto.RegisterCacheServer(srv, providerhost.NewCacheServer(cacheValue, pluginName))
				}
			}(value),
		})
	}
	if len(boundCaches) == 1 {
		value := boundCaches[0]
		hostServices = append(hostServices, providerhost.HostService{
			EnvVar: providerhost.DefaultCacheSocketEnv,
			Register: func(srv *grpc.Server) {
				proto.RegisterCacheServer(srv, providerhost.NewCacheServer(value, pluginName))
			},
		})
	}
	return hostServices, func() {
		_ = closeCaches(boundCaches...)
	}, nil
}

func buildPluginS3HostServices(pluginName string, entry *config.ProviderEntry, deps Deps) ([]providerhost.HostService, error) {
	if len(deps.S3) == 0 {
		return nil, fmt.Errorf("s3 host services are not available")
	}

	hostServices := make([]providerhost.HostService, 0, len(entry.S3)+1)
	for _, binding := range entry.S3 {
		client, ok := deps.S3[binding]
		if !ok || client == nil {
			return nil, fmt.Errorf("s3 %q is not available", binding)
		}
		hostServices = append(hostServices, providerhost.HostService{
			EnvVar: providerhost.S3SocketEnv(binding),
			Register: func(client s3store.Client) func(*grpc.Server) {
				return func(srv *grpc.Server) {
					proto.RegisterS3Server(srv, providerhost.NewS3Server(client, pluginName))
				}
			}(client),
		})
	}
	if len(entry.S3) == 1 {
		client := deps.S3[entry.S3[0]]
		hostServices = append(hostServices, providerhost.HostService{
			EnvVar: providerhost.DefaultS3SocketEnv,
			Register: func(srv *grpc.Server) {
				proto.RegisterS3Server(srv, providerhost.NewS3Server(client, pluginName))
			},
		})
	}
	return hostServices, nil
}

func buildWorkflowIndexedDBHostServices(name string, effective config.EffectiveWorkflowIndexedDB, deps Deps) ([]providerhost.HostService, func(), error) {
	if deps.IndexedDBFactory == nil || len(deps.IndexedDBDefs) == 0 {
		return nil, nil, fmt.Errorf("indexeddb host services are not available")
	}

	ds, err := buildWorkflowScopedIndexedDB(name, effective, deps)
	if err != nil {
		return nil, nil, err
	}

	hostServices := []providerhost.HostService{{
		EnvVar: providerhost.DefaultIndexedDBSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, providerhost.NewIndexedDBServer(ds, name, providerhost.IndexedDBServerOptions{
				AllowedStores: effective.ObjectStores,
			}))
		},
	}}
	return hostServices, func() {
		_ = closeIndexedDBs(ds)
	}, nil
}

func buildPluginWorkflowManagerHostService(pluginName string, deps Deps, tokens *providerhost.InvocationTokenManager) providerhost.HostService {
	manager := deps.WorkflowManager
	if manager == nil {
		manager = unavailableWorkflowManager{}
	}
	return providerhost.HostService{
		EnvVar: providerhost.DefaultWorkflowManagerSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterWorkflowManagerHostServer(srv, providerhost.NewWorkflowManagerServer(pluginName, manager, tokens))
		},
	}
}

func buildPluginAgentManagerHostService(pluginName string, deps Deps, tokens *providerhost.InvocationTokenManager) providerhost.HostService {
	manager := deps.AgentManager
	if manager == nil {
		manager = unavailableAgentManager{}
	}
	return providerhost.HostService{
		EnvVar: providerhost.DefaultAgentManagerSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterAgentManagerHostServer(srv, providerhost.NewAgentManagerServer(pluginName, manager, tokens))
		},
	}
}

func buildPluginAuthorizationHostService(provider core.AuthorizationProvider) providerhost.HostService {
	return providerhost.HostService{
		EnvVar: providerhost.DefaultAuthorizationSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterAuthorizationProviderServer(srv, providerhost.NewAuthorizationProviderServer(provider))
		},
	}
}

func buildPluginInvokerHostService(pluginName string, entry *config.ProviderEntry, deps Deps, tokens *providerhost.InvocationTokenManager) providerhost.HostService {
	invoker := deps.PluginInvoker
	if invoker == nil {
		invoker = unavailablePluginInvoker{}
	}
	return providerhost.HostService{
		EnvVar: providerhost.DefaultPluginInvokerSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterPluginInvokerServer(srv, providerhost.NewPluginInvokerServer(pluginName, entry.Invokes, invoker, tokens))
		},
	}
}

type unavailablePluginInvoker struct{}

func (unavailablePluginInvoker) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return nil, fmt.Errorf("plugin invoker is not available")
}

func (unavailablePluginInvoker) InvokeGraphQL(context.Context, *principal.Principal, string, string, invocation.GraphQLRequest) (*core.OperationResult, error) {
	return nil, fmt.Errorf("plugin invoker is not available")
}

type unavailableWorkflowManager struct{}

func (unavailableWorkflowManager) ListSchedules(context.Context, *principal.Principal) ([]*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) CreateSchedule(context.Context, *principal.Principal, workflowmanager.ScheduleUpsert) (*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetSchedule(context.Context, *principal.Principal, string) (*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) UpdateSchedule(context.Context, *principal.Principal, string, workflowmanager.ScheduleUpsert) (*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) DeleteSchedule(context.Context, *principal.Principal, string) error {
	return fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) PauseSchedule(context.Context, *principal.Principal, string) (*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) ResumeSchedule(context.Context, *principal.Principal, string) (*workflowmanager.ManagedSchedule, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) ListEventTriggers(context.Context, *principal.Principal) ([]*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) CreateEventTrigger(context.Context, *principal.Principal, workflowmanager.EventTriggerUpsert) (*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetEventTrigger(context.Context, *principal.Principal, string) (*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) UpdateEventTrigger(context.Context, *principal.Principal, string, workflowmanager.EventTriggerUpsert) (*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) DeleteEventTrigger(context.Context, *principal.Principal, string) error {
	return fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) PauseEventTrigger(context.Context, *principal.Principal, string) (*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) ResumeEventTrigger(context.Context, *principal.Principal, string) (*workflowmanager.ManagedEventTrigger, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) ListRuns(context.Context, *principal.Principal) ([]*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetRun(context.Context, *principal.Principal, string) (*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) CancelRun(context.Context, *principal.Principal, string, string) (*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) PublishEvent(context.Context, *principal.Principal, coreworkflow.Event) (coreworkflow.Event, error) {
	return coreworkflow.Event{}, fmt.Errorf("workflow manager is not available")
}

type unavailableAgentManager struct{}

func (unavailableAgentManager) Run(context.Context, *principal.Principal, coreagent.ManagerRunRequest) (*coreagent.ManagedRun, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) GetRun(context.Context, *principal.Principal, string) (*coreagent.ManagedRun, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListRuns(context.Context, *principal.Principal) ([]*coreagent.ManagedRun, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) CancelRun(context.Context, *principal.Principal, string, string) (*coreagent.ManagedRun, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func buildPluginScopedIndexedDB(pluginName string, effective config.EffectivePluginIndexedDB, deps Deps) (indexeddb.IndexedDB, error) {
	return buildScopedIndexedDB(scopedIndexedDBBuildOptions{
		MetricsName:        effective.ProviderName,
		ProviderName:       effective.ProviderName,
		DB:                 effective.DB,
		AllowedStores:      effective.ObjectStores,
		LegacyStorePrefix:  legacyPluginIndexedDBPrefix(pluginName),
		LegacyConfigPrefix: legacyPluginIndexedDBPrefix(pluginName),
	}, deps)
}

func buildWorkflowScopedIndexedDB(name string, effective config.EffectiveWorkflowIndexedDB, deps Deps) (indexeddb.IndexedDB, error) {
	return buildScopedIndexedDB(scopedIndexedDBBuildOptions{
		MetricsName:   name,
		ProviderName:  effective.ProviderName,
		DB:            effective.DB,
		AllowedStores: effective.ObjectStores,
	}, deps)
}

type scopedIndexedDBBuildOptions struct {
	MetricsName        string
	ProviderName       string
	DB                 string
	AllowedStores      []string
	LegacyStorePrefix  string
	LegacyConfigPrefix string
}

func buildScopedIndexedDB(opts scopedIndexedDBBuildOptions, deps Deps) (indexeddb.IndexedDB, error) {
	def, ok := deps.IndexedDBDefs[opts.ProviderName]
	if !ok || def == nil {
		return nil, fmt.Errorf("indexeddb %q is not available", opts.ProviderName)
	}
	scopedDef, transportPrefix, err := newScopedIndexedDBDef(def, scopedIndexedDBDefOptions{
		DB:                 opts.DB,
		LegacyConfigPrefix: opts.LegacyConfigPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("indexeddb %q: %w", opts.ProviderName, err)
	}
	ds, err := buildIndexedDB(scopedDef, &FactoryRegistry{IndexedDB: deps.IndexedDBFactory})
	if err != nil {
		return nil, fmt.Errorf("indexeddb %q: %w", opts.ProviderName, err)
	}
	ds = newPluginIndexedDBTransport(ds, pluginIndexedDBTransportOptions{
		StorePrefix:       transportPrefix,
		LegacyStorePrefix: opts.LegacyStorePrefix,
		AllowedStores:     opts.AllowedStores,
	})
	return metricutil.InstrumentIndexedDB(ds, opts.MetricsName), nil
}

type scopedIndexedDBDefOptions struct {
	DB                 string
	LegacyConfigPrefix string
}

func newScopedIndexedDBDef(entry *config.ProviderEntry, opts scopedIndexedDBDefOptions) (*config.ProviderEntry, string, error) {
	if entry == nil {
		return nil, "", fmt.Errorf("datastore provider is required")
	}
	cfg, err := config.NodeToMap(entry.Config)
	if err != nil {
		return nil, "", fmt.Errorf("decode config: %w", err)
	}
	if cfg == nil {
		cfg = make(map[string]any)
	}

	transportPrefix := ""
	if pluginIndexedDBUsesScopedProviderConfig(entry, cfg) {
		if opts.LegacyConfigPrefix != "" {
			cfg["legacy_table_prefix"] = opts.LegacyConfigPrefix
		} else {
			delete(cfg, "legacy_table_prefix")
		}
		delete(cfg, "legacy_prefix")
		delete(cfg, "namespace")
		if isSQLiteIndexedDBConfig(cfg) {
			delete(cfg, "schema")
			cfg["table_prefix"] = opts.DB + "_"
			cfg["prefix"] = opts.DB + "_"
		} else {
			delete(cfg, "table_prefix")
			delete(cfg, "prefix")
			cfg["schema"] = opts.DB
		}
	} else {
		transportPrefix = opts.DB + "_"
	}

	configNode, err := mapToYAMLNode(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("encode config: %w", err)
	}

	cloned := *entry
	cloned.Config = configNode
	return &cloned, transportPrefix, nil
}

func pluginIndexedDBUsesScopedProviderConfig(entry *config.ProviderEntry, cfg map[string]any) bool {
	if !isRelationalIndexedDBEntry(entry) {
		return false
	}
	dsn, _ := cfg["dsn"].(string)
	return strings.TrimSpace(dsn) != ""
}

func isRelationalIndexedDBEntry(entry *config.ProviderEntry) bool {
	if entry == nil {
		return false
	}
	if entry.ResolvedManifest != nil {
		return strings.HasSuffix(strings.TrimSpace(entry.ResolvedManifest.Source), "/indexeddb/relationaldb")
	}
	if metadataURL := strings.TrimSpace(entry.SourceMetadataURL()); metadataURL != "" {
		parsed, err := url.Parse(metadataURL)
		if err == nil {
			path := filepath.ToSlash(parsed.Path)
			return strings.Contains(path, "/indexeddb/relationaldb/") && strings.HasSuffix(path, "/provider-release.yaml")
		}
	}
	if path := strings.TrimSpace(entry.SourcePath()); path != "" {
		path = filepath.ToSlash(path)
		return strings.HasSuffix(path, "/indexeddb/relationaldb") ||
			strings.HasSuffix(path, "/relationaldb") ||
			strings.HasSuffix(path, "/indexeddb/relationaldb/manifest.yaml") ||
			strings.HasSuffix(path, "/relationaldb/manifest.yaml")
	}
	return false
}

func legacyPluginIndexedDBPrefix(pluginName string) string {
	pluginName = strings.TrimSpace(pluginName)
	if pluginName == "" {
		return ""
	}
	return "plugin_" + pluginName + "_"
}

func isSQLiteIndexedDBConfig(cfg map[string]any) bool {
	dsn, _ := cfg["dsn"].(string)
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return false
	}
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return false
	case strings.HasPrefix(dsn, "mysql://"), strings.Contains(dsn, "@tcp("), strings.Contains(dsn, "@unix("):
		return false
	case strings.HasPrefix(dsn, "sqlserver://"):
		return false
	default:
		return true
	}
}

func mapToYAMLNode(value map[string]any) (yaml.Node, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return yaml.Node{}, err
	}
	var out yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&out); err != nil {
		return yaml.Node{}, err
	}
	if out.Kind == yaml.DocumentNode && len(out.Content) == 1 {
		return *out.Content[0], nil
	}
	return out, nil
}

func chainCleanup(cleanups ...func()) func() {
	var combined []func()
	for _, cleanup := range cleanups {
		if cleanup != nil {
			combined = append(combined, cleanup)
		}
	}
	if len(combined) == 0 {
		return nil
	}
	return func() {
		for i := len(combined) - 1; i >= 0; i-- {
			combined[i]()
		}
	}
}

func closeCaches(values ...corecache.Cache) error {
	var errs []error
	for _, value := range values {
		if value == nil {
			continue
		}
		if err := value.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func clonePluginEnv(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func buildPluginStaticSpec(name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, meta providerMetadata) (providerhost.StaticProviderSpec, config.StaticConnectionPlan, error) {
	if manifest == nil || manifest.Spec == nil {
		return providerhost.StaticProviderSpec{}, config.StaticConnectionPlan{}, fmt.Errorf("resolved manifest is required")
	}
	plan, err := config.BuildStaticConnectionPlan(entry, manifest.Spec)
	if err != nil {
		return providerhost.StaticProviderSpec{}, config.StaticConnectionPlan{}, err
	}

	displayName := meta.displayNameOr(manifest.DisplayName)
	if displayName == "" {
		displayName = name
	}
	description := meta.descriptionOr(manifest.Description)
	iconSVG := meta.iconSVG
	if iconPath := entry.ResolvedIconFile; iconPath != "" {
		svg, err := provider.ReadIconFile(iconPath)
		if err != nil {
			slog.Warn("could not read manifest icon_file", "path", iconPath, "error", err)
		} else if iconSVG == "" {
			iconSVG = svg
		}
	}

	conn := plan.PluginConnection()
	connMode := plan.ConnectionMode()

	var staticCatalog *catalog.Catalog
	if manifestRoot := filepath.Dir(entry.ResolvedManifestPath); entry.ResolvedManifestPath != "" {
		var err error
		staticCatalog, err = providerpkg.ReadStaticCatalog(manifestRoot, name)
		if err != nil {
			return providerhost.StaticProviderSpec{}, config.StaticConnectionPlan{}, err
		}
	}
	if staticCatalog == nil && providerpkg.StaticCatalogRequired(manifest) {
		if entry.ResolvedManifestPath == "" {
			return providerhost.StaticProviderSpec{}, config.StaticConnectionPlan{}, fmt.Errorf("resolved manifest path is required for executable provider static catalog")
		}
		return providerhost.StaticProviderSpec{}, config.StaticConnectionPlan{}, fmt.Errorf("executable providers without declarative or spec surfaces must define %s", providerpkg.StaticCatalogFile)
	}
	if staticCatalog != nil {
		if displayName != "" {
			staticCatalog.DisplayName = displayName
		}
		if description != "" {
			staticCatalog.Description = description
		}
		if iconSVG != "" {
			staticCatalog.IconSVG = iconSVG
		}
	}

	return providerhost.StaticProviderSpec{
		Name:             name,
		DisplayName:      displayName,
		Description:      description,
		IconSVG:          iconSVG,
		ConnectionMode:   connMode,
		Catalog:          staticCatalog,
		AuthTypes:        staticAuthTypes(conn.Auth.Type),
		ConnectionParams: providerhost.ConnectionParamDefsFromManifest(conn.ConnectionParams),
		CredentialFields: providerhost.CredentialFieldsFromManifest(conn.Auth.Credentials),
		DiscoveryConfig:  providerhost.DiscoveryConfigFromManifest(conn.Discovery),
	}, plan, nil
}

func staticAuthTypes(authType providermanifestv1.AuthType) []string {
	switch authType {
	case "", providermanifestv1.AuthTypeNone:
		return nil
	case providermanifestv1.AuthTypeManual, providermanifestv1.AuthTypeBearer:
		return []string{"manual"}
	default:
		return []string{"oauth"}
	}
}

func mcpOAuthBuildOpts(conn config.ConnectionDef, mcpURL string, deps Deps) []provider.BuildOption {
	if conn.Auth.Type != providermanifestv1.AuthTypeMCPOAuth || mcpURL == "" {
		return nil
	}
	return []provider.BuildOption{
		provider.WithAuthHandler(buildMCPOAuthHandler(conn, mcpURL, buildRegistrationStore(deps), deps)),
	}
}

func manifestHeaders(manifestPlugin *providermanifestv1.Spec) map[string]string {
	if manifestPlugin == nil || len(manifestPlugin.Headers) == 0 {
		return nil
	}
	return maps.Clone(manifestPlugin.Headers)
}

func applyProviderHeaders(def *provider.Definition, manifestPlugin *providermanifestv1.Spec) {
	if def == nil {
		return
	}
	headers := manifestHeaders(manifestPlugin)
	if len(headers) == 0 {
		return
	}
	def.Headers = headers
}

func applyManagedParameters(def *provider.Definition, manifestPlugin *providermanifestv1.Spec) error {
	if def == nil || manifestPlugin == nil || len(manifestPlugin.ManagedParameters) == 0 {
		return nil
	}

	if def.Headers == nil {
		def.Headers = make(map[string]string)
	}
	for _, param := range manifestPlugin.ManagedParameters {
		location := strings.ToLower(strings.TrimSpace(param.In))
		name := strings.TrimSpace(param.Name)
		switch location {
		case "header":
			if _, exists := def.Headers[name]; exists {
				return fmt.Errorf("managed parameter %q conflicts with configured header", name)
			}
			def.Headers[name] = param.Value
		case "path":
		default:
			return fmt.Errorf("unsupported managed parameter location %q", param.In)
		}
	}

	for opName := range def.Operations {
		op := def.Operations[opName]
		for _, param := range manifestPlugin.ManagedParameters {
			if strings.EqualFold(strings.TrimSpace(param.In), "path") {
				op.Path = strings.ReplaceAll(op.Path, "{"+strings.TrimSpace(param.Name)+"}", param.Value)
			}
		}
		filtered := op.Parameters[:0]
		for _, param := range op.Parameters {
			if isManagedOperationParameter(param, manifestPlugin.ManagedParameters) {
				continue
			}
			filtered = append(filtered, param)
		}
		op.Parameters = filtered
		def.Operations[opName] = op
	}
	return nil
}

func isManagedOperationParameter(param provider.ParameterDef, managed []providermanifestv1.ManagedParameter) bool {
	location := strings.ToLower(strings.TrimSpace(param.Location))
	if location == "" {
		return false
	}
	wireName := strings.TrimSpace(param.WireName)
	if wireName == "" {
		wireName = strings.TrimSpace(param.Name)
	}
	for _, managedParam := range managed {
		if strings.ToLower(strings.TrimSpace(managedParam.In)) != location {
			continue
		}
		if strings.TrimSpace(managedParam.Name) == wireName {
			return true
		}
	}
	return false
}

func applyProviderResponseMapping(def *provider.Definition, manifestPlugin *providermanifestv1.Spec) {
	if def == nil || manifestPlugin == nil || manifestPlugin.ResponseMapping == nil {
		return
	}
	rm := &provider.ResponseMappingDef{
		DataPath: manifestPlugin.ResponseMapping.DataPath,
	}
	if manifestPlugin.ResponseMapping.Pagination != nil {
		rm.Pagination = &provider.PaginationMappingDef{
			HasMore: cloneManifestValueSelectorDef(manifestPlugin.ResponseMapping.Pagination.HasMore),
			Cursor:  cloneManifestValueSelectorDef(manifestPlugin.ResponseMapping.Pagination.Cursor),
		}
	}
	def.ResponseMapping = rm
}

func applyProviderPagination(def *provider.Definition, manifestPlugin *providermanifestv1.Spec, allowedOperations map[string]*config.OperationOverride) {
	if def == nil || manifestPlugin == nil {
		return
	}
	for opName, override := range allowedOperations {
		if override == nil || !override.Paginate {
			continue
		}
		pgn := mergedPaginationConfig(manifestPlugin.Pagination, override.Pagination)
		if pgn == nil {
			continue
		}
		op := def.Operations[opName]
		op.Pagination = &provider.PaginationDef{
			Style:        string(pgn.Style),
			CursorParam:  pgn.CursorParam,
			Cursor:       cloneManifestValueSelectorDef(pgn.Cursor),
			LimitParam:   pgn.LimitParam,
			DefaultLimit: pgn.DefaultLimit,
			ResultsPath:  pgn.ResultsPath,
			MaxPages:     pgn.MaxPages,
		}
		def.Operations[opName] = op
	}
}

func mergedPaginationConfig(base, override *providermanifestv1.ManifestPaginationConfig) *providermanifestv1.ManifestPaginationConfig {
	if base == nil && override == nil {
		return nil
	}
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}
	merged := *base
	if override.Style != "" {
		merged.Style = override.Style
	}
	if override.CursorParam != "" {
		merged.CursorParam = override.CursorParam
	}
	if override.Cursor != nil {
		merged.Cursor = cloneManifestValueSelector(override.Cursor)
	}
	if override.LimitParam != "" {
		merged.LimitParam = override.LimitParam
	}
	if override.DefaultLimit != 0 {
		merged.DefaultLimit = override.DefaultLimit
	}
	if override.ResultsPath != "" {
		merged.ResultsPath = override.ResultsPath
	}
	if override.MaxPages != 0 {
		merged.MaxPages = override.MaxPages
	}
	return &merged
}

func cloneManifestValueSelector(in *providermanifestv1.ManifestValueSelector) *providermanifestv1.ManifestValueSelector {
	if in == nil {
		return nil
	}
	return &providermanifestv1.ManifestValueSelector{
		Source: in.Source,
		Path:   in.Path,
	}
}

func cloneManifestValueSelectorDef(in *providermanifestv1.ManifestValueSelector) *provider.ValueSelectorDef {
	if in == nil {
		return nil
	}
	return &provider.ValueSelectorDef{
		Source: in.Source,
		Path:   in.Path,
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

func buildOAuthHandlerFromAuth(auth *config.ConnectionAuthDef, pluginConfig map[string]any, deps Deps) (OAuthHandler, error) {
	if auth == nil || auth.Type != "oauth2" {
		return nil, nil
	}

	clientID := auth.ClientID
	clientSecret := auth.ClientSecret
	if id, _ := pluginConfig["clientId"].(string); id != "" {
		clientID = id
	}
	if sec, _ := pluginConfig["clientSecret"].(string); sec != "" {
		clientSecret = sec
	}
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("clientId and clientSecret are required for oauth2 auth")
	}

	var tokenExchange oauth.TokenExchangeFormat
	switch auth.TokenExchange {
	case "", "form":
		tokenExchange = oauth.TokenExchangeForm
	case "json":
		tokenExchange = oauth.TokenExchangeJSON
	default:
		return nil, fmt.Errorf("unknown tokenExchange %q", auth.TokenExchange)
	}

	oauthCfg := oauth.UpstreamConfig{
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		AuthorizationURL:    auth.AuthorizationURL,
		TokenURL:            auth.TokenURL,
		RedirectURL:         deps.BaseURL + config.IntegrationCallbackPath,
		PKCE:                auth.PKCE,
		DefaultScopes:       auth.Scopes,
		ScopeParam:          auth.ScopeParam,
		ScopeSeparator:      auth.ScopeSeparator,
		TokenExchange:       tokenExchange,
		AuthorizationParams: auth.AuthorizationParams,
		TokenParams:         auth.TokenParams,
		RefreshParams:       auth.RefreshParams,
		AcceptHeader:        auth.AcceptHeader,
		AccessTokenPath:     auth.AccessTokenPath,
	}
	if auth.ClientAuth == "header" {
		oauthCfg.ClientAuthMethod = oauth.ClientAuthHeader
	}

	return WrapUpstreamHandler(oauth.NewUpstream(oauthCfg)), nil
}

func buildOAuthHandlerFromDefinition(def *provider.Definition, conn config.ConnectionDef, pluginConfig map[string]any, deps Deps) (OAuthHandler, error) {
	if def == nil || def.Auth.Type != "oauth2" {
		return nil, nil
	}

	effectiveConn := conn
	if id, _ := pluginConfig["clientId"].(string); id != "" {
		effectiveConn.Auth.ClientID = id
	}
	if sec, _ := pluginConfig["clientSecret"].(string); sec != "" {
		effectiveConn.Auth.ClientSecret = sec
	}
	if effectiveConn.Auth.ClientID == "" || effectiveConn.Auth.ClientSecret == "" {
		return nil, fmt.Errorf("clientId and clientSecret are required for oauth2 auth")
	}
	if effectiveConn.Auth.RedirectURL == "" {
		effectiveConn.Auth.RedirectURL = deps.BaseURL + config.IntegrationCallbackPath
	}

	defCopy := *def
	provider.ApplyConnectionAuth(&defCopy, effectiveConn)
	upstream, err := provider.BuildOAuthUpstream(&defCopy, effectiveConn, defCopy.BaseURL, nil)
	if err != nil {
		return nil, err
	}
	return WrapUpstreamHandler(upstream), nil
}

func buildMCPOAuthHandler(conn config.ConnectionDef, mcpURL string, store mcpoauth.RegistrationStore, deps Deps) *mcpoauth.Handler {
	redirectURL := conn.Auth.RedirectURL
	if redirectURL == "" {
		redirectURL = deps.BaseURL + config.IntegrationCallbackPath
	}
	return mcpoauth.NewHandler(mcpoauth.HandlerConfig{
		MCPURL:       mcpURL,
		Store:        store,
		RedirectURL:  redirectURL,
		ClientID:     conn.Auth.ClientID,
		ClientSecret: conn.Auth.ClientSecret,
	})
}

func lazyRefreshers(ready <-chan struct{}, resolver func() map[string]map[string]OAuthHandler) invocation.RefresherResolver {
	var once sync.Once
	var result map[string]map[string]invocation.OAuthRefresher
	return func() map[string]map[string]invocation.OAuthRefresher {
		once.Do(func() {
			<-ready
			result = connectionAuthToRefreshers(resolver())
		})
		return result
	}
}

func connectionAuthToRefreshers(m map[string]map[string]OAuthHandler) map[string]map[string]invocation.OAuthRefresher {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]map[string]invocation.OAuthRefresher, len(m))
	for intg, conns := range m {
		inner := make(map[string]invocation.OAuthRefresher, len(conns))
		for conn, handler := range conns {
			inner[conn] = handler
		}
		out[intg] = inner
	}
	return out
}

package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/composite"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/graphql"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/mcpoauth"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/openapi"
	"github.com/valon-technologies/gestalt/server/internal/operationexposure"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/registry"
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
		if deps.WorkflowRuntime != nil && deps.WorkflowRuntime.HasBinding(name) {
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
	switch mode {
	case core.ConnectionModeNone, core.ConnectionModeUser, core.ConnectionModeIdentity:
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
	mcpURL := ""
	if resolved, ok := plan.ResolvedSurface(config.SpecSurfaceMCP); ok {
		mcpURL = resolved.URL
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

	resolved, hasSpecSurface := plan.ConfiguredSpecSurface()
	if !hasSpecSurface {
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

	specProv, specDef, err := buildConfiguredSpecProvider(ctx, name, resolved, meta, specProviderConfig{
		manifestPlugin:       manifestPlugin,
		allowedOperations:    allowedOperations,
		allowedHosts:         entry.AllowedHosts,
		baseURL:              config.EffectiveProviderSpecBaseURL(entry, manifestPlugin),
		applyResponseMapping: true,
		providerBuildOptions: func(conn config.ConnectionDef) []provider.BuildOption {
			return mcpOAuthBuildOpts(conn, mcpURL, deps)
		},
	}, deps)
	if err != nil {
		closeIfPossible(pluginProv)
		return nil, fmt.Errorf("build hybrid spec provider %q: %w", name, err)
	}
	merged, err := composite.NewMergedWithConnections(
		name,
		pluginProv.DisplayName(),
		pluginProv.Description(),
		firstProviderIconSVG(pluginProv, specProv),
		composite.BoundProvider{Provider: pluginProv, Connection: hybridPluginOperationConnection(plan, resolved.ConnectionName)},
		composite.BoundProvider{Provider: specProv, Connection: resolved.ConnectionName},
	)
	if err != nil {
		closeIfPossible(specProv, pluginProv)
		return nil, err
	}
	var authFallback *specAuthFallback
	if specDef != nil {
		authFallback = &specAuthFallback{
			definition:     specDef,
			connectionName: resolved.ConnectionName,
		}
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
	definition     *provider.Definition
	connectionName string
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

func buildSpecLoadedProvider(ctx context.Context, name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, pluginConfig map[string]any, meta providerMetadata, deps Deps, allowedOperations map[string]*config.OperationOverride) (*ProviderBuildResult, error) {
	mp := manifest.Spec
	plan, err := config.BuildStaticConnectionPlan(entry, mp)
	if err != nil {
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}
	apiResolved, hasAPI := plan.ConfiguredAPISurface()
	mcpResolved, hasMCP := plan.ResolvedSurface(config.SpecSurfaceMCP)
	mcpURL := ""
	if hasMCP {
		mcpURL = mcpResolved.URL
	}
	if !hasAPI && !hasMCP {
		return nil, fmt.Errorf("build spec-loaded provider %q: no spec URL", name)
	}

	buildSpec := func(resolved config.ResolvedSpecSurface, allowed map[string]*config.OperationOverride) (core.Provider, *provider.Definition, error) {
		return buildConfiguredSpecProvider(ctx, name, resolved, meta, specProviderConfig{
			manifestPlugin:       mp,
			allowedOperations:    allowed,
			allowedHosts:         entry.AllowedHosts,
			baseURL:              config.EffectiveProviderSpecBaseURL(entry, mp),
			applyResponseMapping: true,
			providerBuildOptions: func(conn config.ConnectionDef) []provider.BuildOption {
				return mcpOAuthBuildOpts(conn, mcpURL, deps)
			},
		}, deps)
	}

	if !hasAPI {
		prov, _, err := buildSpec(mcpResolved, allowedOperations)
		if err != nil {
			return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
		}
		return newProviderBuildResult(name, entry, manifest, pluginConfig, prov, nil, deps)
	}

	apiProv, apiDef, err := buildSpec(apiResolved, allowedOperations)
	if err != nil {
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}
	authFallback := &specAuthFallback{
		definition:     apiDef,
		connectionName: apiResolved.ConnectionName,
	}

	if !hasMCP {
		return newProviderBuildResult(name, entry, manifest, pluginConfig, apiProv, authFallback, deps)
	}

	mcpProv, _, err := buildSpec(mcpResolved, nil)
	if err != nil {
		closeIfPossible(apiProv)
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}
	mcpUp, ok := mcpProv.(composite.MCPUpstream)
	if !ok {
		closeIfPossible(mcpProv, apiProv)
		return nil, fmt.Errorf("build spec-loaded provider %q: unexpected mcp provider type %T", name, mcpProv)
	}

	filtered := operationexposure.MatchingAllowedOperations(allowedOperations, mcpUp.Catalog())
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

	return newProviderBuildResult(name, entry, manifest, pluginConfig, composite.New(name, apiProv, mcpUp), authFallback, deps)
}

func loadConfiguredAPIDefinition(ctx context.Context, name string, resolved config.ResolvedSpecSurface, meta providerMetadata, cfg specProviderConfig) (*provider.Definition, error) {
	def, err := loadSpecDefinition(ctx, name, resolved, cfg.allowedOperations)
	if err != nil {
		return nil, fmt.Errorf("load %s definition: %w", resolved.Surface, err)
	}
	if cfg.baseURL != "" {
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
		return graphql.LoadDefinition(ctx, name, resolved.URL, allowedOperations)
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
	if command == "" {
		if entry.ResolvedManifestPath == "" {
			return nil, fmt.Errorf("resolved manifest path is required for synthesized source provider execution")
		}
		rootDir := filepath.Dir(entry.ResolvedManifestPath)
		var err error
		command, args, cleanup, err = providerpkg.SourceProviderExecutionCommand(rootDir, runtime.GOOS, runtime.GOARCH)
		if errors.Is(err, providerpkg.ErrNoSourceProviderPackage) {
			return nil, fmt.Errorf("prepare synthesized source provider execution: no Go or Python provider source found")
		}
		if err != nil {
			return nil, fmt.Errorf("prepare synthesized source provider execution: %w", err)
		}
		execEnv, err := providerpkg.SourceProviderExecutionEnv(rootDir, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return nil, fmt.Errorf("prepare synthesized source provider environment: %w", err)
		}
		if len(execEnv) > 0 {
			if env == nil {
				env = make(map[string]string, len(execEnv))
			}
			maps.Copy(env, execEnv)
		}
	}
	execCfg := providerhost.ExecConfig{
		Command:       command,
		Args:          args,
		Env:           env,
		StaticSpec:    spec,
		Config:        pluginConfig,
		AllowedHosts:  entry.AllowedHosts,
		DefaultAction: deps.Egress.DefaultAction,
		HostBinary:    entry.HostBinary,
	}
	effectiveIndexedDB, err := config.ResolveEffectivePluginIndexedDB(name, entry, deps.SelectedIndexedDBName, deps.IndexedDBDefs)
	if err != nil {
		return nil, err
	}
	if effectiveIndexedDB.Enabled {
		hostServices, indexedDBCleanup, err := buildPluginIndexedDBHostServices(name, effectiveIndexedDB, deps)
		if err != nil {
			return nil, err
		}
		execCfg.HostServices = append(execCfg.HostServices, hostServices...)
		cleanup = chainCleanup(cleanup, indexedDBCleanup)
	}
	if len(entry.Cache) > 0 {
		hostServices, cacheCleanup, err := buildPluginCacheHostServices(name, entry, deps)
		if err != nil {
			return nil, err
		}
		execCfg.HostServices = append(execCfg.HostServices, hostServices...)
		cleanup = chainCleanup(cleanup, cacheCleanup)
	}
	if entry.Workflow != nil {
		hostServices, err := buildPluginWorkflowHostServices(name, deps)
		if err != nil {
			return nil, err
		}
		execCfg.HostServices = append(execCfg.HostServices, hostServices...)
	}
	if len(entry.S3) > 0 {
		hostServices, err := buildPluginS3HostServices(name, entry, deps)
		if err != nil {
			return nil, err
		}
		execCfg.HostServices = append(execCfg.HostServices, hostServices...)
	}
	if len(entry.Invokes) > 0 {
		hostService, snapshots := buildPluginInvokerHostService(name, entry, deps)
		execCfg.HostServices = append(execCfg.HostServices, hostService)
		execCfg.RequestSnapshots = snapshots
	}
	execCfg.Cleanup = cleanup
	prov, err := providerhost.NewExecutableProvider(ctx, execCfg)
	if err != nil {
		return nil, err
	}
	cleanup = nil
	return prov, nil
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

func buildPluginWorkflowHostServices(pluginName string, deps Deps) ([]providerhost.HostService, error) {
	if deps.WorkflowRuntime == nil {
		return nil, fmt.Errorf("workflow host services are not available")
	}
	return []providerhost.HostService{{
		EnvVar: providerhost.DefaultWorkflowSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterWorkflowServer(srv, providerhost.NewWorkflowServer(pluginName, func() (coreworkflow.Provider, map[string]struct{}, providerhost.WorkflowManagedIDs, error) {
				provider, allowed, err := deps.WorkflowRuntime.ResolvePlugin(pluginName)
				if err != nil {
					return nil, nil, providerhost.WorkflowManagedIDs{}, err
				}
				return provider, allowed, providerhost.WorkflowManagedIDs{
					Schedules:     deps.WorkflowRuntime.ManagedScheduleIDs(pluginName),
					EventTriggers: deps.WorkflowRuntime.ManagedEventTriggerIDs(pluginName),
				}, nil
			}))
		},
	}}, nil
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

func buildPluginInvokerHostService(pluginName string, entry *config.ProviderEntry, deps Deps) (providerhost.HostService, *providerhost.RequestSnapshotStore) {
	invoker := deps.PluginInvoker
	if invoker == nil {
		invoker = unavailablePluginInvoker{}
	}
	snapshots := providerhost.NewRequestSnapshotStore()
	return providerhost.HostService{
		EnvVar: providerhost.DefaultPluginInvokerSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterPluginInvokerServer(srv, providerhost.NewPluginInvokerServer(pluginName, entry.Invokes, invoker, snapshots))
		},
	}, snapshots
}

type unavailablePluginInvoker struct{}

func (unavailablePluginInvoker) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return nil, fmt.Errorf("plugin invoker is not available")
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
		DiscoveryConfig:  providerhost.DiscoveryConfigFromManifest(manifest.Spec.Discovery),
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

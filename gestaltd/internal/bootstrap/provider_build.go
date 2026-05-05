package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
	cacheservice "github.com/valon-technologies/gestalt/server/services/cache"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/plugins/composite"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
	"github.com/valon-technologies/gestalt/server/services/plugins/graphql"
	"github.com/valon-technologies/gestalt/server/services/plugins/mcpoauth"
	"github.com/valon-technologies/gestalt/server/services/plugins/mcpupstream"
	"github.com/valon-technologies/gestalt/server/services/plugins/oauth"
	"github.com/valon-technologies/gestalt/server/services/plugins/openapi"
	"github.com/valon-technologies/gestalt/server/services/plugins/operationexposure"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
	"github.com/valon-technologies/gestalt/server/services/s3"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
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

func buildProviders(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (*registry.ProviderMap[core.Provider], <-chan struct{}, func() map[string]map[string]OAuthHandler, func() map[string]map[string]ManualTokenExchanger, error) {
	providers, ready, connAuthResolver, manualConnAuthResolver, _, err := buildProvidersAsync(ctx, cfg, factories, deps, buildProvider)
	return providers, ready, connAuthResolver, manualConnAuthResolver, err
}

func buildProvidersAsync(
	ctx context.Context,
	cfg *config.Config,
	factories *FactoryRegistry,
	deps Deps,
	builder func(context.Context, string, *config.ProviderEntry, Deps) (*ProviderBuildResult, error),
) (*registry.ProviderMap[core.Provider], <-chan struct{}, func() map[string]map[string]OAuthHandler, func() map[string]map[string]ManualTokenExchanger, func() []error, error) {
	reg := registry.New()
	connAuth := make(map[string]map[string]OAuthHandler)
	manualConnAuth := make(map[string]map[string]ManualTokenExchanger)
	var buildErrs []error
	var connMu sync.Mutex

	for _, builtin := range factories.Builtins {
		if err := validateProviderConnectionMode(builtin.Name(), builtin.ConnectionMode()); err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("bootstrap: builtin provider %q: %w", builtin.Name(), err)
		}
		if err := reg.Providers.Register(builtin.Name(), builtin); errors.Is(err, core.ErrAlreadyRegistered) {
			continue
		} else if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("bootstrap: registering builtin %q: %w", builtin.Name(), err)
		}
		slog.Info("loaded builtin provider", "provider", builtin.Name(), "operations", catalogOperationCount(builtin.Catalog()))
	}

	ready := make(chan struct{})
	if len(cfg.Plugins) == 0 {
		close(ready)
		return &reg.Providers, ready,
			func() map[string]map[string]OAuthHandler { return connAuth },
			func() map[string]map[string]ManualTokenExchanger { return manualConnAuth },
			func() []error { return nil },
			nil
	}

	var wg sync.WaitGroup
	var errMu sync.Mutex
	for name := range cfg.Plugins {
		intgDef := cfg.Plugins[name]
		var proxy *startupProviderProxy
		if deps.WorkflowRuntime != nil {
			spec, operationRouting, err := buildStartupProviderSpec(name, intgDef)
			if err != nil {
				slog.Warn("building startup provider proxy metadata failed", "provider", name, "error", err)
			} else {
				proxy = newStartupProviderProxy(spec, operationRouting, deps.WorkflowRuntime.StartupWaitTracker())
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
			if len(result.ManualConnectionAuth) > 0 {
				connMu.Lock()
				manualConnAuth[name] = result.ManualConnectionAuth
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
	manualResolver := func() map[string]map[string]ManualTokenExchanger {
		<-ready
		return manualConnAuth
	}
	errResolver := func() []error {
		<-ready
		errMu.Lock()
		defer errMu.Unlock()
		return append([]error(nil), buildErrs...)
	}
	return &reg.Providers, ready, resolver, manualResolver, errResolver, nil
}

func validateProviderConnectionMode(provider string, mode core.ConnectionMode) error {
	switch core.NormalizeConnectionMode(mode) {
	case core.ConnectionModeNone, core.ConnectionModeUser, core.ConnectionModePlatform:
		return nil
	default:
		return fmt.Errorf("unsupported connection mode %q for provider %q", mode, provider)
	}
}

func BuildStartupProviderSpec(name string, entry *config.ProviderEntry) (pluginservice.StaticProviderSpec, map[string]string, error) {
	spec, routing, err := buildStartupProviderSpec(name, entry)
	return spec, routing.connections, err
}

type startupOperationRouting struct {
	connections    map[string]string
	resolver       core.OperationConnectionResolver
	overridePolicy core.OperationConnectionOverridePolicy
}

func buildStartupProviderSpec(name string, entry *config.ProviderEntry) (pluginservice.StaticProviderSpec, startupOperationRouting, error) {
	if entry == nil {
		return pluginservice.StaticProviderSpec{}, startupOperationRouting{}, fmt.Errorf("integration %q has no plugin defined", name)
	}
	manifest := entry.ResolvedManifest
	manifestPlugin := entry.ManifestSpec()
	if manifest == nil || manifestPlugin == nil {
		return pluginservice.StaticProviderSpec{}, startupOperationRouting{}, fmt.Errorf("integration %q must resolve to a provider manifest", name)
	}

	meta := resolveProviderMetadata(entry)
	spec, plan, err := buildPluginStaticSpec(name, entry, manifest, meta)
	if err != nil {
		return pluginservice.StaticProviderSpec{}, startupOperationRouting{}, err
	}
	restConnections, restSelectors, restLocks, err := plan.RESTOperationConnectionBindings(manifestPlugin)
	if err != nil {
		return pluginservice.StaticProviderSpec{}, startupOperationRouting{}, err
	}
	if spec.Catalog != nil {
		return spec, startupOperationRouting{connections: operationConnectionsForCatalog(spec.Catalog, plan, restConnections)}, nil
	}
	if !manifestPlugin.IsDeclarative() && !manifestPlugin.IsSpecLoaded() {
		return spec, startupOperationRouting{connections: map[string]string{}}, nil
	}
	declarative, err := pluginservice.NewDeclarativeProvider(
		manifest,
		nil,
		pluginservice.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
		pluginservice.WithDeclarativeConnectionMode(plan.ConnectionMode()),
		pluginservice.WithDeclarativeOperationConnections(restConnections, restSelectors, restLocks),
	)
	if err != nil {
		return pluginservice.StaticProviderSpec{}, startupOperationRouting{}, err
	}
	spec.Catalog = declarative.Catalog()
	return spec, startupOperationRouting{
		connections:    operationConnectionsForCatalog(spec.Catalog, plan, restConnections),
		resolver:       declarative,
		overridePolicy: declarative,
	}, nil
}

func operationConnectionsForCatalog(cat *catalog.Catalog, plan config.StaticConnectionPlan, restConnections map[string]string) map[string]string {
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
			if resolved := restConnections[operation.ID]; resolved != "" {
				connection = resolved
			} else {
				connection = plan.RESTConnection()
			}
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
	pluginConnection := plan.ResolvedPluginConnection()
	if pluginConnection.Source.ModeSource == config.ConfigSourceDeploy ||
		pluginConnection.Source.AuthSource == config.ConfigSourceDeploy ||
		len(pluginConnection.Params) > 0 {
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
		restConnections, restSelectors, restLocks, err := plan.RESTOperationConnectionBindings(manifestPlugin)
		if err != nil {
			return nil, fmt.Errorf("build declarative provider %q: %w", name, err)
		}
		declarative, err := pluginservice.NewDeclarativeProvider(
			manifest,
			nil,
			pluginservice.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
			pluginservice.WithDeclarativeConnectionMode(plan.ConnectionMode()),
			pluginservice.WithDeclarativeOperationConnections(restConnections, restSelectors, restLocks),
			pluginservice.WithDeclarativeEgressCheck(deps.Egress.CheckFunc(entry.EffectiveAllowedHosts())),
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
		restConnections, restSelectors, restLocks, err := plan.RESTOperationConnectionBindings(manifestPlugin)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, fmt.Errorf("build declarative provider %q: %w", name, err)
		}
		filteredPluginProv, err := applyAllowedOperations(name, staticAllowedOperations, pluginProv)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, err
		}
		pluginProv = filteredPluginProv
		declarative, err := pluginservice.NewDeclarativeProvider(
			manifest,
			nil,
			pluginservice.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
			pluginservice.WithDeclarativeConnectionMode(plan.ConnectionMode()),
			pluginservice.WithDeclarativeOperationConnections(restConnections, restSelectors, restLocks),
			pluginservice.WithDeclarativeEgressCheck(deps.Egress.CheckFunc(entry.EffectiveAllowedHosts())),
		)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, fmt.Errorf("create declarative provider %q: %w", name, err)
		}
		if len(declarative.PostConnectConfigs) > 0 && core.SupportsPostConnect(pluginProv) {
			closeIfPossible(pluginProv)
			closeIfPossible(declarative)
			return nil, fmt.Errorf("provider %q declares postConnect in the manifest and implements provider post-connect; remove one to avoid metadata conflicts", name)
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
			composite.BoundProvider{Provider: apiProv, FallbackConnection: plan.RESTConnection()},
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
	if core.SupportsPostConnect(specProv) && core.SupportsPostConnect(pluginProv) {
		closeIfPossible(specProv, pluginProv)
		return nil, fmt.Errorf("provider %q declares postConnect in the manifest and implements provider post-connect; remove one to avoid metadata conflicts", name)
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
	providerBuildOptions func(config.ConnectionDef) []declarative.BuildOption
	applyResponseMapping bool
}

type specAuthFallback struct {
	definitions map[string]*declarative.Definition
}

func newSpecAuthFallback() *specAuthFallback {
	return &specAuthFallback{definitions: make(map[string]*declarative.Definition)}
}

func (f *specAuthFallback) add(connectionName string, def *declarative.Definition) {
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

func (f *specAuthFallback) definitionFor(connectionName string) *declarative.Definition {
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
	result.ManualConnectionAuth, err = buildManualConnectionAuthMap(name, entry, manifest, authFallback)
	if err != nil {
		closeIfPossible(prov)
		return nil, err
	}
	return result, nil
}

type builtSpecSurface struct {
	provider   core.Provider
	resolved   config.ResolvedSpecSurface
	definition *declarative.Definition
}

type postConnectOnlyProvider struct {
	connection string
	provider   core.Provider
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
		allowedHosts:         entry.EffectiveAllowedHosts(),
		baseURL:              config.EffectiveProviderSpecBaseURL(entry, manifestPlugin),
		applyResponseMapping: true,
		providerBuildOptions: func(conn config.ConnectionDef) []declarative.BuildOption {
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
			FilterOperations(map[string]*operationexposure.OperationOverride) error
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
	surfaceConnections := make(map[string]bool, len(resolvedSurfaces))
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
		if resolved.ConnectionName != "" {
			surfaceConnections[resolved.ConnectionName] = true
		}
		authFallback.add(resolved.ConnectionName, def)
	}

	postConnectOnly, err := buildPostConnectOnlyProviders(name, plan, meta, cfg, deps, surfaceConnections)
	if err != nil {
		closeBuiltSpecSurfaces(built)
		return nil, nil, err
	}

	if len(built) == 1 && len(postConnectOnly) == 0 {
		if authFallback.empty() {
			authFallback = nil
		}
		return bindProviderConnection(built[0].provider, built[0].resolved.ConnectionName), authFallback, nil
	}

	boundProviders := make([]composite.BoundProvider, 0, len(built)+len(postConnectOnly))
	providers := make([]core.Provider, 0, len(built)+len(postConnectOnly))
	for i := range built {
		specSurface := &built[i]
		boundProviders = append(boundProviders, composite.BoundProvider{
			Provider:   specSurface.provider,
			Connection: specSurface.resolved.ConnectionName,
		})
		providers = append(providers, specSurface.provider)
	}
	for i := range postConnectOnly {
		boundProviders = append(boundProviders, composite.BoundProvider{
			Provider:   postConnectOnly[i].provider,
			Connection: postConnectOnly[i].connection,
		})
		providers = append(providers, postConnectOnly[i].provider)
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
		closePostConnectOnlyProviders(postConnectOnly)
		return nil, nil, err
	}
	if authFallback.empty() {
		authFallback = nil
	}
	return merged, authFallback, nil
}

func buildPostConnectOnlyProviders(name string, plan config.StaticConnectionPlan, meta providerMetadata, cfg specProviderConfig, deps Deps, excludedConnections map[string]bool) ([]postConnectOnlyProvider, error) {
	var providers []postConnectOnlyProvider
	for _, connectionName := range plan.NamedConnectionNames() {
		if excludedConnections[connectionName] {
			continue
		}
		conn, ok := plan.NamedConnectionDef(connectionName)
		if !ok || conn.PostConnect == nil {
			continue
		}
		prov, err := buildPostConnectOnlyProvider(name, connectionName, conn, meta, cfg, deps)
		if err != nil {
			closePostConnectOnlyProviders(providers)
			return nil, err
		}
		providers = append(providers, postConnectOnlyProvider{
			connection: connectionName,
			provider:   prov,
		})
	}
	return providers, nil
}

func buildPostConnectOnlyProvider(name, connectionName string, conn config.ConnectionDef, meta providerMetadata, cfg specProviderConfig, deps Deps) (core.Provider, error) {
	def := &declarative.Definition{
		Provider:    name,
		DisplayName: meta.displayName,
		Description: meta.description,
		IconSVG:     meta.iconSVG,
		BaseURL:     cfg.baseURL,
		Headers:     manifestHeaders(cfg.manifestPlugin),
	}
	buildOpts := []declarative.BuildOption{
		declarative.WithEgressCheck(deps.Egress.CheckFunc(cfg.allowedHosts)),
	}
	if cfg.providerBuildOptions != nil {
		buildOpts = append(buildOpts, cfg.providerBuildOptions(conn)...)
	}
	prov, err := declarative.Build(def, declarativeNamedConnectionDef(connectionName, conn), buildOpts...)
	if err != nil {
		return nil, fmt.Errorf("build post-connect provider for connection %q: %w", connectionName, err)
	}
	return prov, nil
}

func closeBuiltSpecSurfaces(surfaces []builtSpecSurface) {
	for i := range surfaces {
		closeIfPossible(surfaces[i].provider)
	}
}

func closePostConnectOnlyProviders(providers []postConnectOnlyProvider) {
	for i := range providers {
		closeIfPossible(providers[i].provider)
	}
}

func loadConfiguredAPIDefinition(ctx context.Context, name string, resolved config.ResolvedSpecSurface, meta providerMetadata, cfg specProviderConfig) (*declarative.Definition, error) {
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

func buildConfiguredSpecProvider(ctx context.Context, name string, resolved config.ResolvedSpecSurface, meta providerMetadata, cfg specProviderConfig, deps Deps) (core.Provider, *declarative.Definition, error) {
	var buildOpts []declarative.BuildOption
	buildOpts = append(buildOpts, declarative.WithEgressCheck(deps.Egress.CheckFunc(cfg.allowedHosts)))
	if cfg.providerBuildOptions != nil {
		buildOpts = append(buildOpts, cfg.providerBuildOptions(resolved.Connection)...)
	}

	switch resolved.Surface {
	case config.SpecSurfaceOpenAPI, config.SpecSurfaceGraphQL:
		def, err := loadConfiguredAPIDefinition(ctx, name, resolved, meta, cfg)
		if err != nil {
			return nil, nil, err
		}
		prov, err := declarative.Build(def, declarativeNamedConnectionDef(resolved.ConnectionName, resolved.Connection), buildOpts...)
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

func loadSpecDefinition(ctx context.Context, name string, resolved config.ResolvedSpecSurface, allowedOperations map[string]*config.OperationOverride) (*declarative.Definition, error) {
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

func buildPluginProvider(ctx context.Context, name string, entry *config.ProviderEntry, pluginConfig map[string]any, spec pluginservice.StaticProviderSpec, deps Deps) (core.Provider, error) {
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
	runtimeSupport, err := runtimeProvider.Support(ctx)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, fmt.Errorf("query %s support: %w", hostedRuntimeLabel(runtimeConfig), err)
	}
	runtimePlan, err := buildPluginRuntimePlan(name, entry, deps, runtimeSupport)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	if err := runtimePlan.Validate(hostedRuntimeLabel(runtimeConfig)); err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	if command == "" && !hostedRuntimeUsesImageEntrypoint(runtimeConfig) {
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
	launch, err := prepareHostedProcessLaunch(providermanifestv1.KindPlugin, name, entry, command, args, cleanup, runtimeConfig)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	command = launch.command
	args = launch.args
	cleanup = launch.cleanup
	session, err := runtimeProvider.StartSession(ctx, buildHostedRuntimeStartSessionRequest(providermanifestv1.KindPlugin, name, runtimeConfig))
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
	if _, err := waitForPluginRuntimeSessionReady(ctx, runtimeProvider, sessionID); err != nil {
		return nil, fmt.Errorf("wait for plugin runtime session %q ready: %w", sessionID, err)
	}

	hostServices, invTokens, runtimeCleanup, err := buildPluginRuntimeHostServices(name, entry, deps)
	if err != nil {
		return nil, err
	}
	hostServices = appendRuntimeLogHostService(hostServices, runtimeConfig, deps, runtimePlan)
	publicHostServicesCleanup, err := registerPublicRuntimeHostServices(name, hostServices, deps, runtimePlan, runtimeProvider)
	if err != nil {
		return nil, err
	}
	cleanup = chainCleanup(cleanup, runtimeCleanup, publicHostServicesCleanup)
	startEnv := maps.Clone(env)
	startEnv = withRuntimeSessionEnv(startEnv, sessionID)
	startEnv = withHostServiceTLSCAEnv(startEnv, deps)
	egressPolicy := deps.Egress.ProviderPolicy(entry)
	allowedHosts := entry.EffectiveAllowedHosts()
	bindingTargets := hostServiceBindingDescriptorsFromConfigured(hostServices)
	for _, hostService := range bindingTargets {
		bindingEnv, relayHost, err := buildHostedRuntimeHostServiceEnv(name, sessionID, hostService, deps)
		if err != nil {
			return nil, err
		}
		if len(bindingEnv) > 0 {
			if startEnv == nil {
				startEnv = make(map[string]string, len(bindingEnv))
			}
			maps.Copy(startEnv, bindingEnv)
		}
		if runtimePlan.RequiresHostnameEgress {
			allowedHosts = appendAllowedHost(allowedHosts, relayHost)
		}
	}
	egressPlan, err := buildHostedRuntimeEgressLaunchPlan(name, sessionID, egressPolicy, allowedHosts, runtimePlan, deps)
	if err != nil {
		return nil, err
	}
	if len(egressPlan.Env) > 0 {
		if startEnv == nil {
			startEnv = make(map[string]string, len(egressPlan.Env))
		}
		maps.Copy(startEnv, egressPlan.Env)
	}

	hostedPlugin, err := runtimeProvider.StartPlugin(ctx, pluginruntime.StartPluginRequest{
		SessionID:  sessionID,
		PluginName: name,
		Command:    command,
		Args:       args,
		Env:        startEnv,
		Egress: pluginruntime.RuntimeEgressPolicy{
			AllowedHosts:  egressPlan.RuntimeAllowedHosts,
			DefaultAction: pluginruntime.PolicyAction(deps.Egress.DefaultAction),
		},
		HostBinary: entry.HostBinary,
	})
	if err != nil {
		return nil, fmt.Errorf("start hosted plugin: %w", err)
	}
	conn, err := pluginruntime.DialHostedPlugin(ctx, hostedPlugin.DialTarget,
		pluginruntime.WithProviderName(name),
		pluginruntime.WithTelemetry(deps.Telemetry),
	)
	if err != nil {
		return nil, fmt.Errorf("dial hosted plugin: %w", err)
	}
	opts := []pluginservice.RemoteProviderOption{
		pluginservice.WithCloser(&runtimeBackedHostedCloser{
			conn:         conn,
			runtime:      runtimeProvider,
			sessionID:    sessionID,
			closeRuntime: runtimeOwned,
			cleanup:      cleanup,
		}),
		pluginservice.WithHostContext(deps.BaseURL),
	}
	if invTokens != nil {
		opts = append(opts,
			pluginservice.WithInvocationTokens(invTokens),
			pluginservice.WithInvocationTokenSubject(name, plugininvokerservice.InvocationDependencyGrants(pluginInvocationDependencies(entry.Invokes))),
		)
	}
	prov, err := pluginservice.NewRemote(ctx, conn.Integration(), spec, pluginConfig, opts...)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	stopSession = false
	cleanup = nil
	return prov, nil
}

func buildHostedAgentProvider(ctx context.Context, name string, entry *config.ProviderEntry, node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (coreagent.Provider, error) {
	launch, err := prepareHostedAgentProviderLaunch(ctx, name, entry, node, deps)
	if err != nil {
		return nil, err
	}
	runtimeCfg := entry.HostedRuntimeConfig()
	policy, err := runtimeCfg.LifecyclePolicy()
	if err != nil {
		launch.close()
		return nil, fmt.Errorf("parse hosted agent runtime lifecycle policy: %w", err)
	}
	if policy.RestartPolicy == config.HostedRuntimeRestartPolicyAlways && entry.IndexedDB == nil {
		launch.close()
		return nil, fmt.Errorf("hosted agent runtime restart policy %q requires indexeddb persistence hook", config.HostedRuntimeRestartPolicyAlways)
	}
	hostServices = appendRuntimeLogHostService(hostServices, launch.runtimeConfig, deps, launch.runtimePlan)
	publicHostServicesCleanup, err := registerPublicRuntimeHostServices(name, hostServices, deps, launch.runtimePlan, launch.runtimeProvider)
	if err != nil {
		launch.close()
		return nil, err
	}
	launch.cleanup = chainCleanup(launch.cleanup, publicHostServicesCleanup)
	return newHostedAgentProviderPool(ctx, launch, hostServices, deps, policy)
}

type hostedAgentProviderLaunch struct {
	name            string
	runtimeConfig   config.EffectiveHostedRuntime
	runtimeProvider pluginruntime.Provider
	runtimeOwned    bool
	runtimePlan     HostedRuntimePlan
	cfg             componentprovider.YAMLConfig
	allowedHosts    []string
	launch          hostedProcessLaunch
	cleanup         func()
}

type hostedAgentProviderInstance struct {
	provider         coreagent.Provider
	runtimeProvider  pluginruntime.Provider
	runtimeSessionID string
	runtimeSession   *pluginruntime.Session
}

func (p *hostedAgentProviderLaunch) close() {
	if p == nil {
		return
	}
	if p.runtimeOwned && p.runtimeProvider != nil {
		_ = p.runtimeProvider.Close()
	}
	if p.cleanup != nil {
		p.cleanup()
		p.cleanup = nil
	}
}

func prepareHostedAgentProviderLaunch(ctx context.Context, name string, entry *config.ProviderEntry, node yaml.Node, deps Deps) (*hostedAgentProviderLaunch, error) {
	runtimeConfig, runtimeProvider, runtimeOwned, err := effectiveConfiguredHostedRuntime(ctx, "providers.agent."+name, entry, deps)
	if err != nil {
		return nil, err
	}
	if runtimeProvider == nil {
		return nil, fmt.Errorf("agent provider: runtime is required")
	}
	runtimeSupport, err := runtimeProvider.Support(ctx)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, fmt.Errorf("query %s support: %w", hostedRuntimeLabel(runtimeConfig), err)
	}
	requiresHostServiceAccess, requiresHostnameEgress, err := agentRuntimeRequirementsForProvider(name, entry, deps)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	runtimePlan := buildHostedRuntimePlan(runtimeSupport, deps, requiresHostServiceAccess, requiresHostnameEgress)
	if err := runtimePlan.Validate(hostedRuntimeLabel(runtimeConfig)); err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}

	cfg, err := componentprovider.DecodeYAMLConfig(node, "agent provider")
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	cleanup := func() {}
	if !hostedRuntimeUsesImageEntrypoint(runtimeConfig) {
		prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
			Kind:                 providermanifestv1.KindAgent,
			Subject:              "agent provider",
			SourceMissingMessage: "no Go, Rust, Python, or TypeScript agent provider source package found",
			Config:               cfg,
		})
		if err != nil {
			if runtimeOwned {
				_ = runtimeProvider.Close()
			}
			return nil, err
		}
		cfg = prepared.YAMLConfig
		cleanup = prepared.Cleanup
	}
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	launch, err := prepareHostedProcessLaunch(providermanifestv1.KindAgent, name, entry, cfg.Command, cfg.Args, cleanup, runtimeConfig)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	cleanup = launch.cleanup

	preparedLaunch := &hostedAgentProviderLaunch{
		name:            name,
		runtimeConfig:   runtimeConfig,
		runtimeProvider: runtimeProvider,
		runtimeOwned:    runtimeOwned,
		runtimePlan:     runtimePlan,
		cfg:             cfg,
		allowedHosts:    entry.EffectiveAllowedHosts(),
		launch:          launch,
		cleanup:         cleanup,
	}
	cleanup = nil
	return preparedLaunch, nil
}

func startHostedAgentProviderInstance(ctx context.Context, launch *hostedAgentProviderLaunch, hostServices []runtimehost.HostService, deps Deps, closeRuntime bool, cleanup func()) (*hostedAgentProviderInstance, error) {
	if launch == nil {
		return nil, fmt.Errorf("hosted agent launch is required")
	}
	runtimeProvider := launch.runtimeProvider
	if runtimeProvider == nil {
		return nil, fmt.Errorf("agent provider: runtime is required")
	}
	cfg := launch.cfg
	runtimePlan := launch.runtimePlan
	name := launch.name

	phaseStarted := time.Now()
	session, err := runtimeProvider.StartSession(ctx, buildHostedRuntimeStartSessionRequest(providermanifestv1.KindAgent, name, launch.runtimeConfig))
	recordHostedAgentRuntimeStartPhase(ctx, name, "runtime_session_start", phaseStarted, err)
	if err != nil {
		if closeRuntime {
			_ = runtimeProvider.Close()
		}
		if cleanup != nil {
			cleanup()
		}
		return nil, fmt.Errorf("start agent runtime session: %w", err)
	}
	sessionID := session.ID
	stopSession := true
	closeOnFailure := closeRuntime
	defer func() {
		if !stopSession {
			return
		}
		_ = stopPluginRuntimeSession(runtimeProvider, sessionID)
		if closeOnFailure {
			_ = runtimeProvider.Close()
		}
		if cleanup != nil {
			cleanup()
		}
	}()
	phaseStarted = time.Now()
	readySession, err := waitForPluginRuntimeSessionReady(ctx, runtimeProvider, sessionID)
	if err != nil {
		recordHostedAgentRuntimeStartPhase(ctx, name, "runtime_session_ready", phaseStarted, err)
		return nil, fmt.Errorf("wait for hosted agent runtime session %q ready: %w", sessionID, err)
	}
	recordHostedAgentRuntimeStartPhase(ctx, name, "runtime_session_ready", phaseStarted, nil)

	startEnv := withRuntimeSessionEnv(maps.Clone(cfg.Env), sessionID)
	startEnv = withHostServiceTLSCAEnv(startEnv, deps)
	agentAllowedHosts := cfg.EgressPolicy("").AllowedHosts
	if len(agentAllowedHosts) == 0 {
		agentAllowedHosts = slices.Clone(launch.allowedHosts)
	}
	allowedHosts := hostedAgentAllowedHosts(agentAllowedHosts, runtimePlan)
	phaseStarted = time.Now()
	bindingTargets := hostServiceBindingDescriptorsFromConfigured(hostServices)
	for _, hostService := range bindingTargets {
		bindingEnv, relayHost, err := buildHostedRuntimeHostServiceEnv(name, sessionID, hostService, deps)
		if err != nil {
			recordHostedAgentRuntimeStartPhase(ctx, name, "host_services_relay", phaseStarted, err)
			return nil, err
		}
		if len(bindingEnv) > 0 {
			if startEnv == nil {
				startEnv = make(map[string]string, len(bindingEnv))
			}
			maps.Copy(startEnv, bindingEnv)
		}
		if runtimePlan.RequiresHostnameEgress {
			allowedHosts = appendAllowedHost(allowedHosts, relayHost)
		}
	}
	recordHostedAgentRuntimeStartPhase(ctx, name, "host_services_relay", phaseStarted, nil)
	phaseStarted = time.Now()
	egressPlan, err := buildHostedRuntimeEgressLaunchPlan(name, sessionID, deps.Egress.Policy(agentAllowedHosts), allowedHosts, runtimePlan, deps)
	if runtimePlan.HostnameEgressDelivery == RuntimeHostnameEgressDeliveryPublicProxy {
		recordHostedAgentRuntimeStartPhase(ctx, name, "public_egress_proxy", phaseStarted, err)
	}
	if err != nil {
		return nil, err
	}
	if len(egressPlan.Env) > 0 {
		if startEnv == nil {
			startEnv = make(map[string]string, len(egressPlan.Env))
		}
		maps.Copy(startEnv, egressPlan.Env)
	}

	phaseStarted = time.Now()
	hostedPlugin, err := runtimeProvider.StartPlugin(ctx, pluginruntime.StartPluginRequest{
		SessionID:  sessionID,
		PluginName: name,
		Command:    launch.launch.command,
		Args:       launch.launch.args,
		Env:        startEnv,
		Egress: pluginruntime.RuntimeEgressPolicy{
			AllowedHosts:  egressPlan.RuntimeAllowedHosts,
			DefaultAction: pluginruntime.PolicyAction(deps.Egress.DefaultAction),
		},
		HostBinary: cfg.HostBinary,
	})
	recordHostedAgentRuntimeStartPhase(ctx, name, "plugin_start", phaseStarted, err)
	if err != nil {
		return nil, fmt.Errorf("start hosted agent provider: %w", err)
	}
	phaseStarted = time.Now()
	conn, err := pluginruntime.DialHostedAgent(ctx, hostedPlugin.DialTarget,
		pluginruntime.WithProviderName(name),
		pluginruntime.WithTelemetry(deps.Telemetry),
	)
	recordHostedAgentRuntimeStartPhase(ctx, name, "provider_dial", phaseStarted, err)
	if err != nil {
		return nil, fmt.Errorf("dial hosted agent provider: %w", err)
	}
	phaseStarted = time.Now()
	provider, err := agentservice.NewRemote(ctx, agentservice.RemoteConfig{
		Client:  conn.Agent(),
		Runtime: conn.Lifecycle(),
		Closer: &runtimeBackedHostedCloser{
			conn:         conn,
			runtime:      runtimeProvider,
			sessionID:    sessionID,
			closeRuntime: closeRuntime,
			cleanup:      cleanup,
		},
		Config: cfg.Config,
		Name:   name,
	})
	recordHostedAgentRuntimeStartPhase(ctx, name, "provider_configure", phaseStarted, err)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	stopSession = false
	closeOnFailure = false
	cleanup = nil
	return &hostedAgentProviderInstance{
		provider:         provider,
		runtimeProvider:  runtimeProvider,
		runtimeSessionID: sessionID,
		runtimeSession:   readySession,
	}, nil
}

func effectiveConfiguredHostedRuntime(ctx context.Context, configPath string, entry *config.ProviderEntry, deps Deps) (config.EffectiveHostedRuntime, pluginruntime.Provider, bool, error) {
	if entry == nil || !entry.UsesHostedExecution() {
		return config.EffectiveHostedRuntime{}, nil, false, nil
	}
	explicitRuntimeConfig := providerEntryHostedRuntimeConfig(entry)
	if deps.PluginRuntime != nil {
		return explicitRuntimeConfig, deps.PluginRuntime, false, nil
	}
	if deps.PluginRuntimeRegistry != nil {
		runtimeConfig, runtimeProvider, err := deps.PluginRuntimeRegistry.Resolve(ctx, configPath, entry)
		if err != nil {
			return config.EffectiveHostedRuntime{}, nil, false, err
		}
		if runtimeProvider != nil {
			return runtimeConfig, runtimeProvider, false, nil
		}
		if runtimeConfig.Enabled {
			return localHostedRuntimeConfig(runtimeConfig), newLocalPluginRuntime(runtimeConfig.ProviderName, deps), true, nil
		}
	}
	return localHostedRuntimeConfig(explicitRuntimeConfig), newLocalPluginRuntime(explicitRuntimeConfig.ProviderName, deps), true, nil
}

func effectivePluginRuntime(ctx context.Context, name string, entry *config.ProviderEntry, deps Deps) (config.EffectiveHostedRuntime, pluginruntime.Provider, bool, error) {
	if deps.PluginRuntime != nil {
		return providerEntryHostedRuntimeConfig(entry), deps.PluginRuntime, false, nil
	}
	if deps.PluginRuntimeRegistry != nil {
		runtimeConfig, runtimeProvider, err := deps.PluginRuntimeRegistry.Resolve(ctx, "plugins."+name, entry)
		if err != nil {
			return config.EffectiveHostedRuntime{}, nil, false, err
		}
		if runtimeProvider != nil {
			return runtimeConfig, runtimeProvider, false, nil
		}
		if runtimeConfig.Enabled {
			return localHostedRuntimeConfig(runtimeConfig), newLocalPluginRuntime(runtimeConfig.ProviderName, deps), true, nil
		}
	}
	return localHostedRuntimeConfig(config.EffectiveHostedRuntime{}), newLocalPluginRuntime("", deps), true, nil
}

func providerEntryHostedRuntimeConfig(entry *config.ProviderEntry) config.EffectiveHostedRuntime {
	if entry == nil {
		return config.EffectiveHostedRuntime{}
	}
	runtimeCfg := entry.HostedRuntimeConfig()
	if runtimeCfg == nil {
		return config.EffectiveHostedRuntime{Enabled: entry.UsesHostedExecution()}
	}
	effective := config.EffectiveHostedRuntime{
		Enabled:       entry.UsesHostedExecution(),
		ProviderName:  strings.TrimSpace(runtimeCfg.Provider),
		Template:      strings.TrimSpace(runtimeCfg.Template),
		Image:         strings.TrimSpace(runtimeCfg.Image),
		ImagePullAuth: hostedRuntimeConfigImagePullAuth(runtimeCfg.ImagePullAuth),
		Metadata:      maps.Clone(runtimeCfg.Metadata),
	}
	return effective
}

func hostedRuntimeConfigImagePullAuth(auth *config.HostedRuntimeImagePullAuth) *config.HostedRuntimeImagePullAuth {
	if auth == nil {
		return nil
	}
	return &config.HostedRuntimeImagePullAuth{
		DockerConfigJSON: auth.DockerConfigJSON,
	}
}

func localHostedRuntimeConfig(runtimeConfig config.EffectiveHostedRuntime) config.EffectiveHostedRuntime {
	if runtimeConfig.Provider == nil {
		runtimeConfig.Provider = &config.RuntimeProviderEntry{Driver: config.RuntimeProviderDriverLocal}
	}
	return runtimeConfig
}

func newLocalPluginRuntime(runtimeProviderName string, deps Deps) pluginruntime.Provider {
	runtimeProviderName = strings.TrimSpace(runtimeProviderName)
	if runtimeProviderName == "" {
		runtimeProviderName = "local"
	}
	opts := []pluginruntime.LocalOption{pluginruntime.WithLocalTelemetry(deps.Telemetry)}
	if deps.Services != nil && deps.Services.RuntimeSessionLogs != nil {
		opts = append(opts, pluginruntime.WithLocalRuntimeSessionLogs(runtimeProviderName, deps.Services.RuntimeSessionLogs))
	}
	return pluginruntime.NewLocalProvider(opts...)
}

const (
	pluginRuntimeHostServiceRelayTokenTTL = 30 * 24 * time.Hour
	pluginRuntimeEgressProxyTokenTTL      = 30 * 24 * time.Hour
	hostServiceTLSCAFileEnv               = "GESTALT_HOST_SERVICE_TLS_CA_FILE"
	hostServiceTLSCAPEMEnv                = "GESTALT_HOST_SERVICE_TLS_CA_PEM"
)

type RuntimeEgressLaunchPlan struct {
	Policy              egress.Policy
	Delivery            RuntimeHostnameEgressDelivery
	Env                 map[string]string
	RuntimeAllowedHosts []string
}

func hostedRuntimeLabel(runtimeConfig config.EffectiveHostedRuntime) string {
	if name := strings.TrimSpace(runtimeConfig.ProviderName); name != "" {
		return fmt.Sprintf("runtime provider %q", name)
	}
	return "hosted runtime"
}

func buildHostedRuntimeStartSessionRequest(kind, name string, runtimeConfig config.EffectiveHostedRuntime) pluginruntime.StartSessionRequest {
	metadata := maps.Clone(runtimeConfig.Metadata)
	if metadata == nil {
		metadata = map[string]string{}
	}
	if kind != "" {
		metadata["provider_kind"] = kind
	}
	if name != "" {
		metadata["provider_name"] = name
	}
	return pluginruntime.StartSessionRequest{
		PluginName:    name,
		Template:      runtimeConfig.Template,
		Image:         runtimeConfig.Image,
		ImagePullAuth: hostedRuntimeImagePullAuth(runtimeConfig.ImagePullAuth),
		Metadata:      metadata,
	}
}

func hostedRuntimeImagePullAuth(auth *config.HostedRuntimeImagePullAuth) *pluginruntime.ImagePullAuth {
	if auth == nil {
		return nil
	}
	return &pluginruntime.ImagePullAuth{
		DockerConfigJSON: auth.DockerConfigJSON,
	}
}

func buildHostedRuntimeEgressLaunchPlan(providerName, sessionID string, policy egress.Policy, runtimeAllowedHosts []string, runtimePlan HostedRuntimePlan, deps Deps) (RuntimeEgressLaunchPlan, error) {
	plan := RuntimeEgressLaunchPlan{
		Policy: egress.Policy{
			AllowedHosts:  slices.Clone(policy.AllowedHosts),
			DefaultAction: policy.DefaultAction,
		},
		Delivery:            runtimePlan.HostnameEgressDelivery,
		RuntimeAllowedHosts: slices.Clone(runtimeAllowedHosts),
	}
	if runtimePlan.HostnameEgressDelivery != RuntimeHostnameEgressDeliveryPublicProxy {
		return plan, nil
	}
	env, err := buildHostedRuntimePublicEgressProxy(providerName, sessionID, policy.AllowedHosts, policy.DefaultAction, deps)
	if err != nil {
		return RuntimeEgressLaunchPlan{}, err
	}
	plan.Env = env
	return plan, nil
}

const pluginRuntimeStopTimeout = 3 * time.Second

type runtimeBackedHostedCloser struct {
	conn         io.Closer
	runtime      pluginruntime.Provider
	sessionID    string
	closeRuntime bool
	cleanup      func()
}

func (c *runtimeBackedHostedCloser) Close() error {
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

func waitForPluginRuntimeSessionReady(ctx context.Context, runtimeProvider pluginruntime.Provider, sessionID string) (*pluginruntime.Session, error) {
	for {
		session, err := runtimeProvider.GetSession(ctx, pluginruntime.GetSessionRequest{SessionID: sessionID})
		if err != nil {
			return nil, err
		}
		switch session.State {
		case pluginruntime.SessionStateReady, pluginruntime.SessionStateRunning:
			return session, nil
		case pluginruntime.SessionStateFailed, pluginruntime.SessionStateStopped:
			return nil, fmt.Errorf("session entered %q state", session.State)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func buildPluginRuntimeHostServices(name string, entry *config.ProviderEntry, deps Deps) ([]runtimehost.HostService, *plugininvokerservice.InvocationTokenManager, func(), error) {
	var (
		hostServices []runtimehost.HostService
		cleanup      func()
		invTokens    *plugininvokerservice.InvocationTokenManager
	)
	fail := func(err error) ([]runtimehost.HostService, *plugininvokerservice.InvocationTokenManager, func(), error) {
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
	needInvocationTokens := len(entry.Invokes) > 0
	if includeWorkflowManager || includeAgentManager {
		needInvocationTokens = true
	}
	if needInvocationTokens {
		invTokens, err = plugininvokerservice.NewInvocationTokenManager(deps.EncryptionKey)
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
	if len(entry.Invokes) > 0 {
		hostServices = append(hostServices, buildPluginInvokerHostService(name, entry, deps, invTokens))
	}
	return hostServices, invTokens, cleanup, nil
}

func appendRuntimeLogHostService(hostServices []runtimehost.HostService, runtimeConfig config.EffectiveHostedRuntime, deps Deps, runtimePlan HostedRuntimePlan) []runtimehost.HostService {
	if deps.Services == nil || deps.Services.RuntimeSessionLogs == nil || runtimePlan.Resolved.HostServiceAccess == RuntimeHostServiceAccessNone {
		return hostServices
	}
	runtimeProviderName := runtimeSessionLogProviderName(runtimeConfig)
	return append(hostServices, runtimehost.HostService{
		Name:   "runtime_log_host",
		EnvVar: runtimehost.DefaultRuntimeLogHostSocketEnv,
		Register: func(srv *grpc.Server) {
			runtimehost.RegisterRuntimeLogHostServer(srv, runtimeProviderName, deps.Services.RuntimeSessionLogs.AppendSessionLogs)
		},
	})
}

func runtimeSessionLogProviderName(runtimeConfig config.EffectiveHostedRuntime) string {
	if name := strings.TrimSpace(runtimeConfig.ProviderName); name != "" {
		return name
	}
	return "local"
}

func withRuntimeSessionEnv(env map[string]string, sessionID string) map[string]string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return env
	}
	if env == nil {
		env = map[string]string{}
	}
	env[runtimehost.DefaultRuntimeSessionIDEnv] = sessionID
	return env
}

func withHostServiceTLSCAEnv(env map[string]string, deps Deps) map[string]string {
	caPEM := strings.TrimSpace(deps.HostServiceTLSCAPEM)
	caFile := strings.TrimSpace(deps.HostServiceTLSCAFile)
	if caPEM == "" && caFile == "" {
		return env
	}
	if env == nil {
		env = map[string]string{}
	}
	if caPEM != "" {
		env[hostServiceTLSCAPEMEnv] = caPEM
	} else {
		env[hostServiceTLSCAFileEnv] = caFile
	}
	return env
}

type hostServiceBindingDescriptor struct {
	Name   string
	EnvVar string
}

func hostServiceBindingDescriptorFromConfigured(hostService runtimehost.HostService) hostServiceBindingDescriptor {
	return hostServiceBindingDescriptor{
		Name:   strings.TrimSpace(hostService.Name),
		EnvVar: strings.TrimSpace(hostService.EnvVar),
	}
}

func hostServiceBindingDescriptorsFromConfigured(hostServices []runtimehost.HostService) []hostServiceBindingDescriptor {
	if len(hostServices) == 0 {
		return nil
	}
	out := make([]hostServiceBindingDescriptor, 0, len(hostServices))
	for _, hostService := range hostServices {
		out = append(out, hostServiceBindingDescriptorFromConfigured(hostService))
	}
	return out
}

func buildHostedRuntimeHostServiceEnv(providerName, sessionID string, hostService hostServiceBindingDescriptor, deps Deps) (map[string]string, string, error) {
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
	case hostService.EnvVar == workflowservice.DefaultManagerSocketEnv:
		serviceKey = "workflow_manager"
		serviceLabel = "workflow manager"
		methodPrefix = "/" + proto.WorkflowManagerHost_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == agentservice.DefaultHostSocketEnv:
		serviceKey = "agent_host"
		serviceLabel = "agent host"
		methodPrefix = "/" + proto.AgentHost_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == agentservice.DefaultManagerSocketEnv:
		serviceKey = "agent_manager"
		serviceLabel = "agent manager"
		methodPrefix = "/" + proto.AgentManagerHost_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == authorizationservice.DefaultSocketEnv:
		serviceKey = "authorization"
		serviceLabel = "authorization"
		methodPrefix = "/" + proto.AuthorizationProvider_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == plugininvokerservice.DefaultSocketEnv:
		serviceKey = "plugin_invoker"
		serviceLabel = "plugin invoker"
		methodPrefix = "/" + proto.PluginInvoker_ServiceDesc.ServiceName + "/"
	case hostService.EnvVar == runtimehost.DefaultRuntimeLogHostSocketEnv:
		serviceKey = "runtime_log_host"
		serviceLabel = "runtime log host"
		methodPrefix = "/" + proto.PluginRuntimeLogHost_ServiceDesc.ServiceName + "/"
	default:
		return nil, "", fmt.Errorf("host service %q requires public host service relay support", hostService.EnvVar)
	}
	relayDialTarget, relayEnv, relayHost, ok, err := buildHostedRuntimePublicHostServiceRelay(
		providerName,
		sessionID,
		hostService,
		deps,
		serviceKey,
		serviceLabel,
		methodPrefix,
	)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", fmt.Errorf("provider %q requires server.baseURL and server.encryptionKey to relay %s through the public host service relay", providerName, serviceLabel)
	}
	relayEnv[hostService.EnvVar] = relayDialTarget
	return relayEnv, relayHost, nil
}

type runtimeHostServiceSessionVerifier struct {
	providerName string
	provider     pluginruntime.Provider
}

func (v runtimeHostServiceSessionVerifier) VerifyHostServiceSession(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("runtime session id is required")
	}
	if v.provider == nil {
		return fmt.Errorf("plugin runtime provider is not configured")
	}
	session, err := v.provider.GetSession(ctx, pluginruntime.GetSessionRequest{SessionID: sessionID})
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("plugin runtime session %q was not found", sessionID)
	}
	if expected := strings.TrimSpace(v.providerName); expected != "" {
		if got := strings.TrimSpace(session.Metadata["provider_name"]); got != "" && got != expected {
			return fmt.Errorf("plugin runtime session %q belongs to provider %q", sessionID, got)
		}
	}
	if session.Lifecycle != nil && session.Lifecycle.ExpiresAt != nil {
		expiresAt := session.Lifecycle.ExpiresAt.UTC()
		if !time.Now().UTC().Before(expiresAt) {
			return fmt.Errorf("plugin runtime session %q expired at %s", sessionID, expiresAt.Format(time.RFC3339Nano))
		}
	}
	switch session.State {
	case pluginruntime.SessionStatePending, pluginruntime.SessionStateReady, pluginruntime.SessionStateRunning:
		return nil
	default:
		return fmt.Errorf("plugin runtime session %q is %s", sessionID, session.State)
	}
}

func registerPublicRuntimeHostServices(providerName string, hostServices []runtimehost.HostService, deps Deps, runtimePlan HostedRuntimePlan, runtimeProvider pluginruntime.Provider) (func(), error) {
	if runtimePlan.Resolved.HostServiceAccess != RuntimeHostServiceAccessRelay || deps.PublicHostServices == nil {
		return nil, nil
	}
	registerHostServices := publicRuntimeRegistryHostServices(hostServices)
	if len(registerHostServices) == 0 {
		return nil, nil
	}
	for _, hostService := range registerHostServices {
		if strings.TrimSpace(hostService.Name) == "" {
			return nil, fmt.Errorf("host service %q requires a service name for public relay", hostService.EnvVar)
		}
	}
	registration := deps.PublicHostServices.RegisterVerified(providerName, runtimeHostServiceSessionVerifier{
		providerName: providerName,
		provider:     runtimeProvider,
	}, registerHostServices...)
	return func() {
		registration.Unregister()
	}, nil
}

func buildHostedRuntimePublicEgressProxy(providerName, sessionID string, allowedHosts []string, defaultAction egress.PolicyAction, deps Deps) (map[string]string, error) {
	baseURL, explicitRelayBaseURL := hostedRuntimeRelayBaseURL(deps)
	if baseURL == "" || len(deps.EncryptionKey) == 0 {
		return nil, fmt.Errorf("provider %q requires server.baseURL and server.encryptionKey to enforce hostname-based egress for hosted runtimes", providerName)
	}
	proxyBaseURL, _, err := pluginRuntimePublicProxyBaseURL(baseURL, explicitRelayBaseURL)
	if err != nil {
		return nil, err
	}
	tokenManager, err := egressproxy.NewTokenManager(deps.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("init egress proxy tokens: %w", err)
	}
	token, err := tokenManager.MintToken(egressproxy.TokenRequest{
		PluginName:    providerName,
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

func publicRuntimeRegistryHostServices(hostServices []runtimehost.HostService) []runtimehost.HostService {
	if len(hostServices) == 0 {
		return nil
	}
	return append([]runtimehost.HostService(nil), hostServices...)
}

func buildHostedRuntimePublicHostServiceRelay(providerName, sessionID string, hostService hostServiceBindingDescriptor, deps Deps, serviceKey, serviceLabel, methodPrefix string) (string, map[string]string, string, bool, error) {
	baseURL, explicitRelayBaseURL := hostedRuntimeRelayBaseURL(deps)
	if baseURL == "" || len(deps.EncryptionKey) == 0 {
		return "", nil, "", false, nil
	}
	dialTarget, relayHost, err := pluginRuntimePublicRelayTarget(baseURL, explicitRelayBaseURL)
	if err != nil {
		return "", nil, "", false, err
	}
	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(deps.EncryptionKey)
	if err != nil {
		return "", nil, "", false, fmt.Errorf("init host service relay tokens: %w", err)
	}
	token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		PluginName:   providerName,
		SessionID:    sessionID,
		Service:      serviceKey,
		EnvVar:       hostService.EnvVar,
		MethodPrefix: methodPrefix,
		TTL:          pluginRuntimeHostServiceRelayTokenTTL,
	})
	if err != nil {
		return "", nil, "", false, fmt.Errorf("mint %s host service relay token: %w", serviceLabel, err)
	}
	return dialTarget, map[string]string{
		hostService.EnvVar + "_TOKEN": token,
	}, relayHost, true, nil
}

func pluginRuntimePublicRelayTarget(baseURL string, allowInsecureHTTP bool) (string, string, error) {
	parsed, host, err := pluginRuntimePublicProxyBaseURL(baseURL, allowInsecureHTTP)
	if err != nil {
		return "", "", err
	}
	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(parsed.Scheme, "http") {
			port = "80"
		} else {
			port = "443"
		}
	}
	target := net.JoinHostPort(host, port)
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return "tls://" + target, host, nil
	case "http":
		return "tcp://" + target, host, nil
	default:
		return "", "", fmt.Errorf("server.baseURL %q has unsupported public runtime relay scheme %q", baseURL, parsed.Scheme)
	}
}

func pluginRuntimePublicProxyBaseURL(baseURL string, allowInsecureHTTP bool) (*url.URL, string, error) {
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
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		if !allowInsecureHTTP && !isLoopbackAllowedHost(host) {
			return nil, "", fmt.Errorf("server.baseURL %q must use https for public runtime relay unless it targets loopback", baseURL)
		}
	default:
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
	return envVar == indexeddbservice.DefaultSocketEnv || strings.HasPrefix(envVar, indexeddbservice.DefaultSocketEnv+"_")
}

func isCacheHostServiceEnv(envVar string) bool {
	envVar = strings.TrimSpace(envVar)
	return envVar == cacheservice.DefaultSocketEnv || strings.HasPrefix(envVar, cacheservice.DefaultSocketEnv+"_")
}

func isS3HostServiceEnv(envVar string) bool {
	envVar = strings.TrimSpace(envVar)
	return envVar == s3.DefaultSocketEnv || strings.HasPrefix(envVar, s3.DefaultSocketEnv+"_")
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

func hostedAgentAllowedHosts(allowedHosts []string, runtimePlan HostedRuntimePlan) []string {
	cloned := slices.Clone(allowedHosts)
	if runtimePlan.Resolved.HostServiceAccess != RuntimeHostServiceAccessRelay || runtimePlan.RequiresHostnameEgress {
		return cloned
	}
	// Hosted agent bundles include loopback host allowances for local SDK
	// transports. Once the agent host is exposed over the public relay, those
	// loopback hosts are no longer relevant and can spuriously force hosted
	// runtimes into proxy-enforced egress mode.
	out := cloned[:0]
	for _, host := range cloned {
		if isLoopbackAllowedHost(host) {
			continue
		}
		out = append(out, host)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isLoopbackAllowedHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func buildPluginIndexedDBHostServices(pluginName string, effective config.EffectiveHostIndexedDBBinding, deps Deps) ([]runtimehost.HostService, func(), error) {
	if deps.IndexedDBFactory == nil || len(deps.IndexedDBDefs) == 0 {
		return nil, nil, fmt.Errorf("indexeddb host services are not available")
	}

	ds, err := buildPluginScopedIndexedDB(pluginName, effective, deps)
	if err != nil {
		return nil, nil, err
	}

	hostServices := []runtimehost.HostService{{
		Name:   "indexeddb",
		EnvVar: indexeddbservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, indexeddbservice.NewServer(ds, pluginName, indexeddbservice.ServerOptions{
				AllowedStores: effective.ObjectStores,
			}))
		},
	}}
	return hostServices, func() {
		_ = closeIndexedDBs(ds)
	}, nil
}

func buildPluginCacheHostServices(pluginName string, entry *config.ProviderEntry, deps Deps) ([]runtimehost.HostService, func(), error) {
	if deps.CacheFactory == nil || len(deps.CacheDefs) == 0 {
		return nil, nil, fmt.Errorf("cache host services are not available")
	}

	hostServices := make([]runtimehost.HostService, 0, len(entry.Cache)+1)
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
		hostServices = append(hostServices, runtimehost.HostService{
			Name:   "cache",
			EnvVar: cacheservice.SocketEnv(bindingName),
			Register: func(cacheValue corecache.Cache) func(*grpc.Server) {
				return func(srv *grpc.Server) {
					proto.RegisterCacheServer(srv, cacheservice.NewServer(cacheValue, pluginName))
				}
			}(value),
		})
	}
	if len(boundCaches) == 1 {
		value := boundCaches[0]
		hostServices = append(hostServices, runtimehost.HostService{
			Name:   "cache",
			EnvVar: cacheservice.DefaultSocketEnv,
			Register: func(srv *grpc.Server) {
				proto.RegisterCacheServer(srv, cacheservice.NewServer(value, pluginName))
			},
		})
	}
	return hostServices, func() {
		_ = closeCaches(boundCaches...)
	}, nil
}

func buildPluginS3HostServices(pluginName string, entry *config.ProviderEntry, deps Deps) ([]runtimehost.HostService, error) {
	if len(deps.S3) == 0 {
		return nil, fmt.Errorf("s3 host services are not available")
	}

	var accessURLs *s3.ObjectAccessURLManager
	if len(deps.EncryptionKey) != 0 {
		var err error
		accessURLs, err = s3.NewObjectAccessURLManager(deps.EncryptionKey, deps.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("s3 object access URLs: %w", err)
		}
	}

	hostServices := make([]runtimehost.HostService, 0, len(entry.S3)+1)
	for _, binding := range entry.S3 {
		client, ok := deps.S3[binding]
		if !ok || client == nil {
			return nil, fmt.Errorf("s3 %q is not available", binding)
		}
		hostServices = append(hostServices, runtimehost.HostService{
			Name:   "s3",
			EnvVar: s3.SocketEnv(binding),
			Register: func(client s3store.Client, binding string) func(*grpc.Server) {
				return func(srv *grpc.Server) {
					proto.RegisterS3Server(srv, s3.NewServerWithOptions(client, pluginName, s3.ServerOptions{
						BindingName: binding,
						AccessURLs:  accessURLs,
					}))
					proto.RegisterS3ObjectAccessServer(srv, s3.NewObjectAccessServer(accessURLs, pluginName, binding))
				}
			}(client, binding),
		})
	}
	if len(entry.S3) == 1 {
		binding := entry.S3[0]
		client := deps.S3[binding]
		hostServices = append(hostServices, runtimehost.HostService{
			Name:   "s3",
			EnvVar: s3.DefaultSocketEnv,
			Register: func(srv *grpc.Server) {
				proto.RegisterS3Server(srv, s3.NewServerWithOptions(client, pluginName, s3.ServerOptions{
					BindingName: binding,
					AccessURLs:  accessURLs,
				}))
				proto.RegisterS3ObjectAccessServer(srv, s3.NewObjectAccessServer(accessURLs, pluginName, binding))
			},
		})
	}
	return hostServices, nil
}

func buildWorkflowIndexedDBHostServices(name string, effective config.EffectiveHostIndexedDBBinding, deps Deps) ([]runtimehost.HostService, func(), error) {
	if deps.IndexedDBFactory == nil || len(deps.IndexedDBDefs) == 0 {
		return nil, nil, fmt.Errorf("indexeddb host services are not available")
	}

	ds, err := buildWorkflowScopedIndexedDB(name, effective, deps)
	if err != nil {
		return nil, nil, err
	}

	hostServices := []runtimehost.HostService{{
		Name:   "indexeddb",
		EnvVar: indexeddbservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, indexeddbservice.NewServer(ds, name, indexeddbservice.ServerOptions{
				AllowedStores: effective.ObjectStores,
			}))
		},
	}}
	return hostServices, func() {
		_ = closeIndexedDBs(ds)
	}, nil
}

func buildAgentIndexedDBHostServices(name string, effective config.EffectiveHostIndexedDBBinding, deps Deps) ([]runtimehost.HostService, func(), error) {
	if deps.IndexedDBFactory == nil || len(deps.IndexedDBDefs) == 0 {
		return nil, nil, fmt.Errorf("indexeddb host services are not available")
	}

	ds, err := buildAgentScopedIndexedDB(name, effective, deps)
	if err != nil {
		return nil, nil, err
	}

	hostServices := []runtimehost.HostService{{
		Name:   "indexeddb",
		EnvVar: indexeddbservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, indexeddbservice.NewServer(ds, name, indexeddbservice.ServerOptions{
				AllowedStores: effective.ObjectStores,
			}))
		},
	}}
	return hostServices, func() {
		_ = closeIndexedDBs(ds)
	}, nil
}

func buildPluginWorkflowManagerHostService(pluginName string, deps Deps, tokens *plugininvokerservice.InvocationTokenManager) runtimehost.HostService {
	manager := deps.WorkflowManager
	if manager == nil {
		manager = unavailableWorkflowManager{}
	}
	return runtimehost.HostService{
		Name:   "workflow_manager",
		EnvVar: workflowservice.DefaultManagerSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterWorkflowManagerHostServer(srv, workflowservice.NewManagerServer(pluginName, manager, tokens))
		},
	}
}

func buildPluginAgentManagerHostService(pluginName string, deps Deps, tokens *plugininvokerservice.InvocationTokenManager) runtimehost.HostService {
	manager := deps.AgentManager
	if manager == nil {
		manager = unavailableAgentManager{}
	}
	return runtimehost.HostService{
		Name:   "agent_manager",
		EnvVar: agentservice.DefaultManagerSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterAgentManagerHostServer(srv, agentservice.NewManagerServer(pluginName, manager, tokens))
		},
	}
}

func buildPluginAuthorizationHostService(provider core.AuthorizationProvider) runtimehost.HostService {
	return runtimehost.HostService{
		Name:   "authorization",
		EnvVar: authorizationservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterAuthorizationProviderServer(srv, authorizationservice.NewProviderServer(provider))
		},
	}
}

func buildPluginInvokerHostService(pluginName string, entry *config.ProviderEntry, deps Deps, tokens *plugininvokerservice.InvocationTokenManager) runtimehost.HostService {
	invoker := deps.PluginInvoker
	if invoker == nil {
		invoker = unavailablePluginInvoker{}
	}
	return runtimehost.HostService{
		Name:   "plugin_invoker",
		EnvVar: plugininvokerservice.DefaultSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterPluginInvokerServer(srv, plugininvokerservice.NewServer(pluginName, pluginInvocationDependencies(entry.Invokes), invoker, tokens))
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

func (unavailableWorkflowManager) StartRun(context.Context, *principal.Principal, workflowmanager.RunStart) (*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetRun(context.Context, *principal.Principal, string) (*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) CancelRun(context.Context, *principal.Principal, string, string) (*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) SignalRun(context.Context, *principal.Principal, workflowmanager.RunSignal) (*workflowmanager.ManagedRunSignal, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) SignalOrStartRun(context.Context, *principal.Principal, workflowmanager.RunSignalOrStart) (*workflowmanager.ManagedRunSignal, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) PublishEvent(context.Context, *principal.Principal, string, coreworkflow.Event) (coreworkflow.Event, error) {
	return coreworkflow.Event{}, fmt.Errorf("workflow manager is not available")
}

type unavailableAgentManager struct{}

func (unavailableAgentManager) Available() bool {
	return false
}

func (unavailableAgentManager) ResolveTool(context.Context, *principal.Principal, coreagent.ToolRef) (coreagent.Tool, error) {
	return coreagent.Tool{}, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ResolveTools(context.Context, *principal.Principal, coreagent.ResolveToolsRequest) ([]coreagent.Tool, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListTools(context.Context, *principal.Principal, coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) CreateSession(context.Context, *principal.Principal, coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) GetSession(context.Context, *principal.Principal, string) (*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListSessions(context.Context, *principal.Principal, coreagent.ManagerListSessionsRequest) ([]*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) UpdateSession(context.Context, *principal.Principal, coreagent.ManagerUpdateSessionRequest) (*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) CreateTurn(context.Context, *principal.Principal, coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) GetTurn(context.Context, *principal.Principal, string) (*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListTurns(context.Context, *principal.Principal, coreagent.ManagerListTurnsRequest) ([]*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) CancelTurn(context.Context, *principal.Principal, string, string) (*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListTurnEvents(context.Context, *principal.Principal, string, int64, int) ([]*coreagent.TurnEvent, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListInteractions(context.Context, *principal.Principal, string) ([]*coreagent.Interaction, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ResolveInteraction(context.Context, *principal.Principal, string, string, map[string]any) (*coreagent.Interaction, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func buildPluginScopedIndexedDB(_ string, effective config.EffectiveHostIndexedDBBinding, deps Deps) (indexeddb.IndexedDB, error) {
	return buildScopedIndexedDB(scopedIndexedDBBuildOptions{
		MetricsName:   effective.ProviderName,
		ProviderName:  effective.ProviderName,
		DB:            effective.DB,
		AllowedStores: effective.ObjectStores,
	}, deps)
}

func buildWorkflowScopedIndexedDB(name string, effective config.EffectiveHostIndexedDBBinding, deps Deps) (indexeddb.IndexedDB, error) {
	return buildScopedIndexedDB(scopedIndexedDBBuildOptions{
		MetricsName:   name,
		ProviderName:  effective.ProviderName,
		DB:            effective.DB,
		AllowedStores: effective.ObjectStores,
	}, deps)
}

func buildAgentScopedIndexedDB(name string, effective config.EffectiveHostIndexedDBBinding, deps Deps) (indexeddb.IndexedDB, error) {
	return buildScopedIndexedDB(scopedIndexedDBBuildOptions{
		MetricsName:   name,
		ProviderName:  effective.ProviderName,
		DB:            effective.DB,
		AllowedStores: effective.ObjectStores,
	}, deps)
}

type scopedIndexedDBBuildOptions struct {
	MetricsName   string
	ProviderName  string
	DB            string
	AllowedStores []string
}

func buildScopedIndexedDB(opts scopedIndexedDBBuildOptions, deps Deps) (indexeddb.IndexedDB, error) {
	def, ok := deps.IndexedDBDefs[opts.ProviderName]
	if !ok || def == nil {
		return nil, fmt.Errorf("indexeddb %q is not available", opts.ProviderName)
	}
	scopedDef, err := newScopedIndexedDBDef(def, scopedIndexedDBDefOptions{
		DB: opts.DB,
	})
	if err != nil {
		return nil, fmt.Errorf("indexeddb %q: %w", opts.ProviderName, err)
	}
	ds, err := buildIndexedDB(scopedDef, &FactoryRegistry{IndexedDB: deps.IndexedDBFactory})
	if err != nil {
		return nil, fmt.Errorf("indexeddb %q: %w", opts.ProviderName, err)
	}
	ds = newIndexedDBStoreAllowlist(ds, indexedDBStoreAllowlistOptions{
		AllowedStores: opts.AllowedStores,
	})
	return metricutil.InstrumentIndexedDB(ds, opts.MetricsName), nil
}

type scopedIndexedDBDefOptions struct {
	DB string
}

func newScopedIndexedDBDef(entry *config.ProviderEntry, opts scopedIndexedDBDefOptions) (*config.ProviderEntry, error) {
	if entry == nil {
		return nil, fmt.Errorf("datastore provider is required")
	}
	cfg, err := config.NodeToMap(entry.Config)
	if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if cfg == nil {
		cfg = make(map[string]any)
	}

	switch {
	case isRelationalIndexedDBEntry(entry):
		if isSQLiteIndexedDBConfig(cfg) {
			delete(cfg, "schema")
			cfg["table_prefix"] = opts.DB + "_"
			cfg["prefix"] = opts.DB + "_"
		} else {
			delete(cfg, "table_prefix")
			delete(cfg, "prefix")
			cfg["schema"] = opts.DB
		}
	case isMongoDBIndexedDBEntry(entry):
		cfg["database"] = opts.DB
	case isDynamoDBIndexedDBEntry(entry):
		cfg["table"] = opts.DB
	default:
		return nil, fmt.Errorf("scoped indexeddb bindings require a provider with config-level namespace support")
	}

	configNode, err := mapToYAMLNode(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}

	cloned := *entry
	cloned.Config = configNode
	return &cloned, nil
}

func isRelationalIndexedDBEntry(entry *config.ProviderEntry) bool {
	return isIndexedDBProviderEntry(entry, "relationaldb")
}

func isMongoDBIndexedDBEntry(entry *config.ProviderEntry) bool {
	return isIndexedDBProviderEntry(entry, "mongodb")
}

func isDynamoDBIndexedDBEntry(entry *config.ProviderEntry) bool {
	return isIndexedDBProviderEntry(entry, "dynamodb")
}

func isIndexedDBProviderEntry(entry *config.ProviderEntry, providerName string) bool {
	if entry == nil {
		return false
	}
	providerPath := "/indexeddb/" + providerName
	if entry.ResolvedManifest != nil {
		source := strings.TrimSpace(filepath.ToSlash(entry.ResolvedManifest.Source))
		return strings.HasSuffix(source, providerPath)
	}
	if metadataURL := strings.TrimSpace(entry.SourceMetadataURL()); metadataURL != "" {
		parsed, err := url.Parse(metadataURL)
		if err == nil {
			path := filepath.ToSlash(parsed.Path)
			return strings.Contains(path, providerPath+"/") && strings.HasSuffix(path, "/provider-release.yaml")
		}
	}
	if path := strings.TrimSpace(entry.SourcePath()); path != "" {
		path = filepath.ToSlash(path)
		return strings.HasSuffix(path, providerPath) ||
			strings.HasSuffix(path, "/"+providerName) ||
			strings.HasSuffix(path, providerPath+"/manifest.yaml") ||
			strings.HasSuffix(path, "/"+providerName+"/manifest.yaml")
	}
	return false
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

func buildPluginStaticSpec(name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, meta providerMetadata) (pluginservice.StaticProviderSpec, config.StaticConnectionPlan, error) {
	if manifest == nil || manifest.Spec == nil {
		return pluginservice.StaticProviderSpec{}, config.StaticConnectionPlan{}, fmt.Errorf("resolved manifest is required")
	}
	plan, err := config.BuildStaticConnectionPlan(entry, manifest.Spec)
	if err != nil {
		return pluginservice.StaticProviderSpec{}, config.StaticConnectionPlan{}, err
	}

	displayName := meta.displayNameOr(manifest.DisplayName)
	if displayName == "" {
		displayName = name
	}
	description := meta.descriptionOr(manifest.Description)
	iconSVG := meta.iconSVG
	if iconPath := entry.ResolvedIconFile; iconPath != "" {
		svg, err := declarative.ReadIconFile(iconPath)
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
			return pluginservice.StaticProviderSpec{}, config.StaticConnectionPlan{}, err
		}
	}
	if staticCatalog == nil && providerpkg.StaticCatalogRequired(manifest) {
		if entry.ResolvedManifestPath == "" {
			return pluginservice.StaticProviderSpec{}, config.StaticConnectionPlan{}, fmt.Errorf("resolved manifest path is required for executable provider static catalog")
		}
		return pluginservice.StaticProviderSpec{}, config.StaticConnectionPlan{}, fmt.Errorf("executable providers without declarative or spec surfaces must define %s", providerpkg.StaticCatalogFile)
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

	return pluginservice.StaticProviderSpec{
		Name:               name,
		DisplayName:        displayName,
		Description:        description,
		IconSVG:            iconSVG,
		ConnectionMode:     connMode,
		Catalog:            staticCatalog,
		AuthTypes:          staticAuthTypes(conn.Auth.Type),
		ConnectionParams:   pluginservice.ConnectionParamDefsFromManifest(conn.ConnectionParams),
		CredentialFields:   pluginservice.CredentialFieldsFromManifest(conn.Auth.Credentials),
		DiscoveryConfig:    pluginservice.DiscoveryConfigFromManifest(conn.Discovery),
		PostConnectConfigs: pluginservice.PostConnectConfigsFromManifestConnections(manifest.Spec.Connections),
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

func mcpOAuthBuildOpts(conn config.ConnectionDef, mcpURL string, deps Deps) []declarative.BuildOption {
	if conn.Auth.Type != providermanifestv1.AuthTypeMCPOAuth || mcpURL == "" {
		return nil
	}
	return []declarative.BuildOption{
		declarative.WithAuthHandler(buildMCPOAuthHandler(conn, mcpURL, buildRegistrationStore(deps), deps)),
	}
}

func manifestHeaders(manifestPlugin *providermanifestv1.Spec) map[string]string {
	if manifestPlugin == nil || len(manifestPlugin.Headers) == 0 {
		return nil
	}
	return maps.Clone(manifestPlugin.Headers)
}

func applyProviderHeaders(def *declarative.Definition, manifestPlugin *providermanifestv1.Spec) {
	if def == nil {
		return
	}
	headers := manifestHeaders(manifestPlugin)
	if len(headers) == 0 {
		return
	}
	def.Headers = headers
}

func applyManagedParameters(def *declarative.Definition, manifestPlugin *providermanifestv1.Spec) error {
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

func isManagedOperationParameter(param declarative.ParameterDef, managed []providermanifestv1.ManagedParameter) bool {
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

func applyProviderResponseMapping(def *declarative.Definition, manifestPlugin *providermanifestv1.Spec) {
	if def == nil || manifestPlugin == nil || manifestPlugin.ResponseMapping == nil {
		return
	}
	rm := &declarative.ResponseMappingDef{
		DataPath: manifestPlugin.ResponseMapping.DataPath,
	}
	if manifestPlugin.ResponseMapping.Pagination != nil {
		rm.Pagination = &declarative.PaginationMappingDef{
			HasMore: cloneManifestValueSelectorDef(manifestPlugin.ResponseMapping.Pagination.HasMore),
			Cursor:  cloneManifestValueSelectorDef(manifestPlugin.ResponseMapping.Pagination.Cursor),
		}
	}
	def.ResponseMapping = rm
}

func applyProviderPagination(def *declarative.Definition, manifestPlugin *providermanifestv1.Spec, allowedOperations map[string]*config.OperationOverride) {
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
		exposedName := opName
		if override.Alias != "" {
			exposedName = override.Alias
		}
		op, ok := def.Operations[exposedName]
		if !ok {
			continue
		}
		op.Pagination = &declarative.PaginationDef{
			Style:        string(pgn.Style),
			CursorParam:  pgn.CursorParam,
			Cursor:       cloneManifestValueSelectorDef(pgn.Cursor),
			LimitParam:   pgn.LimitParam,
			DefaultLimit: pgn.DefaultLimit,
			ResultsPath:  pgn.ResultsPath,
			MaxPages:     pgn.MaxPages,
		}
		def.Operations[exposedName] = op
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

func cloneManifestValueSelectorDef(in *providermanifestv1.ManifestValueSelector) *declarative.ValueSelectorDef {
	if in == nil {
		return nil
	}
	return &declarative.ValueSelectorDef{
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

	tokenExchange, err := oauth.ParseTokenExchangeFormat(auth.TokenExchange)
	if err != nil {
		return nil, err
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

func buildOAuthHandlerFromDefinition(def *declarative.Definition, conn config.ConnectionDef, pluginConfig map[string]any, deps Deps) (OAuthHandler, error) {
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
	serviceConn := declarativeConnectionDef(effectiveConn)
	declarative.ApplyConnectionAuth(&defCopy, serviceConn)
	upstream, err := declarative.BuildOAuthUpstream(&defCopy, serviceConn, defCopy.BaseURL, nil)
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

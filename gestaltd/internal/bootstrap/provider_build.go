package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/apps/composite"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/graphql"
	"github.com/valon-technologies/gestalt/server/services/apps/mcpoauth"
	"github.com/valon-technologies/gestalt/server/services/apps/mcpupstream"
	"github.com/valon-technologies/gestalt/server/services/apps/oauth"
	"github.com/valon-technologies/gestalt/server/services/apps/openapi"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"gopkg.in/yaml.v3"
)

type pendingProviderBuild struct {
	name  string
	entry *config.ProviderEntry
	proxy *startupProviderProxy
}

type preparedProviderBuilds struct {
	providers      *registry.ProviderMap[core.Provider]
	pending        []pendingProviderBuild
	connAuth       map[string]map[string]OAuthHandler
	manualConnAuth map[string]map[string]ManualTokenExchanger
	errs           []error
}

func prepareProviderBuilds(
	cfg *config.Config,
	factories *FactoryRegistry,
	deps Deps,
) (*preparedProviderBuilds, error) {
	reg := registry.New()
	connAuth := make(map[string]map[string]OAuthHandler)
	manualConnAuth := make(map[string]map[string]ManualTokenExchanger)

	for _, builtin := range factories.Builtins {
		if err := validateProviderConnectionMode(builtin.Name(), builtin.ConnectionMode()); err != nil {
			return nil, fmt.Errorf("bootstrap: builtin provider %q: %w", builtin.Name(), err)
		}
		if err := reg.Providers.Register(builtin.Name(), builtin); errors.Is(err, core.ErrAlreadyRegistered) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("bootstrap: registering builtin %q: %w", builtin.Name(), err)
		}
		slog.Info("loaded builtin provider", "provider", builtin.Name(), "operations", catalogOperationCount(builtin.Catalog()))
	}

	builds := &preparedProviderBuilds{
		providers:      &reg.Providers,
		connAuth:       connAuth,
		manualConnAuth: manualConnAuth,
	}
	if len(cfg.Apps) == 0 {
		return builds, nil
	}

	for name := range cfg.Apps {
		intgDef := cfg.Apps[name]
		var proxy *startupProviderProxy
		if deps.WorkflowRuntime != nil {
			spec, operationRouting, err := buildStartupProviderSpec(name, intgDef)
			if err != nil {
				slog.Warn("building startup provider proxy metadata failed", "provider", name, "error", err)
			} else {
				proxy = newStartupProviderProxy(spec, operationRouting, deps.WorkflowRuntime.StartupWaitTracker())
				if err := reg.Providers.Register(name, proxy); err != nil {
					builds.errs = append(builds.errs, fmt.Errorf("integration %q: %w", name, err))
					slog.Warn("registering startup provider proxy failed", "provider", name, "error", err)
					proxy = nil
				}
			}
		}
		builds.pending = append(builds.pending, pendingProviderBuild{name: name, entry: intgDef, proxy: proxy})
	}
	return builds, nil
}

func (b *preparedProviderBuilds) Start(
	ctx context.Context,
	deps Deps,
	builder func(context.Context, string, *config.ProviderEntry, Deps) (*ProviderBuildResult, error),
) (<-chan struct{}, func() map[string]map[string]OAuthHandler, func() map[string]map[string]ManualTokenExchanger, func() []error) {
	ready := make(chan struct{})
	if b == nil || b.providers == nil || len(b.pending) == 0 {
		close(ready)
		return ready,
			func() map[string]map[string]OAuthHandler {
				if b == nil {
					return nil
				}
				return b.connAuth
			},
			func() map[string]map[string]ManualTokenExchanger {
				if b == nil {
					return nil
				}
				return b.manualConnAuth
			},
			func() []error {
				if b == nil {
					return nil
				}
				return append([]error(nil), b.errs...)
			}
	}

	buildErrs := append([]error(nil), b.errs...)
	var connMu sync.Mutex
	var errMu sync.Mutex
	var wg sync.WaitGroup

	for _, pending := range b.pending {
		wg.Add(1)
		go func(pending pendingProviderBuild) {
			defer wg.Done()
			buildCtx := invocation.WithCallerProvider(ctx, invocation.ProviderKindApp, pending.name)
			result, err := builder(buildCtx, pending.name, pending.entry, deps)
			if err != nil {
				errMu.Lock()
				buildErrs = append(buildErrs, fmt.Errorf("integration %q: %w", pending.name, err))
				errMu.Unlock()
				if pending.proxy != nil {
					pending.proxy.fail(err)
					b.providers.Remove(pending.name)
				}
				slog.Warn("skipping provider", "provider", pending.name, "error", err)
				return
			}
			if err := validateProviderConnectionMode(pending.name, result.Provider.ConnectionMode()); err != nil {
				errMu.Lock()
				buildErrs = append(buildErrs, fmt.Errorf("integration %q: %w", pending.name, err))
				errMu.Unlock()
				if pending.proxy != nil {
					pending.proxy.fail(err)
					b.providers.Remove(pending.name)
				}
				closeIfPossible(result.Provider)
				slog.Warn("skipping provider", "provider", pending.name, "error", err)
				return
			}
			if pending.proxy != nil {
				if err := b.providers.Replace(pending.name, result.Provider); err != nil {
					errMu.Lock()
					buildErrs = append(buildErrs, fmt.Errorf("integration %q: %w", pending.name, err))
					errMu.Unlock()
					pending.proxy.fail(err)
					b.providers.Remove(pending.name)
					closeIfPossible(result.Provider)
					slog.Warn("replacing startup provider proxy failed", "provider", pending.name, "error", err)
					return
				}
				pending.proxy.publish(result.Provider)
			} else {
				if err := b.providers.Register(pending.name, result.Provider); err != nil {
					errMu.Lock()
					buildErrs = append(buildErrs, fmt.Errorf("integration %q: %w", pending.name, err))
					errMu.Unlock()
					closeIfPossible(result.Provider)
					slog.Warn("registering provider failed", "provider", pending.name, "error", err)
					return
				}
			}
			if len(result.ConnectionAuth) > 0 {
				connMu.Lock()
				b.connAuth[pending.name] = result.ConnectionAuth
				connMu.Unlock()
			}
			if len(result.ManualConnectionAuth) > 0 {
				connMu.Lock()
				b.manualConnAuth[pending.name] = result.ManualConnectionAuth
				connMu.Unlock()
			}
			slog.Info("loaded provider", "provider", pending.name, "operations", catalogOperationCount(result.Provider.Catalog()))
		}(pending)
	}

	go func() {
		wg.Wait()
		close(ready)
	}()

	resolver := func() map[string]map[string]OAuthHandler {
		<-ready
		return b.connAuth
	}
	manualResolver := func() map[string]map[string]ManualTokenExchanger {
		<-ready
		return b.manualConnAuth
	}
	errResolver := func() []error {
		<-ready
		errMu.Lock()
		defer errMu.Unlock()
		return append([]error(nil), buildErrs...)
	}
	return ready, resolver, manualResolver, errResolver
}

func validateProviderConnectionMode(provider string, mode core.ConnectionMode) error {
	switch core.NormalizeConnectionMode(mode) {
	case core.ConnectionModeNone, core.ConnectionModeSubject:
		return nil
	default:
		return fmt.Errorf("unsupported connection mode %q for provider %q", mode, provider)
	}
}

func BuildStartupProviderSpec(name string, entry *config.ProviderEntry) (appservice.StaticProviderSpec, map[string]string, error) {
	spec, routing, err := buildStartupProviderSpec(name, entry)
	return spec, routing.connections, err
}

type startupOperationRouting struct {
	connections    map[string]string
	resolver       core.OperationConnectionResolver
	overridePolicy core.OperationConnectionOverridePolicy
}

func buildStartupProviderSpec(name string, entry *config.ProviderEntry) (appservice.StaticProviderSpec, startupOperationRouting, error) {
	if entry == nil {
		return appservice.StaticProviderSpec{}, startupOperationRouting{}, fmt.Errorf("integration %q has no app defined", name)
	}
	manifest := entry.ResolvedManifest
	manifestApp := entry.ManifestSpec()
	if manifest == nil || manifestApp == nil {
		return appservice.StaticProviderSpec{}, startupOperationRouting{}, fmt.Errorf("integration %q must resolve to a provider manifest", name)
	}

	meta := resolveProviderMetadata(entry)
	spec, plan, err := buildAppStaticSpec(name, entry, manifest, meta)
	if err != nil {
		return appservice.StaticProviderSpec{}, startupOperationRouting{}, err
	}
	restConnections, restSelectors, restLocks, err := plan.RESTOperationConnectionBindings(manifestApp)
	if err != nil {
		return appservice.StaticProviderSpec{}, startupOperationRouting{}, err
	}
	if spec.Catalog == nil && manifestApp.IsDeclarative() {
		declarative, err := appservice.NewDeclarativeProvider(
			manifest,
			nil,
			appservice.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
			appservice.WithDeclarativeConnectionMode(plan.ConnectionMode()),
			appservice.WithDeclarativeOperationConnections(restConnections, restSelectors, restLocks),
		)
		if err != nil {
			return appservice.StaticProviderSpec{}, startupOperationRouting{}, err
		}
		spec.Catalog = declarative.Catalog()
		return spec, startupOperationRouting{
			connections:    operationConnectionsForCatalog(spec.Catalog, plan, restConnections),
			resolver:       declarative,
			overridePolicy: declarative,
		}, nil
	}
	return spec, startupOperationRouting{connections: operationConnectionsForCatalog(spec.Catalog, plan, restConnections)}, nil
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
		return config.AppConnectionName
	}
	if specConnection != "" && specConnection != config.AppConnectionName {
		return specConnection
	}
	if fallback := plan.AuthDefaultConnection(); fallback != "" {
		return fallback
	}
	return config.AppConnectionName
}

func explicitPluginConnection(plan config.StaticConnectionPlan) bool {
	pluginConnection := plan.ResolvedAppConnection()
	if pluginConnection.Source.ModeSource == config.ConfigSourceDeploy ||
		pluginConnection.Source.AuthSource == config.ConfigSourceDeploy ||
		len(pluginConnection.Params) > 0 {
		return true
	}
	return plan.AuthDefaultConnection() == config.AppConnectionName && len(plan.NamedConnectionNames()) > 0
}

func buildProvider(ctx context.Context, name string, entry *config.ProviderEntry, deps Deps) (*ProviderBuildResult, error) {
	if entry == nil {
		return nil, fmt.Errorf("integration %q has no app defined", name)
	}

	meta := resolveProviderMetadata(entry)
	pluginConfig, err := config.NodeToMap(entry.Config)
	if err != nil {
		return nil, fmt.Errorf("decode app config for %q: %w", name, err)
	}

	manifest := entry.ResolvedManifest
	manifestApp := entry.ManifestSpec()
	if manifest == nil || manifestApp == nil {
		return nil, fmt.Errorf("integration %q must resolve to a provider manifest", name)
	}

	allowedOperations := entry.AllowedOperations
	if allowedOperations == nil {
		allowedOperations = maps.Clone(manifestApp.AllowedOperations)
	}

	switch {
	case manifestApp.IsSpecLoaded() && manifest.Entrypoint == nil:
		return buildSpecLoadedProvider(ctx, name, entry, manifest, pluginConfig, meta, deps, allowedOperations)
	case manifestApp.IsDeclarative() && manifest.Entrypoint == nil:
		plan, err := config.BuildStaticConnectionPlan(entry, manifestApp)
		if err != nil {
			return nil, fmt.Errorf("build declarative provider %q: %w", name, err)
		}
		restConnections, restSelectors, restLocks, err := plan.RESTOperationConnectionBindings(manifestApp)
		if err != nil {
			return nil, fmt.Errorf("build declarative provider %q: %w", name, err)
		}
		declarative, err := appservice.NewDeclarativeProvider(
			manifest,
			nil,
			appservice.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
			appservice.WithDeclarativeConnectionMode(plan.ConnectionMode()),
			appservice.WithDeclarativeOperationConnections(restConnections, restSelectors, restLocks),
			appservice.WithDeclarativeEgressCheck(deps.Egress.CheckFunc(entry.EffectiveAllowedHosts())),
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
		return buildExecutableAppProvider(ctx, name, entry, pluginConfig, meta, deps)
	}
}

func buildExecutableAppProvider(ctx context.Context, name string, entry *config.ProviderEntry, pluginConfig map[string]any, meta providerMetadata, deps Deps) (*ProviderBuildResult, error) {
	manifest := entry.ResolvedManifest
	manifestApp := entry.ManifestSpec()
	if manifest == nil || manifestApp == nil {
		return nil, fmt.Errorf("build executable app provider %q: resolved manifest is required", name)
	}
	staticSpec, plan, err := buildAppStaticSpec(name, entry, manifest, meta)
	if err != nil {
		return nil, fmt.Errorf("build executable app provider %q: %w", name, err)
	}
	pluginProv, err := buildAppProvider(ctx, name, entry, pluginConfig, staticSpec, deps)
	if err != nil {
		return nil, err
	}
	allowedOperations := entry.AllowedOperations
	if allowedOperations == nil && manifestApp != nil {
		allowedOperations = maps.Clone(manifestApp.AllowedOperations)
	}
	staticAllowedOperations := operationexposure.MatchingAllowedOperations(allowedOperations, pluginProv.Catalog())

	if manifestApp.IsDeclarative() {
		restConnections, restSelectors, restLocks, err := plan.RESTOperationConnectionBindings(manifestApp)
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
		declarative, err := appservice.NewDeclarativeProvider(
			manifest,
			nil,
			appservice.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
			appservice.WithDeclarativeConnectionMode(plan.ConnectionMode()),
			appservice.WithDeclarativeOperationConnections(restConnections, restSelectors, restLocks),
			appservice.WithDeclarativeEgressCheck(deps.Egress.CheckFunc(entry.EffectiveAllowedHosts())),
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
			composite.BoundProvider{Provider: apiProv, FallbackConnection: plan.RESTConnection()},
		)
		if err != nil {
			closeIfPossible(apiProv, pluginProv)
			return nil, err
		}
		return newProviderBuildResult(name, entry, manifest, pluginConfig, merged, nil, deps)
	}

	specProv, authFallback, err := buildConfiguredSpecComposite(ctx, name, entry, plan, manifestApp, meta, deps, allowedOperations)
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
	manifestApp          *providermanifestv1.Spec
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
		resolvedName = config.AppConnectionName
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
		resolvedName = config.AppConnectionName
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

func buildConfiguredSpecComposite(ctx context.Context, name string, entry *config.ProviderEntry, plan config.StaticConnectionPlan, manifestApp *providermanifestv1.Spec, meta providerMetadata, deps Deps, allowedOperations map[string]*config.OperationOverride) (core.Provider, *specAuthFallback, error) {
	mcpResolved, hasMCP := plan.ResolvedSurface(config.SpecSurfaceMCP)
	mcpURL := ""
	if hasMCP {
		mcpURL = mcpResolved.URL
	}

	cfg := specProviderConfig{
		manifestApp:          manifestApp,
		allowedOperations:    allowedOperations,
		allowedHosts:         entry.EffectiveAllowedHosts(),
		baseURL:              config.EffectiveProviderSpecBaseURL(entry, manifestApp),
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

	mcpCfg := cfg
	mcpCfg.allowedOperations = nil
	mcpProv, _, err := buildConfiguredSpecProvider(ctx, name, mcpResolved, meta, mcpCfg, deps)
	if err != nil {
		closeIfPossible(apiProv)
		return nil, nil, err
	}
	mcpUp, ok := mcpProv.(composite.MCPUpstream)
	if !ok {
		closeIfPossible(mcpProv, apiProv)
		return nil, nil, fmt.Errorf("unexpected mcp provider type %T", mcpProv)
	}

	var apiCatalog *catalog.Catalog
	if apiProv != nil {
		apiCatalog = apiProv.Catalog()
	}
	mcpAllowedOperations, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, apiProv != nil, apiCatalog, mcpUp.Catalog())
	if !includeMCP {
		closeIfPossible(mcpUp)
		return apiProv, authFallback, nil
	}
	if mcpAllowedOperations != nil {
		filterable, ok := any(mcpUp).(interface {
			FilterOperations(map[string]*operationexposure.OperationOverride) error
		})
		if !ok {
			closeIfPossible(mcpUp, apiProv)
			return nil, nil, fmt.Errorf("unexpected non-filterable mcp provider type %T", mcpProv)
		}
		if err := filterable.FilterOperations(mcpAllowedOperations); err != nil {
			closeIfPossible(mcpUp, apiProv)
			return nil, nil, fmt.Errorf("filter mcp operations: %w", err)
		}
	}

	if apiProv == nil {
		return mcpUp, nil, nil
	}
	return composite.New(name, apiProv, mcpUp), authFallback, nil
}

func mcpAllowedOperationsForSpecComposite(allowedOperations map[string]*config.OperationOverride, hasAPI bool, apiCatalog, mcpCatalog *catalog.Catalog) (map[string]*config.OperationOverride, bool) {
	if allowedOperations == nil {
		return nil, true
	}
	if !hasAPI {
		return allowedOperations, true
	}
	mcpAllowed := dynamicMCPAllowedOperations(allowedOperations, apiCatalog)
	if mcpCatalog != nil && len(mcpCatalog.Operations) > 0 {
		matched := operationexposure.MatchingAllowedOperations(mcpAllowed, mcpCatalog)
		return matched, len(matched) > 0
	}
	if len(mcpAllowed) == 0 {
		return nil, false
	}
	return mcpAllowed, true
}

func dynamicMCPAllowedOperations(allowedOperations map[string]*config.OperationOverride, apiCatalog *catalog.Catalog) map[string]*config.OperationOverride {
	apiOps := catalogOperationIDs(apiCatalog)
	filtered := make(map[string]*config.OperationOverride)
	for name, override := range allowedOperations {
		if override != nil && override.GraphQL != nil {
			continue
		}
		if _, ok := apiOps[name]; ok {
			continue
		}
		if override != nil && override.Alias != "" {
			if _, ok := apiOps[override.Alias]; ok {
				continue
			}
		}
		filtered[name] = override
	}
	return filtered
}

func catalogOperationIDs(cat *catalog.Catalog) map[string]struct{} {
	ids := make(map[string]struct{})
	if cat == nil {
		return ids
	}
	for i := range cat.Operations {
		ids[cat.Operations[i].ID] = struct{}{}
	}
	return ids
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

func loadConfiguredAPIDefinition(ctx context.Context, name string, resolved config.ResolvedSpecSurface, meta providerMetadata, cfg specProviderConfig) (*declarative.Definition, error) {
	def, err := loadSpecDefinition(ctx, name, resolved, cfg.allowedOperations)
	if err != nil {
		return nil, fmt.Errorf("load %s definition: %w", resolved.Surface, err)
	}
	if cfg.baseURL != "" && resolved.Surface == config.SpecSurfaceOpenAPI {
		def.BaseURL = cfg.baseURL
	}
	applyProviderHeaders(def, cfg.manifestApp)
	if err := applyManagedParameters(def, cfg.manifestApp); err != nil {
		return nil, err
	}
	if cfg.applyResponseMapping {
		applyProviderResponseMapping(def, cfg.manifestApp)
	}
	applyProviderPagination(def, cfg.manifestApp, cfg.allowedOperations)
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
		if resolved.Surface == config.SpecSurfaceGraphQL && len(def.Operations) == 0 {
			prov = wrapGraphQLSessionCatalogProvider(prov, name, resolved.URL, cfg.allowedOperations)
		}
		return prov, def, nil
	case config.SpecSurfaceMCP:
		connMode := core.ConnectionMode(resolved.Connection.Mode)
		if connMode == "" {
			connMode = core.ConnectionModeSubject
		}
		connMode = core.NormalizeConnectionMode(connMode)
		mcpOpts := []mcpupstream.Option{
			mcpupstream.WithMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
			mcpupstream.WithConnectionName(resolved.ConnectionName),
		}
		up, err := mcpupstream.New(
			ctx,
			name,
			resolved.URL,
			connMode,
			manifestHeaders(cfg.manifestApp),
			deps.Egress.CheckFunc(cfg.allowedHosts),
			mcpOpts...,
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
		if def, err := graphql.StaticAllowedOperationsDefinition(name, resolved.URL, allowedOperations); def != nil || err != nil {
			return def, err
		}
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

func buildAppProvider(ctx context.Context, name string, entry *config.ProviderEntry, pluginConfig map[string]any, spec appservice.StaticProviderSpec, deps Deps) (core.Provider, error) {
	command := entry.Command
	args := entry.Args
	workdir := ""
	env := clonePluginEnv(entry.Env)
	var cleanup func()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	runtimeConfig, runtimeProvider, runtimeOwned, err := effectiveRuntime(ctx, name, entry, deps)
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
	runtimePlan := buildRuntimePlan(entry, deps, runtimeSupport)
	if err := runtimePlan.Validate(hostedRuntimeLabel(runtimeConfig), deps); err != nil {
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
		execution, err := providerpkg.SourceManifestExecution(entry.ResolvedManifestPath, providermanifestv1.KindApp, providerpkg.SourceBuildOptions{})
		if err != nil {
			if runtimeOwned {
				_ = runtimeProvider.Close()
			}
			return nil, fmt.Errorf("prepare synthesized source provider execution: %w", err)
		}
		command = execution.Command
		args = execution.Args
		workdir = execution.Workdir
		if len(execution.Env) > 0 {
			if env == nil {
				env = maps.Clone(execution.Env)
			} else {
				maps.Copy(env, execution.Env)
			}
		}
		cleanup = execution.Cleanup
	}
	launch, err := prepareHostedProcessLaunch(providermanifestv1.KindApp, name, entry, command, args, cleanup, runtimeConfig)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	cleanup = nil
	launchCleanup := launch.cleanup
	defer func() {
		if launchCleanup != nil {
			launchCleanup()
		}
	}()
	command = launch.command
	args = launch.args
	session, err := runtimeProvider.StartSession(ctx, buildHostedRuntimeStartSessionRequest(providermanifestv1.KindApp, name, runtimeConfig))
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, fmt.Errorf("start runtime session: %w", err)
	}
	sessionID := session.GetId()
	stopSession := true
	defer func() {
		if !stopSession {
			return
		}
		_ = stopRuntimeSession(runtimeProvider, sessionID)
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
	}()
	if _, err := waitForRuntimeSessionReady(ctx, runtimeProvider, sessionID); err != nil {
		return nil, fmt.Errorf("wait for runtime session %q ready: %w", sessionID, err)
	}

	hostServices, err := buildProviderHostServices(name, appProviderHostServiceDeps(entry, deps))
	if err != nil {
		return nil, err
	}
	publicHostServicesCleanup, err := registerPublicRuntimeHostServices(name, hostServices, deps, runtimeProvider)
	if err != nil {
		return nil, err
	}
	cleanup = chainCleanup(launchCleanup, publicHostServicesCleanup)
	launchCleanup = nil
	startEnv := maps.Clone(env)
	startEnv = withHostServiceTLSCAEnv(startEnv, deps)
	if startEnv == nil {
		startEnv = map[string]string{}
	}
	maps.Copy(startEnv, runtimehost.ProviderTelemetryEnv(deps.Telemetry, name))
	egressPolicy := deps.Egress.ProviderPolicy(entry)
	allowedHosts := entry.EffectiveAllowedHosts()
	startEnv, allowedHosts, err = applyHostedRuntimeHostServiceRelayEnv(name, sessionID, hostServices, runtimePlan, deps, startEnv, allowedHosts)
	if err != nil {
		return nil, err
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

	hostedApp, err := runtimeProvider.StartApp(ctx, &proto.StartHostedAppRequest{
		SessionId:     sessionID,
		AppName:       name,
		Command:       command,
		Args:          args,
		Workdir:       workdir,
		Env:           startEnv,
		AllowedHosts:  egressPlan.RuntimeAllowedHosts,
		DefaultAction: string(deps.Egress.DefaultAction),
		HostBinary:    entry.HostBinary,
	})
	if err != nil {
		return nil, fmt.Errorf("start hosted app: %w", err)
	}
	conn, err := runtimeprovider.DialHostedApp(ctx, hostedApp.GetDialTarget(),
		runtimeprovider.WithProviderName(name),
		runtimeprovider.WithTelemetry(deps.Telemetry),
	)
	if err != nil {
		return nil, fmt.Errorf("dial hosted app: %w", err)
	}
	opts := []appservice.RemoteProviderOption{
		appservice.WithCloser(&runtimeBackedHostedCloser{
			conn:         conn,
			runtime:      runtimeProvider,
			sessionID:    sessionID,
			closeRuntime: runtimeOwned,
			cleanup:      cleanup,
		}),
		appservice.WithHostContext(deps.BaseURL),
		appservice.WithCallerProvider(invocation.ProviderKindApp, name),
	}
	prov, err := appservice.NewRemote(ctx, conn.Integration(), spec, pluginConfig, opts...)
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
	runtimeCfg := entry.RuntimePlacementConfig()
	policy, err := runtimeCfg.LifecyclePolicy()
	if err != nil {
		launch.close()
		return nil, fmt.Errorf("parse hosted agent runtime lifecycle policy: %w", err)
	}
	publicHostServicesCleanup, err := registerPublicRuntimeHostServices(name, hostServices, deps, launch.runtimeProvider)
	if err != nil {
		launch.close()
		return nil, err
	}
	launch.cleanup = chainCleanup(launch.cleanup, publicHostServicesCleanup)
	return newHostedAgentProviderPool(ctx, launch, hostServices, deps, policy)
}

type hostedAgentProviderLaunch struct {
	name            string
	runtimeConfig   config.EffectiveRuntimePlacement
	runtimeProvider runtimeprovider.Provider
	runtimeOwned    bool
	runtimePlan     RuntimePlacementPlan
	cfg             componentprovider.YAMLConfig
	allowedHosts    []string
	launch          hostedProcessLaunch
	cleanup         func()
}

type hostedAgentProviderInstance struct {
	provider         coreagent.Provider
	runtimeProvider  runtimeprovider.Provider
	runtimeSessionID string
	runtimeSession   *proto.RuntimeSession
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
	runtimePlan := buildRuntimePlacementPlan(runtimeSupport, deps, runtimeRequiresHostnameEgress(entry, deps))
	if err := runtimePlan.Validate(hostedRuntimeLabel(runtimeConfig), deps); err != nil {
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
	sessionID := session.GetId()
	stopSession := true
	closeOnFailure := closeRuntime
	defer func() {
		if !stopSession {
			return
		}
		_ = stopRuntimeSession(runtimeProvider, sessionID)
		if closeOnFailure {
			_ = runtimeProvider.Close()
		}
		if cleanup != nil {
			cleanup()
		}
	}()
	phaseStarted = time.Now()
	readySession, err := waitForRuntimeSessionReady(ctx, runtimeProvider, sessionID)
	if err != nil {
		recordHostedAgentRuntimeStartPhase(ctx, name, "runtime_session_ready", phaseStarted, err)
		return nil, fmt.Errorf("wait for hosted agent runtime session %q ready: %w", sessionID, err)
	}
	recordHostedAgentRuntimeStartPhase(ctx, name, "runtime_session_ready", phaseStarted, nil)

	startEnv := maps.Clone(cfg.Env)
	startEnv = withHostServiceTLSCAEnv(startEnv, deps)
	if startEnv == nil {
		startEnv = map[string]string{}
	}
	maps.Copy(startEnv, runtimehost.ProviderTelemetryEnv(deps.Telemetry, name))
	agentAllowedHosts := cfg.EgressPolicy("").AllowedHosts
	if len(agentAllowedHosts) == 0 {
		agentAllowedHosts = slices.Clone(launch.allowedHosts)
	}
	allowedHosts := hostedAgentAllowedHosts(agentAllowedHosts, runtimePlan)
	phaseStarted = time.Now()
	startEnv, allowedHosts, err = applyHostedRuntimeHostServiceRelayEnv(name, sessionID, hostServices, runtimePlan, deps, startEnv, allowedHosts)
	if err != nil {
		recordHostedAgentRuntimeStartPhase(ctx, name, "host_services_relay", phaseStarted, err)
		return nil, err
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
	hostedApp, err := runtimeProvider.StartApp(ctx, &proto.StartHostedAppRequest{
		SessionId:     sessionID,
		AppName:       name,
		Command:       launch.launch.command,
		Args:          launch.launch.args,
		Workdir:       cfg.Workdir,
		Env:           startEnv,
		AllowedHosts:  egressPlan.RuntimeAllowedHosts,
		DefaultAction: string(deps.Egress.DefaultAction),
		HostBinary:    cfg.HostBinary,
	})
	recordHostedAgentRuntimeStartPhase(ctx, name, "plugin_start", phaseStarted, err)
	if err != nil {
		return nil, fmt.Errorf("start hosted agent provider: %w", err)
	}
	phaseStarted = time.Now()
	conn, err := runtimeprovider.DialHostedAgent(ctx, hostedApp.GetDialTarget(),
		runtimeprovider.WithProviderName(name),
		runtimeprovider.WithTelemetry(deps.Telemetry),
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

func effectiveConfiguredHostedRuntime(ctx context.Context, configPath string, entry *config.ProviderEntry, deps Deps) (config.EffectiveRuntimePlacement, runtimeprovider.Provider, bool, error) {
	if entry == nil || !entry.UsesRuntimePlacement() {
		return config.EffectiveRuntimePlacement{}, nil, false, nil
	}
	explicitRuntimeConfig := providerEntryRuntimePlacementConfig(entry)
	if deps.Runtime != nil {
		return explicitRuntimeConfig, deps.Runtime, false, nil
	}
	if deps.RuntimeRegistry != nil {
		runtimeConfig, runtimeProvider, err := deps.RuntimeRegistry.Resolve(ctx, configPath, entry)
		if err != nil {
			return config.EffectiveRuntimePlacement{}, nil, false, err
		}
		if runtimeProvider != nil {
			return runtimeConfig, runtimeProvider, false, nil
		}
		if runtimeConfig.Enabled {
			return localRuntimePlacementConfig(runtimeConfig), newLocalRuntime(runtimeConfig.ProviderName, deps), true, nil
		}
	}
	return localRuntimePlacementConfig(explicitRuntimeConfig), newLocalRuntime(explicitRuntimeConfig.ProviderName, deps), true, nil
}

func effectiveRuntime(ctx context.Context, name string, entry *config.ProviderEntry, deps Deps) (config.EffectiveRuntimePlacement, runtimeprovider.Provider, bool, error) {
	if deps.Runtime != nil {
		return providerEntryRuntimePlacementConfig(entry), deps.Runtime, false, nil
	}
	if deps.RuntimeRegistry != nil {
		runtimeConfig, runtimeProvider, err := deps.RuntimeRegistry.Resolve(ctx, "apps."+name, entry)
		if err != nil {
			return config.EffectiveRuntimePlacement{}, nil, false, err
		}
		if runtimeProvider != nil {
			return runtimeConfig, runtimeProvider, false, nil
		}
		if runtimeConfig.Enabled {
			return localRuntimePlacementConfig(runtimeConfig), newLocalRuntime(runtimeConfig.ProviderName, deps), true, nil
		}
	}
	return localRuntimePlacementConfig(config.EffectiveRuntimePlacement{}), newLocalRuntime("", deps), true, nil
}

func providerEntryRuntimePlacementConfig(entry *config.ProviderEntry) config.EffectiveRuntimePlacement {
	if entry == nil {
		return config.EffectiveRuntimePlacement{}
	}
	runtimeCfg := entry.RuntimePlacementConfig()
	if runtimeCfg == nil {
		return config.EffectiveRuntimePlacement{Enabled: entry.UsesRuntimePlacement()}
	}
	effective := config.EffectiveRuntimePlacement{
		Enabled:       entry.UsesRuntimePlacement(),
		ProviderName:  strings.TrimSpace(runtimeCfg.Provider),
		Template:      strings.TrimSpace(runtimeCfg.Template),
		Image:         strings.TrimSpace(runtimeCfg.Image),
		ImagePullAuth: hostedRuntimeConfigImagePullAuth(runtimeCfg.ImagePullAuth),
		Metadata:      maps.Clone(runtimeCfg.Metadata),
		Workspace:     hostedRuntimeWorkspaceConfig(runtimeCfg.Workspace),
	}
	return effective
}

func hostedRuntimeConfigImagePullAuth(auth *config.RuntimePlacementImagePullAuth) *config.RuntimePlacementImagePullAuth {
	if auth == nil {
		return nil
	}
	return &config.RuntimePlacementImagePullAuth{
		DockerConfigJSON: auth.DockerConfigJSON,
	}
}

func hostedRuntimeWorkspaceConfig(workspace *config.RuntimePlacementWorkspaceConfig) *config.RuntimePlacementWorkspaceConfig {
	if workspace == nil {
		return nil
	}
	out := &config.RuntimePlacementWorkspaceConfig{
		PrepareTimeout: workspace.PrepareTimeout,
	}
	if workspace.Git != nil {
		out.Git = &config.RuntimePlacementWorkspaceGitConfig{
			AllowedRepositories: slices.Clone(workspace.Git.AllowedRepositories),
		}
	}
	return out
}

func localRuntimePlacementConfig(runtimeConfig config.EffectiveRuntimePlacement) config.EffectiveRuntimePlacement {
	if runtimeConfig.Provider == nil {
		runtimeConfig.Provider = &config.RuntimeProviderEntry{Driver: config.RuntimeProviderDriverLocal}
	}
	return runtimeConfig
}

func newLocalRuntime(runtimeProviderName string, deps Deps) runtimeprovider.Provider {
	name := strings.TrimSpace(runtimeProviderName)
	if name == "" {
		name = "local"
	}
	_ = name
	return runtimeprovider.NewLocalProvider(runtimeprovider.WithLocalTelemetry(deps.Telemetry))
}

const (
	runtimeEgressProxyTokenTTL = 30 * 24 * time.Hour
)

type RuntimeEgressLaunchPlan struct {
	Policy              egress.Policy
	Delivery            RuntimeHostnameEgressDelivery
	Env                 map[string]string
	RuntimeAllowedHosts []string
}

func hostedRuntimeLabel(runtimeConfig config.EffectiveRuntimePlacement) string {
	if name := strings.TrimSpace(runtimeConfig.ProviderName); name != "" {
		return fmt.Sprintf("runtime provider %q", name)
	}
	return "hosted runtime"
}

func buildHostedRuntimeStartSessionRequest(kind, name string, runtimeConfig config.EffectiveRuntimePlacement) *proto.StartRuntimeSessionRequest {
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
	return &proto.StartRuntimeSessionRequest{
		AppName:       name,
		Template:      runtimeConfig.Template,
		Image:         runtimeConfig.Image,
		ImagePullAuth: hostedRuntimeImagePullAuth(runtimeConfig.ImagePullAuth),
		Metadata:      metadata,
	}
}

func hostedRuntimeImagePullAuth(auth *config.RuntimePlacementImagePullAuth) *proto.RuntimeImagePullAuth {
	if auth == nil {
		return nil
	}
	return &proto.RuntimeImagePullAuth{
		DockerConfigJson: auth.DockerConfigJSON,
	}
}

func buildHostedRuntimeEgressLaunchPlan(providerName, sessionID string, policy egress.Policy, runtimeAllowedHosts []string, runtimePlan RuntimePlacementPlan, deps Deps) (RuntimeEgressLaunchPlan, error) {
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

const runtimeStopTimeout = 3 * time.Second

type runtimeBackedHostedCloser struct {
	conn         io.Closer
	runtime      runtimeprovider.Provider
	sessionID    string
	closeRuntime bool
	cleanup      func()
	stopTimeout  time.Duration
}

func (c *runtimeBackedHostedCloser) Close() error {
	if c == nil {
		return nil
	}
	var errs []error
	if c.runtime != nil && c.sessionID != "" {
		errs = append(errs, stopRuntimeSessionWithTimeout(c.runtime, c.sessionID, c.stopTimeout))
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

func stopRuntimeSession(runtimeProvider runtimeprovider.Provider, sessionID string) error {
	return stopRuntimeSessionWithTimeout(runtimeProvider, sessionID, 0)
}

func stopRuntimeSessionWithTimeout(runtimeProvider runtimeprovider.Provider, sessionID string, timeout time.Duration) error {
	if runtimeProvider == nil || sessionID == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = runtimeStopTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runtimeProvider.StopSession(ctx, &proto.StopRuntimeSessionRequest{SessionId: sessionID})
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("stop runtime session %q: %w", sessionID, ctx.Err())
	}
}

func waitForRuntimeSessionReady(ctx context.Context, runtimeProvider runtimeprovider.Provider, sessionID string) (*proto.RuntimeSession, error) {
	for {
		session, err := runtimeProvider.GetSession(ctx, &proto.GetRuntimeSessionRequest{SessionId: sessionID})
		if err != nil {
			return nil, err
		}
		switch session.GetState() {
		case runtimeprovider.SessionStateReady, runtimeprovider.SessionStateRunning:
			return session, nil
		case runtimeprovider.SessionStateFailed, runtimeprovider.SessionStateStopped:
			return nil, fmt.Errorf("session entered %q state", session.GetState())
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func buildHostedRuntimePublicEgressProxy(providerName, sessionID string, allowedHosts []string, defaultAction egress.PolicyAction, deps Deps) (map[string]string, error) {
	baseURL, explicitRelayBaseURL := hostedRuntimeRelayBaseURL(deps)
	if baseURL == "" || len(deps.EncryptionKey) == 0 {
		return nil, fmt.Errorf("provider %q requires server.baseURL and server.encryptionKey to enforce hostname-based egress for hosted runtimes", providerName)
	}
	proxyBaseURL, _, err := runtimePublicProxyBaseURL(baseURL, explicitRelayBaseURL)
	if err != nil {
		return nil, err
	}
	tokenManager, err := egressproxy.NewTokenManager(deps.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("init egress proxy tokens: %w", err)
	}
	token, err := tokenManager.MintToken(egressproxy.TokenRequest{
		AppName:       providerName,
		SessionID:     sessionID,
		AllowedHosts:  slices.Clone(allowedHosts),
		DefaultAction: defaultAction,
		TTL:           runtimeEgressProxyTokenTTL,
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

func hostedAgentAllowedHosts(allowedHosts []string, runtimePlan RuntimePlacementPlan) []string {
	cloned := slices.Clone(allowedHosts)
	if runtimePlan.RequiresHostnameEgress {
		return cloned
	}
	// Hosted agent bundles include loopback host allowances for local SDK
	// transports. Once the agent provider is exposed over the public relay, those
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

type unavailableAppInvocation struct{}

func (unavailableAppInvocation) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return nil, fmt.Errorf("plugin invoker is not available")
}

func (unavailableAppInvocation) InvokeGraphQL(context.Context, *principal.Principal, string, string, invocation.GraphQLRequest) (*core.OperationResult, error) {
	return nil, fmt.Errorf("plugin invoker is not available")
}

type unavailableWorkflowManager struct{}

func (unavailableWorkflowManager) ApplyDefinition(context.Context, *principal.Principal, workflowmanager.DefinitionApply) (*workflowmanager.ManagedDefinition, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetDefinition(context.Context, *principal.Principal, string) (*workflowmanager.ManagedDefinition, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) ListDefinitions(context.Context, *principal.Principal) (*workflowmanager.ListDefinitionsResponse, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) SetDefinitionPaused(context.Context, *principal.Principal, string, bool) (*workflowmanager.ManagedDefinition, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) SetActivationPaused(context.Context, *principal.Principal, string, string, bool) (*workflowmanager.ManagedDefinition, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) DeleteDefinition(context.Context, *principal.Principal, string) error {
	return fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) ListRuns(context.Context, *principal.Principal, coreworkflow.ListRunsRequest) (*workflowmanager.ListRunsResponse, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) StartRun(context.Context, *principal.Principal, workflowmanager.RunStart) (*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetRun(context.Context, *principal.Principal, string) (*workflowmanager.ManagedRun, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetRunEvents(context.Context, *principal.Principal, string) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	return nil, fmt.Errorf("workflow manager is not available")
}

func (unavailableWorkflowManager) GetRunOutput(context.Context, *principal.Principal, string) (*proto.GetWorkflowProviderRunOutputResponse, error) {
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

func (unavailableWorkflowManager) DeliverEvent(context.Context, *principal.Principal, workflowmanager.EventDeliver) (coreworkflow.Event, error) {
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

func (unavailableAgentManager) CreateSession(context.Context, *principal.Principal, *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) GetSession(context.Context, *principal.Principal, *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListSessions(context.Context, *principal.Principal, *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) UpdateSession(context.Context, *principal.Principal, *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) CreateTurn(context.Context, *principal.Principal, *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) GetTurn(context.Context, *principal.Principal, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListTurns(context.Context, *principal.Principal, *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) CancelTurn(context.Context, *principal.Principal, *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListTurnEvents(context.Context, *principal.Principal, *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ListInteractions(context.Context, *principal.Principal, *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) ResolveInteraction(context.Context, *principal.Principal, *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	return nil, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) AuthorizeAppInvocation(context.Context, invocation.AgentAppAuthorizationRequest) (invocation.AgentAppAuthorization, error) {
	return invocation.AgentAppAuthorization{}, fmt.Errorf("agent manager is not available")
}

func (unavailableAgentManager) AuthorizeWorkflowInvocation(context.Context, invocation.AgentWorkflowAuthorizationRequest) (invocation.AgentWorkflowAuthorization, error) {
	return invocation.AgentWorkflowAuthorization{}, fmt.Errorf("agent manager is not available")
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

func buildAppStaticSpec(name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, meta providerMetadata) (appservice.StaticProviderSpec, config.StaticConnectionPlan, error) {
	if manifest == nil || manifest.Spec == nil {
		return appservice.StaticProviderSpec{}, config.StaticConnectionPlan{}, fmt.Errorf("resolved manifest is required")
	}
	plan, err := config.BuildStaticConnectionPlan(entry, manifest.Spec)
	if err != nil {
		return appservice.StaticProviderSpec{}, config.StaticConnectionPlan{}, err
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

	conn := plan.AppConnection()
	connMode := plan.ConnectionMode()

	var staticCatalog *catalog.Catalog
	if manifestRoot := filepath.Dir(entry.ResolvedManifestPath); entry.ResolvedManifestPath != "" {
		var err error
		staticCatalog, err = providerpkg.ReadStaticCatalog(manifestRoot, name)
		if err != nil {
			return appservice.StaticProviderSpec{}, config.StaticConnectionPlan{}, err
		}
	}
	if staticCatalog == nil && providerpkg.StaticCatalogRequired(manifest) {
		if entry.ResolvedManifestPath == "" {
			return appservice.StaticProviderSpec{}, config.StaticConnectionPlan{}, fmt.Errorf("resolved manifest path is required for executable provider static catalog")
		}
		return appservice.StaticProviderSpec{}, config.StaticConnectionPlan{}, fmt.Errorf("executable providers without declarative or spec surfaces must define %s", providerpkg.StaticCatalogFile)
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

	return appservice.StaticProviderSpec{
		Name:             name,
		DisplayName:      displayName,
		Description:      description,
		IconSVG:          iconSVG,
		ConnectionMode:   connMode,
		Catalog:          staticCatalog,
		AuthTypes:        staticAuthTypes(conn.Auth.Type),
		ConnectionParams: appservice.ConnectionParamDefsFromManifest(conn.ConnectionParams),
		CredentialFields: appservice.CredentialFieldsFromManifest(conn.Auth.Credentials),
		DiscoveryConfig:  appservice.DiscoveryConfigFromManifest(conn.Discovery),
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
		declarative.WithAuthHandler(buildMCPOAuthHandler(conn, mcpURL, deps)),
	}
}

func manifestHeaders(manifestApp *providermanifestv1.Spec) map[string]string {
	if manifestApp == nil || len(manifestApp.Headers) == 0 {
		return nil
	}
	return maps.Clone(manifestApp.Headers)
}

func applyProviderHeaders(def *declarative.Definition, manifestApp *providermanifestv1.Spec) {
	if def == nil {
		return
	}
	headers := manifestHeaders(manifestApp)
	if len(headers) == 0 {
		return
	}
	def.Headers = headers
}

func applyManagedParameters(def *declarative.Definition, manifestApp *providermanifestv1.Spec) error {
	if def == nil || manifestApp == nil || len(manifestApp.ManagedParameters) == 0 {
		return nil
	}

	if def.Headers == nil {
		def.Headers = make(map[string]string)
	}
	for _, param := range manifestApp.ManagedParameters {
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
		for _, param := range manifestApp.ManagedParameters {
			if strings.EqualFold(strings.TrimSpace(param.In), "path") {
				op.Path = strings.ReplaceAll(op.Path, "{"+strings.TrimSpace(param.Name)+"}", param.Value)
			}
		}
		filtered := op.Parameters[:0]
		for _, param := range op.Parameters {
			if isManagedOperationParameter(param, manifestApp.ManagedParameters) {
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

func applyProviderResponseMapping(def *declarative.Definition, manifestApp *providermanifestv1.Spec) {
	if def == nil || manifestApp == nil || manifestApp.ResponseMapping == nil {
		return
	}
	rm := &declarative.ResponseMappingDef{
		DataPath: manifestApp.ResponseMapping.DataPath,
	}
	if manifestApp.ResponseMapping.Pagination != nil {
		rm.Pagination = &declarative.PaginationMappingDef{
			HasMore: cloneManifestValueSelectorDef(manifestApp.ResponseMapping.Pagination.HasMore),
			Cursor:  cloneManifestValueSelectorDef(manifestApp.ResponseMapping.Pagination.Cursor),
		}
	}
	def.ResponseMapping = rm
}

func applyProviderPagination(def *declarative.Definition, manifestApp *providermanifestv1.Spec, allowedOperations map[string]*config.OperationOverride) {
	if def == nil || manifestApp == nil {
		return
	}
	for opName, override := range allowedOperations {
		if override == nil || !override.Paginate {
			continue
		}
		pgn := mergedPaginationConfig(manifestApp.Pagination, override.Pagination)
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

func buildMCPOAuthHandler(conn config.ConnectionDef, mcpURL string, deps Deps) *mcpoauth.Handler {
	redirectURL := conn.Auth.RedirectURL
	if redirectURL == "" {
		redirectURL = deps.BaseURL + config.IntegrationCallbackPath
	}
	return mcpoauth.NewHandler(mcpoauth.HandlerConfig{
		MCPURL:       mcpURL,
		Credentials:  deps.Services.ExternalCredentials,
		RedirectURL:  redirectURL,
		ClientID:     conn.Auth.ClientID,
		ClientSecret: conn.Auth.ClientSecret,
	})
}

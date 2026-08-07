package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/autodeploy"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	gestaltmcp "github.com/valon-technologies/gestalt/server/services/apps/mcp"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
)

const (
	runtimeShutdownTimeout        = 15 * time.Second
	readinessIndexedDBPingTimeout = 2 * time.Second
)

func httpCatalogConnectionMap(connMaps bootstrap.ConnectionMaps) map[string]string {
	return connMaps.APIConnection
}

func Run(ctx context.Context, cfg *config.Config, result *bootstrap.Result, gestaltdVersion string) error {
	return run(ctx, cfg, result, gestaltdVersion, nil)
}

// RunWithReady is like Run but invokes onReady after all configured public
// listeners have been bound and the HTTP serving goroutines have started.
func RunWithReady(ctx context.Context, cfg *config.Config, result *bootstrap.Result, gestaltdVersion string, onReady func()) error {
	return run(ctx, cfg, result, gestaltdVersion, onReady)
}

func run(ctx context.Context, cfg *config.Config, result *bootstrap.Result, gestaltdVersion string, onReady func()) error {
	httpInvoker := invocation.NewGuarded(result.Invoker, result.CapabilityLister, "http", result.AuditSink, invocation.WithoutRateLimit())
	mcpInvoker := invocation.NewGuarded(result.Invoker, result.CapabilityLister, "mcp", result.AuditSink, invocation.WithoutRateLimit())

	connMaps, err := bootstrap.BuildConnectionMaps(cfg)
	if err != nil {
		return err
	}
	restartDelay, disableRestartDelay, err := appRegistryRestartDelay(cfg)
	if err != nil {
		return err
	}

	if cfg.Server.BaseURL != "" {
		slog.Debug("gestaltd base URL configured",
			"base_url", cfg.Server.BaseURL,
			"auth_callback", cfg.Server.BaseURL+config.AuthCallbackPath,
			"integration_callback", cfg.Server.BaseURL+config.IntegrationCallbackPath,
		)
	}

	publicBrandHref := "/admin/"
	for _, entry := range cfg.Apps {
		if entry != nil && entry.Static != nil && strings.TrimSpace(entry.Static.Mount) == "/" {
			publicBrandHref = "/"
			break
		}
	}

	managementAddr := cfg.Server.ManagementAddr()
	mcpSlot := &switchableHandler{}
	workflowProvidersReady := make(chan struct{})
	authorizationName, authorizationEntry, err := cfg.SelectedAuthorizationProvider()
	if err != nil {
		return err
	}
	var authorizationProvider core.AuthorizationProvider
	if authorizationEntry != nil {
		authorizationProvider = result.Authorization[authorizationName]
	}
	var publicIndexedDB indexeddb.IndexedDB
	if _, indexedDBEntry, err := cfg.SelectedIndexedDBProvider(); err == nil && config.EntryBuildsLocal(indexedDBEntry) && result.Services != nil {
		publicIndexedDB = result.Services.DB
	}
	appRuntimeState, _ := result.AppRestarter.(AppRuntimeState)
	var reverseRemote *reverseRemoteSetup
	reverseRemote, err = setupReverseRemoteUpstream(ctx, cfg, result.Services, authorizationProvider)
	if err != nil {
		return err
	}
	defer reverseRemote.shutdown(context.Background())
	heartbeatTTL, err := cfg.Server.AppRegistry.HeartbeatTTLDuration()
	if err != nil {
		return fmt.Errorf("resolve app registry heartbeat TTL: %w", err)
	}
	baseConfig := Config{
		Auth:                  result.Auth,
		SelectedAuthProvider:  result.SelectedAuthProvider,
		AuthProviders:         result.AuthProviders,
		Authorization:         authorizationProvider,
		ProviderKinds:         bootstrap.ProviderAuthorizationKinds(cfg),
		AuthorizationPolicies: bootstrap.ProviderAuthorizationPolicies(cfg),
		AuditSink:             result.AuditSink,
		Services:              result.Services,
		Providers:             result.Providers,
		Agent:                 result.AgentControl,
		AgentManager:          result.AgentManager,
		Workflow:              result.WorkflowControl,
		Runtimes:              result.Runtimes,
		Invoker:               httpInvoker,
		AppInvocation:         result.AppInvocation,
		DefaultConnection:     connMaps.DefaultConnection,
		// HTTP routes expose REST-visible operations via the API surface catalog map.
		// Dynamic session/MCP operation resolution uses MCPConnection below.
		CatalogConnection:      httpCatalogConnectionMap(connMaps),
		MCPConnection:          connMaps.MCPConnection,
		ConnectionAuth:         result.ConnectionAuth,
		ManualConnectionAuth:   result.ManualConnectionAuth,
		AppDefs:                cfg.Apps,
		PublicBaseURL:          cfg.Server.BaseURL,
		PublicGatewayTransport: result.PublicGatewayTransport,
		ManagementBaseURL:      cfg.Server.ManagementBaseURL(),
		SecureCookies:          strings.HasPrefix(cfg.Server.BaseURL, "https://"),
		StateSecret:            crypto.DeriveKey(cfg.Server.EncryptionKey),
		S3:                     result.S3,
		Readiness: ReadinessChecker(func() string {
			if reason := runtimeReadinessStatus(workflowProvidersReady, result.Services)(); reason != "" {
				return reason
			}
			return reverseRemote.readinessReason()
		}),
		PrometheusMetrics:    result.Telemetry.PrometheusHandler(),
		PublicHostServices:   result.PublicHostServices,
		ActivateAppProviders: result.ActivateAppProviders,
		IndexedDB:            publicIndexedDB,
		RemoteManagement:     reverseRemote.remoteManagement,
		FrpsHandler:          reverseRemote.frpsHandler,
		FrpsConnectHandler:   reverseRemote.frpsConnectHandler,
		TunnelResolver: TunnelResolverConfig{
			RemoteRegistrations: tunnelRemoteRegistrations(reverseRemote, result),
			ConnectAddr:         reverseRemote.connectAddr,
			ClientIdentity:      tunnelClientIdentity(reverseRemote),
		},
		Admin: AdminRouteConfig{
			AuthorizationPolicy: cfg.Server.Admin.AuthorizationPolicy,
			AllowedRoles:        append([]string(nil), cfg.Server.Admin.AllowedRoles...),
		},
		AppRegistries:           cfg.AppRegistries,
		AppRegistryHeartbeatTTL: heartbeatTTL,
		AppRegistryRolloutMode:  cfg.Server.AppRegistry.RolloutMode,
		ArtifactsDir:            cfg.Server.ArtifactsDir,
		GestaltdVersion:         strings.TrimSpace(gestaltdVersion),
		SourceVersion:           appregistry.ResolveSourceVersion(),
		AppRuntimeState:         appRuntimeState,
	}

	result.RegistryAppStartup = registryAppStartup(cfg, result, nil)
	if err := result.Start(ctx); err != nil {
		return err
	}
	autoDeployController, err := startAppRegistryAutoDeployController(ctx, cfg, result, gestaltdVersion)
	if err != nil {
		return err
	}
	if autoDeployController != nil {
		defer autoDeployController.Stop()
	}
	var onRolloutTerminal func(string)
	if autoDeployController != nil {
		onRolloutTerminal = autoDeployController.Notify
		baseConfig.AppAutoDeployNotify = autoDeployController.Notify
	}
	catalogPoller := startAppRegistryCatalogPoller(ctx, cfg, result, restartDelay, disableRestartDelay, onRolloutTerminal)
	if catalogPoller != nil {
		defer catalogPoller.Stop()
	}
	heartbeatWriter, err := startAppRegistryHeartbeatWriter(ctx, cfg, result)
	if err != nil {
		return err
	}
	if heartbeatWriter != nil {
		defer heartbeatWriter.Stop()
	}
	recoveryObserver, err := startAppRegistryRecoveryObserver(ctx, cfg, result)
	if err != nil {
		return err
	}
	if recoveryObserver != nil {
		defer recoveryObserver.Stop()
	}

	publicConfig := baseConfig
	if managementAddr == "" {
		publicConfig.RouteProfile = RouteProfileAll
	} else {
		publicConfig.RouteProfile = RouteProfilePublic
	}
	publicConfig.MCPHandler = mcpSlot

	devSupervisor := result.DevSupervisor
	if devSupervisor != nil {
		publicConfig.DevHandlerResolver = devSupervisor.DevHandler
	}

	publicConfig.BuiltinAdminUI = &BuiltinAdminUIOptions{
		BrandHref: publicBrandHref,
		LoginBase: browserLoginPath,
	}

	publicHandler, err := New(publicConfig)
	if err != nil {
		if devSupervisor != nil {
			devSupervisor.Stop()
		}
		return fmt.Errorf("creating public server: %w", err)
	}

	servers := []namedHTTPServer{{
		name:   "public",
		server: newHTTPServer(cfg.Server.PublicAddr(), publicHandler),
	}}

	if managementAddr != "" {
		if cfg.Server.Admin.AuthorizationPolicy != "" {
			slog.Warn(
				"management listener serves /metrics without Gestalt auth; /admin requires Gestalt session auth and server.admin policy access",
			)
		} else {
			slog.Warn(
				"management listener serves /admin and /metrics without Gestalt auth; protect server.management with private networking or an internal reverse proxy",
			)
		}
		slog.Debug("management listener address", "addr", managementAddr)

		managementConfig := baseConfig
		managementConfig.RouteProfile = RouteProfileManagement
		managementConfig.DevHandlerResolver = publicConfig.DevHandlerResolver
		managementLoginBase := browserLoginPath
		if baseURL := strings.TrimRight(cfg.Server.BaseURL, "/"); baseURL != "" {
			managementLoginBase = baseURL + browserLoginPath
		}
		managementConfig.BuiltinAdminUI = &BuiltinAdminUIOptions{
			BrandHref: "/admin/",
			LoginBase: managementLoginBase,
		}

		managementHandler, err := New(managementConfig)
		if err != nil {
			if devSupervisor != nil {
				devSupervisor.Stop()
			}
			return fmt.Errorf("creating management server: %w", err)
		}
		servers = append(servers, namedHTTPServer{
			name:   "management",
			server: newHTTPServer(managementAddr, managementHandler),
		})
	}

	return serveRuntime(ctx, cfg, connMaps, result, mcpInvoker, servers, mcpSlot, workflowProvidersReady, devSupervisor, onReady, reverseRemote)
}

func registryAppStartup(cfg *config.Config, result *bootstrap.Result, reader *appregistry.RegistryReader) func(context.Context) {
	return func(ctx context.Context) {
		if cfg == nil || result == nil || result.Services == nil ||
			result.Services.AppVersionChangeRequests == nil || result.AppRestarter == nil {
			return
		}
		materializer := &appregistry.Materializer{
			Registries:   cfg.AppRegistries,
			ArtifactsDir: cfg.Server.ArtifactsDir,
			Reader:       reader,
		}
		names := make([]string, 0, len(cfg.Apps))
		for name := range cfg.Apps {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, appName := range names {
			entry := cfg.Apps[appName]
			if entry == nil || !entry.Source.IsRegistry() || !config.EntryBuildsLocal(entry) {
				continue
			}
			known, err := result.Services.AppVersionChangeRequests.ListKnownVersionsByApp(ctx, appName)
			if err != nil {
				slog.Warn("registry app bootstrap could not list known versions", "app", appName, "error", err)
				continue
			}
			version := coredata.LatestKnownVersion(known)
			if version == "" {
				if err := result.AppRestarter.StopApp(ctx, appName); err != nil {
					slog.Warn("registry app bootstrap could not clear stopped app", "app", appName, "error", err)
				} else if err := materializer.PruneSuperseded(appName, ""); err != nil {
					slog.Warn("registry app bootstrap could not remove stale packages", "app", appName, "error", err)
				}
				result.AppRestarter.AbortRestarts()
				continue
			}
			var desired *core.AppInstallation
			for _, installation := range known {
				if installation != nil && strings.TrimSpace(installation.Version) == version {
					desired = installation
					break
				}
			}
			if desired == nil || strings.TrimSpace(desired.Registry) != strings.TrimSpace(entry.Source.Registry) {
				slog.Warn("registry app bootstrap rejected catalog registry mismatch", "app", appName, "version", version)
				continue
			}
			if _, err := materializer.Ensure(ctx, desired); err != nil {
				slog.Warn("registry app bootstrap could not materialize app", "app", appName, "version", version, "error", err)
				continue
			}
			if err := result.AppRestarter.StartApp(ctx, appName, version); err != nil {
				slog.Warn("registry app bootstrap could not start app", "app", appName, "version", version, "error", err)
				continue
			}
			if err := materializer.PruneSuperseded(appName, version); err != nil {
				slog.Warn("registry app bootstrap could not remove superseded packages", "app", appName, "version", version, "error", err)
			}
		}
	}
}

type indexedDBPinger interface {
	Ping(context.Context) error
}

func runtimeReadinessStatus(workflowProvidersReady <-chan struct{}, services indexedDBPinger) ReadinessChecker {
	return func() string {
		select {
		case <-workflowProvidersReady:
		default:
			return "workflow providers loading"
		}

		if services == nil {
			return "indexeddb unavailable"
		}
		pingCtx, cancel := context.WithTimeout(context.Background(), readinessIndexedDBPingTimeout)
		defer cancel()
		if err := services.Ping(pingCtx); err != nil {
			return "indexeddb unavailable"
		}
		return ""
	}
}

func serveRuntime(ctx context.Context, cfg *config.Config, connMaps bootstrap.ConnectionMaps, result *bootstrap.Result, mcpInvoker invocation.Invoker, servers []namedHTTPServer, mcpSlot *switchableHandler, workflowProvidersReady chan<- struct{}, devSupervisor *providerdev.Supervisor, readyCallback func(), reverseRemote *reverseRemoteSetup) error {
	if devSupervisor != nil {
		defer devSupervisor.Stop()
	}

	type boundServer struct {
		name     string
		server   *http.Server
		listener net.Listener
	}
	bound := make([]boundServer, 0, len(servers))
	for _, entry := range servers {
		ln, err := net.Listen("tcp", entry.server.Addr)
		if err != nil {
			for _, prev := range bound {
				_ = prev.listener.Close()
			}
			return fmt.Errorf("%s http server: %w", entry.name, err)
		}
		bound = append(bound, boundServer{name: entry.name, server: entry.server, listener: ln})
	}

	listenErr := make(chan namedListenFailure, len(bound))
	for _, entry := range bound {
		entry := entry
		go func() {
			slog.Debug("gestaltd listening", "listener", entry.name, "addr", entry.listener.Addr())
			if err := entry.server.Serve(entry.listener); err != nil && err != http.ErrServerClosed {
				listenErr <- namedListenFailure{name: entry.name, err: err}
			}
		}()
	}
	if readyCallback != nil {
		readyCallback()
	}

	defer func() {
		drainCtx, drainCancel := context.WithTimeout(context.Background(), runtimeShutdownTimeout)
		defer drainCancel()
		for _, entry := range bound {
			if err := entry.server.Shutdown(drainCtx); err != nil {
				slog.Warn("server shutdown", "listener", entry.name, "error", err)
			}
			if closer, ok := entry.server.Handler.(interface{ Close() }); ok {
				closer.Close()
			}
		}
	}()

	workflowErr := make(chan error, 1)
	go func() {
		if err := result.StartAppProviders(ctx); err != nil {
			if ctx.Err() == nil {
				workflowErr <- err
			}
			return
		}
		if err := result.StartWorkflowProviders(ctx); err != nil {
			workflowErr <- err
			return
		}
		close(workflowProvidersReady)

		result.StartWorkflowConfigReconciliation(ctx)
		slog.Debug("workflow providers ready", "count", len(result.ExtraWorkflows))

		mcpHandler, err := newMCPHandler(cfg, connMaps, result, mcpInvoker)
		if err != nil {
			workflowErr <- err
			return
		}
		mcpSlot.Set(mcpHandler)
		slog.Debug("MCP endpoint enabled", "path", "/mcp")

		select {
		case <-result.ProvidersReady:
			slog.DebugContext(ctx, "all deferred app providers ready")
		case <-ctx.Done():
		}

		if ctx.Err() == nil && cfg.Server.Dev {
			var devHandlerResolver func(string) http.Handler
			if devSupervisor != nil {
				devHandlerResolver = devSupervisor.DevHandler
			}
			if err := startReversePublisher(ctx, result.Providers, reverseRemote, devHandlerResolver, slog.Default()); err != nil {
				workflowErr <- err
				return
			}
		}
	}()

	select {
	case failure := <-listenErr:
		return fmt.Errorf("%s http server: %v", failure.name, failure.err)
	case err := <-workflowErr:
		return err
	case err := <-result.FatalAppProviderState:
		return fmt.Errorf("unrecoverable app provider state: %w", err)
	case <-ctx.Done():
		return nil
	}
}

func newMCPHandler(cfg *config.Config, connMaps bootstrap.ConnectionMaps, result *bootstrap.Result, invoker invocation.Invoker) (http.Handler, error) {
	broker, ok := result.Invoker.(*invocation.Broker)
	if !ok {
		return nil, fmt.Errorf("MCP token resolution requires *invocation.Broker as invoker")
	}
	projectionServer := &Server{pluginDefs: cfg.Apps}

	names := make([]string, 0, len(cfg.Apps))
	for name := range cfg.Apps {
		names = append(names, name)
	}
	slices.Sort(names)

	allowedProviders := make([]string, 0, len(names))
	toolPrefixes := make(map[string]string)
	includeREST := make(map[string]bool)
	mcpConnection := make(map[string]string)
	for _, name := range names {
		entry := cfg.Apps[name]
		if entry == nil || !entry.ExposesMCP() {
			continue
		}
		allowedProviders = append(allowedProviders, name)
		includeREST[name] = entry.IncludeRESTInMCP()
		mcpConnection[name] = connMaps.MCPConnection[name]
		if entry.MCPToolPrefix != "" {
			toolPrefixes[name] = entry.MCPToolPrefix
			continue
		}
		if entry.ResolvedManifest != nil {
			if src, err := source.Parse(strings.TrimSpace(entry.ResolvedManifest.Source)); err == nil {
				toolPrefixes[name] = src.AppName() + "_"
			}
		}
	}

	return gestaltmcp.NewStatelessHTTPHandler(gestaltmcp.Config{
		Invoker:           invoker,
		TokenResolver:     broker,
		AuditSink:         result.AuditSink,
		Providers:         result.Providers,
		AllowedProviders:  allowedProviders,
		ToolPrefixes:      toolPrefixes,
		IncludeREST:       includeREST,
		MCPConnection:     mcpConnection,
		CatalogProjection: projectionServer.publicCatalog,
		InvocationValidator: func(ctx context.Context, provName string, prov core.Provider, op catalog.CatalogOperation, params map[string]any, explicitConnection string) error {
			return projectionServer.validatePublicOperationInvocation(provName, prov, op, params, explicitConnection)
		},
	}), nil
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	protocols := &http.Protocols{}
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

type namedHTTPServer struct {
	name   string
	server *http.Server
}

type namedListenFailure struct {
	name string
	err  error
}

type switchableHandler struct {
	mu      sync.RWMutex
	handler http.Handler
}

func (h *switchableHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	inner := h.handler
	h.mu.RUnlock()
	if inner == nil {
		http.Error(w, "service starting", http.StatusServiceUnavailable)
		return
	}
	inner.ServeHTTP(w, r)
}

func (h *switchableHandler) Set(handler http.Handler) {
	h.mu.Lock()
	h.handler = handler
	h.mu.Unlock()
}

func startAppRegistryCatalogPoller(
	ctx context.Context,
	cfg *config.Config,
	result *bootstrap.Result,
	restartDelay time.Duration,
	disableRestartDelay bool,
	onRolloutTerminal func(string),
) *appregistry.CatalogPoller {
	if result == nil || result.Services == nil {
		return nil
	}
	changeRequests := result.Services.AppVersionChangeRequests
	materializations := result.Services.AppInstanceMaterializations
	rollouts := result.Services.AppRollouts
	if changeRequests == nil || materializations == nil || rollouts == nil {
		return nil
	}
	var materializer *appregistry.Materializer
	if cfg != nil && len(cfg.AppRegistries) > 0 {
		artifactsDir := strings.TrimSpace(cfg.Server.ArtifactsDir)
		if artifactsDir != "" {
			materializer = &appregistry.Materializer{
				Registries:   cfg.AppRegistries,
				ArtifactsDir: artifactsDir,
			}
		}
	}
	heartbeatInterval, err := cfg.Server.AppRegistry.HeartbeatIntervalDuration()
	if err != nil {
		heartbeatInterval = config.DefaultAppRegistryHeartbeatInterval
	}
	heartbeatTTL, err := cfg.Server.AppRegistry.HeartbeatTTLDuration()
	if err != nil {
		heartbeatTTL = config.DefaultAppRegistryHeartbeatTTL
	}
	stabilityWindow, err := cfg.Server.AppRegistry.HealthyStabilityWindowDuration()
	if err != nil {
		stabilityWindow = config.DefaultAppRegistryHealthyStabilityWindow
	}
	poller := appregistry.NewCatalogPoller(appregistry.CatalogPollerConfig{
		ChangeRequests:              changeRequests,
		Materializations:            materializations,
		Rollouts:                    rollouts,
		RolloutOutcomes:             result.Services.AppVersionRolloutOutcomes,
		Heartbeats:                  result.Services.GestaltdInstanceHeartbeats,
		AppMaterializer:             materializer,
		AppRestarter:                result.AppRestarter,
		InstanceID:                  appregistry.ResolveInstanceID(),
		SourceVersion:               appregistry.ResolveSourceVersion(),
		HeartbeatEvaluationInterval: heartbeatInterval,
		HeartbeatTTL:                heartbeatTTL,
		HealthyStabilityWindow:      stabilityWindow,
		RestartDelay:                restartDelay,
		DisableRestartDelay:         disableRestartDelay,
		RestartReady:                result.AppProvidersInitialized,
		BootstrapReady:              result.AppProvidersInitialized,
		MaxReconcileAttempts:        cfg.Server.AppRegistry.MaxReconcileAttempts,
		OnRolloutTerminal:           onRolloutTerminal,
	})
	poller.Start(ctx)
	return poller
}

func startAppRegistryHeartbeatWriter(
	ctx context.Context,
	cfg *config.Config,
	result *bootstrap.Result,
) (*appregistry.HeartbeatWriter, error) {
	if cfg == nil || result == nil || result.Services == nil ||
		result.Services.GestaltdInstanceHeartbeats == nil ||
		result.Services.AppVersionChangeRequests == nil ||
		result.AppRuntimeSnapshotter == nil {
		return nil, nil
	}
	sourceVersion := appregistry.ResolveSourceVersion()
	if sourceVersion == "" {
		return nil, nil
	}
	interval, err := cfg.Server.AppRegistry.HeartbeatIntervalDuration()
	if err != nil {
		return nil, fmt.Errorf("server.appRegistry.heartbeatInterval: %w", err)
	}
	retention, err := cfg.Server.AppRegistry.HeartbeatRetentionDuration()
	if err != nil {
		return nil, fmt.Errorf("server.appRegistry.heartbeatRetention: %w", err)
	}
	writer := appregistry.NewHeartbeatWriter(appregistry.HeartbeatWriterConfig{
		Heartbeats:     result.Services.GestaltdInstanceHeartbeats,
		ChangeRequests: result.Services.AppVersionChangeRequests,
		ConfiguredApps: cfg.Apps,
		Runtime:        result.AppRuntimeSnapshotter,
		InstanceID:     appregistry.ResolveInstanceID(),
		SourceVersion:  sourceVersion,
		Ready:          result.AppProvidersInitialized,
		Interval:       interval,
		Retention:      retention,
	})
	writer.Start(ctx)
	return writer, nil
}

func startAppRegistryRecoveryObserver(
	ctx context.Context,
	cfg *config.Config,
	result *bootstrap.Result,
) (*appregistry.RecoveryObserver, error) {
	if cfg == nil || result == nil || result.Services == nil {
		return nil, nil
	}
	services := result.Services
	if services.AppVersionChangeRequests == nil ||
		services.AppVersionRolloutOutcomes == nil ||
		services.AppVersionRecoveryObservations == nil ||
		services.GestaltdSourceVersionState == nil ||
		services.GestaltdInstanceHeartbeats == nil {
		return nil, nil
	}
	interval, err := cfg.Server.AppRegistry.HeartbeatIntervalDuration()
	if err != nil {
		return nil, fmt.Errorf("server.appRegistry.heartbeatInterval: %w", err)
	}
	ttl, err := cfg.Server.AppRegistry.HeartbeatTTLDuration()
	if err != nil {
		return nil, fmt.Errorf("server.appRegistry.heartbeatTtl: %w", err)
	}
	stabilityWindow, err := cfg.Server.AppRegistry.HealthyStabilityWindowDuration()
	if err != nil {
		return nil, fmt.Errorf("server.appRegistry.healthyStabilityWindow: %w", err)
	}
	observer := appregistry.NewRecoveryObserver(appregistry.RecoveryObserverConfig{
		ChangeRequests:  services.AppVersionChangeRequests,
		Outcomes:        services.AppVersionRolloutOutcomes,
		Observations:    services.AppVersionRecoveryObservations,
		SourceVersions:  services.GestaltdSourceVersionState,
		Heartbeats:      services.GestaltdInstanceHeartbeats,
		HeartbeatTTL:    ttl,
		StabilityWindow: stabilityWindow,
		Interval:        interval,
		Ready:           result.AppProvidersInitialized,
	})
	observer.Start(ctx)
	return observer, nil
}

func startAppRegistryAutoDeployController(
	ctx context.Context,
	cfg *config.Config,
	result *bootstrap.Result,
	gestaltdVersion string,
) (*autodeploy.Controller, error) {
	if cfg == nil || result == nil || result.Services == nil {
		return nil, nil
	}
	services := result.Services
	if services.AutoDeploySettings == nil || services.AppRollouts == nil ||
		services.AppVersionChangeRequests == nil || services.AppVersionInstallLocks == nil {
		return nil, nil
	}
	apps := make(map[string]autodeploy.AppConfig)
	for name, entry := range cfg.Apps {
		if entry == nil || !entry.Source.IsRegistry() {
			continue
		}
		registryName := strings.TrimSpace(entry.Source.Registry)
		registry, ok := cfg.AppRegistries[registryName]
		if !ok || strings.TrimSpace(registry.Kind) != config.AppRegistryKindGCS {
			continue
		}
		publicRoot, err := registry.PublicURL()
		if err != nil {
			return nil, fmt.Errorf("configure app registry auto-deploy for %s: %w", name, err)
		}
		apps[name] = autodeploy.AppConfig{
			Registry:   registryName,
			PublicRoot: publicRoot,
		}
	}
	if len(apps) == 0 {
		return nil, nil
	}
	interval, err := cfg.Server.AppRegistry.AutoDeployPollIntervalDuration()
	if err != nil {
		return nil, fmt.Errorf("server.appRegistry.autoDeployPollInterval: %w", err)
	}
	reader := &appregistry.RegistryReader{}
	installer := &appregistry.Installer{
		Registries:       cfg.AppRegistries,
		ConfigApps:       cfg.Apps,
		Reader:           reader,
		ChangeRequests:   services.AppVersionChangeRequests,
		Locks:            services.AppVersionInstallLocks,
		SourceVersions:   services.GestaltdSourceVersionState,
		Rollouts:         services.AppRollouts,
		RetentionCatalog: appregistry.NewGCSCatalogStore(cfg.AppRegistries),
		GestaltdVersion:  strings.TrimSpace(gestaltdVersion),
		SourceVersion:    appregistry.ResolveSourceVersion(),
		RolloutMode:      core.AppRolloutMode(cfg.Server.AppRegistry.RolloutMode),
	}
	controller := autodeploy.New(
		services.AutoDeploySettings,
		services.AppRollouts,
		services.AppVersionChangeRequests,
		reader,
		installer,
		apps,
		interval,
	)
	controller.Start(ctx)
	return controller, nil
}

func appRegistryRestartDelay(cfg *config.Config) (time.Duration, bool, error) {
	if cfg == nil {
		return 0, true, nil
	}
	raw := strings.TrimSpace(cfg.Server.AppRegistry.RestartDelay)
	if raw == "" {
		return 0, true, nil
	}
	delay, err := config.ParseDuration(raw)
	if err != nil {
		return 0, false, fmt.Errorf("server.appRegistry.restartDelay: %w", err)
	}
	return delay, false, nil
}

// tunnelRemoteRegistrations returns the RemoteRegistrationService when the
// reverse remote setup has one (dev mode with coredata), or nil otherwise.
func tunnelRemoteRegistrations(setup *reverseRemoteSetup, result *bootstrap.Result) *coredata.RemoteRegistrationService {
	if setup == nil || setup.frps == nil || result == nil || result.Services == nil {
		return nil
	}
	return result.Services.RemoteRegistrations
}

// tunnelClientIdentity returns the TLS certificate from the reverse remote
// setup's identity, or a zero certificate if not configured.
func tunnelClientIdentity(setup *reverseRemoteSetup) tls.Certificate {
	if setup == nil || setup.clientIdentity == nil {
		return tls.Certificate{}
	}
	return setup.clientIdentity.Certificate
}

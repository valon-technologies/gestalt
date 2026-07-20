package server

import (
	"context"
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
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
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

func Run(ctx context.Context, cfg *config.Config, result *bootstrap.Result) error {
	return run(ctx, cfg, result, nil)
}

// RunWithReady is like Run but invokes onReady after all configured public
// listeners have been bound and the HTTP serving goroutines have started.
func RunWithReady(ctx context.Context, cfg *config.Config, result *bootstrap.Result, onReady func()) error {
	return run(ctx, cfg, result, onReady)
}

func run(ctx context.Context, cfg *config.Config, result *bootstrap.Result, onReady func()) error {
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
	if strings.TrimSpace(cfg.Server.Remote) == "" && result.Services != nil {
		publicIndexedDB = result.Services.DB
	}
	baseConfig := Config{
		Auth:                 result.Auth,
		SelectedAuthProvider: result.SelectedAuthProvider,
		AuthProviders:        result.AuthProviders,
		Authorization:        authorizationProvider,
		ProviderKinds:        bootstrap.ProviderAuthorizationKinds(cfg),
		AuditSink:            result.AuditSink,
		Services:             result.Services,
		Providers:            result.Providers,
		Agent:                result.AgentControl,
		AgentManager:         result.AgentManager,
		Workflow:             result.WorkflowControl,
		Runtimes:             result.Runtimes,
		Invoker:              httpInvoker,
		AppInvocation:        result.AppInvocation,
		DefaultConnection:    connMaps.DefaultConnection,
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
		Readiness:              runtimeReadinessStatus(workflowProvidersReady, result.Services),
		PrometheusMetrics:      result.Telemetry.PrometheusHandler(),
		PublicHostServices:     result.PublicHostServices,
		ActivateAppProviders:   result.ActivateAppProviders,
		IndexedDB:              publicIndexedDB,
		Admin: AdminRouteConfig{
			AuthorizationPolicy: cfg.Server.Admin.AuthorizationPolicy,
			AllowedRoles:        append([]string(nil), cfg.Server.Admin.AllowedRoles...),
		},
		AppRegistries: cfg.AppRegistries,
		ArtifactsDir:  cfg.Server.ArtifactsDir,
	}

	if err := result.Start(ctx); err != nil {
		return err
	}
	catalogPoller := startAppRegistryCatalogPoller(ctx, result, restartDelay, disableRestartDelay)
	if catalogPoller != nil {
		defer catalogPoller.Stop()
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

	return serveRuntime(ctx, cfg, connMaps, result, mcpInvoker, servers, mcpSlot, workflowProvidersReady, devSupervisor, onReady)
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

func serveRuntime(ctx context.Context, cfg *config.Config, connMaps bootstrap.ConnectionMaps, result *bootstrap.Result, mcpInvoker invocation.Invoker, servers []namedHTTPServer, mcpSlot *switchableHandler, workflowProvidersReady chan<- struct{}, devSupervisor *providerdev.Supervisor, readyCallback func()) error {
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
	}()

	select {
	case failure := <-listenErr:
		return fmt.Errorf("%s http server: %v", failure.name, failure.err)
	case err := <-workflowErr:
		return err
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

func startAppRegistryCatalogPoller(ctx context.Context, result *bootstrap.Result, restartDelay time.Duration, disableRestartDelay bool) *appregistry.CatalogPoller {
	if result == nil || result.Services == nil {
		return nil
	}
	changeRequests := result.Services.AppVersionChangeRequests
	materializations := result.Services.AppInstanceMaterializations
	rollouts := result.Services.AppRollouts
	if changeRequests == nil || materializations == nil || rollouts == nil {
		return nil
	}
	poller := appregistry.NewCatalogPoller(appregistry.CatalogPollerConfig{
		ChangeRequests:      changeRequests,
		Materializations:    materializations,
		Rollouts:            rollouts,
		AppMaterializer:     result.RegistryMaterializer,
		AppRestarter:        result.AppRestarter,
		InstanceID:          appregistry.ResolveInstanceID(),
		RestartDelay:        restartDelay,
		DisableRestartDelay: disableRestartDelay,
		RestartReady:        result.StartupProvidersReady,
	})
	poller.Start(ctx)
	return poller
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

package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/server/services/plugins/mcp"
	"github.com/valon-technologies/gestalt/server/services/plugins/source"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

const (
	runtimeShutdownTimeout        = 15 * time.Second
	readinessDatastorePingTimeout = 2 * time.Second
)

func httpCatalogConnectionMap(connMaps bootstrap.ConnectionMaps) map[string]string {
	return connMaps.APIConnection
}

func Run(ctx context.Context, cfg *config.Config, result *bootstrap.Result) error {
	httpInvoker := invocation.NewGuarded(result.Invoker, result.CapabilityLister, "http", result.AuditSink, invocation.WithoutRateLimit())
	mcpInvoker := invocation.NewGuarded(result.Invoker, result.CapabilityLister, "mcp", result.AuditSink, invocation.WithoutRateLimit())

	connMaps, err := bootstrap.BuildConnectionMaps(cfg)
	if err != nil {
		return err
	}

	if cfg.Server.BaseURL != "" {
		slog.Info("gestaltd base URL configured",
			"base_url", cfg.Server.BaseURL,
			"auth_callback", cfg.Server.BaseURL+config.AuthCallbackPath,
			"integration_callback", cfg.Server.BaseURL+config.IntegrationCallbackPath,
		)
	}

	apiTokenTTL := time.Duration(0)
	if cfg.Server.APITokenTTL != "" {
		apiTokenTTL, err = config.ParseDuration(cfg.Server.APITokenTTL)
		if err != nil {
			return fmt.Errorf("parsing server.api_token_ttl: %w", err)
		}
	}

	publicBrandHref := "/admin/"
	for _, entry := range cfg.Providers.UI {
		if entry != nil && entry.Path == "/" {
			publicBrandHref = "/"
			break
		}
	}

	managementAddr := cfg.Server.ManagementAddr()
	mcpSlot := &switchableHandler{}
	workflowProvidersReady := make(chan struct{})
	baseConfig := Config{
		Auth:                 result.Auth,
		SelectedAuthProvider: result.SelectedAuthProvider,
		AuthProviders:        result.AuthProviders,
		AuditSink:            result.AuditSink,
		Services:             result.Services,
		Providers:            result.Providers,
		Agent:                result.AgentControl,
		AgentManager:         result.AgentManager,
		Workflow:             result.WorkflowControl,
		PluginRuntimes:       result.PluginRuntimes,
		Invoker:              httpInvoker,
		PluginInvoker:        result.PluginInvoker,
		DefaultConnection:    connMaps.DefaultConnection,
		// HTTP routes expose REST-visible operations, so unqualified session-catalog
		// resolution should follow the API surface by default. The MCP server keeps
		// its own MCP-specific routing below.
		CatalogConnection:     httpCatalogConnectionMap(connMaps),
		ConnectionAuth:        result.ConnectionAuth,
		ManualConnectionAuth:  result.ManualConnectionAuth,
		PluginDefs:            cfg.Plugins,
		AgentDefs:             cfg.Providers.Agent,
		Authorizer:            result.Authorizer,
		AuthorizationProvider: result.AuthorizationProvider,
		PublicBaseURL:         cfg.Server.BaseURL,
		ManagementBaseURL:     cfg.Server.ManagementBaseURL(),
		SecureCookies:         strings.HasPrefix(cfg.Server.BaseURL, "https://"),
		StateSecret:           crypto.DeriveKey(cfg.Server.EncryptionKey),
		S3:                    result.S3,
		APITokenTTL:           apiTokenTTL,
		Readiness:             runtimeReadinessStatus(result.ProvidersReady, workflowProvidersReady, result.Services),
		PrometheusMetrics:     result.Telemetry.PrometheusHandler(),
		ProviderDevSessions:   result.ProviderDevSessions,
		ProviderDevAttach:     cfg.Server.Dev.AttachmentState != "",
		PublicHostServices:    result.PublicHostServices,
		Admin: AdminRouteConfig{
			AuthorizationPolicy: cfg.Server.Admin.AuthorizationPolicy,
			AllowedRoles:        append([]string(nil), cfg.Server.Admin.AllowedRoles...),
		},
		AdminUIProvider: strings.TrimSpace(cfg.Server.Admin.UI),
	}

	if err := result.Start(ctx); err != nil {
		return err
	}

	publicConfig := baseConfig
	if managementAddr == "" {
		publicConfig.RouteProfile = RouteProfileAll
	} else {
		publicConfig.RouteProfile = RouteProfilePublic
	}
	publicConfig.MCPHandler = mcpSlot
	publicConfig.ProviderUIs = cfg.Providers.UI
	publicConfig.BuiltinAdminUI = &BuiltinAdminUIOptions{
		BrandHref: publicBrandHref,
		LoginBase: browserLoginPath,
	}

	publicHandler, err := New(publicConfig)
	if err != nil {
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
				"addr", managementAddr,
			)
		} else {
			slog.Warn(
				"management listener serves /admin and /metrics without Gestalt auth; protect server.management with private networking or an internal reverse proxy",
				"addr", managementAddr,
			)
		}

		managementConfig := baseConfig
		managementConfig.RouteProfile = RouteProfileManagement
		managementConfig.ProviderUIs = cfg.Providers.UI
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
			return fmt.Errorf("creating management server: %w", err)
		}
		servers = append(servers, namedHTTPServer{
			name:   "management",
			server: newHTTPServer(managementAddr, managementHandler),
		})
	}

	return serveRuntime(ctx, cfg, connMaps, result, mcpInvoker, servers, mcpSlot, workflowProvidersReady)
}

type datastorePinger interface {
	Ping(context.Context) error
}

func runtimeReadinessStatus(providersReady, workflowProvidersReady <-chan struct{}, services datastorePinger) ReadinessChecker {
	return func() string {
		select {
		case <-providersReady:
		default:
			return "providers loading"
		}

		select {
		case <-workflowProvidersReady:
		default:
			return "workflow providers loading"
		}

		if services == nil {
			return "datastore unavailable"
		}
		pingCtx, cancel := context.WithTimeout(context.Background(), readinessDatastorePingTimeout)
		defer cancel()
		if err := services.Ping(pingCtx); err != nil {
			return "datastore unavailable"
		}
		return ""
	}
}

func serveRuntime(ctx context.Context, cfg *config.Config, connMaps bootstrap.ConnectionMaps, result *bootstrap.Result, mcpInvoker invocation.Invoker, servers []namedHTTPServer, mcpSlot *switchableHandler, workflowProvidersReady chan<- struct{}) error {
	listenErr := make(chan namedListenFailure, len(servers))
	for _, entry := range servers {
		entry := entry
		go func() {
			slog.Info("gestaltd listening", "listener", entry.name, "addr", entry.server.Addr)
			if err := entry.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				listenErr <- namedListenFailure{name: entry.name, err: err}
			}
		}()
	}

	defer func() {
		drainCtx, drainCancel := context.WithTimeout(context.Background(), runtimeShutdownTimeout)
		defer drainCancel()
		for _, entry := range servers {
			if err := entry.server.Shutdown(drainCtx); err != nil {
				slog.Warn("server shutdown", "listener", entry.name, "error", err)
			}
		}
	}()

	select {
	case <-result.ProvidersReady:
		slog.Info("all providers ready", "count", len(result.Providers.List()))
	case failure := <-listenErr:
		return fmt.Errorf("%s http server: %v", failure.name, failure.err)
	case <-ctx.Done():
		return nil
	}

	if err := result.StartWorkflowProviders(ctx); err != nil {
		return err
	}
	close(workflowProvidersReady)
	result.StartWorkflowConfigReconciliation(ctx)
	slog.Info("workflow providers ready", "count", len(result.ExtraWorkflows))

	mcpHandler, err := newMCPHandler(cfg, connMaps, result, mcpInvoker)
	if err != nil {
		return err
	}
	mcpSlot.Set(mcpHandler)
	slog.Info("MCP endpoint enabled", "path", "/mcp")

	select {
	case failure := <-listenErr:
		return fmt.Errorf("%s http server: %v", failure.name, failure.err)
	case <-ctx.Done():
		return nil
	}
}

func newMCPHandler(cfg *config.Config, connMaps bootstrap.ConnectionMaps, result *bootstrap.Result, invoker invocation.Invoker) (http.Handler, error) {
	broker, ok := result.Invoker.(*invocation.Broker)
	if !ok {
		return nil, fmt.Errorf("MCP token resolution requires *invocation.Broker as invoker")
	}
	projectionServer := &Server{pluginDefs: cfg.Plugins}

	names := make([]string, 0, len(cfg.Plugins))
	for name := range cfg.Plugins {
		names = append(names, name)
	}
	slices.Sort(names)

	allowedProviders := make([]string, 0, len(names))
	toolPrefixes := make(map[string]string)
	includeREST := make(map[string]bool)
	mcpConnection := make(map[string]string)
	for _, name := range names {
		entry := cfg.Plugins[name]
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
				toolPrefixes[name] = src.PluginName() + "_"
			}
		}
	}

	return mcpserver.NewStreamableHTTPServer(
		gestaltmcp.NewServer(gestaltmcp.Config{
			Invoker:           invoker,
			TokenResolver:     broker,
			AuditSink:         result.AuditSink,
			Providers:         result.Providers,
			Authorizer:        result.Authorizer,
			AllowedProviders:  allowedProviders,
			ToolPrefixes:      toolPrefixes,
			IncludeREST:       includeREST,
			MCPConnection:     mcpConnection,
			CatalogProjection: projectionServer.publicMCPCatalog,
			InvocationValidator: func(ctx context.Context, provName string, prov core.Provider, op catalog.CatalogOperation, params map[string]any, explicitConnection string) error {
				return projectionServer.validatePublicOperationInvocation(provName, prov, op, params, explicitConnection)
			},
		}),
		mcpserver.WithStateLess(true),
	), nil
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h2c.NewHandler(handler, &http2.Server{}),
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

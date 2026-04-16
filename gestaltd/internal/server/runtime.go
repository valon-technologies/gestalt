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
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/server/internal/mcp"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
)

const runtimeShutdownTimeout = 15 * time.Second

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
	baseConfig := Config{
		Auth:              result.Auth,
		AuditSink:         result.AuditSink,
		Services:          result.Services,
		Providers:         result.Providers,
		Invoker:           httpInvoker,
		DefaultConnection: connMaps.DefaultConnection,
		CatalogConnection: connMaps.MCPConnection,
		ConnectionAuth:    result.ConnectionAuth,
		PluginDefs:        cfg.Plugins,
		Authorizer:        result.Authorizer,
		PublicBaseURL:     cfg.Server.BaseURL,
		ManagementBaseURL: cfg.Server.ManagementBaseURL(),
		SecureCookies:     strings.HasPrefix(cfg.Server.BaseURL, "https://"),
		StateSecret:       crypto.DeriveKey(cfg.Server.EncryptionKey),
		APITokenTTL:       apiTokenTTL,
		Readiness: func() string {
			select {
			case <-result.ProvidersReady:
			default:
				return "providers loading"
			}

			pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := result.Services.Ping(pingCtx); err != nil {
				return "datastore unavailable"
			}
			return ""
		},
		PrometheusMetrics: result.Telemetry.PrometheusHandler(),
		Admin: AdminRouteConfig{
			AuthorizationPolicy: cfg.Server.Admin.AuthorizationPolicy,
			AllowedRoles:        append([]string(nil), cfg.Server.Admin.AllowedRoles...),
		},
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

	return serveRuntime(ctx, cfg, connMaps, result, mcpInvoker, servers, mcpSlot)
}

func serveRuntime(ctx context.Context, cfg *config.Config, connMaps bootstrap.ConnectionMaps, result *bootstrap.Result, mcpInvoker invocation.Invoker, servers []namedHTTPServer, mcpSlot *switchableHandler) error {
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

	names := make([]string, 0, len(cfg.Plugins))
	for name := range cfg.Plugins {
		names = append(names, name)
	}
	slices.Sort(names)

	allowedProviders := make([]string, 0, len(names))
	toolPrefixes := make(map[string]string)
	mcpConnection := make(map[string]string)
	for _, name := range names {
		entry := cfg.Plugins[name]
		if entry == nil || !entry.DeclaresMCP() {
			continue
		}
		allowedProviders = append(allowedProviders, name)
		mcpConnection[name] = connMaps.MCPConnection[name]
		if entry.MCPToolPrefix != "" {
			toolPrefixes[name] = entry.MCPToolPrefix
			continue
		}
		if entry.HasManagedSource() {
			if src, err := pluginsource.Parse(entry.SourceRef()); err == nil {
				toolPrefixes[name] = src.PluginName() + "_"
			}
		}
	}

	return mcpserver.NewStreamableHTTPServer(
		gestaltmcp.NewServer(gestaltmcp.Config{
			Invoker:          invoker,
			TokenResolver:    broker,
			AuditSink:        result.AuditSink,
			Providers:        result.Providers,
			Authorizer:       result.Authorizer,
			AllowedProviders: allowedProviders,
			ToolPrefixes:     toolPrefixes,
			MCPConnection:    mcpConnection,
		}),
		mcpserver.WithStateLess(true),
	), nil
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
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

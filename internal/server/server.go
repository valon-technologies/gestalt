package server

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/registry"
)

// ReadinessChecker reports whether the server is ready to handle requests.
// Returning a non-empty string means not ready; the string is used as the
// status message in the /ready response.
type ReadinessChecker func() string

type Server struct {
	router     chi.Router
	auth       core.AuthProvider
	datastore  core.Datastore
	providers  *registry.PluginMap[core.Provider]
	runtimes   *registry.PluginMap[core.Runtime]
	bindings   *registry.PluginMap[core.Binding]
	resolver   *principal.Resolver
	invoker    invocation.Invoker
	devMode    bool
	stateCodec *integrationOAuthStateCodec
	now        func() time.Time
	readiness  ReadinessChecker
	mcpHandler http.Handler
	webUI      http.Handler
}

type Config struct {
	Auth        core.AuthProvider
	Datastore   core.Datastore
	Providers   *registry.PluginMap[core.Provider]
	Runtimes    *registry.PluginMap[core.Runtime]
	Bindings    *registry.PluginMap[core.Binding]
	Invoker     invocation.Invoker
	DevMode     bool
	StateSecret []byte
	Now         func() time.Time
	Readiness   ReadinessChecker
	MCPHandler  http.Handler
	WebUI       http.Handler
}

func New(cfg Config) (*Server, error) {
	if cfg.Invoker == nil {
		return nil, fmt.Errorf("invoker is required")
	}
	var stateCodec *integrationOAuthStateCodec
	if len(cfg.StateSecret) > 0 {
		codec, err := newIntegrationOAuthStateCodec(cfg.StateSecret)
		if err != nil {
			return nil, fmt.Errorf("init oauth state codec: %w", err)
		}
		stateCodec = codec
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	s := &Server{
		router:     chi.NewRouter(),
		auth:       cfg.Auth,
		datastore:  cfg.Datastore,
		providers:  cfg.Providers,
		runtimes:   cfg.Runtimes,
		bindings:   cfg.Bindings,
		resolver:   principal.NewResolver(cfg.Auth, cfg.Datastore),
		invoker:    cfg.Invoker,
		devMode:    cfg.DevMode,
		stateCodec: stateCodec,
		now:        now,
		readiness:  cfg.Readiness,
		mcpHandler: cfg.MCPHandler,
		webUI:      cfg.WebUI,
	}

	s.routes()
	return s, nil
}

func (s *Server) routes() {
	r := s.router
	r.Use(maxBodyMiddleware(1 << 20)) // 1 MB

	if s.devMode {
		r.Use(devCORS)
	}

	r.Get("/health", s.healthCheck)
	r.Get("/ready", s.readinessCheck)

	if s.mcpHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)
			r.Handle("/mcp", s.mcpHandler)
		})
	}

	if s.devMode {
		r.Post("/api/dev-login", s.devLogin)
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/auth/info", s.authInfo)
		r.Post("/auth/login", s.startLogin)
		r.Get("/auth/login/callback", s.loginCallback)
		r.Post("/auth/logout", s.logout)
		r.Get("/auth/callback", s.integrationOAuthCallback)

		if s.bindings != nil {
			for _, name := range s.bindings.List() {
				binding, err := s.bindings.Get(name)
				if err != nil {
					log.Printf("warning: skipping binding %q routes: %v", name, err)
					continue
				}
				for _, route := range binding.Routes() {
					r.Method(route.Method, "/bindings/"+name+route.Pattern, route.Handler)
				}
			}
		}

		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)

			r.Get("/integrations", s.listIntegrations)
			r.Delete("/integrations/{name}", s.disconnectIntegration)
			r.Get("/integrations/{name}/operations", s.listOperations)
			r.Get("/runtimes", s.listRuntimes)
			r.Get("/bindings", s.listBindings)

			r.Get("/{integration}/{operation}", s.executeOperation)
			r.Post("/{integration}/{operation}", s.executeOperation)

			r.Post("/auth/start-oauth", s.startIntegrationOAuth)
			r.Post("/auth/connect-manual", s.connectManual)

			r.Get("/connections/staged/{id}", s.getStagedConnection)
			r.Post("/connections/staged/{id}/select", s.selectStagedConnection)
			r.Delete("/connections/staged/{id}", s.cancelStagedConnection)

			r.Post("/tokens", s.createAPIToken)
			r.Get("/tokens", s.listAPITokens)
			r.Delete("/tokens/{id}", s.revokeAPIToken)
		})
	})

	if s.webUI != nil {
		r.NotFound(s.webUI.ServeHTTP)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

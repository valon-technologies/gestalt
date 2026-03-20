package server

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/invocation"
	"github.com/valon-technologies/toolshed/internal/principal"
	"github.com/valon-technologies/toolshed/internal/registry"
)

type Server struct {
	router     chi.Router
	auth       core.AuthProvider
	datastore  core.Datastore
	providers  *registry.PluginMap[core.Provider]
	runtimes   *registry.PluginMap[core.Runtime]
	bindings   *registry.PluginMap[core.Binding]
	resolver   *principal.Resolver
	broker     *invocation.Broker
	devMode    bool
	stateCodec *integrationOAuthStateCodec
	now        func() time.Time
	mcpHandler http.Handler
}

type Config struct {
	Auth        core.AuthProvider
	Datastore   core.Datastore
	Providers   *registry.PluginMap[core.Provider]
	Runtimes    *registry.PluginMap[core.Runtime]
	Bindings    *registry.PluginMap[core.Binding]
	Broker      *invocation.Broker
	DevMode     bool
	StateSecret []byte
	Now         func() time.Time
	MCPHandler  http.Handler
}

func New(cfg Config) (*Server, error) {
	if cfg.Broker == nil {
		return nil, fmt.Errorf("broker is required")
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
		broker:     cfg.Broker,
		devMode:    cfg.DevMode,
		stateCodec: stateCodec,
		now:        now,
		mcpHandler: cfg.MCPHandler,
	}

	s.routes()
	return s, nil
}

func (s *Server) routes() {
	r := s.router

	r.Get("/health", s.healthCheck)
	r.Get("/ready", s.readinessCheck)

	if s.mcpHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)
			r.Handle("/mcp", s.mcpHandler)
		})
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/auth/info", s.authInfo)
		r.Post("/auth/login", s.startLogin)
		r.Get("/auth/login/callback", s.loginCallback)
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
			r.Get("/integrations/{name}/operations", s.listOperations)
			r.Get("/runtimes", s.listRuntimes)
			r.Get("/bindings", s.listBindings)

			r.Get("/{integration}/{operation}", s.executeOperation)
			r.Post("/{integration}/{operation}", s.executeOperation)

			r.Post("/auth/start-oauth", s.startIntegrationOAuth)
			r.Post("/auth/connect-manual", s.connectManual)

			r.Post("/tokens", s.createAPIToken)
			r.Get("/tokens", s.listAPITokens)
			r.Delete("/tokens/{id}", s.revokeAPIToken)
		})
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

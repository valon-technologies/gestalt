package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/registry"
)

// Server is the REST API server that composes auth, datastore, and integration plugins.
type Server struct {
	router       chi.Router
	auth         core.AuthProvider
	datastore    core.Datastore
	integrations *registry.PluginMap[core.Integration]
	devMode      bool
}

// Config holds all dependencies required to construct a Server.
type Config struct {
	Auth         core.AuthProvider
	Datastore    core.Datastore
	Integrations *registry.PluginMap[core.Integration]
	DevMode      bool
}

// New creates a Server with all routes registered.
func New(cfg Config) *Server {
	s := &Server{
		router:       chi.NewRouter(),
		auth:         cfg.Auth,
		datastore:    cfg.Datastore,
		integrations: cfg.Integrations,
		devMode:      cfg.DevMode,
	}

	s.routes()
	return s
}

func (s *Server) routes() {
	r := s.router

	r.Get("/health", s.healthCheck)

	r.Route("/api/v1", func(r chi.Router) {
		// Public auth endpoints (no middleware).
		r.Post("/auth/login", s.startLogin)
		r.Get("/auth/login/callback", s.loginCallback)

		// Authenticated routes.
		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)

			r.Get("/integrations", s.listIntegrations)
			r.Get("/integrations/{name}/operations", s.listOperations)

			r.Get("/{integration}/{operation}", s.executeOperation)
			r.Post("/{integration}/{operation}", s.executeOperation)

			r.Post("/auth/start-oauth", s.startIntegrationOAuth)
			r.Get("/auth/callback", s.integrationOAuthCallback)

			r.Post("/tokens", s.createAPIToken)
			r.Get("/tokens", s.listAPITokens)
			r.Delete("/tokens/{id}", s.revokeAPIToken)
		})
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

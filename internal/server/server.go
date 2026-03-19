package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/broker"
	"github.com/valon-technologies/toolshed/internal/registry"
)

type Server struct {
	router     chi.Router
	auth       core.AuthProvider
	datastore  core.Datastore
	providers  *registry.PluginMap[core.Provider]
	broker     *broker.Broker
	devMode    bool
	stateCodec *integrationOAuthStateCodec
	now        func() time.Time
}

type Config struct {
	Auth        core.AuthProvider
	Datastore   core.Datastore
	Providers   *registry.PluginMap[core.Provider]
	DevMode     bool
	StateSecret []byte
	Now         func() time.Time
}

func New(cfg Config) (*Server, error) {
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
		broker:     broker.New(cfg.Providers, cfg.Datastore),
		devMode:    cfg.DevMode,
		stateCodec: stateCodec,
		now:        now,
	}

	s.routes()
	return s, nil
}

func (s *Server) routes() {
	r := s.router

	r.Get("/health", s.healthCheck)
	r.Get("/ready", s.readinessCheck)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/auth/info", s.authInfo)
		r.Post("/auth/login", s.startLogin)
		r.Get("/auth/login/callback", s.loginCallback)
		r.Get("/auth/callback", s.integrationOAuthCallback)

		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)

			r.Get("/integrations", s.listIntegrations)
			r.Get("/integrations/{name}/operations", s.listOperations)

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

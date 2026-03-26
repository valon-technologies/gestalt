package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"
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
	router         chi.Router
	auth           core.AuthProvider
	datastore      core.Datastore
	providers      *registry.PluginMap[core.Provider]
	runtimes       *registry.PluginMap[core.Runtime]
	bindings       *registry.PluginMap[core.Binding]
	resolver       *principal.Resolver
	invoker        invocation.Invoker
	devMode        bool
	stateCodec     *integrationOAuthStateCodec
	now            func() time.Time
	readiness      ReadinessChecker
	mcpHandler     http.Handler
	webUI          http.Handler
	connectHandler http.Handler // CONNECT dispatched outside chi (authority-form URIs bypass path routing)
	adminEmails    map[string]struct{}
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
	AdminEmails []string
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

	adminSet := make(map[string]struct{}, len(cfg.AdminEmails))
	for _, email := range cfg.AdminEmails {
		adminSet[strings.ToLower(email)] = struct{}{}
	}

	s := &Server{
		router:      chi.NewRouter(),
		auth:        cfg.Auth,
		datastore:   cfg.Datastore,
		providers:   cfg.Providers,
		runtimes:    cfg.Runtimes,
		bindings:    cfg.Bindings,
		resolver:    principal.NewResolver(cfg.Auth, cfg.Datastore, resolverOpts(cfg.Datastore)...),
		invoker:     cfg.Invoker,
		devMode:     cfg.DevMode,
		stateCodec:  stateCodec,
		now:         now,
		readiness:   cfg.Readiness,
		mcpHandler:  cfg.MCPHandler,
		webUI:       cfg.WebUI,
		adminEmails: adminSet,
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

		s.mountBindingRoutes(r)

		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)
			r.Use(s.adminMiddleware)
			r.Post("/egress/deny-rules", s.createEgressDenyRule)
			r.Get("/egress/deny-rules", s.listEgressDenyRules)
			r.Delete("/egress/deny-rules/{id}", s.deleteEgressDenyRule)
		})

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

			r.Post("/egress-clients", s.createEgressClient)
			r.Get("/egress-clients", s.listEgressClients)
			r.Delete("/egress-clients/{id}", s.deleteEgressClient)
			r.Post("/egress-clients/{id}/tokens", s.createEgressClientToken)
			r.Get("/egress-clients/{id}/tokens", s.listEgressClientTokens)
			r.Delete("/egress-clients/{id}/tokens/{tokenID}", s.revokeEgressClientToken)
		})
	})

	if s.webUI != nil {
		r.NotFound(s.webUI.ServeHTTP)
	}
}

func resolverOpts(ds core.Datastore) []principal.ResolverOption {
	var opts []principal.ResolverOption
	if ecs, ok := ds.(core.EgressClientStore); ok {
		opts = append(opts, principal.WithEgressClientStore(ecs))
	}
	return opts
}

func (s *Server) stagedConnectionStore() (core.StagedConnectionStore, error) {
	scs, ok := s.datastore.(core.StagedConnectionStore)
	if !ok {
		return nil, fmt.Errorf("datastore does not support staged connections; use a SQL-backed datastore (sqlite, postgres, mysql)")
	}
	return scs, nil
}

func (s *Server) mountBindingRoutes(r chi.Router) {
	if s.bindings == nil {
		return
	}
	for _, name := range s.bindings.List() {
		binding, err := s.bindings.Get(name)
		if err != nil {
			log.Printf("warning: skipping binding %q routes: %v", name, err)
			continue
		}
		for _, route := range binding.Routes() {
			handler := route.Handler
			if !route.Public {
				if route.ProxyAuth {
					handler = s.proxyAuthMiddleware(handler)
				} else {
					handler = s.authMiddleware(handler)
				}
			}
			if route.Connect {
				if s.connectHandler != nil {
					log.Printf("warning: binding %q registers CONNECT but another binding already claimed it; skipping", name)
					continue
				}
				s.connectHandler = handler
				continue
			}
			r.Method(route.Method, "/bindings/"+name+route.Pattern, handler)
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect && s.connectHandler != nil {
		s.connectHandler.ServeHTTP(w, r)
		return
	}
	s.router.ServeHTTP(w, r)
}

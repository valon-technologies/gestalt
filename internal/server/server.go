package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/registry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// ReadinessChecker reports whether the server is ready to handle requests.
// Returning a non-empty string means not ready; the string is used as the
// status message in the /ready response.
type ReadinessChecker func() string

type Server struct {
	router             chi.Router
	handler            http.Handler
	auth               core.AuthProvider
	datastore          core.Datastore
	providers          *registry.PluginMap[core.Provider]
	runtimes           *registry.PluginMap[core.Runtime]
	bindings           *registry.PluginMap[core.Binding]
	resolver           *principal.Resolver
	invoker            invocation.Invoker
	defaultConnection  map[string]string
	connectionAuth     func() map[string]map[string]bootstrap.OAuthHandler
	integrationDefs    map[string]config.IntegrationDef
	noAuth             bool
	anonymousPrincipal *principal.Principal
	secureCookies      bool
	stateCodec         *integrationOAuthStateCodec
	apiTokenTTL        time.Duration
	now                func() time.Time
	readiness          ReadinessChecker
	mcpHandler         http.Handler
	webUI              http.Handler
}

type Config struct {
	Auth              core.AuthProvider
	Datastore         core.Datastore
	Providers         *registry.PluginMap[core.Provider]
	Runtimes          *registry.PluginMap[core.Runtime]
	Bindings          *registry.PluginMap[core.Binding]
	Invoker           invocation.Invoker
	DefaultConnection map[string]string
	ConnectionAuth    func() map[string]map[string]bootstrap.OAuthHandler
	IntegrationDefs   map[string]config.IntegrationDef
	SecureCookies     bool
	StateSecret       []byte
	APITokenTTL       time.Duration
	Now               func() time.Time
	Readiness         ReadinessChecker
	MCPHandler        http.Handler
	WebUI             http.Handler
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

	resolver := principal.NewResolver(cfg.Auth, cfg.Datastore)
	noAuth := cfg.Auth.Name() == "none"

	router := chi.NewRouter()
	s := &Server{
		router:            router,
		handler:           otelhttp.NewHandler(router, "gestaltd"),
		auth:              cfg.Auth,
		datastore:         cfg.Datastore,
		providers:         cfg.Providers,
		runtimes:          cfg.Runtimes,
		bindings:          cfg.Bindings,
		resolver:          resolver,
		invoker:           cfg.Invoker,
		defaultConnection: cfg.DefaultConnection,
		connectionAuth:    cfg.ConnectionAuth,
		integrationDefs:   cfg.IntegrationDefs,
		noAuth:            noAuth,
		secureCookies:     cfg.SecureCookies,
		stateCodec:        stateCodec,
		apiTokenTTL:       cfg.APITokenTTL,
		now:               now,
		readiness:         cfg.Readiness,
		mcpHandler:        cfg.MCPHandler,
		webUI:             cfg.WebUI,
	}
	if noAuth {
		s.anonymousPrincipal = resolver.ResolveEmail(anonymousEmail)
	}

	s.routes()
	return s, nil
}

func (s *Server) stagedConnectionStore() (core.StagedConnectionStore, error) {
	scs, ok := s.datastore.(core.StagedConnectionStore)
	if !ok {
		return nil, fmt.Errorf("datastore does not support staged connections; use a SQL-backed datastore (sqlite, postgres, mysql)")
	}
	return scs, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

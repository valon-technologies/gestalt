package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	cryptoutil "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/session"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// ReadinessChecker reports whether the server is ready to handle requests.
// Returning a non-empty string means not ready; the string is used as the
// status message in the /ready response.
type ReadinessChecker func() string

type RouteProfile int

const (
	RouteProfileAll RouteProfile = iota
	RouteProfilePublic
	RouteProfileManagement
)

type Server struct {
	router             chi.Router
	handler            http.Handler
	auth               core.AuthProvider
	auditSink          core.AuditSink
	users              *coredata.UserService
	tokens             *coredata.TokenService
	apiTokens          *coredata.APITokenService
	providers          *registry.ProviderMap[core.Provider]
	resolver           *principal.Resolver
	invoker            invocation.Invoker
	defaultConnection  map[string]string
	catalogConnection  map[string]string
	connectionAuth     func() map[string]map[string]bootstrap.OAuthHandler
	pluginDefs         map[string]*config.ProviderEntry
	noAuth             bool
	anonymousPrincipal *principal.Principal
	publicBaseURL      string
	secureCookies      bool
	encryptor          *cryptoutil.AESGCMEncryptor
	sessionIssuer      []byte
	stateCodec         *integrationOAuthStateCodec
	apiTokenTTL        time.Duration
	now                func() time.Time
	readiness          ReadinessChecker
	prometheusMetrics  http.Handler
	mcpHandler         http.Handler
	clientUI           http.Handler
	adminUI            http.Handler
	routeProfile       RouteProfile
}

type Config struct {
	Auth              core.AuthProvider
	AuditSink         core.AuditSink
	Services          *coredata.Services
	Providers         *registry.ProviderMap[core.Provider]
	Invoker           invocation.Invoker
	DefaultConnection map[string]string
	CatalogConnection map[string]string
	ConnectionAuth    func() map[string]map[string]bootstrap.OAuthHandler
	PluginDefs        map[string]*config.ProviderEntry
	PublicBaseURL     string
	SecureCookies     bool
	StateSecret       []byte
	APITokenTTL       time.Duration
	Now               func() time.Time
	Readiness         ReadinessChecker
	PrometheusMetrics http.Handler
	MCPHandler        http.Handler
	ClientUI          http.Handler
	AdminUI           http.Handler
	RouteProfile      RouteProfile
}

func New(cfg Config) (*Server, error) {
	if cfg.Invoker == nil {
		return nil, fmt.Errorf("invoker is required")
	}
	noAuth := cfg.Auth == nil || cfg.Auth.Name() == "none"
	var stateCodec *integrationOAuthStateCodec
	var encryptor *cryptoutil.AESGCMEncryptor
	if len(cfg.StateSecret) > 0 {
		codec, err := newIntegrationOAuthStateCodec(cfg.StateSecret)
		if err != nil {
			return nil, fmt.Errorf("init oauth state codec: %w", err)
		}
		stateCodec = codec
		enc, err := cryptoutil.NewAESGCM(cfg.StateSecret)
		if err != nil {
			return nil, fmt.Errorf("init state encryptor: %w", err)
		}
		encryptor = enc
	} else if !noAuth {
		return nil, fmt.Errorf("state secret is required when auth is enabled")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	users := cfg.Services.Users
	tokens := cfg.Services.Tokens
	apiTokens := cfg.Services.APITokens
	resolver := principal.NewResolver(cfg.Auth, users, apiTokens)

	router := chi.NewRouter()
	s := &Server{
		router:            router,
		handler:           otelhttp.NewHandler(router, "gestaltd"),
		auth:              cfg.Auth,
		auditSink:         cfg.AuditSink,
		users:             users,
		tokens:            tokens,
		apiTokens:         apiTokens,
		providers:         cfg.Providers,
		resolver:          resolver,
		invoker:           cfg.Invoker,
		defaultConnection: cfg.DefaultConnection,
		catalogConnection: cfg.CatalogConnection,
		connectionAuth:    cfg.ConnectionAuth,
		pluginDefs:        cfg.PluginDefs,
		noAuth:            noAuth,
		publicBaseURL:     strings.TrimRight(cfg.PublicBaseURL, "/"),
		secureCookies:     cfg.SecureCookies,
		encryptor:         encryptor,
		sessionIssuer:     cfg.StateSecret,
		stateCodec:        stateCodec,
		apiTokenTTL:       cfg.APITokenTTL,
		now:               now,
		readiness:         cfg.Readiness,
		prometheusMetrics: cfg.PrometheusMetrics,
		mcpHandler:        cfg.MCPHandler,
		clientUI:          cfg.ClientUI,
		adminUI:           cfg.AdminUI,
		routeProfile:      cfg.RouteProfile,
	}
	if noAuth {
		s.anonymousPrincipal = resolver.ResolveEmail(anonymousEmail)
	}

	s.routes()
	return s, nil
}

func (s *Server) issueSessionToken(identity *core.UserIdentity) (string, error) {
	if issuer, ok := s.auth.(SessionTokenIssuer); ok {
		return issuer.IssueSessionToken(identity)
	}
	if len(s.sessionIssuer) == 0 {
		return "", fmt.Errorf("session secret is not configured")
	}
	ttl := defaultSessionCookieTTL
	if p, ok := s.auth.(SessionTokenTTLProvider); ok {
		ttl = p.SessionTokenTTL()
	}
	return session.IssueToken(identity, s.sessionIssuer, ttl)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

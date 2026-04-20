package server

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	cryptoutil "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/session"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
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

type MountedWebUIRoute = providermanifestv1.WebUIRoute

type MountedWebUI struct {
	Name                string
	Path                string
	PluginName          string
	AuthorizationPolicy string
	Routes              []MountedWebUIRoute
	Handler             http.Handler
	builtInAdmin        bool
}

type AdminRouteConfig struct {
	AuthorizationPolicy string
	AllowedRoles        []string
}

type BuiltinAdminUIOptions struct {
	BrandHref string
	LoginBase string
}

type Server struct {
	router                chi.Router
	handler               http.Handler
	auth                  core.AuthProvider
	auditSink             core.AuditSink
	users                 *coredata.UserService
	tokens                *coredata.TokenService
	apiTokens             *coredata.APITokenService
	managedIdentities     *coredata.ManagedIdentityService
	identityMemberships   *coredata.ManagedIdentityMembershipService
	identityGrants        *coredata.ManagedIdentityGrantService
	workspaceRoles        *coredata.WorkspaceRoleService
	identityPluginAccess  *coredata.IdentityPluginAccessService
	authorizationProvider core.AuthorizationProvider
	managedIdentityMu     sync.Mutex
	providers             *registry.ProviderMap[core.Provider]
	resolver              *principal.Resolver
	invoker               invocation.Invoker
	defaultConnection     map[string]string
	catalogConnection     map[string]string
	connectionAuth        func() map[string]map[string]bootstrap.OAuthHandler
	pluginDefs            map[string]*config.ProviderEntry
	authorizer            authorization.RuntimeAuthorizer
	noAuth                bool
	anonymousPrincipal    *principal.Principal
	publicBaseURL         string
	managementBaseURL     string
	secureCookies         bool
	encryptor             *cryptoutil.AESGCMEncryptor
	sessionIssuer         []byte
	stateCodec            *integrationOAuthStateCodec
	apiTokenTTL           time.Duration
	now                   func() time.Time
	readiness             ReadinessChecker
	prometheusMetrics     http.Handler
	mcpHandler            http.Handler
	mountedWebUIs         []MountedWebUI
	adminRoute            AdminRouteConfig
	adminUI               http.Handler
	routeProfile          RouteProfile
}

type Config struct {
	Auth                  core.AuthProvider
	AuditSink             core.AuditSink
	Services              *coredata.Services
	Providers             *registry.ProviderMap[core.Provider]
	Invoker               invocation.Invoker
	DefaultConnection     map[string]string
	CatalogConnection     map[string]string
	ConnectionAuth        func() map[string]map[string]bootstrap.OAuthHandler
	PluginDefs            map[string]*config.ProviderEntry
	ProviderUIs           map[string]*config.UIEntry
	Authorizer            authorization.RuntimeAuthorizer
	AuthorizationProvider core.AuthorizationProvider
	PublicBaseURL         string
	ManagementBaseURL     string
	SecureCookies         bool
	StateSecret           []byte
	APITokenTTL           time.Duration
	Now                   func() time.Time
	Readiness             ReadinessChecker
	PrometheusMetrics     http.Handler
	MCPHandler            http.Handler
	MountedWebUIs         []MountedWebUI
	Admin                 AdminRouteConfig
	AdminUI               http.Handler
	BuiltinAdminUI        *BuiltinAdminUIOptions
	RouteProfile          RouteProfile
	MeterProvider         metric.MeterProvider
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
	adminRoute, err := normalizeAdminRouteConfig(cfg.Admin)
	if err != nil {
		return nil, fmt.Errorf("normalize admin route: %w", err)
	}
	if err := validateAdminRouteRuntime(adminRoute, noAuth, cfg.PublicBaseURL, cfg.ManagementBaseURL, cfg.RouteProfile); err != nil {
		return nil, fmt.Errorf("validate admin route: %w", err)
	}
	mountedWebUIs := cfg.MountedWebUIs
	if len(mountedWebUIs) == 0 && len(cfg.ProviderUIs) != 0 {
		mountedWebUIs, err = mountedWebUIsFromEntries(cfg.ProviderUIs)
		if err != nil {
			return nil, fmt.Errorf("resolve mounted ui handlers: %w", err)
		}
	}
	mountedWebUIs, err = normalizeMountedWebUIs(mountedWebUIs)
	if err != nil {
		return nil, err
	}
	adminUI := cfg.AdminUI
	if adminUI == nil && cfg.BuiltinAdminUI != nil {
		adminUI, err = resolveBuiltinAdminUI(*cfg.BuiltinAdminUI)
		if err != nil {
			return nil, fmt.Errorf("resolve admin ui: %w", err)
		}
	}

	users := cfg.Services.Users
	tokens := cfg.Services.Tokens
	apiTokens := cfg.Services.APITokens
	managedIdentities := cfg.Services.ManagedIdentities
	identityMemberships := cfg.Services.IdentityMemberships
	identityGrants := cfg.Services.IdentityGrants
	workspaceRoles := cfg.Services.WorkspaceRoles
	identityPluginAccess := cfg.Services.IdentityPluginAccess
	resolver := principal.NewResolver(cfg.Auth, users, apiTokens, managedIdentities, identityGrants, cfg.Authorizer)

	router := chi.NewRouter()
	otelOptions := []otelhttp.Option{}
	if cfg.MeterProvider != nil {
		otelOptions = append(otelOptions, otelhttp.WithMeterProvider(cfg.MeterProvider))
	}
	s := &Server{
		router:                router,
		handler:               withRequestMeterProvider(otelhttp.NewHandler(router, "gestaltd", otelOptions...), cfg.MeterProvider),
		auth:                  cfg.Auth,
		auditSink:             cfg.AuditSink,
		users:                 users,
		tokens:                tokens,
		apiTokens:             apiTokens,
		managedIdentities:     managedIdentities,
		identityMemberships:   identityMemberships,
		identityGrants:        identityGrants,
		workspaceRoles:        workspaceRoles,
		identityPluginAccess:  identityPluginAccess,
		authorizationProvider: cfg.AuthorizationProvider,
		providers:             cfg.Providers,
		resolver:              resolver,
		invoker:               cfg.Invoker,
		defaultConnection:     cfg.DefaultConnection,
		catalogConnection:     cfg.CatalogConnection,
		connectionAuth:        cfg.ConnectionAuth,
		pluginDefs:            cfg.PluginDefs,
		authorizer:            cfg.Authorizer,
		noAuth:                noAuth,
		publicBaseURL:         strings.TrimRight(cfg.PublicBaseURL, "/"),
		managementBaseURL:     strings.TrimRight(cfg.ManagementBaseURL, "/"),
		secureCookies:         cfg.SecureCookies,
		encryptor:             encryptor,
		sessionIssuer:         cfg.StateSecret,
		stateCodec:            stateCodec,
		apiTokenTTL:           cfg.APITokenTTL,
		now:                   now,
		readiness:             cfg.Readiness,
		prometheusMetrics:     cfg.PrometheusMetrics,
		mcpHandler:            cfg.MCPHandler,
		mountedWebUIs:         mountedWebUIs,
		adminRoute:            adminRoute,
		adminUI:               adminUI,
		routeProfile:          cfg.RouteProfile,
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

func withRequestMeterProvider(next http.Handler, provider metric.MeterProvider) http.Handler {
	if provider == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(metricutil.WithMeterProvider(r.Context(), provider)))
	})
}

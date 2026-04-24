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
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
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

type MountedUIRoute = providermanifestv1.UIRoute

type MountedUI struct {
	Name                string
	Path                string
	PluginName          string
	AuthorizationPolicy string
	Routes              []MountedUIRoute
	Handler             http.Handler
	builtInAdmin        bool
}

type MountedHTTPBinding struct {
	Name           string
	PluginName     string
	Path           string
	Method         string
	Target         string
	CredentialMode core.ConnectionMode
	RequestBody    *providermanifestv1.HTTPRequestBody
	Ack            *providermanifestv1.HTTPAck
	SecurityName   string
	Security       *providermanifestv1.HTTPSecurityScheme
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
	router                 chi.Router
	handler                http.Handler
	auth                   core.AuthenticationProvider
	authProviders          map[string]core.AuthenticationProvider
	serverAuthProvider     string
	auditSink              core.AuditSink
	users                  *coredata.UserService
	externalCredentials    core.ExternalCredentialProvider
	apiTokens              *coredata.APITokenService
	identities             *coredata.IdentityService
	identityGrants         *coredata.IdentityManagementGrantService
	workspaceRoles         *coredata.WorkspaceRoleService
	identityPluginAccess   *coredata.IdentityPluginAccessService
	workflowSchedules      *workflowmanager.Manager
	agentRuns              agentmanager.Service
	authorizationProvider  core.AuthorizationProvider
	providers              *registry.ProviderMap[core.Provider]
	workflow               bootstrap.WorkflowControl
	pluginRuntimes         bootstrap.RuntimeInspector
	resolver               *principal.Resolver
	authResolvers          map[string]*principal.Resolver
	invoker                invocation.Invoker
	defaultConnection      map[string]string
	catalogConnection      map[string]string
	connectionAuth         func() map[string]map[string]bootstrap.OAuthHandler
	pluginDefs             map[string]*config.ProviderEntry
	authorizer             authorization.RuntimeAuthorizer
	noAuth                 bool
	anonymousPrincipal     *principal.Principal
	publicBaseURL          string
	managementBaseURL      string
	secureCookies          bool
	encryptor              *cryptoutil.AESGCMEncryptor
	sessionIssuer          []byte
	stateCodec             *integrationOAuthStateCodec
	apiTokenTTL            time.Duration
	now                    func() time.Time
	readiness              ReadinessChecker
	prometheusMetrics      http.Handler
	mcpHandler             http.Handler
	hostServiceRelayTokens *providerhost.HostServiceRelayTokenManager
	egressProxyTokens      *providerhost.EgressProxyTokenManager
	mountedHTTPBindings    []MountedHTTPBinding
	mountedUIs             []MountedUI
	adminRoute             AdminRouteConfig
	adminUI                http.Handler
	routeProfile           RouteProfile
	httpBindingReplayStore httpBindingReplayStore
}

func (s *Server) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
		Authorizer:        s.authorizer,
		Invoker:           s.invoker,
		CatalogConnection: s.catalogConnection,
		DefaultConnection: s.defaultConnection,
	}
}

type Config struct {
	Auth                  core.AuthenticationProvider
	SelectedAuthProvider  string
	AuthProviders         map[string]core.AuthenticationProvider
	AuditSink             core.AuditSink
	Services              *coredata.Services
	Providers             *registry.ProviderMap[core.Provider]
	AgentManager          agentmanager.Service
	Workflow              bootstrap.WorkflowControl
	PluginRuntimes        bootstrap.RuntimeInspector
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
	MountedUIs            []MountedUI
	Admin                 AdminRouteConfig
	AdminUIProvider       string
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
	serverAuthProvider := strings.TrimSpace(cfg.SelectedAuthProvider)
	if serverAuthProvider == "" {
		if cfg.Auth == nil {
			serverAuthProvider = "none"
		} else {
			serverAuthProvider = cfg.Auth.Name()
		}
	}
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
	mountedUIs := cfg.MountedUIs
	if len(mountedUIs) == 0 && len(cfg.ProviderUIs) != 0 {
		mountedUIs, err = mountedUIsFromEntries(cfg.ProviderUIs)
		if err != nil {
			return nil, fmt.Errorf("resolve mounted ui handlers: %w", err)
		}
	}
	mountedUIs, err = normalizeMountedUIs(mountedUIs)
	if err != nil {
		return nil, err
	}
	mountedHTTPBindings, err := mountedHTTPBindingsFromEntries(cfg.PluginDefs, cfg.Providers, mountedUIs)
	if err != nil {
		return nil, err
	}
	adminUI := cfg.AdminUI
	if adminUI == nil {
		adminUIOpts := BuiltinAdminUIOptions{}
		if cfg.BuiltinAdminUI != nil {
			adminUIOpts = *cfg.BuiltinAdminUI
		}
		adminUI, err = resolveConfiguredAdminUI(adminUIOpts, cfg.AdminUIProvider, cfg.ProviderUIs)
		if err != nil {
			return nil, fmt.Errorf("resolve configured admin ui: %w", err)
		}
	}
	if adminUI == nil && cfg.BuiltinAdminUI != nil {
		adminUI, err = resolveBuiltinAdminUI(*cfg.BuiltinAdminUI)
		if err != nil {
			return nil, fmt.Errorf("resolve admin ui: %w", err)
		}
	}

	users := cfg.Services.Users
	externalCredentials := coredata.EffectiveExternalCredentialProvider(cfg.Services)
	apiTokens := cfg.Services.APITokens
	identities := cfg.Services.Identities
	identityGrants := cfg.Services.IdentityManagementGrants
	workspaceRoles := cfg.Services.WorkspaceRoles
	identityPluginAccess := cfg.Services.IdentityPluginAccess
	resolver := principal.NewResolver(cfg.Auth, users, apiTokens, cfg.Authorizer)
	authProviders := make(map[string]core.AuthenticationProvider, len(cfg.AuthProviders)+1)
	for name, provider := range cfg.AuthProviders {
		if provider == nil {
			continue
		}
		authProviders[name] = provider
	}
	if cfg.Auth != nil && cfg.SelectedAuthProvider != "" {
		if _, ok := authProviders[cfg.SelectedAuthProvider]; !ok {
			authProviders[cfg.SelectedAuthProvider] = cfg.Auth
		}
	}
	authResolvers := make(map[string]*principal.Resolver, len(authProviders))
	for name, provider := range authProviders {
		authResolvers[name] = principal.NewResolver(provider, users, apiTokens, cfg.Authorizer)
	}

	router := chi.NewRouter()
	otelOptions := []otelhttp.Option{}
	if cfg.MeterProvider != nil {
		otelOptions = append(otelOptions, otelhttp.WithMeterProvider(cfg.MeterProvider))
	}
	var hostServiceRelayTokens *providerhost.HostServiceRelayTokenManager
	var egressProxyTokens *providerhost.EgressProxyTokenManager
	if len(cfg.StateSecret) > 0 {
		hostServiceRelayTokens, err = providerhost.NewHostServiceRelayTokenManager(cfg.StateSecret)
		if err != nil {
			return nil, fmt.Errorf("init host service relay tokens: %w", err)
		}
		egressProxyTokens, err = providerhost.NewEgressProxyTokenManager(cfg.StateSecret)
		if err != nil {
			return nil, fmt.Errorf("init egress proxy tokens: %w", err)
		}
	}
	s := &Server{
		router:                 router,
		handler:                withRequestMeterProvider(otelhttp.NewHandler(router, "gestaltd", otelOptions...), cfg.MeterProvider),
		auth:                   cfg.Auth,
		authProviders:          authProviders,
		serverAuthProvider:     serverAuthProvider,
		auditSink:              cfg.AuditSink,
		users:                  users,
		externalCredentials:    externalCredentials,
		apiTokens:              apiTokens,
		identities:             identities,
		identityGrants:         identityGrants,
		workspaceRoles:         workspaceRoles,
		identityPluginAccess:   identityPluginAccess,
		agentRuns:              cfg.AgentManager,
		authorizationProvider:  cfg.AuthorizationProvider,
		providers:              cfg.Providers,
		workflow:               cfg.Workflow,
		pluginRuntimes:         cfg.PluginRuntimes,
		resolver:               resolver,
		authResolvers:          authResolvers,
		invoker:                cfg.Invoker,
		defaultConnection:      cfg.DefaultConnection,
		catalogConnection:      cfg.CatalogConnection,
		connectionAuth:         cfg.ConnectionAuth,
		pluginDefs:             cfg.PluginDefs,
		authorizer:             cfg.Authorizer,
		noAuth:                 noAuth,
		publicBaseURL:          strings.TrimRight(cfg.PublicBaseURL, "/"),
		managementBaseURL:      strings.TrimRight(cfg.ManagementBaseURL, "/"),
		secureCookies:          cfg.SecureCookies,
		encryptor:              encryptor,
		sessionIssuer:          cfg.StateSecret,
		stateCodec:             stateCodec,
		apiTokenTTL:            cfg.APITokenTTL,
		now:                    now,
		readiness:              cfg.Readiness,
		prometheusMetrics:      cfg.PrometheusMetrics,
		mcpHandler:             cfg.MCPHandler,
		hostServiceRelayTokens: hostServiceRelayTokens,
		egressProxyTokens:      egressProxyTokens,
		mountedHTTPBindings:    mountedHTTPBindings,
		mountedUIs:             mountedUIs,
		adminRoute:             adminRoute,
		adminUI:                adminUI,
		routeProfile:           cfg.RouteProfile,
		httpBindingReplayStore: newMemoryHTTPBindingReplayStore(),
	}
	s.workflowSchedules = workflowmanager.New(workflowmanager.Config{
		Providers:         cfg.Providers,
		Workflow:          cfg.Workflow,
		Invoker:           cfg.Invoker,
		Authorizer:        cfg.Authorizer,
		DefaultConnection: cfg.DefaultConnection,
		CatalogConnection: cfg.CatalogConnection,
		Now:               now,
	})
	if noAuth || hasAnonymousAuthProvider(authProviders) {
		s.anonymousPrincipal = resolver.ResolveEmail(anonymousEmail)
	}

	s.routes()
	return s, nil
}

func (s *Server) issueSessionToken(provider core.AuthenticationProvider, identity *core.UserIdentity) (string, error) {
	if issuer, ok := provider.(SessionTokenIssuer); ok {
		return issuer.IssueSessionToken(identity)
	}
	if len(s.sessionIssuer) == 0 {
		return "", fmt.Errorf("session secret is not configured")
	}
	ttl := defaultSessionCookieTTL
	if p, ok := provider.(SessionTokenTTLProvider); ok {
		ttl = p.SessionTokenTTL()
	}
	return session.IssueToken(identity, s.sessionIssuer, ttl)
}

func hasAnonymousAuthProvider(providers map[string]core.AuthenticationProvider) bool {
	for _, provider := range providers {
		if provider != nil && provider.Name() == "none" {
			return true
		}
	}
	return false
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

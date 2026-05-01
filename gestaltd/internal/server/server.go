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
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	"github.com/valon-technologies/gestalt/server/core/session"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/providerdev"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/s3"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
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
	router                  chi.Router
	handler                 http.Handler
	auth                    core.AuthenticationProvider
	authProviders           map[string]core.AuthenticationProvider
	serverAuthProvider      string
	auditSink               core.AuditSink
	users                   *coredata.UserService
	externalCredentials     core.ExternalCredentialProvider
	apiTokens               *coredata.APITokenService
	managedSubjects         *coredata.ManagedSubjectService
	agent                   bootstrap.AgentControl
	workflowSchedules       *workflowmanager.Manager
	agentRuns               agentmanager.Service
	authorizationProvider   core.AuthorizationProvider
	providers               *registry.ProviderMap[core.Provider]
	workflow                bootstrap.WorkflowControl
	pluginRuntimes          bootstrap.RuntimeInspector
	resolver                *principal.Resolver
	authResolvers           map[string]*principal.Resolver
	invoker                 invocation.Invoker
	pluginInvoker           invocation.Invoker
	defaultConnection       map[string]string
	catalogConnection       map[string]string
	connectionAuth          func() map[string]map[string]bootstrap.OAuthHandler
	pluginDefs              map[string]*config.ProviderEntry
	authorizer              authorization.RuntimeAuthorizer
	noAuth                  bool
	anonymousPrincipal      *principal.Principal
	publicBaseURL           string
	managementBaseURL       string
	secureCookies           bool
	encryptor               *cryptoutil.AESGCMEncryptor
	sessionIssuer           []byte
	stateCodec              *integrationOAuthStateCodec
	apiTokenTTL             time.Duration
	now                     func() time.Time
	readiness               ReadinessChecker
	meterProvider           metric.MeterProvider
	prometheusMetrics       http.Handler
	mcpHandler              http.Handler
	hostServiceRelayTokens  *runtimehost.HostServiceRelayTokenManager
	invocationTokens        *plugininvokerservice.InvocationTokenManager
	hostServiceMu           sync.Mutex
	coreHostServiceHandlers map[hostServiceHandlerKey]hostServiceHandlerEntry
	hostServiceHandlers     map[uint64]http.Handler
	publicHostServices      *runtimehost.PublicHostServiceRegistry
	s3                      map[string]s3store.Client
	s3ObjectAccessURLs      *s3.ObjectAccessURLManager
	egressProxyTokens       *egressproxy.TokenManager
	providerDevSessions     *providerdev.Manager
	providerDevAttach       bool
	mountedHTTPBindings     []MountedHTTPBinding
	mountedUIs              []MountedUI
	adminRoute              AdminRouteConfig
	adminUI                 http.Handler
	routeProfile            RouteProfile
	httpBindingReplayStore  httpBindingReplayStore
}

func (s *Server) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
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
	Agent                 bootstrap.AgentControl
	AgentManager          agentmanager.Service
	Workflow              bootstrap.WorkflowControl
	PluginRuntimes        bootstrap.RuntimeInspector
	Invoker               invocation.Invoker
	PluginInvoker         invocation.Invoker
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
	PublicHostServices    *runtimehost.PublicHostServiceRegistry
	S3                    map[string]s3store.Client
	ProviderDevSessions   *providerdev.Manager
	ProviderDevAttach     bool
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
	pluginInvoker := cfg.PluginInvoker
	if pluginInvoker == nil {
		pluginInvoker = cfg.Invoker
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

	if cfg.Services == nil {
		return nil, fmt.Errorf("services are required")
	}
	users := cfg.Services.Users
	externalCredentials := coredata.EffectiveExternalCredentialProvider(cfg.Services)
	if coredata.ExternalCredentialProviderMissing(externalCredentials) {
		return nil, fmt.Errorf("external credentials provider is required")
	}
	apiTokens := cfg.Services.APITokens
	managedSubjects := cfg.Services.ManagedSubjects
	resolver := principal.NewResolver(cfg.Auth, users, apiTokens)
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
		authResolvers[name] = principal.NewResolver(provider, users, apiTokens)
	}

	router := chi.NewRouter()
	otelOptions := []otelhttp.Option{}
	if cfg.MeterProvider != nil {
		otelOptions = append(otelOptions, otelhttp.WithMeterProvider(cfg.MeterProvider))
	}
	var hostServiceRelayTokens *runtimehost.HostServiceRelayTokenManager
	var invocationTokens *plugininvokerservice.InvocationTokenManager
	var egressProxyTokens *egressproxy.TokenManager
	var s3ObjectAccessURLs *s3.ObjectAccessURLManager
	if len(cfg.StateSecret) > 0 {
		hostServiceRelayTokens, err = runtimehost.NewHostServiceRelayTokenManager(cfg.StateSecret)
		if err != nil {
			return nil, fmt.Errorf("init host service relay tokens: %w", err)
		}
		invocationTokens, err = plugininvokerservice.NewInvocationTokenManager(cfg.StateSecret)
		if err != nil {
			return nil, fmt.Errorf("init invocation tokens: %w", err)
		}
		egressProxyTokens, err = egressproxy.NewTokenManager(cfg.StateSecret)
		if err != nil {
			return nil, fmt.Errorf("init egress proxy tokens: %w", err)
		}
		s3ObjectAccessURLs, err = s3.NewObjectAccessURLManager(cfg.StateSecret, cfg.PublicBaseURL)
		if err != nil {
			return nil, fmt.Errorf("init s3 object access URLs: %w", err)
		}
	}
	if err := validatePublicHostServices(cfg.PublicHostServices.Snapshot()); err != nil {
		return nil, fmt.Errorf("init public host services: %w", err)
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
		managedSubjects:        managedSubjects,
		agent:                  cfg.Agent,
		agentRuns:              cfg.AgentManager,
		authorizationProvider:  cfg.AuthorizationProvider,
		providers:              cfg.Providers,
		workflow:               cfg.Workflow,
		pluginRuntimes:         cfg.PluginRuntimes,
		resolver:               resolver,
		authResolvers:          authResolvers,
		invoker:                cfg.Invoker,
		pluginInvoker:          pluginInvoker,
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
		meterProvider:          cfg.MeterProvider,
		prometheusMetrics:      cfg.PrometheusMetrics,
		mcpHandler:             cfg.MCPHandler,
		hostServiceRelayTokens: hostServiceRelayTokens,
		invocationTokens:       invocationTokens,
		publicHostServices:     cfg.PublicHostServices,
		s3:                     cfg.S3,
		s3ObjectAccessURLs:     s3ObjectAccessURLs,
		egressProxyTokens:      egressProxyTokens,
		providerDevSessions:    cfg.ProviderDevSessions,
		providerDevAttach:      cfg.ProviderDevAttach,
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
		Agent:             cfg.Agent,
		AgentManager:      cfg.AgentManager,
		Invoker:           cfg.Invoker,
		Authorizer:        cfg.Authorizer,
		DefaultConnection: cfg.DefaultConnection,
		CatalogConnection: cfg.CatalogConnection,
		PluginInvokes:     pluginInvokesFromProviderEntries(cfg.PluginDefs),
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

func pluginInvokesFromProviderEntries(entries map[string]*config.ProviderEntry) map[string][]config.PluginInvocationDependency {
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string][]config.PluginInvocationDependency, len(entries))
	for pluginName, entry := range entries {
		if entry == nil || len(entry.Invokes) == 0 {
			continue
		}
		out[pluginName] = append([]config.PluginInvocationDependency(nil), entry.Invokes...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

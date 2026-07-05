package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	"github.com/valon-technologies/gestalt/server/core"
	cryptoutil "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/s3"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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
	AppName             string
	AuthorizationPolicy string
	PublicPaths         []string
	Routes              []MountedUIRoute
	Handler             http.Handler
	// ThemeStylesheet and ThemeAssetsDir are resolved absolute paths to a
	// deployment-configured theme, served at <mount>/theme.css and under
	// <mount>/theme/ respectively. Both are optional.
	ThemeStylesheet string
	ThemeAssetsDir  string
	IsDev           bool
	AppLevelAuth    bool
	builtInAdmin    bool
}

type MountedHTTPBinding struct {
	Name           string
	AppName        string
	Path           string
	Method         string
	Target         string
	CredentialMode core.ConnectionMode
	RequestBody    *providermanifestv1.HTTPRequestBody
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
	auth                   core.IdentityProvider
	authProviders          map[string]core.IdentityProvider
	serverAuthProvider     string
	authorization          core.AuthorizationProvider
	auditSink              core.AuditSink
	users                  *coredata.UserService
	externalCredentials    core.ExternalCredentialProvider
	managedSubjects        *coredata.ManagedSubjectService
	agent                  bootstrap.AgentControl
	workflowSchedules      *workflowmanager.Manager
	agentRuns              agentmanager.Service
	providers              *registry.ProviderMap[core.Provider]
	callerTokenIssuer      *providergateway.CallerTokenIssuer
	workflow               bootstrap.WorkflowControl
	pluginRuntimes         bootstrap.RuntimeInspector
	resolver               *principal.Resolver
	authResolvers          map[string]*principal.Resolver
	invoker                invocation.Invoker
	pluginInvoker          invocation.Invoker
	apiRouteTimeout        time.Duration
	agentStreamHeartbeat   time.Duration
	defaultConnection      map[string]string
	catalogConnection      map[string]string
	mcpConnection          map[string]string
	connectionAuth         func() map[string]map[string]bootstrap.OAuthHandler
	manualConnectionAuth   func() map[string]map[string]bootstrap.ManualTokenExchanger
	pluginDefs             map[string]*config.ProviderEntry
	agentDefs              map[string]*config.ProviderEntry
	noAuth                 bool
	anonymousPrincipal     *principal.Principal
	publicBaseURL          string
	managementBaseURL      string
	secureCookies          bool
	encryptor              *cryptoutil.AESGCMEncryptor
	sessionIssuer          []byte
	stateCodec             *integrationOAuthStateCodec
	now                    func() time.Time
	readiness              ReadinessChecker
	meterProvider          metric.MeterProvider
	prometheusMetrics      http.Handler
	mcpHandler             http.Handler
	hostServiceRelayTokens *runtimehost.HostServiceRelayTokenManager
	hostServiceMu          sync.Mutex
	hostServiceHandlers    map[uint64]http.Handler
	publicHostServices     *runtimehost.PublicHostServiceRegistry
	s3                     map[string]s3sdk.S3
	s3ObjectAccessURLs     *s3.ObjectAccessURLManager
	egressProxyTokens      *egressproxy.TokenManager
	mountedHTTPBindings    []MountedHTTPBinding
	mountedUIs             []MountedUI
	adminRoute             AdminRouteConfig
	adminUI                http.Handler
	routeProfile           RouteProfile
	activateAppProviders   func(context.Context)
}

func (s *Server) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
		Invoker:           s.invoker,
		CatalogConnection: s.catalogConnection,
		MCPConnection:     s.mcpConnection,
		DefaultConnection: s.defaultConnection,
	}
}

type Config struct {
	Auth                 core.IdentityProvider
	SelectedAuthProvider string
	AuthProviders        map[string]core.IdentityProvider
	Authorization        core.AuthorizationProvider
	AuditSink            core.AuditSink
	Services             *coredata.Services
	Providers            *registry.ProviderMap[core.Provider]
	CallerTokenIssuer    *providergateway.CallerTokenIssuer
	Agent                bootstrap.AgentControl
	AgentManager         agentmanager.Service
	Workflow             bootstrap.WorkflowControl
	Runtimes             bootstrap.RuntimeInspector
	Invoker              invocation.Invoker
	AppInvocation        invocation.Invoker
	DefaultConnection    map[string]string
	CatalogConnection    map[string]string
	MCPConnection        map[string]string
	ConnectionAuth       func() map[string]map[string]bootstrap.OAuthHandler
	ManualConnectionAuth func() map[string]map[string]bootstrap.ManualTokenExchanger
	AppDefs              map[string]*config.ProviderEntry
	AgentDefs            map[string]*config.ProviderEntry
	ProviderUIs          map[string]*config.UIEntry
	PublicBaseURL        string
	ManagementBaseURL    string
	SecureCookies        bool
	StateSecret          []byte
	APIRouteTimeout      time.Duration
	AgentStreamHeartbeat time.Duration
	Now                  func() time.Time
	Readiness            ReadinessChecker
	PrometheusMetrics    http.Handler
	MCPHandler           http.Handler
	PublicHostServices   *runtimehost.PublicHostServiceRegistry
	S3                   map[string]s3sdk.S3
	MountedUIs           []MountedUI
	DevHandlerResolver   func(name string) http.Handler
	Admin                AdminRouteConfig
	AdminUIProvider      string
	AdminUI              http.Handler
	BuiltinAdminUI       *BuiltinAdminUIOptions
	RouteProfile         RouteProfile
	MeterProvider        metric.MeterProvider
	TracerProvider       trace.TracerProvider
	ActivateAppProviders func(context.Context)
}

func New(cfg Config) (*Server, error) {
	if cfg.Invoker == nil {
		return nil, fmt.Errorf("invoker is required")
	}
	pluginInvoker := cfg.AppInvocation
	if pluginInvoker == nil {
		pluginInvoker = cfg.Invoker
	}
	serverAuthProvider := strings.TrimSpace(cfg.SelectedAuthProvider)
	if serverAuthProvider == "" {
		if cfg.Auth == nil {
			serverAuthProvider = "none"
		} else {
			serverAuthProvider = "identity"
		}
	}
	noAuth := cfg.Auth == nil || serverAuthProvider == "none"
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
	apiRouteTimeout := cfg.APIRouteTimeout
	if apiRouteTimeout <= 0 {
		apiRouteTimeout = defaultAPIRouteTimeout
	}
	agentStreamHeartbeat := cfg.AgentStreamHeartbeat
	if agentStreamHeartbeat <= 0 {
		agentStreamHeartbeat = defaultAgentTurnEventStreamHeartbeatInterval
	}
	defaultAdminResource := ""
	if !noAuth && cfg.Authorization != nil {
		defaultAdminResource = defaultAdminAuthorizationResource
	}
	adminRoute, err := normalizeAdminRouteConfig(cfg.Admin, defaultAdminResource)
	if err != nil {
		return nil, fmt.Errorf("normalize admin route: %w", err)
	}
	if err := validateAdminRouteRuntime(adminRoute, noAuth, cfg.PublicBaseURL, cfg.ManagementBaseURL, cfg.RouteProfile); err != nil {
		return nil, fmt.Errorf("validate admin route: %w", err)
	}
	mountedUIs := cfg.MountedUIs
	if len(mountedUIs) == 0 && len(cfg.ProviderUIs) != 0 {
		mountedUIs, err = mountedUIsFromEntries(cfg.ProviderUIs, cfg.DevHandlerResolver)
		if err != nil {
			return nil, fmt.Errorf("resolve mounted ui handlers: %w", err)
		}
	}
	appStatics, err := mountedAppStaticsFromEntries(cfg.AppDefs, cfg.DevHandlerResolver)
	if err != nil {
		return nil, fmt.Errorf("resolve mounted app static handlers: %w", err)
	}
	mountedUIs = append(mountedUIs, appStatics...)
	mountedUIs, err = normalizeMountedUIs(mountedUIs)
	if err != nil {
		return nil, err
	}
	mountedHTTPBindings, err := mountedHTTPBindingsFromEntries(cfg.AppDefs, cfg.Providers, mountedUIs)
	if err != nil {
		return nil, err
	}
	adminUI := cfg.AdminUI
	if adminUI == nil {
		adminUIOpts := BuiltinAdminUIOptions{}
		if cfg.BuiltinAdminUI != nil {
			adminUIOpts = *cfg.BuiltinAdminUI
		}
		adminUI, err = resolveConfiguredAdminUI(adminUIOpts, cfg.AdminUIProvider, cfg.ProviderUIs, cfg.AppDefs)
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
	externalCredentials := cfg.Services.ExternalCredentials
	if core.ExternalCredentialProviderMissing(externalCredentials) {
		return nil, fmt.Errorf("external credentials provider is required")
	}
	managedSubjects := cfg.Services.ManagedSubjects
	resolver := principal.NewResolverNamed(cfg.SelectedAuthProvider, cfg.Auth)
	authProviders := make(map[string]core.IdentityProvider, len(cfg.AuthProviders)+1)
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
		authResolvers[name] = principal.NewResolverNamed(name, provider)
	}

	router := chi.NewRouter()
	otelOptions := []otelhttp.Option{}
	if cfg.MeterProvider != nil {
		otelOptions = append(otelOptions, otelhttp.WithMeterProvider(cfg.MeterProvider))
	}
	if cfg.TracerProvider != nil {
		otelOptions = append(otelOptions, otelhttp.WithTracerProvider(cfg.TracerProvider))
	}
	var hostServiceRelayTokens *runtimehost.HostServiceRelayTokenManager
	var egressProxyTokens *egressproxy.TokenManager
	var s3ObjectAccessURLs *s3.ObjectAccessURLManager
	if len(cfg.StateSecret) > 0 {
		hostServiceRelayTokens, err = runtimehost.NewHostServiceRelayTokenManager(cfg.StateSecret)
		if err != nil {
			return nil, fmt.Errorf("init host service relay tokens: %w", err)
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
		handler:                withRequestTelemetryProviders(otelhttp.NewHandler(router, "gestaltd", otelOptions...), cfg.MeterProvider, cfg.TracerProvider),
		auth:                   cfg.Auth,
		authProviders:          authProviders,
		serverAuthProvider:     serverAuthProvider,
		authorization:          cfg.Authorization,
		auditSink:              cfg.AuditSink,
		users:                  users,
		externalCredentials:    externalCredentials,
		managedSubjects:        managedSubjects,
		agent:                  cfg.Agent,
		agentRuns:              cfg.AgentManager,
		providers:              cfg.Providers,
		callerTokenIssuer:      cfg.CallerTokenIssuer,
		workflow:               cfg.Workflow,
		pluginRuntimes:         cfg.Runtimes,
		resolver:               resolver,
		authResolvers:          authResolvers,
		invoker:                cfg.Invoker,
		pluginInvoker:          pluginInvoker,
		apiRouteTimeout:        apiRouteTimeout,
		agentStreamHeartbeat:   agentStreamHeartbeat,
		defaultConnection:      cfg.DefaultConnection,
		catalogConnection:      cfg.CatalogConnection,
		mcpConnection:          cfg.MCPConnection,
		connectionAuth:         cfg.ConnectionAuth,
		manualConnectionAuth:   cfg.ManualConnectionAuth,
		pluginDefs:             cfg.AppDefs,
		agentDefs:              cfg.AgentDefs,
		noAuth:                 noAuth,
		publicBaseURL:          strings.TrimRight(cfg.PublicBaseURL, "/"),
		managementBaseURL:      strings.TrimRight(cfg.ManagementBaseURL, "/"),
		secureCookies:          cfg.SecureCookies,
		encryptor:              encryptor,
		sessionIssuer:          cfg.StateSecret,
		stateCodec:             stateCodec,
		now:                    now,
		readiness:              cfg.Readiness,
		meterProvider:          cfg.MeterProvider,
		prometheusMetrics:      cfg.PrometheusMetrics,
		mcpHandler:             cfg.MCPHandler,
		hostServiceRelayTokens: hostServiceRelayTokens,
		publicHostServices:     cfg.PublicHostServices,
		s3:                     cfg.S3,
		s3ObjectAccessURLs:     s3ObjectAccessURLs,
		egressProxyTokens:      egressProxyTokens,
		mountedHTTPBindings:    mountedHTTPBindings,
		mountedUIs:             mountedUIs,
		adminRoute:             adminRoute,
		adminUI:                adminUI,
		routeProfile:           cfg.RouteProfile,
		activateAppProviders:   cfg.ActivateAppProviders,
	}
	s.workflowSchedules = workflowmanager.New(workflowmanager.Config{
		Providers:         cfg.Providers,
		Workflow:          cfg.Workflow,
		Agent:             cfg.Agent,
		AgentManager:      cfg.AgentManager,
		Invoker:           cfg.Invoker,
		Audit:             cfg.AuditSink,
		DefaultConnection: cfg.DefaultConnection,
		CatalogConnection: cfg.CatalogConnection,
		MCPConnection:     cfg.MCPConnection,
		Now:               now,
	})
	if noAuth || serverAuthProvider == "none" {
		s.anonymousPrincipal = resolver.ResolveEmail(anonymousEmail)
	}

	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func withRequestTelemetryProviders(next http.Handler, meterProvider metric.MeterProvider, tracerProvider trace.TracerProvider) http.Handler {
	if meterProvider == nil && tracerProvider == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if meterProvider != nil {
			ctx = metricutil.WithMeterProvider(ctx, meterProvider)
		}
		if tracerProvider != nil {
			ctx = observability.WithTracerProvider(ctx, tracerProvider)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

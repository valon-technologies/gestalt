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
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/egressproxy"
	"github.com/valon-technologies/gestalt/server/services/hostserviceingress"
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

type MountedUI struct {
	Name                string
	Path                string
	AppName             string
	AuthorizationPolicy string
	AllowedRoles        []string
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
	Streaming      bool
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

type AppRuntimeState interface {
	WithRunningVersion(app string, fn func(version string) error) error
}

type Server struct {
	router                 chi.Router
	handler                http.Handler
	auth                   core.IdentityProvider
	authProviders          map[string]core.IdentityProvider
	serverAuthProvider     string
	authorization          core.AuthorizationProvider
	providerKinds          map[string]invocation.ProviderKind
	auditSink              core.AuditSink
	users                  *coredata.UserService
	externalCredentials    core.ExternalCredentialProvider
	managedSubjects        *coredata.ManagedSubjectService
	agent                  bootstrap.AgentControl
	workflowSchedules      *workflowmanager.Manager
	workflowProviderName   string
	agentRuns              agentmanager.Service
	agentContract          agentmanager.ContractService
	providers              *registry.ProviderMap[core.Provider]
	workflow               bootstrap.WorkflowControl
	pluginRuntimes         bootstrap.RuntimeInspector
	resolver               *principal.Resolver
	authResolvers          map[string]*principal.Resolver
	invoker                invocation.Invoker
	pluginInvoker          invocation.Invoker
	appPrompts             map[string][]appPromptInfo
	apiRouteTimeout        time.Duration
	defaultConnection      map[string]string
	catalogConnection      map[string]string
	mcpConnection          map[string]string
	connectionAuth         func() map[string]map[string]bootstrap.OAuthHandler
	manualConnectionAuth   func() map[string]map[string]bootstrap.ManualTokenExchanger
	pluginDefs             map[string]*config.ProviderEntry
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
	publicGRPCHandler      http.Handler
	publicRESTHandler      http.Handler
	frpsHandler            http.Handler
	frpsConnectHandler     http.Handler
	tunnelResolver         *tunnelProviderResolver
	publicGatewayConn      *publicrpc.InProcessConn
	publicHostServices     *runtimehost.PublicHostServiceRegistry
	s3                     map[string]s3sdk.S3
	s3ObjectAccessURLs     *s3.ObjectAccessURLManager
	egressProxyTokens      *egressproxy.TokenManager
	mountedHTTPBindings    []MountedHTTPBinding
	mountedUIs             []MountedUI
	adminRoute             AdminRouteConfig
	adminUI                http.Handler
	appRegistries          map[string]config.AppRegistryConfig
	appRegistryReader      *appregistry.RegistryReader
	appRegistryInstaller   *appregistry.Installer
	appFleetProjector      *appregistry.FleetProjector
	appVersionChanges      *coredata.AppVersionChangeRequestService
	gestaltdSourceVersions *coredata.GestaltdSourceVersionService
	instanceHeartbeats     *coredata.GestaltdInstanceHeartbeatService
	appRollouts            *coredata.AppRolloutService
	appMaterializations    *coredata.AppInstanceMaterializationService
	autoDeploySettings     *coredata.AutoDeploySettingsService
	appRolloutOutcomes     *coredata.AppVersionRolloutOutcomeService
	recoveryObservations   *coredata.AppVersionRecoveryObservationService
	appAutoDeployNotify    func(string)
	artifactsDir           string
	sourceVersion          string
	appRegistryRolloutMode config.AppRegistryRolloutMode
	appRuntimeState        AppRuntimeState
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
	Auth                    core.IdentityProvider
	SelectedAuthProvider    string
	AuthProviders           map[string]core.IdentityProvider
	Authorization           core.AuthorizationProvider
	ProviderKinds           map[string]invocation.ProviderKind
	AuditSink               core.AuditSink
	Services                *coredata.Services
	Providers               *registry.ProviderMap[core.Provider]
	Agent                   bootstrap.AgentControl
	AgentManager            agentmanager.Service
	Workflow                bootstrap.WorkflowControl
	Runtimes                bootstrap.RuntimeInspector
	Invoker                 invocation.Invoker
	AppInvocation           invocation.Invoker
	DefaultConnection       map[string]string
	CatalogConnection       map[string]string
	MCPConnection           map[string]string
	ConnectionAuth          func() map[string]map[string]bootstrap.OAuthHandler
	ManualConnectionAuth    func() map[string]map[string]bootstrap.ManualTokenExchanger
	AppDefs                 map[string]*config.ProviderEntry
	PublicBaseURL           string
	ManagementBaseURL       string
	SecureCookies           bool
	StateSecret             []byte
	APIRouteTimeout         time.Duration
	Now                     func() time.Time
	Readiness               ReadinessChecker
	PrometheusMetrics       http.Handler
	MCPHandler              http.Handler
	PublicHostServices      *runtimehost.PublicHostServiceRegistry
	PublicGatewayTransport  *providergateway.ProviderGatewayTransport
	S3                      map[string]s3sdk.S3
	MountedUIs              []MountedUI
	DevHandlerResolver      func(name string) http.Handler
	Admin                   AdminRouteConfig
	AdminUI                 http.Handler
	BuiltinAdminUI          *BuiltinAdminUIOptions
	AppRegistries           map[string]config.AppRegistryConfig
	AppRegistryReader       *appregistry.RegistryReader
	AppFleetProjector       *appregistry.FleetProjector
	AppRegistryHeartbeatTTL time.Duration
	AppRegistryRolloutMode  config.AppRegistryRolloutMode
	ArtifactsDir            string
	GestaltdVersion         string
	SourceVersion           string
	AppRuntimeState         AppRuntimeState
	RouteProfile            RouteProfile
	MeterProvider           metric.MeterProvider
	TracerProvider          trace.TracerProvider
	ActivateAppProviders    func(context.Context)
	IndexedDB               indexeddb.IndexedDB
	RemoteManagement        proto.RemoteManagementServer
	FrpsHandler             http.Handler
	FrpsConnectHandler      http.Handler
	TunnelResolver          TunnelResolverConfig
	// AppAutoDeployNotify requests prompt auto-deploy reconciliation for an app.
	AppAutoDeployNotify func(app string)
}

func New(cfg Config) (*Server, error) {
	if cfg.Invoker == nil {
		return nil, fmt.Errorf("invoker is required")
	}
	rootPrompts, err := config.RootAppPrompts(cfg.AppDefs)
	if err != nil {
		return nil, fmt.Errorf("resolve root app prompts: %w", err)
	}
	appPrompts := make(map[string][]appPromptInfo, len(rootPrompts))
	for appName, prompts := range rootPrompts {
		infos := make([]appPromptInfo, 0, len(prompts))
		for _, prompt := range prompts {
			infos = append(infos, appPromptInfo{ID: prompt.ID, Text: prompt.Text})
		}
		appPrompts[appName] = infos
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
	mountedUIs := append([]MountedUI(nil), cfg.MountedUIs...)
	appStatics, err := mountedAppStaticsFromEntries(cfg.AppDefs, cfg.Providers, cfg.ArtifactsDir, cfg.AppRuntimeState, cfg.DevHandlerResolver)
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
		hostServiceRelayTokens.SetCapabilityIngressDecorator(hostserviceingress.ApplyCapability)
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
	tunnelResolver := newTunnelProviderResolver(cfg.TunnelResolver)
	if tunnelResolver != nil && cfg.Providers != nil {
		cfg.Providers.SetRemoteResolver(tunnelResolver)
	}
	fleetProjector := cfg.AppFleetProjector
	if fleetProjector == nil {
		heartbeatTTL := cfg.AppRegistryHeartbeatTTL
		if heartbeatTTL <= 0 {
			heartbeatTTL = config.DefaultAppRegistryHeartbeatTTL
		}
		fleetProjector = &appregistry.FleetProjector{
			ChangeRequests: cfg.Services.AppVersionChangeRequests,
			SourceVersions: cfg.Services.GestaltdSourceVersionState,
			Heartbeats:     cfg.Services.GestaltdInstanceHeartbeats,
			Rollouts:       cfg.Services.AppRollouts,
			HeartbeatTTL:   heartbeatTTL,
			Now:            now,
		}
	}
	s := &Server{
		router:                 router,
		handler:                withRequestTelemetryProviders(otelhttp.NewHandler(router, "gestaltd", otelOptions...), cfg.MeterProvider, cfg.TracerProvider),
		auth:                   cfg.Auth,
		authProviders:          authProviders,
		serverAuthProvider:     serverAuthProvider,
		authorization:          cfg.Authorization,
		providerKinds:          cfg.ProviderKinds,
		auditSink:              cfg.AuditSink,
		users:                  users,
		externalCredentials:    externalCredentials,
		managedSubjects:        managedSubjects,
		agent:                  cfg.Agent,
		agentRuns:              cfg.AgentManager,
		providers:              cfg.Providers,
		tunnelResolver:         tunnelResolver,
		workflow:               cfg.Workflow,
		pluginRuntimes:         cfg.Runtimes,
		resolver:               resolver,
		authResolvers:          authResolvers,
		invoker:                cfg.Invoker,
		pluginInvoker:          pluginInvoker,
		appPrompts:             appPrompts,
		apiRouteTimeout:        apiRouteTimeout,
		defaultConnection:      cfg.DefaultConnection,
		catalogConnection:      cfg.CatalogConnection,
		mcpConnection:          cfg.MCPConnection,
		connectionAuth:         cfg.ConnectionAuth,
		manualConnectionAuth:   cfg.ManualConnectionAuth,
		pluginDefs:             cfg.AppDefs,
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
		appRegistries:          cloneAppRegistryConfig(cfg.AppRegistries),
		appRegistryReader:      cfg.AppRegistryReader,
		appRegistryInstaller:   newAppRegistryInstaller(cfg),
		appFleetProjector:      fleetProjector,
		appVersionChanges:      cfg.Services.AppVersionChangeRequests,
		gestaltdSourceVersions: cfg.Services.GestaltdSourceVersionState,
		instanceHeartbeats:     cfg.Services.GestaltdInstanceHeartbeats,
		appRollouts:            cfg.Services.AppRollouts,
		appMaterializations:    cfg.Services.AppInstanceMaterializations,
		autoDeploySettings:     cfg.Services.AutoDeploySettings,
		appRolloutOutcomes:     cfg.Services.AppVersionRolloutOutcomes,
		recoveryObservations:   cfg.Services.AppVersionRecoveryObservations,
		appAutoDeployNotify:    cfg.AppAutoDeployNotify,
		artifactsDir:           strings.TrimSpace(cfg.ArtifactsDir),
		sourceVersion:          strings.TrimSpace(cfg.SourceVersion),
		appRegistryRolloutMode: cfg.AppRegistryRolloutMode,
		appRuntimeState:        cfg.AppRuntimeState,
		routeProfile:           cfg.RouteProfile,
		activateAppProviders:   cfg.ActivateAppProviders,
	}
	if contract, ok := cfg.AgentManager.(agentmanager.ContractService); ok {
		s.agentContract = contract
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
	if cfg.Workflow != nil {
		s.workflowProviderName = strings.TrimSpace(cfg.Workflow.DefaultProviderName())
	}
	if cfg.RouteProfile != RouteProfileManagement {
		conn, restHandler, err := buildPublicGateway(publicGRPCConfig{
			Transport:           cfg.PublicGatewayTransport,
			Invoker:             cfg.Invoker,
			AgentManager:        cfg.AgentManager,
			WorkflowManager:     s.workflowSchedules,
			Authentication:      cfg.Auth,
			Authorization:       cfg.Authorization,
			IndexedDB:           cfg.IndexedDB,
			ExternalCredentials: externalCredentials,
			RemoteManagement:    cfg.RemoteManagement,
		})
		if err != nil {
			return nil, err
		}
		s.publicGatewayConn = conn
		if conn != nil && conn.Server != nil {
			s.publicGRPCHandler = http.HandlerFunc(conn.Server.ServeHTTP)
		}
		s.publicRESTHandler = restHandler
	}
	s.frpsHandler = cfg.FrpsHandler
	s.frpsConnectHandler = cfg.FrpsConnectHandler
	if noAuth || serverAuthProvider == "none" {
		s.anonymousPrincipal = resolver.ResolveEmail(anonymousEmail)
	}

	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// Close releases in-process public gateway resources.
func (s *Server) Close() {
	if s == nil {
		return
	}
	if s.publicGatewayConn != nil {
		s.publicGatewayConn.Close()
		s.publicGatewayConn = nil
	}
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

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

// OAuthHandler covers every OAuth method needed by the server (start, exchange,
// refresh) and the broker (refresh). mcpoauth.Handler satisfies this directly;
// use WrapUpstreamHandler to adapt an oauth.UpstreamHandler.
type OAuthHandler interface {
	AuthorizationURL(state string, scopes []string) string
	StartOAuth(state string, scopes []string) (authURL string, verifier string)
	StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error)
	ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error)
	RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	AuthorizationBaseURL() string
	TokenURL() string
}

// upstreamHandlerAdapter wraps an oauth.UpstreamHandler to satisfy OAuthHandler.
type upstreamHandlerAdapter struct {
	*oauth.UpstreamHandler
}

// WrapUpstreamHandler adapts an oauth.UpstreamHandler to the OAuthHandler
// interface. The adapter maps StartOAuth to AuthorizationURLWithPKCE and
// ExchangeCodeWithVerifier to ExchangeCode with option injection.
func WrapUpstreamHandler(h *oauth.UpstreamHandler) OAuthHandler {
	return &upstreamHandlerAdapter{UpstreamHandler: h}
}

func (a *upstreamHandlerAdapter) AuthorizationURL(state string, scopes []string) string {
	url, _ := a.AuthorizationURLWithPKCE(state, scopes)
	return url
}

func (a *upstreamHandlerAdapter) StartOAuth(state string, scopes []string) (string, string) {
	return a.AuthorizationURLWithPKCE(state, scopes)
}

func (a *upstreamHandlerAdapter) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	return a.AuthorizationURLWithOverride(authBaseURL, state, scopes)
}

func (a *upstreamHandlerAdapter) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return a.UpstreamHandler.ExchangeCode(ctx, code)
}

func (a *upstreamHandlerAdapter) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	var opts []oauth.ExchangeOption
	if verifier != "" {
		opts = append(opts, oauth.WithPKCEVerifier(verifier))
	}
	opts = append(opts, extraOpts...)
	return a.UpstreamHandler.ExchangeCode(ctx, code, opts...)
}

// ProviderBuildResult carries the constructed provider and an OAuth handler
// for each named connection that uses oauth2 or mcp_oauth auth.
type ProviderBuildResult struct {
	Provider       core.Provider
	ConnectionAuth map[string]OAuthHandler
}

type providerMetadata struct {
	displayName string
	description string
	iconSVG     string
}

func resolveProviderMetadata(entry *config.ProviderEntry) providerMetadata {
	meta := providerMetadata{
		displayName: entry.DisplayName,
		description: entry.Description,
	}
	if entry.IconFile == "" {
		return meta
	}

	svg, err := provider.ReadIconFile(entry.IconFile)
	if err != nil {
		slog.Warn("could not read icon_file", "path", entry.IconFile, "error", err)
		return meta
	}
	meta.iconSVG = svg
	return meta
}

func (m providerMetadata) displayNameOr(v string) string {
	if m.displayName != "" {
		return m.displayName
	}
	return v
}

func (m providerMetadata) descriptionOr(v string) string {
	if m.description != "" {
		return m.description
	}
	return v
}

type Deps struct {
	EncryptionKey         []byte
	BaseURL               string
	SecretManager         core.SecretManager
	Services              *coredata.Services
	SelectedIndexedDBName string
	IndexedDBs            map[string]indexeddb.IndexedDB
	IndexedDBDefs         map[string]*config.ProviderEntry
	IndexedDBFactory      IndexedDBFactory
	CacheDefs             map[string]*config.ProviderEntry
	CacheFactory          CacheFactory
	S3                    map[string]s3store.Client
	WorkflowRuntime       *workflowRuntime
	WorkflowManager       workflowmanager.Service
	Egress                EgressDeps
	PluginInvoker         invocation.Invoker
	PluginRuntime         pluginruntime.Provider
	PluginRuntimeRegistry *pluginRuntimeRegistry
}

type AuthFactory func(node yaml.Node, deps Deps) (core.AuthenticationProvider, error)
type AuthorizationFactory func(node yaml.Node, hostServices []providerhost.HostService, deps Deps) (core.AuthorizationProvider, error)
type SecretManagerFactory func(node yaml.Node) (core.SecretManager, error)
type IndexedDBFactory func(node yaml.Node) (indexeddb.IndexedDB, error)
type CacheFactory func(node yaml.Node) (corecache.Cache, error)
type S3Factory func(node yaml.Node) (s3store.Client, error)
type WorkflowFactory func(ctx context.Context, name string, node yaml.Node, hostServices []providerhost.HostService, deps Deps) (coreworkflow.Provider, error)
type TelemetryFactory func(node yaml.Node) (core.TelemetryProvider, error)
type AuditFactory func(ctx context.Context, cfg config.ProviderEntry, telemetry core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error)

type FactoryRegistry struct {
	Auth           AuthFactory
	Authorization  AuthorizationFactory
	Secrets        map[string]SecretManagerFactory
	IndexedDB      IndexedDBFactory
	Cache          CacheFactory
	PluginRuntimes map[config.RuntimeProviderDriver]pluginRuntimeFactory
	S3             S3Factory
	Workflow       WorkflowFactory
	Telemetry      map[string]TelemetryFactory
	Audit          AuditFactory
	Builtins       []core.Provider
}

func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{
		Secrets: make(map[string]SecretManagerFactory),
		PluginRuntimes: map[config.RuntimeProviderDriver]pluginRuntimeFactory{
			config.RuntimeProviderDriverLocal: func(context.Context, string, *config.RuntimeProviderEntry, Deps) (pluginruntime.Provider, error) {
				return pluginruntime.NewLocalProvider(), nil
			},
		},
		Telemetry: make(map[string]TelemetryFactory),
	}
}

type Result struct {
	Auth                  core.AuthenticationProvider
	SelectedAuthProvider  string
	AuthProviders         map[string]core.AuthenticationProvider
	AuthorizationProvider core.AuthorizationProvider
	Services              *coredata.Services
	ExtraIndexedDBs       []indexeddb.IndexedDB
	ExtraS3s              []s3store.Client
	ExtraWorkflows        []coreworkflow.Provider
	Providers             *registry.ProviderMap[core.Provider]
	WorkflowControl       WorkflowControl
	ProvidersReady        <-chan struct{}
	Authorizer            authorization.RuntimeAuthorizer
	ConnectionAuth        func() map[string]map[string]OAuthHandler
	Invoker               invocation.Invoker
	CapabilityLister      invocation.CapabilityLister
	AuditSink             core.AuditSink
	SecretManager         core.SecretManager
	Telemetry             core.TelemetryProvider

	pluginRuntimeRegistry *pluginRuntimeRegistry
	auditClose            func(context.Context) error
	mu                    sync.Mutex
	closed                bool
}

func (r *Result) Start(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("bootstrap result already closed")
	}
	if r.Authorizer != nil {
		if err := r.Authorizer.Start(ctx); err != nil {
			return err
		}
	}
	if err := syncProviderBackedHumanCanonicalState(ctx, r.Services, r.Authorizer, r.AuthorizationProvider); err != nil {
		return err
	}
	return nil
}

func (r *Result) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if r.ProvidersReady != nil {
		<-r.ProvidersReady
	}

	var errs []error
	authCloseErr := closeAuth(r.Auth)
	if len(r.AuthProviders) != 0 {
		authCloseErr = closeAuthProviders(r.AuthProviders)
	}
	errs = append(errs,
		closeAuthorizer(r.Authorizer),
		authCloseErr,
		closeAuthorizationProvider(r.AuthorizationProvider),
		CloseProviders(r.Providers),
		r.Services.Close(),
		closeIndexedDBs(r.ExtraIndexedDBs...),
		closeS3s(r.ExtraS3s...),
		closeWorkflows(r.ExtraWorkflows...),
		closeSecretManager(r.SecretManager),
		closePluginRuntimeRegistry(r.pluginRuntimeRegistry),
	)
	if r.auditClose != nil {
		errs = append(errs, r.auditClose(ctx))
	}
	if r.Telemetry != nil {
		errs = append(errs, r.Telemetry.Shutdown(ctx))
	}
	r.closed = true
	return errors.Join(errs...)
}

func closeIfPossible(values ...any) {
	for _, value := range values {
		if c, ok := value.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
}

func closeIndexedDBs(stores ...indexeddb.IndexedDB) error {
	var errs []error
	for _, store := range stores {
		if store == nil {
			continue
		}
		if err := store.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func closeS3s(clients ...s3store.Client) error {
	var errs []error
	for _, client := range clients {
		if client == nil {
			continue
		}
		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func closeWorkflows(providers ...coreworkflow.Provider) error {
	var errs []error
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		if err := provider.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type workflowProviderWithCleanup struct {
	coreworkflow.Provider
	cleanup func()
}

func (p *workflowProviderWithCleanup) Close() error {
	var errs []error
	if p != nil && p.Provider != nil {
		if err := p.Provider.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if p != nil && p.cleanup != nil {
		p.cleanup()
	}
	return errors.Join(errs...)
}

type preparedCore struct {
	Auth                  core.AuthenticationProvider
	SelectedAuthProvider  string
	AuthProviders         map[string]core.AuthenticationProvider
	AuthorizationProvider core.AuthorizationProvider
	Services              *coredata.Services
	ExtraIndexedDBs       []indexeddb.IndexedDB
	ExtraS3s              []s3store.Client
	SecretManager         core.SecretManager
	Telemetry             core.TelemetryProvider
	Deps                  Deps

	pluginRuntimeRegistry *pluginRuntimeRegistry
}

type configSecretManagers struct {
	ctx       context.Context
	cfg       *config.Config
	factories *FactoryRegistry
	managers  map[string]core.SecretManager
}

func newConfigSecretManagers(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) (*configSecretManagers, error) {
	referenced, err := config.ReferencedConfigSecretProviders(cfg)
	if err != nil {
		return nil, err
	}
	if len(referenced) == 0 {
		return nil, nil
	}
	return &configSecretManagers{
		ctx:       ctx,
		cfg:       cfg,
		factories: factories,
		managers:  make(map[string]core.SecretManager, len(referenced)),
	}, nil
}

func (r *configSecretManagers) resolve(ref config.SecretRef) (string, error) {
	sm, err := r.manager(ref.Provider)
	if err != nil {
		return "", err
	}
	value, err := sm.GetSecret(r.ctx, ref.Name)
	if err != nil {
		return "", fmt.Errorf("provider %q: %w", ref.Provider, err)
	}
	return value, nil
}

func (r *configSecretManagers) manager(name string) (core.SecretManager, error) {
	if sm, ok := r.managers[name]; ok {
		return sm, nil
	}
	entry := r.cfg.Providers.Secrets[name]
	if entry == nil {
		return nil, fmt.Errorf("config validation: secret refs reference unknown secrets provider %q", name)
	}
	sm, err := buildNamedSecretManager(name, entry, r.factories)
	if err != nil {
		return nil, err
	}
	r.managers[name] = sm
	return sm, nil
}

func (r *configSecretManagers) Close() error {
	if r == nil {
		return nil
	}
	var errs []error
	for _, sm := range r.managers {
		errs = append(errs, closeSecretManager(sm))
	}
	return errors.Join(errs...)
}

// ResolveConfigSecrets resolves structured config secret refs using their
// referenced secrets providers, then closes the temporary secret managers.
func ResolveConfigSecrets(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) error {
	if err := config.CanonicalizeStructure(cfg); err != nil {
		return err
	}
	resolver, err := newConfigSecretManagers(ctx, cfg, factories)
	if err != nil {
		return err
	}
	if resolver == nil {
		return nil
	}
	defer func() { _ = resolver.Close() }()
	resolveValue := func(val string) (string, error) {
		ref, ok, err := config.ParseSecretRefTransport(val)
		if err != nil {
			return "", err
		}
		if !ok {
			if config.IsLegacySecretRefString(val) {
				return "", fmt.Errorf("legacy secret:// syntax should have been rejected during config load")
			}
			return val, nil
		}
		resolved, err := resolver.resolve(ref)
		if err != nil {
			var secretErr *core.SecretResolutionError
			if errors.As(err, &secretErr) {
				return "", err
			}
			return "", &core.SecretResolutionError{
				Name: ref.Name,
				Err:  err,
			}
		}
		if resolved == "" {
			return "", &core.SecretResolutionError{Name: ref.Name, Err: fmt.Errorf("resolved to empty value")}
		}
		return resolved, nil
	}
	if err := config.TransformConfigStringFields(cfg, resolveValue); err != nil {
		return err
	}
	return config.CanonicalizeStructure(cfg)
}

func prepareCore(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, requireEncryptionKey bool) (*preparedCore, error) {
	if err := ResolveConfigSecrets(ctx, cfg, factories); err != nil {
		return nil, err
	}
	sm, err := buildRuntimeSecretManager(cfg, factories)
	if err != nil {
		return nil, err
	}
	closeSM := true
	defer func() {
		if closeSM {
			_ = closeSecretManager(sm)
		}
	}()

	tp, err := buildTelemetry(cfg, factories)
	if err != nil {
		return nil, err
	}
	shutdownTelemetry := true
	defer func() {
		if shutdownTelemetry {
			_ = tp.Shutdown(context.Background())
		}
	}()

	encKey := crypto.DeriveKey(cfg.Server.EncryptionKey)
	if requireEncryptionKey && encKey == nil {
		return nil, fmt.Errorf("bootstrap: server.encryption_key is required")
	}

	deps := Deps{
		EncryptionKey: encKey,
		BaseURL:       cfg.Server.BaseURL,
		SecretManager: sm,
	}
	workflowRuntime, err := newWorkflowRuntime(cfg)
	if err != nil {
		return nil, err
	}
	workflowRuntime.InitProviderPlaceholders(cfg.Providers.Workflow)
	deps.WorkflowRuntime = workflowRuntime

	selectedAuthName, authProviders, err := buildAuthProviders(cfg, factories, deps)
	if err != nil {
		return nil, err
	}
	auth := authProviders[selectedAuthName]
	var authzProvider core.AuthorizationProvider
	closeAuthorizationOnError := true
	defer func() {
		if closeAuthorizationOnError {
			_ = closeAuthorizationProvider(authzProvider)
		}
	}()

	selectedIndexedDBName, def, err := cfg.SelectedIndexedDBProvider()
	if err != nil {
		return nil, err
	}
	if selectedIndexedDBName == "" || def == nil {
		return nil, fmt.Errorf("bootstrap: datastore resource name is required")
	}
	enc, encErr := crypto.NewAESGCM(encKey)
	if encErr != nil {
		return nil, fmt.Errorf("bootstrap: create encryptor: %w", encErr)
	}
	store, storeErr := buildIndexedDB(def, factories)
	if storeErr != nil {
		return nil, fmt.Errorf("bootstrap: system indexeddb from resource %q: %w", selectedIndexedDBName, storeErr)
	}
	store = metricutil.InstrumentIndexedDB(store, selectedIndexedDBName)
	svc, svcErr := coredata.New(store, enc)
	if svcErr != nil {
		_ = store.Close()
		return nil, fmt.Errorf("bootstrap: system indexeddb from resource %q: %w", selectedIndexedDBName, svcErr)
	}
	hostIndexedDBs := map[string]indexeddb.IndexedDB{selectedIndexedDBName: store}
	var extraIndexedDBs []indexeddb.IndexedDB
	for name, entry := range cfg.Providers.IndexedDB {
		if name == selectedIndexedDBName || entry == nil {
			continue
		}
		ds, err := buildIndexedDB(entry, factories)
		if err != nil {
			_ = svc.Close()
			_ = closeIndexedDBs(extraIndexedDBs...)
			return nil, fmt.Errorf("bootstrap: indexeddb from resource %q: %w", name, err)
		}
		ds = metricutil.InstrumentIndexedDB(ds, name)
		hostIndexedDBs[name] = ds
		extraIndexedDBs = append(extraIndexedDBs, ds)
	}
	closeSvc := true
	closeExtraStores := true
	defer func() {
		if closeSvc {
			_ = svc.Close()
		}
		if closeExtraStores {
			_ = closeIndexedDBs(extraIndexedDBs...)
		}
	}()

	hostS3s := make(map[string]s3store.Client, len(cfg.Providers.S3))
	var extraS3s []s3store.Client
	for name, entry := range cfg.Providers.S3 {
		if entry == nil {
			continue
		}
		client, err := buildS3(name, entry, factories)
		if err != nil {
			_ = closeS3s(extraS3s...)
			return nil, fmt.Errorf("bootstrap: s3 from resource %q: %w", name, err)
		}
		hostS3s[name] = client
		extraS3s = append(extraS3s, client)
	}
	closeExtraS3s := true
	defer func() {
		if closeExtraS3s {
			_ = closeS3s(extraS3s...)
		}
	}()

	deps.Egress = newEgressDeps(cfg)
	deps.Services = svc
	deps.IndexedDBs = hostIndexedDBs
	deps.SelectedIndexedDBName = selectedIndexedDBName
	deps.IndexedDBDefs = cfg.Providers.IndexedDB
	deps.IndexedDBFactory = factories.IndexedDB
	deps.CacheDefs = cfg.Providers.Cache
	deps.CacheFactory = factories.Cache
	deps.S3 = hostS3s

	authzProvider, err = buildAuthorization(cfg, factories, deps)
	if err != nil {
		_ = closeAuthProviders(authProviders)
		return nil, err
	}
	runtimeRegistry := newPluginRuntimeRegistry(cfg, factories.PluginRuntimes, deps)
	deps.PluginRuntimeRegistry = runtimeRegistry

	closeSM = false
	shutdownTelemetry = false
	closeSvc = false
	closeExtraStores = false
	closeExtraS3s = false
	closeAuthorizationOnError = false
	return &preparedCore{
		Auth:                  auth,
		SelectedAuthProvider:  selectedAuthName,
		AuthProviders:         authProviders,
		AuthorizationProvider: authzProvider,
		Services:              svc,
		ExtraIndexedDBs:       extraIndexedDBs,
		ExtraS3s:              extraS3s,
		SecretManager:         sm,
		Telemetry:             tp,
		Deps:                  deps,
		pluginRuntimeRegistry: runtimeRegistry,
	}, nil
}

func (p *preparedCore) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}

	var errs []error
	authCloseErr := closeAuth(p.Auth)
	if len(p.AuthProviders) != 0 {
		authCloseErr = closeAuthProviders(p.AuthProviders)
	}
	errs = append(errs,
		authCloseErr,
		closeAuthorizationProvider(p.AuthorizationProvider),
		p.Services.Close(),
		closeIndexedDBs(p.ExtraIndexedDBs...),
		closeS3s(p.ExtraS3s...),
		closeSecretManager(p.SecretManager),
		closePluginRuntimeRegistry(p.pluginRuntimeRegistry),
	)
	if p.Telemetry != nil {
		errs = append(errs, p.Telemetry.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

func Bootstrap(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) (*Result, error) {
	prepared, err := prepareCore(ctx, cfg, factories, true)
	if err != nil {
		return nil, err
	}
	closeCore := true
	defer func() {
		if closeCore {
			_ = prepared.Close(context.Background())
		}
	}()

	pluginInvoker := newLazyInvoker()
	workflowManager := newLazyWorkflowManager()
	prepared.Deps.PluginInvoker = pluginInvoker
	prepared.Deps.WorkflowManager = workflowManager

	providers, providersReady, connAuthResolver, err := buildProviders(ctx, cfg, factories, prepared.Deps)
	if err != nil {
		return nil, err
	}
	closeProviders := true
	defer func() {
		if closeProviders {
			<-providersReady
			_ = CloseProviders(providers)
		}
	}()

	connMaps, err := BuildConnectionMaps(cfg)
	if err != nil {
		prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
		return nil, err
	}
	baseAuthz, err := authorization.New(cfg.Authorization, cfg.Plugins, providers, connMaps.DefaultConnection)
	if err != nil {
		prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
		return nil, err
	}
	closeAuthz := true
	defer func() {
		if closeAuthz {
			_ = baseAuthz.Close()
		}
	}()
	var authz authorization.RuntimeAuthorizer = baseAuthz
	if prepared.AuthorizationProvider != nil {
		authz, err = authorization.NewProviderBacked(baseAuthz, prepared.AuthorizationProvider)
		if err != nil {
			prepared.Deps.WorkflowRuntime.FailPendingProviders(err)
			return nil, err
		}
	}
	prepared.Deps.WorkflowRuntime.SetExecutionRefs(prepared.Services.WorkflowExecutionRefs)
	sharedInvoker := invocation.NewBroker(providers, prepared.Services.Users, prepared.Services.Tokens,
		invocation.WithAuthorizer(authz),
		invocation.WithConnectionMapper(invocation.ConnectionMap(connMaps.APIConnection)),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(connMaps.MCPConnection)),
		invocation.WithConnectionAuth(lazyRefreshers(providersReady, connAuthResolver)),
	)
	prepared.Deps.WorkflowRuntime.SetInvoker(sharedInvoker)
	workflowManager.SetTarget(workflowmanager.New(workflowmanager.Config{
		Providers:             providers,
		Workflow:              prepared.Deps.WorkflowRuntime,
		WorkflowExecutionRefs: prepared.Services.WorkflowExecutionRefs,
		Invoker:               sharedInvoker,
		Authorizer:            authz,
		DefaultConnection:     connMaps.DefaultConnection,
		CatalogConnection:     connMaps.APIConnection,
	}))
	extraWorkflows, err := buildWorkflows(ctx, cfg, factories, prepared.Deps)
	if err != nil {
		return nil, err
	}
	closeWorkflowsOnError := true
	defer func() {
		if closeWorkflowsOnError {
			_ = closeWorkflows(extraWorkflows...)
		}
	}()
	audit, auditClose, err := buildAuditSink(ctx, cfg, factories, prepared.Telemetry)
	if err != nil {
		return nil, err
	}
	closeAudit := true
	defer func() {
		if closeAudit && auditClose != nil {
			_ = auditClose(context.Background())
		}
	}()
	pluginInvoker.SetTarget(invocation.NewGuarded(sharedInvoker, nil, "plugin", audit, invocation.WithoutRateLimit()))
	if err := reconcileWorkflowConfigSchedules(ctx, cfg, prepared.Deps.WorkflowRuntime, prepared.Services.DB); err != nil {
		return nil, err
	}
	if err := reconcileWorkflowConfigEventTriggers(ctx, cfg, prepared.Deps.WorkflowRuntime, prepared.Services.DB); err != nil {
		return nil, err
	}

	closeProviders = false
	closeCore = false
	closeAudit = false
	closeAuthz = false
	closeWorkflowsOnError = false
	return &Result{
		Auth:                  prepared.Auth,
		SelectedAuthProvider:  prepared.SelectedAuthProvider,
		AuthProviders:         prepared.AuthProviders,
		AuthorizationProvider: prepared.AuthorizationProvider,
		Services:              prepared.Services,
		ExtraIndexedDBs:       prepared.ExtraIndexedDBs,
		ExtraS3s:              prepared.ExtraS3s,
		ExtraWorkflows:        extraWorkflows,
		Providers:             providers,
		WorkflowControl:       prepared.Deps.WorkflowRuntime,
		ProvidersReady:        providersReady,
		Authorizer:            authz,
		ConnectionAuth:        connAuthResolver,
		Invoker:               sharedInvoker,
		CapabilityLister:      sharedInvoker,
		AuditSink:             audit,
		SecretManager:         prepared.SecretManager,
		Telemetry:             prepared.Telemetry,
		pluginRuntimeRegistry: prepared.pluginRuntimeRegistry,
		auditClose:            auditClose,
	}, nil
}

func buildWorkflows(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) ([]coreworkflow.Provider, error) {
	var extraWorkflows []coreworkflow.Provider
	for name, entry := range cfg.Providers.Workflow {
		if entry == nil {
			continue
		}
		value, err := buildWorkflow(ctx, name, entry, factories, deps)
		if err != nil {
			if deps.WorkflowRuntime != nil {
				deps.WorkflowRuntime.FailProvider(name, err)
				deps.WorkflowRuntime.FailPendingProviders(err)
			}
			_ = closeWorkflows(extraWorkflows...)
			return nil, fmt.Errorf("bootstrap: workflow from resource %q: %w", name, err)
		}
		if deps.WorkflowRuntime != nil {
			deps.WorkflowRuntime.PublishProvider(name, value)
		}
		extraWorkflows = append(extraWorkflows, value)
	}
	return extraWorkflows, nil
}

func buildTelemetry(cfg *config.Config, factories *FactoryRegistry) (core.TelemetryProvider, error) {
	_, tel, err := cfg.SelectedTelemetryProvider()
	if err != nil {
		return nil, err
	}
	if tel != nil && !tel.Source.IsBuiltin() {
		return nil, fmt.Errorf("bootstrap: provider-based telemetry providers are not yet supported")
	}
	builtin := ""
	var configNode yaml.Node
	if tel != nil {
		builtin = tel.Source.Builtin
		configNode = tel.Config
	}
	factory, ok := factories.Telemetry[builtin]
	if !ok {
		return nil, fmt.Errorf("bootstrap: unknown telemetry provider %q", builtin)
	}
	tp, err := factory(configNode)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: telemetry provider %q: %w", builtin, err)
	}
	return tp, nil
}

func buildAuditSink(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, telemetry core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error) {
	_, audit, err := cfg.SelectedAuditProvider()
	if err != nil {
		return nil, nil, err
	}
	if audit != nil && !audit.Source.IsBuiltin() {
		return nil, nil, fmt.Errorf("bootstrap: provider-based audit providers are not yet supported")
	}
	builtin := ""
	if audit != nil {
		builtin = audit.Source.Builtin
	}
	if factories.Audit == nil {
		switch builtin {
		case "", "inherit":
			return invocation.NewLoggerAuditSink(telemetry.Logger()), nil, nil
		default:
			return nil, nil, fmt.Errorf("bootstrap: unknown audit provider %q", builtin)
		}
	}
	if audit == nil {
		audit = &config.ProviderEntry{}
	}
	sink, closeFn, err := factories.Audit(ctx, *audit, telemetry)
	if err != nil {
		return nil, nil, fmt.Errorf("bootstrap: audit provider %q: %w", builtin, err)
	}
	return sink, closeFn, nil
}

func buildRuntimeSecretManager(cfg *config.Config, factories *FactoryRegistry) (core.SecretManager, error) {
	name, secrets, err := cfg.SelectedSecretsProvider()
	if err != nil {
		return nil, err
	}
	return buildNamedSecretManager(name, secrets, factories)
}

func buildNamedSecretManager(name string, secrets *config.ProviderEntry, factories *FactoryRegistry) (core.SecretManager, error) {
	logicalName := name
	if logicalName == "" {
		logicalName = "secrets"
	}

	if secrets != nil && (secrets.HasRemoteSource() || secrets.HasLocalSource() || secrets.HasLocalReleaseSource()) {
		factory, ok := factories.Secrets["provider"]
		if !ok {
			return nil, fmt.Errorf("bootstrap: secrets provider factory is not registered")
		}
		node := secrets.Config
		if !config.IsComponentRuntimeConfigNode(node) {
			var err error
			node, err = config.BuildComponentRuntimeConfigNode(logicalName, "secrets", secrets, secrets.Config)
			if err != nil {
				return nil, fmt.Errorf("bootstrap: secrets provider %q: %w", logicalName, err)
			}
		}
		sm, err := factory(node)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: secrets provider %q: %w", logicalName, err)
		}
		return sm, nil
	}

	builtinName := ""
	var configNode yaml.Node
	if secrets != nil {
		builtinName = secrets.Source.Builtin
		configNode = secrets.Config
		if builtinName == "" {
			return nil, fmt.Errorf("bootstrap: secrets provider %q has no source", logicalName)
		}
	}
	if builtinName == "" {
		builtinName = "env"
	}
	factory, ok := factories.Secrets[builtinName]
	if !ok {
		if secrets != nil {
			return nil, fmt.Errorf("bootstrap: secrets provider %q references unknown builtin %q", logicalName, builtinName)
		}
		return nil, fmt.Errorf("bootstrap: unknown secrets provider %q", builtinName)
	}
	sm, err := factory(configNode)
	if err != nil {
		if secrets != nil {
			return nil, fmt.Errorf("bootstrap: secrets provider %q: %w", logicalName, err)
		}
		return nil, fmt.Errorf("bootstrap: secrets provider %q: %w", builtinName, err)
	}
	return sm, nil
}

func closeAuth(provider core.AuthenticationProvider) error {
	closer, ok := provider.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func closeAuthProviders(providers map[string]core.AuthenticationProvider) error {
	if len(providers) == 0 {
		return nil
	}
	var errs []error
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		errs = append(errs, closeAuth(provider))
	}
	return errors.Join(errs...)
}

func closeAuthorizer(authorizer authorization.RuntimeAuthorizer) error {
	if authorizer == nil {
		return nil
	}
	return authorizer.Close()
}

func closeAuthorizationProvider(provider core.AuthorizationProvider) error {
	closer, ok := provider.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func closeSecretManager(sm core.SecretManager) error {
	closer, ok := sm.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func buildAuthProviders(cfg *config.Config, factories *FactoryRegistry, deps Deps) (string, map[string]core.AuthenticationProvider, error) {
	selectedName, _, err := cfg.SelectedAuthenticationProvider()
	if err != nil {
		return "", nil, err
	}
	if len(cfg.Providers.Authentication) == 0 {
		return selectedName, nil, nil
	}
	if factories.Auth == nil {
		return "", nil, fmt.Errorf("bootstrap: authentication factory is not registered")
	}
	providers := make(map[string]core.AuthenticationProvider, len(cfg.Providers.Authentication))
	for name, authEntry := range cfg.Providers.Authentication {
		if authEntry == nil {
			continue
		}
		auth, err := buildNamedAuthProvider(name, authEntry, factories, deps)
		if err != nil {
			_ = closeAuthProviders(providers)
			return "", nil, err
		}
		providers[name] = auth
	}
	return selectedName, providers, nil
}

func buildNamedAuthProvider(name string, authEntry *config.ProviderEntry, factories *FactoryRegistry, deps Deps) (core.AuthenticationProvider, error) {
	if authEntry == nil {
		return nil, nil
	}
	node := authEntry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode(name, "authentication", authEntry, authEntry.Config)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: authentication provider %q: %w", name, err)
		}
	}
	auth, err := factories.Auth(node, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: authentication provider %q: %w", name, err)
	}
	return auth, nil
}

func buildAuthorization(cfg *config.Config, factories *FactoryRegistry, deps Deps) (core.AuthorizationProvider, error) {
	_, authzEntry, err := cfg.SelectedAuthorizationProvider()
	if err != nil {
		return nil, err
	}
	if authzEntry == nil {
		return nil, nil
	}
	if factories.Authorization == nil {
		return nil, fmt.Errorf("bootstrap: authorization factory is not registered")
	}
	node := authzEntry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode("authorization", "authorization", authzEntry, authzEntry.Config)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: authorization provider: %w", err)
		}
	}
	hostServices := buildHostIndexedDBHostServices(deps.SelectedIndexedDBName, deps.IndexedDBs)
	provider, err := factories.Authorization(node, hostServices, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: authorization provider: %w", err)
	}
	return provider, nil
}

func buildIndexedDB(entry *config.ProviderEntry, factories *FactoryRegistry) (indexeddb.IndexedDB, error) {
	if entry == nil {
		return nil, fmt.Errorf("datastore provider is required")
	}
	if factories.IndexedDB == nil {
		return nil, fmt.Errorf("datastore factory is not registered")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode("indexeddb", "indexeddb", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("datastore provider: %w", err)
		}
	}
	ds, err := factories.IndexedDB(node)
	if err != nil {
		return nil, fmt.Errorf("datastore provider: %w", err)
	}
	return ds, nil
}

func buildHostIndexedDBHostServices(selectedName string, indexeddbs map[string]indexeddb.IndexedDB) []providerhost.HostService {
	if len(indexeddbs) == 0 {
		return nil
	}

	hostServices := make([]providerhost.HostService, 0, len(indexeddbs)+1)
	if selected := indexeddbs[selectedName]; strings.TrimSpace(selectedName) != "" && selected != nil {
		hostServices = append(hostServices, indexedDBHostService(providerhost.DefaultIndexedDBSocketEnv, selectedName, selected))
	}

	for _, name := range slices.Sorted(maps.Keys(indexeddbs)) {
		ds := indexeddbs[name]
		if ds == nil {
			continue
		}
		hostServices = append(hostServices, indexedDBHostService(providerhost.IndexedDBSocketEnv(name), name, ds))
	}
	return hostServices
}

func indexedDBHostService(envVar, name string, ds indexeddb.IndexedDB) providerhost.HostService {
	return providerhost.HostService{
		EnvVar: envVar,
		Register: func(srv *grpc.Server) {
			proto.RegisterIndexedDBServer(srv, providerhost.NewIndexedDBServer(ds, name, providerhost.IndexedDBServerOptions{}))
		},
	}
}

func buildCache(entry *config.ProviderEntry, factories *FactoryRegistry) (corecache.Cache, error) {
	if entry == nil {
		return nil, fmt.Errorf("cache provider is required")
	}
	if factories.Cache == nil {
		return nil, fmt.Errorf("cache factory is not registered")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode("cache", "cache", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("cache provider: %w", err)
		}
	}
	value, err := factories.Cache(node)
	if err != nil {
		return nil, fmt.Errorf("cache provider: %w", err)
	}
	return value, nil
}

func buildS3(name string, entry *config.ProviderEntry, factories *FactoryRegistry) (s3store.Client, error) {
	if entry == nil {
		return nil, fmt.Errorf("s3 provider is required")
	}
	if factories.S3 == nil {
		return nil, fmt.Errorf("s3 factory is not registered")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode(name, "s3", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("s3 provider: %w", err)
		}
	}
	client, err := factories.S3(node)
	if err != nil {
		return nil, fmt.Errorf("s3 provider: %w", err)
	}
	return client, nil
}

func buildWorkflow(ctx context.Context, name string, entry *config.ProviderEntry, factories *FactoryRegistry, deps Deps) (coreworkflow.Provider, error) {
	if entry == nil {
		return nil, fmt.Errorf("workflow provider is required")
	}
	if factories.Workflow == nil {
		return nil, fmt.Errorf("workflow factory is not registered")
	}
	node := entry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode(name, "workflow", entry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("workflow provider: %w", err)
		}
	}
	hostServices := []providerhost.HostService{{
		EnvVar: providerhost.DefaultWorkflowHostSocketEnv,
		Register: func(srv *grpc.Server) {
			proto.RegisterWorkflowHostServer(srv, providerhost.NewWorkflowHostServer(name, deps.WorkflowRuntime.Invoke))
		},
	}}
	var cleanup func()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	effectiveIndexedDB, err := config.ResolveEffectiveWorkflowIndexedDB(name, entry, deps.IndexedDBDefs)
	if err != nil {
		return nil, fmt.Errorf("workflow provider: %w", err)
	}
	if effectiveIndexedDB.Enabled {
		indexedDBHostServices, indexedDBCleanup, err := buildWorkflowIndexedDBHostServices(name, effectiveIndexedDB, deps)
		if err != nil {
			return nil, fmt.Errorf("workflow provider: %w", err)
		}
		hostServices = append(hostServices, indexedDBHostServices...)
		cleanup = chainCleanup(cleanup, indexedDBCleanup)
	}
	provider, err := factories.Workflow(ctx, name, node, hostServices, deps)
	if err != nil {
		return nil, fmt.Errorf("workflow provider: %w", err)
	}
	if cleanup != nil {
		provider = &workflowProviderWithCleanup{
			Provider: provider,
			cleanup:  cleanup,
		}
		cleanup = nil
	}
	return provider, nil
}

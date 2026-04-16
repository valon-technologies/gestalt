package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	"github.com/valon-technologies/gestalt/server/internal/registry"
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
	Egress                EgressDeps
	PluginInvoker         invocation.Invoker
}

type AuthFactory func(node yaml.Node, deps Deps) (core.AuthProvider, error)
type SecretManagerFactory func(node yaml.Node) (core.SecretManager, error)
type IndexedDBFactory func(node yaml.Node) (indexeddb.IndexedDB, error)
type CacheFactory func(node yaml.Node) (corecache.Cache, error)
type S3Factory func(node yaml.Node) (s3store.Client, error)
type TelemetryFactory func(node yaml.Node) (core.TelemetryProvider, error)
type AuditFactory func(ctx context.Context, cfg config.ProviderEntry, telemetry core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error)

type FactoryRegistry struct {
	Auth      AuthFactory
	Secrets   map[string]SecretManagerFactory
	IndexedDB IndexedDBFactory
	Cache     CacheFactory
	S3        S3Factory
	Telemetry map[string]TelemetryFactory
	Audit     AuditFactory
	Builtins  []core.Provider
}

func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{
		Secrets:   make(map[string]SecretManagerFactory),
		Telemetry: make(map[string]TelemetryFactory),
	}
}

type Result struct {
	Auth             core.AuthProvider
	Services         *coredata.Services
	ExtraIndexedDBs  []indexeddb.IndexedDB
	ExtraS3s         []s3store.Client
	Providers        *registry.ProviderMap[core.Provider]
	ProvidersReady   <-chan struct{}
	Authorizer       *authorization.Authorizer
	ConnectionAuth   func() map[string]map[string]OAuthHandler
	Invoker          invocation.Invoker
	CapabilityLister invocation.CapabilityLister
	AuditSink        core.AuditSink
	SecretManager    core.SecretManager
	Telemetry        core.TelemetryProvider

	auditClose func(context.Context) error
	mu         sync.Mutex
	closed     bool
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

	var errs []error
	errs = append(errs,
		r.Authorizer.Close(),
		closeAuth(r.Auth),
		CloseProviders(r.Providers),
		r.Services.Close(),
		closeIndexedDBs(r.ExtraIndexedDBs...),
		closeS3s(r.ExtraS3s...),
		closeSecretManager(r.SecretManager),
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

type preparedCore struct {
	Auth            core.AuthProvider
	Services        *coredata.Services
	ExtraIndexedDBs []indexeddb.IndexedDB
	ExtraS3s        []s3store.Client
	SecretManager   core.SecretManager
	Telemetry       core.TelemetryProvider
	Deps            Deps
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

	auth, err := buildAuth(cfg, factories, deps)
	if err != nil {
		return nil, err
	}

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

	closeSM = false
	shutdownTelemetry = false
	closeSvc = false
	closeExtraStores = false
	closeExtraS3s = false
	return &preparedCore{
		Auth:            auth,
		Services:        svc,
		ExtraIndexedDBs: extraIndexedDBs,
		ExtraS3s:        extraS3s,
		SecretManager:   sm,
		Telemetry:       tp,
		Deps:            deps,
	}, nil
}

func (p *preparedCore) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}

	var errs []error
	errs = append(errs,
		closeAuth(p.Auth),
		p.Services.Close(),
		closeIndexedDBs(p.ExtraIndexedDBs...),
		closeS3s(p.ExtraS3s...),
		closeSecretManager(p.SecretManager),
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
	prepared.Deps.PluginInvoker = pluginInvoker

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
		return nil, err
	}
	authz, err := authorization.New(cfg.Authorization, cfg.Plugins, providers, connMaps.DefaultConnection, prepared.Services.PluginAuthorizations)
	if err != nil {
		return nil, err
	}
	authz.SetAdminAuthorizationService(prepared.Services.AdminAuthorizations)
	sharedInvoker := invocation.NewBroker(providers, prepared.Services.Users, prepared.Services.Tokens,
		invocation.WithAuthorizer(authz),
		invocation.WithConnectionMapper(invocation.ConnectionMap(connMaps.APIConnection)),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(connMaps.MCPConnection)),
		invocation.WithConnectionAuth(lazyRefreshers(providersReady, connAuthResolver)),
	)
	audit, auditClose, err := buildAuditSink(ctx, cfg, factories, prepared.Telemetry)
	if err != nil {
		return nil, err
	}
	pluginInvoker.SetTarget(invocation.NewGuarded(sharedInvoker, nil, "plugin", audit, invocation.WithoutRateLimit()))
	closeAudit := true
	defer func() {
		if closeAudit && auditClose != nil {
			_ = auditClose(context.Background())
		}
	}()

	closeProviders = false
	closeCore = false
	closeAudit = false
	return &Result{
		Auth:             prepared.Auth,
		Services:         prepared.Services,
		ExtraIndexedDBs:  prepared.ExtraIndexedDBs,
		ExtraS3s:         prepared.ExtraS3s,
		Providers:        providers,
		ProvidersReady:   providersReady,
		Authorizer:       authz,
		ConnectionAuth:   connAuthResolver,
		Invoker:          sharedInvoker,
		CapabilityLister: sharedInvoker,
		AuditSink:        audit,
		SecretManager:    prepared.SecretManager,
		Telemetry:        prepared.Telemetry,
		auditClose:       auditClose,
	}, nil
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

	if secrets != nil && (secrets.Source.IsManaged() || secrets.Source.IsLocal()) {
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

func closeAuth(provider core.AuthProvider) error {
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

func buildAuth(cfg *config.Config, factories *FactoryRegistry, deps Deps) (core.AuthProvider, error) {
	_, authEntry, err := cfg.SelectedAuthProvider()
	if err != nil {
		return nil, err
	}
	if authEntry == nil {
		return nil, nil
	}
	if factories.Auth == nil {
		return nil, fmt.Errorf("bootstrap: auth factory is not registered")
	}
	node := authEntry.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode("auth", "auth", authEntry, authEntry.Config)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: auth provider: %w", err)
		}
	}
	auth, err := factories.Auth(node, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: auth provider: %w", err)
	}
	return auth, nil
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

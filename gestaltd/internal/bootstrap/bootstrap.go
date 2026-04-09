package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/datastore"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"gopkg.in/yaml.v3"
	"xorm.io/xorm"
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

func resolveProviderMetadata(intg config.IntegrationDef) providerMetadata {
	meta := providerMetadata{
		displayName: intg.DisplayName,
		description: intg.Description,
	}
	if intg.IconFile == "" {
		return meta
	}

	svg, err := provider.ReadIconFile(intg.IconFile)
	if err != nil {
		slog.Warn("could not read icon_file", "path", intg.IconFile, "error", err)
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
	EncryptionKey []byte
	BaseURL       string
	SecretManager core.SecretManager
	Datastore     core.Datastore
	Datastores    map[string]config.DatastoreDef
	SQLDB         any // *sql.DB when available, nil otherwise
	SQLDialect    any // Placeholder(int)string when available, nil otherwise
	Egress        EgressDeps
}

type sqlDBAccessor interface{ RawDB() any }
type sqlDialectAccessor interface{ RawDialect() any }

type AuthFactory func(node yaml.Node, deps Deps) (core.AuthProvider, error)
type SecretManagerFactory func(node yaml.Node) (core.SecretManager, error)
type TelemetryFactory func(node yaml.Node) (core.TelemetryProvider, error)
type AuditFactory func(ctx context.Context, cfg config.AuditConfig, telemetry core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error)

type FactoryRegistry struct {
	Auth      AuthFactory
	Secrets   map[string]SecretManagerFactory
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
	Datastore        core.Datastore
	Providers        *registry.PluginMap[core.Provider]
	ProvidersReady   <-chan struct{}
	ConnectionAuth   func() map[string]map[string]OAuthHandler
	Invoker          invocation.Invoker
	CapabilityLister invocation.CapabilityLister
	AuditSink        core.AuditSink
	SecretManager    core.SecretManager
	Telemetry        core.TelemetryProvider
	Egress           EgressDeps

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
		closeAuth(r.Auth),
		CloseProviders(r.Providers),
		closeDatastore(r.Datastore),
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

type preparedCore struct {
	Auth          core.AuthProvider
	Datastore     core.Datastore
	SecretManager core.SecretManager
	Telemetry     core.TelemetryProvider
	Deps          Deps
}

func prepareSecretManager(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) (core.SecretManager, error) {
	sm, err := buildSecretManager(cfg, factories)
	if err != nil {
		return nil, err
	}
	if err := resolveSecretRefs(ctx, cfg, sm); err != nil {
		_ = closeSecretManager(sm)
		return nil, err
	}
	return sm, nil
}

// ResolveConfigSecrets resolves secret:// references in config using the
// configured secrets provider, then closes the temporary secret manager.
func ResolveConfigSecrets(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) error {
	sm, err := prepareSecretManager(ctx, cfg, factories)
	if err != nil {
		return err
	}
	return closeSecretManager(sm)
}

func prepareCore(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, requireEncryptionKey bool) (*preparedCore, error) {
	sm, err := prepareSecretManager(ctx, cfg, factories)
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
		Datastores:    cfg.Datastores,
	}

	auth, err := buildAuth(cfg, factories, deps)
	if err != nil {
		return nil, err
	}

	if cfg.Datastore.Provider != nil {
		return nil, fmt.Errorf("bootstrap: datastore.provider is no longer supported; use the datastores section with datastore: <name>")
	}
	if cfg.Datastore.Resource == "" {
		return nil, fmt.Errorf("bootstrap: datastore resource name is required")
	}
	def, ok := cfg.Datastores[cfg.Datastore.Resource]
	if !ok {
		return nil, fmt.Errorf("bootstrap: datastore.resource references unknown datastore %q", cfg.Datastore.Resource)
	}
	enc, encErr := crypto.NewAESGCM(encKey)
	if encErr != nil {
		return nil, fmt.Errorf("bootstrap: create encryptor: %w", encErr)
	}
	engine, engErr := xorm.NewEngine(def.Driver, def.DSN)
	if engErr != nil {
		return nil, fmt.Errorf("bootstrap: system datastore from resource %q: create xorm engine: %w", cfg.Datastore.Resource, engErr)
	}
	ds, dsErr := datastore.NewXORMAdapter(engine, enc)
	if dsErr != nil {
		return nil, fmt.Errorf("bootstrap: system datastore from resource %q: %w", cfg.Datastore.Resource, dsErr)
	}
	closeDS := true
	defer func() {
		if closeDS {
			_ = ds.Close()
		}
	}()

	deps.Egress = newEgressDeps(cfg, sm)
	var coreDS core.Datastore = ds
	deps.Datastore = coreDS
	if acc, ok := coreDS.(sqlDBAccessor); ok {
		deps.SQLDB = acc.RawDB()
	}
	if acc, ok := coreDS.(sqlDialectAccessor); ok {
		deps.SQLDialect = acc.RawDialect()
	}

	closeSM = false
	shutdownTelemetry = false
	closeDS = false
	return &preparedCore{
		Auth:          auth,
		Datastore:     ds,
		SecretManager: sm,
		Telemetry:     tp,
		Deps:          deps,
	}, nil
}

func (p *preparedCore) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}

	var errs []error
	errs = append(errs,
		closeAuth(p.Auth),
		closeDatastore(p.Datastore),
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
	sharedInvoker := invocation.NewBroker(providers, prepared.Datastore,
		invocation.WithConnectionMapper(invocation.ConnectionMap(connMaps.APIConnection)),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(connMaps.MCPConnection)),
		invocation.WithConnectionAuth(lazyRefreshers(providersReady, connAuthResolver)),
	)
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

	closeProviders = false
	closeCore = false
	closeAudit = false
	return &Result{
		Auth:             prepared.Auth,
		Datastore:        prepared.Datastore,
		Providers:        providers,
		ProvidersReady:   providersReady,
		ConnectionAuth:   connAuthResolver,
		Invoker:          sharedInvoker,
		CapabilityLister: sharedInvoker,
		AuditSink:        audit,
		SecretManager:    prepared.SecretManager,
		Telemetry:        prepared.Telemetry,
		Egress:           prepared.Deps.Egress,
		auditClose:       auditClose,
	}, nil
}

func buildTelemetry(cfg *config.Config, factories *FactoryRegistry) (core.TelemetryProvider, error) {
	factory, ok := factories.Telemetry[cfg.Telemetry.Provider]
	if !ok {
		return nil, fmt.Errorf("bootstrap: unknown telemetry provider %q", cfg.Telemetry.Provider)
	}
	tp, err := factory(cfg.Telemetry.Config)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: telemetry provider %q: %w", cfg.Telemetry.Provider, err)
	}
	return tp, nil
}

func buildAuditSink(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, telemetry core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error) {
	if factories.Audit == nil {
		switch cfg.Audit.Provider {
		case "", "inherit":
			return invocation.NewLoggerAuditSink(telemetry.Logger()), nil, nil
		default:
			return nil, nil, fmt.Errorf("bootstrap: unknown audit provider %q", cfg.Audit.Provider)
		}
	}
	sink, closeFn, err := factories.Audit(ctx, cfg.Audit, telemetry)
	if err != nil {
		return nil, nil, fmt.Errorf("bootstrap: audit provider %q: %w", cfg.Audit.Provider, err)
	}
	return sink, closeFn, nil
}

func buildSecretManager(cfg *config.Config, factories *FactoryRegistry) (core.SecretManager, error) {
	if cfg.Secrets.Provider != nil {
		factory, ok := factories.Secrets["plugin"]
		if !ok {
			return nil, fmt.Errorf("bootstrap: secrets plugin factory is not registered")
		}
		node := cfg.Secrets.Config
		if !config.IsComponentRuntimeConfigNode(node) {
			var err error
			node, err = config.BuildComponentRuntimeConfigNode("secrets", "secrets", cfg.Secrets.Provider, cfg.Secrets.Config)
			if err != nil {
				return nil, fmt.Errorf("bootstrap: secrets plugin: %w", err)
			}
		}
		sm, err := factory(node)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: secrets plugin: %w", err)
		}
		return sm, nil
	}

	name := cfg.Secrets.BuiltinProvider
	if name == "" {
		name = "env"
	}
	factory, ok := factories.Secrets[name]
	if !ok {
		return nil, fmt.Errorf("bootstrap: unknown secrets provider %q", name)
	}
	sm, err := factory(cfg.Secrets.Config)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: secrets provider %q: %w", name, err)
	}
	return sm, nil
}

func closeDatastore(ds core.Datastore) error {
	if ds == nil {
		return nil
	}
	return ds.Close()
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
	if cfg.Auth.Provider == nil {
		return nil, nil
	}
	if factories.Auth == nil {
		return nil, fmt.Errorf("bootstrap: auth factory is not registered")
	}
	node := cfg.Auth.Config
	if !config.IsComponentRuntimeConfigNode(node) {
		var err error
		node, err = config.BuildComponentRuntimeConfigNode("auth", "auth", cfg.Auth.Provider, cfg.Auth.Config)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: auth plugin: %w", err)
		}
	}
	auth, err := factories.Auth(node, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: auth plugin: %w", err)
	}
	return auth, nil
}

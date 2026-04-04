package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/mcpoauth"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/operationexposure"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
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
	h *oauth.UpstreamHandler
}

// WrapUpstreamHandler adapts an oauth.UpstreamHandler to the OAuthHandler
// interface. The adapter maps StartOAuth to AuthorizationURLWithPKCE and
// ExchangeCodeWithVerifier to ExchangeCode with option injection.
func WrapUpstreamHandler(h *oauth.UpstreamHandler) OAuthHandler {
	return &upstreamHandlerAdapter{h: h}
}

func (a *upstreamHandlerAdapter) AuthorizationURL(state string, scopes []string) string {
	url, _ := a.h.AuthorizationURLWithPKCE(state, scopes)
	return url
}

func (a *upstreamHandlerAdapter) StartOAuth(state string, scopes []string) (string, string) {
	return a.h.AuthorizationURLWithPKCE(state, scopes)
}

func (a *upstreamHandlerAdapter) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	return a.h.AuthorizationURLWithOverride(authBaseURL, state, scopes)
}

func (a *upstreamHandlerAdapter) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return a.h.ExchangeCode(ctx, code)
}

func (a *upstreamHandlerAdapter) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	var opts []oauth.ExchangeOption
	if verifier != "" {
		opts = append(opts, oauth.WithPKCEVerifier(verifier))
	}
	opts = append(opts, extraOpts...)
	return a.h.ExchangeCode(ctx, code, opts...)
}

func (a *upstreamHandlerAdapter) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return a.h.RefreshToken(ctx, refreshToken)
}

func (a *upstreamHandlerAdapter) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	return a.h.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
}

func (a *upstreamHandlerAdapter) AuthorizationBaseURL() string { return a.h.AuthorizationBaseURL() }
func (a *upstreamHandlerAdapter) TokenURL() string             { return a.h.TokenURL() }

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

func (m providerMetadata) applyToDefinition(def *provider.Definition) {
	if def == nil {
		return
	}
	if m.displayName != "" {
		def.DisplayName = m.displayName
	}
	if m.description != "" {
		def.Description = m.description
	}
	if m.iconSVG != "" {
		def.IconSVG = m.iconSVG
	}
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

func (m providerMetadata) iconSVGOr(v string) string {
	if m.iconSVG != "" {
		return m.iconSVG
	}
	return v
}

type Deps struct {
	EncryptionKey []byte
	BaseURL       string
	SecretManager core.SecretManager
	SQLDB         any // *sql.DB when available, nil otherwise
	SQLDialect    any // Placeholder(int)string when available, nil otherwise
	Egress        EgressDeps
}

type sqlDBAccessor interface{ RawDB() any }
type sqlDialectAccessor interface{ RawDialect() any }

type AuthFactory func(node yaml.Node, deps Deps) (core.AuthProvider, error)
type DatastoreFactory func(node yaml.Node, deps Deps) (core.Datastore, error)
type SecretManagerFactory func(node yaml.Node) (core.SecretManager, error)
type TelemetryFactory func(node yaml.Node) (core.TelemetryProvider, error)

type FactoryRegistry struct {
	Auth       map[string]AuthFactory
	Datastores map[string]DatastoreFactory
	Secrets    map[string]SecretManagerFactory
	Telemetry  map[string]TelemetryFactory
	Builtins   []core.Provider
}

func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{
		Auth:       make(map[string]AuthFactory),
		Datastores: make(map[string]DatastoreFactory),
		Secrets:    make(map[string]SecretManagerFactory),
		Telemetry:  make(map[string]TelemetryFactory),
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

	mu     sync.Mutex
	closed bool
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
		CloseProviders(r.Providers),
		closeDatastore(r.Datastore),
		closeSecretManager(r.SecretManager),
	)
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

func prepareCore(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, requireEncryptionKey bool) (*preparedCore, error) {
	sm, err := buildSecretManager(cfg, factories)
	if err != nil {
		return nil, err
	}
	closeSM := true
	defer func() {
		if closeSM {
			_ = closeSecretManager(sm)
		}
	}()

	if err := resolveSecretRefs(ctx, cfg, sm); err != nil {
		return nil, err
	}

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
	if requireEncryptionKey && encKey == nil && cfg.Auth.Provider != "none" {
		return nil, fmt.Errorf("bootstrap: server.encryption_key is required when auth is enabled")
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

	ds, err := buildDatastore(cfg, factories, deps)
	if err != nil {
		return nil, err
	}
	closeDS := true
	defer func() {
		if closeDS {
			_ = ds.Close()
		}
	}()

	deps.Egress = newEgressDeps(cfg, sm)
	if acc, ok := ds.(sqlDBAccessor); ok {
		deps.SQLDB = acc.RawDB()
	}
	if acc, ok := ds.(sqlDialectAccessor); ok {
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
		invocation.WithOperationMetrics(prepared.Telemetry.OperationMetrics()),
	)
	audit := core.AuditSink(invocation.NewLoggerAuditSink(prepared.Telemetry.Logger()))

	closeProviders = false
	closeCore = false
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

func buildSecretManager(cfg *config.Config, factories *FactoryRegistry) (core.SecretManager, error) {
	factory, ok := factories.Secrets[cfg.Secrets.Provider]
	if !ok {
		return nil, fmt.Errorf("bootstrap: unknown secrets provider %q", cfg.Secrets.Provider)
	}
	sm, err := factory(cfg.Secrets.Config)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: secrets provider %q: %w", cfg.Secrets.Provider, err)
	}
	return sm, nil
}

func closeDatastore(ds core.Datastore) error {
	if ds == nil {
		return nil
	}
	return ds.Close()
}

func closeSecretManager(sm core.SecretManager) error {
	closer, ok := sm.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

const secretPrefix = "secret://"

// resolveSecretRefs walks the config struct and replaces any string value
// starting with "secret://" with the resolved secret value. The SecretsConfig
// node is skipped to avoid self-referential resolution.
func resolveSecretRefs(ctx context.Context, cfg *config.Config, sm core.SecretManager) error {
	resolve := func(val string) (string, error) {
		name, ok := strings.CutPrefix(val, secretPrefix)
		if !ok {
			return val, nil
		}
		resolved, err := sm.GetSecret(ctx, name)
		if err != nil {
			return "", &core.SecretResolutionError{Name: name, Err: err}
		}
		if resolved == "" {
			return "", &core.SecretResolutionError{
				Name: name,
				Err:  fmt.Errorf("resolved to empty value"),
			}
		}
		return resolved, nil
	}

	if err := resolveStringFields(&cfg.Server, resolve); err != nil {
		return err
	}
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if err := resolveStringFields(&intg, resolve); err != nil {
			return err
		}
		if intg.Plugin != nil {
			if err := resolveStringFields(intg.Plugin, resolve); err != nil {
				return err
			}
			for _, conn := range intg.Plugin.Connections {
				if conn != nil {
					if err := resolveStringFields(conn, resolve); err != nil {
						return err
					}
				}
			}
			if err := resolveYAMLNode(&intg.Plugin.Config, resolve); err != nil {
				return err
			}
		}
		cfg.Integrations[name] = intg
	}

	// Skip cfg.Secrets.Config to avoid self-referential resolution.
	if err := resolveYAMLNode(&cfg.Auth.Config, resolve); err != nil {
		return err
	}
	if err := resolveYAMLNode(&cfg.Datastore.Config, resolve); err != nil {
		return err
	}
	if err := resolveYAMLNode(&cfg.Telemetry.Config, resolve); err != nil {
		return err
	}

	return nil
}

func resolveStringFields(ptr any, resolve func(string) (string, error)) error {
	v := reflect.ValueOf(ptr).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := v.Field(i)
		switch field.Kind() {
		case reflect.String:
			if !field.CanSet() {
				continue
			}
			resolved, err := resolve(field.String())
			if err != nil {
				return err
			}
			field.SetString(resolved)
		case reflect.Struct:
			if field.CanSet() {
				if err := resolveStringFields(field.Addr().Interface(), resolve); err != nil {
					return err
				}
			}
		case reflect.Pointer:
			if !field.IsNil() && field.Elem().Kind() == reflect.Struct {
				if err := resolveStringFields(field.Interface(), resolve); err != nil {
					return err
				}
			}
		case reflect.Map:
			if field.Type().Key().Kind() == reflect.String && field.Type().Elem().Kind() == reflect.String {
				for _, k := range field.MapKeys() {
					resolved, err := resolve(field.MapIndex(k).String())
					if err != nil {
						return err
					}
					field.SetMapIndex(k, reflect.ValueOf(resolved))
				}
			}
		case reflect.Slice:
			switch field.Type().Elem().Kind() {
			case reflect.String:
				for j := 0; j < field.Len(); j++ {
					elem := field.Index(j)
					resolved, err := resolve(elem.String())
					if err != nil {
						return err
					}
					elem.SetString(resolved)
				}
			case reflect.Struct:
				for j := 0; j < field.Len(); j++ {
					if err := resolveStringFields(field.Index(j).Addr().Interface(), resolve); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func resolveYAMLNode(node *yaml.Node, resolve func(string) (string, error)) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag == "!!str" || node.Tag == "" {
			resolved, err := resolve(node.Value)
			if err != nil {
				return err
			}
			node.Value = resolved
		}
	case yaml.MappingNode:
		// Content is [key, value, key, value, ...]; only resolve values.
		for i := 1; i < len(node.Content); i += 2 {
			if err := resolveYAMLNode(node.Content[i], resolve); err != nil {
				return err
			}
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, child := range node.Content {
			if err := resolveYAMLNode(child, resolve); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildAuth(cfg *config.Config, factories *FactoryRegistry, deps Deps) (core.AuthProvider, error) {
	factory, ok := factories.Auth[cfg.Auth.Provider]
	if !ok {
		return nil, fmt.Errorf("bootstrap: unknown auth provider %q", cfg.Auth.Provider)
	}
	auth, err := factory(cfg.Auth.Config, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: auth provider %q: %w", cfg.Auth.Provider, err)
	}
	return auth, nil
}

func buildDatastore(cfg *config.Config, factories *FactoryRegistry, deps Deps) (core.Datastore, error) {
	factory, ok := factories.Datastores[cfg.Datastore.Provider]
	if !ok {
		return nil, fmt.Errorf("bootstrap: unknown datastore %q", cfg.Datastore.Provider)
	}
	ds, err := factory(cfg.Datastore.Config, deps)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: datastore %q: %w", cfg.Datastore.Provider, err)
	}
	return ds, nil
}

func buildRegistrationStore(deps Deps) mcpoauth.RegistrationStore {
	db, ok := deps.SQLDB.(*sql.DB)
	if !ok || db == nil {
		return nil
	}
	dialect, ok := deps.SQLDialect.(mcpoauth.SQLDialect)
	if !ok || dialect == nil {
		return nil
	}
	enc, err := crypto.NewAESGCM(deps.EncryptionKey)
	if err != nil {
		slog.Warn("cannot create encryptor for registration store", "component", "mcpoauth", "error", err)
		return nil
	}
	store := mcpoauth.NewSQLStore(db, enc, dialect)
	if err := store.Migrate(context.Background()); err != nil {
		slog.Error("registration store migration failed", "component", "mcpoauth", "error", err)
	}
	return store
}

type lazyRegStore struct {
	once  sync.Once
	store mcpoauth.RegistrationStore
	deps  Deps
}

func (l *lazyRegStore) get() mcpoauth.RegistrationStore {
	l.once.Do(func() {
		l.store = buildRegistrationStore(l.deps)
	})
	return l.store
}

func buildProviders(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (*registry.PluginMap[core.Provider], <-chan struct{}, func() map[string]map[string]OAuthHandler, error) {
	reg := registry.New()
	connAuth := make(map[string]map[string]OAuthHandler)
	var connMu sync.Mutex
	regStore := &lazyRegStore{deps: deps}

	for _, builtin := range factories.Builtins {
		if err := reg.Providers.Register(builtin.Name(), builtin); errors.Is(err, core.ErrAlreadyRegistered) {
			continue
		} else if err != nil {
			return nil, nil, nil, fmt.Errorf("bootstrap: registering builtin %q: %w", builtin.Name(), err)
		}
		slog.Info("loaded builtin provider", "provider", builtin.Name(), "operations", catalogOperationCount(builtin.Catalog()))
	}

	ready := make(chan struct{})
	if len(cfg.Integrations) == 0 {
		close(ready)
		return &reg.Providers, ready, func() map[string]map[string]OAuthHandler { return connAuth }, nil
	}

	var wg sync.WaitGroup
	for name := range cfg.Integrations {
		intgDef := cfg.Integrations[name]
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := buildProvider(ctx, name, intgDef, deps, regStore)
			if err != nil {
				slog.Warn("skipping provider", "provider", name, "error", err)
				return
			}
			if err := reg.Providers.Register(name, result.Provider); err != nil {
				closeIfPossible(result.Provider)
				slog.Warn("registering provider failed", "provider", name, "error", err)
				return
			}
			if len(result.ConnectionAuth) > 0 {
				connMu.Lock()
				connAuth[name] = result.ConnectionAuth
				connMu.Unlock()
			}
			slog.Info("loaded provider", "provider", name, "operations", catalogOperationCount(result.Provider.Catalog()))
		}()
	}

	go func() {
		wg.Wait()
		close(ready)
	}()

	resolver := func() map[string]map[string]OAuthHandler {
		<-ready
		return connAuth
	}
	return &reg.Providers, ready, resolver, nil
}

func buildProvider(ctx context.Context, name string, intg config.IntegrationDef, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	if intg.Plugin == nil {
		return nil, fmt.Errorf("integration %q has no plugin defined", name)
	}

	meta := resolveProviderMetadata(intg)
	pluginConfig, err := config.NodeToMap(intg.Plugin.Config)
	if err != nil {
		return nil, fmt.Errorf("decode plugin config for %q: %w", name, err)
	}

	if !intg.Plugin.IsInline() && !intg.Plugin.IsDeclarative {
		return buildExternalPluginProvider(ctx, name, intg, pluginConfig, meta, deps, regStore)
	}

	manifest, allowedOperations, err := config.EffectiveManifestBackedInputs(name, intg.Plugin)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, fmt.Errorf("declarative provider %q has no resolved manifest", name)
	}
	if manifest.Provider.IsSpecLoaded() {
		return buildSpecLoadedProvider(ctx, name, intg, manifest, pluginConfig, meta, deps, regStore, allowedOperations)
	}

	declarative, err := newDeclarativeProvider(manifest, meta)
	if err != nil {
		return nil, fmt.Errorf("create provider %q: %w", name, err)
	}
	prov, err := applyAllowedOperations(name, allowedOperations, declarative)
	if err != nil {
		return nil, err
	}
	return newProviderBuildResult(name, intg, manifest, pluginConfig, prov, nil, deps, regStore)
}

func applyAllowedOperations(name string, allowedOperations map[string]*config.OperationOverride, pluginProv core.Provider) (core.Provider, error) {
	policy, err := operationexposure.New(allowedOperations)
	if err != nil {
		return nil, fmt.Errorf("integration %q plugin: %w", name, err)
	}
	if policy == nil {
		return pluginProv, nil
	}
	if err := policy.ValidateCatalog(pluginProv.Catalog()); err != nil {
		return nil, fmt.Errorf("integration %q plugin: %w", name, err)
	}
	return policy.Wrap(pluginProv), nil
}

func catalogOperationCount(cat *catalog.Catalog) int {
	if cat == nil {
		return 0
	}
	return len(cat.Operations)
}

func buildPluginProvider(ctx context.Context, name string, intg config.IntegrationDef, pluginConfig map[string]any, meta providerMetadata) (core.Provider, error) {
	prov, err := pluginhost.NewExecutableProvider(ctx, pluginhost.ExecConfig{
		Command:      intg.Plugin.Command,
		Args:         intg.Plugin.Args,
		Env:          intg.Plugin.Env,
		Name:         name,
		DisplayName:  meta.displayName,
		Description:  meta.description,
		IconSVG:      meta.iconSVG,
		Config:       pluginConfig,
		AllowedHosts: intg.Plugin.AllowedHosts,
		HostBinary:   intg.Plugin.HostBinary,
	})
	if err != nil {
		return nil, err
	}
	return prov, nil
}

func buildOAuthHandlerFromAuth(auth *config.ConnectionAuthDef, pluginConfig map[string]any, deps Deps) (OAuthHandler, error) {
	if auth == nil || auth.Type != "oauth2" {
		return nil, nil
	}

	clientID := auth.ClientID
	clientSecret := auth.ClientSecret
	if id, _ := pluginConfig["client_id"].(string); id != "" {
		clientID = id
	}
	if sec, _ := pluginConfig["client_secret"].(string); sec != "" {
		clientSecret = sec
	}
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required for oauth2 auth")
	}

	var tokenExchange oauth.TokenExchangeFormat
	switch auth.TokenExchange {
	case "", "form":
		tokenExchange = oauth.TokenExchangeForm
	case "json":
		tokenExchange = oauth.TokenExchangeJSON
	default:
		return nil, fmt.Errorf("unknown token_exchange %q", auth.TokenExchange)
	}

	oauthCfg := oauth.UpstreamConfig{
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		AuthorizationURL:    auth.AuthorizationURL,
		TokenURL:            auth.TokenURL,
		RedirectURL:         deps.BaseURL + config.IntegrationCallbackPath,
		PKCE:                auth.PKCE,
		DefaultScopes:       auth.Scopes,
		ScopeParam:          auth.ScopeParam,
		ScopeSeparator:      auth.ScopeSeparator,
		TokenExchange:       tokenExchange,
		AuthorizationParams: auth.AuthorizationParams,
		TokenParams:         auth.TokenParams,
		RefreshParams:       auth.RefreshParams,
		AcceptHeader:        auth.AcceptHeader,
		AccessTokenPath:     auth.AccessTokenPath,
	}
	if auth.ClientAuth == "header" {
		oauthCfg.ClientAuthMethod = oauth.ClientAuthHeader
	}

	return WrapUpstreamHandler(oauth.NewUpstream(oauthCfg)), nil
}

func buildOAuthHandlerFromDefinition(def *provider.Definition, conn config.ConnectionDef, pluginConfig map[string]any, deps Deps) (OAuthHandler, error) {
	if def == nil || def.Auth.Type != "oauth2" {
		return nil, nil
	}

	effectiveConn := conn
	if id, _ := pluginConfig["client_id"].(string); id != "" {
		effectiveConn.Auth.ClientID = id
	}
	if sec, _ := pluginConfig["client_secret"].(string); sec != "" {
		effectiveConn.Auth.ClientSecret = sec
	}
	if effectiveConn.Auth.ClientID == "" || effectiveConn.Auth.ClientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required for oauth2 auth")
	}
	if effectiveConn.Auth.RedirectURL == "" {
		effectiveConn.Auth.RedirectURL = deps.BaseURL + config.IntegrationCallbackPath
	}

	defCopy := *def
	provider.ApplyConnectionAuth(&defCopy, effectiveConn)
	upstream, err := provider.BuildOAuthUpstream(&defCopy, effectiveConn, defCopy.BaseURL, nil)
	if err != nil {
		return nil, err
	}
	return WrapUpstreamHandler(upstream), nil
}

func buildMCPOAuthHandler(conn config.ConnectionDef, mcpURL string, store mcpoauth.RegistrationStore, deps Deps) *mcpoauth.Handler {
	redirectURL := conn.Auth.RedirectURL
	if redirectURL == "" {
		redirectURL = deps.BaseURL + config.IntegrationCallbackPath
	}
	return mcpoauth.NewHandler(mcpoauth.HandlerConfig{
		MCPURL:       mcpURL,
		Store:        store,
		RedirectURL:  redirectURL,
		ClientID:     conn.Auth.ClientID,
		ClientSecret: conn.Auth.ClientSecret,
	})
}

func applyProviderResponseMapping(def *provider.Definition, manifestProvider *pluginmanifestv1.Provider, plugin *config.PluginDef) {
	responseMapping := config.MergedProviderResponseMapping(manifestProvider, plugin)
	if responseMapping == nil {
		return
	}
	rm := &provider.ResponseMappingDef{
		DataPath: responseMapping.DataPath,
	}
	if responseMapping.Pagination != nil {
		rm.Pagination = &provider.PaginationMappingDef{
			HasMorePath: responseMapping.Pagination.HasMorePath,
			CursorPath:  responseMapping.Pagination.CursorPath,
		}
	}
	def.ResponseMapping = rm
}

func lazyRefreshers(ready <-chan struct{}, resolver func() map[string]map[string]OAuthHandler) invocation.RefresherResolver {
	var once sync.Once
	var result map[string]map[string]invocation.OAuthRefresher
	return func() map[string]map[string]invocation.OAuthRefresher {
		once.Do(func() {
			<-ready
			result = connectionAuthToRefreshers(resolver())
		})
		return result
	}
}

func connectionAuthToRefreshers(m map[string]map[string]OAuthHandler) map[string]map[string]invocation.OAuthRefresher {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]map[string]invocation.OAuthRefresher, len(m))
	for intg, conns := range m {
		inner := make(map[string]invocation.OAuthRefresher, len(conns))
		for conn, handler := range conns {
			inner[conn] = handler
		}
		out[intg] = inner
	}
	return out
}

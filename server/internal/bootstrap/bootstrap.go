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
	"github.com/valon-technologies/gestalt/server/internal/composite"
	"github.com/valon-technologies/gestalt/server/internal/config"
	graphqlupstream "github.com/valon-technologies/gestalt/server/internal/graphql"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/mcpoauth"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/openapi"
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
		invocation.WithConnectionAuth(lazyRefreshers(providersReady, connAuthResolver)),
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

	declarative, err := pluginhost.NewDeclarativeProvider(
		manifest,
		nil,
		pluginhost.WithDeclarativeMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
	)
	if err != nil {
		return nil, fmt.Errorf("create provider %q: %w", name, err)
	}
	prov, err := applyAllowedOperations(name, allowedOperations, declarative)
	if err != nil {
		return nil, err
	}
	return newProviderBuildResult(name, intg, manifest, pluginConfig, prov, nil, deps, regStore)
}

func buildExternalPluginProvider(ctx context.Context, name string, intg config.IntegrationDef, pluginConfig map[string]any, meta providerMetadata, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	pluginProv, err := buildPluginProvider(ctx, name, intg, pluginConfig, meta)
	if err != nil {
		return nil, err
	}
	manifest := intg.Plugin.ResolvedManifest
	manifestProvider := intg.Plugin.ManifestProvider()
	allowedOperations := intg.Plugin.AllowedOperations
	specPlugin := intg.Plugin
	if intg.Plugin.HasResolvedManifest() {
		resolvedManifest, resolvedAllowedOperations, err := config.EffectiveManifestBackedInputs(name, intg.Plugin)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, err
		}
		manifest = resolvedManifest
		if manifest != nil {
			manifestProvider = manifest.Provider
		}
		allowedOperations = resolvedAllowedOperations
		specPlugin = nil
	}
	plan, err := buildPluginConnectionPlan(intg.Plugin, manifestProvider)
	if err != nil {
		closeIfPossible(pluginProv)
		return nil, fmt.Errorf("build external plugin provider %q: %w", name, err)
	}
	resolved, hasSpecSurface := plan.configuredSpecSurface()

	if !hasSpecSurface {
		restricted, err := applyAllowedOperations(name, allowedOperations, pluginProv)
		if err != nil {
			closeIfPossible(pluginProv)
			return nil, err
		}
		return newProviderBuildResult(name, intg, manifest, pluginConfig, restricted, nil, deps, regStore)
	}

	baseURL := intg.Plugin.BaseURL
	if manifestProvider != nil {
		baseURL = manifestProvider.BaseURL
	}
	specProv, _, err := buildConfiguredSpecProvider(ctx, name, resolved, meta, specProviderConfig{
		plugin:               specPlugin,
		manifestProvider:     manifestProvider,
		allowedOperations:    allowedOperations,
		baseURL:              baseURL,
		applyResponseMapping: true,
		providerBuildOptions: func(config.ConnectionDef) []provider.BuildOption {
			return []provider.BuildOption{provider.WithEgressResolver(deps.Egress.Resolver)}
		},
	}, deps)
	if err != nil {
		closeIfPossible(pluginProv)
		return nil, fmt.Errorf("build hybrid spec provider %q: %w", name, err)
	}
	merged, err := composite.NewMergedWithConnections(
		name,
		meta.displayNameOr(pluginProv.DisplayName()),
		meta.descriptionOr(pluginProv.Description()),
		meta.iconSVGOr(firstProviderIconSVG(pluginProv, specProv)),
		composite.BoundProvider{Provider: pluginProv, Connection: config.PluginConnectionName},
		composite.BoundProvider{Provider: specProv, Connection: resolved.connectionName},
	)
	if err != nil {
		closeIfPossible(specProv, pluginProv)
		return nil, err
	}

	return newProviderBuildResult(name, intg, manifest, pluginConfig, merged, nil, deps, regStore)
}

type specProviderConfig struct {
	plugin               *config.PluginDef
	manifestProvider     *pluginmanifestv1.Provider
	allowedOperations    map[string]*config.OperationOverride
	baseURL              string
	providerBuildOptions func(config.ConnectionDef) []provider.BuildOption
	applyResponseMapping bool
}

func loadConfiguredAPIDefinition(ctx context.Context, name string, resolved resolvedSpecSurface, meta providerMetadata, cfg specProviderConfig) (*provider.Definition, error) {
	def, err := loadSpecDefinition(ctx, name, resolved, cfg.allowedOperations)
	if err != nil {
		return nil, fmt.Errorf("load %s definition: %w", resolved.surface, err)
	}
	if cfg.baseURL != "" {
		def.BaseURL = cfg.baseURL
	}
	applyPluginHeaders(def, cfg.plugin, cfg.manifestProvider)
	if err := applyManagedParameters(def, cfg.plugin, cfg.manifestProvider); err != nil {
		return nil, err
	}
	if cfg.applyResponseMapping {
		applyProviderResponseMapping(def, cfg.manifestProvider, cfg.plugin)
	}
	meta.applyToDefinition(def)
	return def, nil
}

func buildConfiguredSpecProvider(ctx context.Context, name string, resolved resolvedSpecSurface, meta providerMetadata, cfg specProviderConfig, deps Deps) (core.Provider, *provider.Definition, error) {
	var buildOpts []provider.BuildOption
	if cfg.providerBuildOptions != nil {
		buildOpts = cfg.providerBuildOptions(resolved.connection)
	}

	switch resolved.surface {
	case config.SpecSurfaceOpenAPI, config.SpecSurfaceGraphQL:
		def, err := loadConfiguredAPIDefinition(ctx, name, resolved, meta, cfg)
		if err != nil {
			return nil, nil, err
		}
		prov, err := provider.Build(def, resolved.connection, buildOpts...)
		if err != nil {
			return nil, nil, err
		}
		return prov, def, nil

	case config.SpecSurfaceMCP:
		connMode := core.ConnectionMode(resolved.connection.Mode)
		if connMode == "" {
			connMode = core.ConnectionModeUser
		}
		up, err := mcpupstream.New(
			ctx,
			name,
			resolved.url,
			connMode,
			config.MergedProviderHeaders(cfg.manifestProvider, cfg.plugin),
			deps.Egress.Resolver,
			mcpupstream.WithMetadataOverrides(meta.displayName, meta.description, meta.iconSVG),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("create mcp upstream: %w", err)
		}
		if cfg.allowedOperations != nil {
			if err := up.FilterOperations(cfg.allowedOperations); err != nil {
				_ = up.Close()
				return nil, nil, fmt.Errorf("filter mcp operations: %w", err)
			}
		}
		return up, nil, nil

	default:
		return nil, nil, fmt.Errorf("unsupported spec surface %q", resolved.surface)
	}
}

func loadSpecDefinition(ctx context.Context, name string, resolved resolvedSpecSurface, allowedOperations map[string]*config.OperationOverride) (*provider.Definition, error) {
	switch resolved.surface {
	case config.SpecSurfaceOpenAPI:
		return openapi.LoadDefinition(ctx, name, resolved.url, allowedOperations)
	case config.SpecSurfaceGraphQL:
		return graphqlupstream.LoadDefinition(ctx, name, resolved.url, allowedOperations)
	default:
		return nil, fmt.Errorf("unsupported spec definition surface %q", resolved.surface)
	}
}

type specAuthFallback struct {
	definition     *provider.Definition
	connectionName string
}

func newProviderBuildResult(name string, intg config.IntegrationDef, manifest *pluginmanifestv1.Manifest, pluginConfig map[string]any, prov core.Provider, authFallback *specAuthFallback, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	result := &ProviderBuildResult{Provider: prov}
	var err error
	result.ConnectionAuth, err = buildConnectionAuthMap(name, intg, manifest, pluginConfig, authFallback, deps, regStore)
	if err != nil {
		closeIfPossible(prov)
		return nil, err
	}
	return result, nil
}

func mcpOAuthBuildOpts(conn config.ConnectionDef, mp *pluginmanifestv1.Provider, regStore *lazyRegStore, deps Deps) []provider.BuildOption {
	if conn.Auth.Type != pluginmanifestv1.AuthTypeMCPOAuth || mp.MCPURL == "" {
		return nil
	}
	handler := buildMCPOAuthHandler(conn, mp.MCPURL, regStore.get(), deps)
	return []provider.BuildOption{provider.WithAuthHandler(handler)}
}

func buildSpecLoadedProvider(ctx context.Context, name string, intg config.IntegrationDef, manifest *pluginmanifestv1.Manifest, pluginConfig map[string]any, meta providerMetadata, deps Deps, regStore *lazyRegStore, allowedOperations map[string]*config.OperationOverride) (*ProviderBuildResult, error) {
	mp := manifest.Provider
	plan, err := buildPluginConnectionPlan(intg.Plugin, mp)
	if err != nil {
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}
	apiResolved, hasAPI := plan.configuredAPISurface()
	mcpResolved, hasMCP := plan.resolvedSurface(config.SpecSurfaceMCP)
	if !hasAPI && !hasMCP {
		return nil, fmt.Errorf("build spec-loaded provider %q: no spec URL", name)
	}

	buildSpec := func(resolved resolvedSpecSurface, allowed map[string]*config.OperationOverride) (core.Provider, error) {
		prov, _, err := buildConfiguredSpecProvider(ctx, name, resolved, meta, specProviderConfig{
			plugin:               nil,
			manifestProvider:     mp,
			allowedOperations:    allowed,
			baseURL:              mp.BaseURL,
			applyResponseMapping: true,
			providerBuildOptions: func(conn config.ConnectionDef) []provider.BuildOption {
				return mcpOAuthBuildOpts(conn, mp, regStore, deps)
			},
		}, deps)
		return prov, err
	}

	if !hasAPI {
		prov, err := buildSpec(mcpResolved, allowedOperations)
		if err != nil {
			return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
		}
		return newProviderBuildResult(name, intg, manifest, pluginConfig, prov, nil, deps, regStore)
	}

	apiProv, apiDef, err := buildConfiguredSpecProvider(ctx, name, apiResolved, meta, specProviderConfig{
		plugin:               nil,
		manifestProvider:     mp,
		allowedOperations:    allowedOperations,
		baseURL:              mp.BaseURL,
		applyResponseMapping: true,
		providerBuildOptions: func(conn config.ConnectionDef) []provider.BuildOption {
			return mcpOAuthBuildOpts(conn, mp, regStore, deps)
		},
	}, deps)
	if err != nil {
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}
	authFallback := &specAuthFallback{
		definition:     apiDef,
		connectionName: apiResolved.connectionName,
	}

	if !hasMCP {
		return newProviderBuildResult(name, intg, manifest, pluginConfig, apiProv, authFallback, deps, regStore)
	}

	mcpProv, err := buildSpec(mcpResolved, nil)
	if err != nil {
		closeIfPossible(apiProv)
		return nil, fmt.Errorf("build spec-loaded provider %q: %w", name, err)
	}
	mcpUp, ok := mcpProv.(composite.MCPUpstream)
	if !ok {
		closeIfPossible(mcpProv, apiProv)
		return nil, fmt.Errorf("build spec-loaded provider %q: unexpected mcp provider type %T", name, mcpProv)
	}

	filtered := matchingAllowedOperations(allowedOperations, mcpUp.Catalog())
	if len(filtered) > 0 {
		filterable, ok := any(mcpUp).(interface {
			FilterOperations(map[string]*config.OperationOverride) error
		})
		if !ok {
			closeIfPossible(mcpUp, apiProv)
			return nil, fmt.Errorf("build spec-loaded provider %q: unexpected non-filterable mcp provider type %T", name, mcpProv)
		}
		if err := filterable.FilterOperations(filtered); err != nil {
			closeIfPossible(mcpUp, apiProv)
			return nil, fmt.Errorf("build spec-loaded provider %q: filter mcp operations: %w", name, err)
		}
	}

	return newProviderBuildResult(name, intg, manifest, pluginConfig, composite.New(name, apiProv, mcpUp), authFallback, deps, regStore)
}

func applyPluginHeaders(def *provider.Definition, plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) {
	if def == nil {
		return
	}
	headers := config.MergedProviderHeaders(manifestProvider, plugin)
	if len(headers) == 0 {
		return
	}
	def.Headers = headers
}

func applyManagedParameters(def *provider.Definition, plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) error {
	if def == nil {
		return nil
	}

	params := config.MergedProviderManagedParameters(manifestProvider, plugin)
	if len(params) == 0 {
		return nil
	}

	if def.Headers == nil {
		def.Headers = make(map[string]string, len(params))
	}
	for _, param := range params {
		switch param.In {
		case config.ManagedParameterInHeader:
			if _, exists := def.Headers[param.Name]; exists {
				return fmt.Errorf("managed parameter %q conflicts with configured header", param.Name)
			}
			def.Headers[param.Name] = param.Value
		case config.ManagedParameterInPath:
		default:
			return fmt.Errorf("unsupported managed parameter location %q", param.In)
		}
	}

	for opName := range def.Operations {
		op := def.Operations[opName]
		for _, param := range params {
			if param.In != config.ManagedParameterInPath {
				continue
			}
			op.Path = strings.ReplaceAll(op.Path, "{"+param.Name+"}", param.Value)
		}
		filtered := op.Parameters[:0]
		for _, param := range op.Parameters {
			if isManagedOperationParameter(param, params) {
				continue
			}
			filtered = append(filtered, param)
		}
		op.Parameters = filtered
		def.Operations[opName] = op
	}

	return nil
}

func isManagedOperationParameter(param provider.ParameterDef, managed []pluginmanifestv1.ManagedParameter) bool {
	location := strings.ToLower(param.Location)
	if location == "" {
		return false
	}

	wireName := param.WireName
	if wireName == "" {
		wireName = param.Name
	}
	target := config.NormalizeManagedParameter(pluginmanifestv1.ManagedParameter{In: location, Name: wireName})

	for _, managedParam := range managed {
		if managedParam.In == target.In && managedParam.Name == target.Name {
			return true
		}
	}
	return false
}

func firstProviderIconSVG(providers ...core.Provider) string {
	for _, prov := range providers {
		cat := prov.Catalog()
		if cat != nil && cat.IconSVG != "" {
			return cat.IconSVG
		}
	}
	return ""
}

func matchingAllowedOperations(allowed map[string]*config.OperationOverride, cat *catalog.Catalog) map[string]*config.OperationOverride {
	if len(allowed) == 0 || cat == nil || len(cat.Operations) == 0 {
		return nil
	}
	available := make(map[string]struct{}, len(cat.Operations))
	for i := range cat.Operations {
		available[cat.Operations[i].ID] = struct{}{}
	}
	filtered := make(map[string]*config.OperationOverride)
	for name, override := range allowed {
		if _, ok := available[name]; ok {
			filtered[name] = override
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
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

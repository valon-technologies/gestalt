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
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/config"
	graphqlupstream "github.com/valon-technologies/gestalt/server/internal/graphql"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/mcpoauth"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/openapi"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
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

type Deps struct {
	EncryptionKey []byte
	BaseURL       string
	SecretManager core.SecretManager
	SQLDB         any // *sql.DB when available, nil otherwise
	SQLDialect    any // Placeholder(int)string when available, nil otherwise
	Egress        EgressDeps
}

type AuthFactory func(node yaml.Node, deps Deps) (core.AuthProvider, error)
type DatastoreFactory func(node yaml.Node, deps Deps) (core.Datastore, error)
type SecretManagerFactory func(node yaml.Node) (core.SecretManager, error)
type TelemetryFactory func(node yaml.Node) (core.TelemetryProvider, error)

type FactoryRegistry struct {
	Auth       map[string]AuthFactory
	Datastores map[string]DatastoreFactory
	Secrets    map[string]SecretManagerFactory
	Telemetry  map[string]TelemetryFactory
	Runtimes   map[string]RuntimeFactory
	Bindings   map[string]BindingFactory
	Builtins   []core.Provider
}

func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{
		Auth:       make(map[string]AuthFactory),
		Datastores: make(map[string]DatastoreFactory),
		Secrets:    make(map[string]SecretManagerFactory),
		Telemetry:  make(map[string]TelemetryFactory),
		Runtimes:   make(map[string]RuntimeFactory),
		Bindings:   make(map[string]BindingFactory),
	}
}

type Result struct {
	Auth             core.AuthProvider
	Datastore        core.Datastore
	Providers        *registry.PluginMap[core.Provider]
	ProvidersReady   <-chan struct{}
	ConnectionAuth   func() map[string]map[string]OAuthHandler
	Runtimes         *registry.PluginMap[core.Runtime]
	Bindings         *registry.PluginMap[core.Binding]
	Invoker          invocation.Invoker
	CapabilityLister invocation.CapabilityLister
	AuditSink        core.AuditSink
	SecretManager    core.SecretManager
	Telemetry        core.TelemetryProvider
	Egress           EgressDeps

	mu                sync.Mutex
	extensionsStarted bool
	closed            bool
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
	if r.extensionsStarted {
		return nil
	}
	if err := startRuntimes(ctx, r.Runtimes); err != nil {
		return err
	}
	if err := startBindings(ctx, r.Bindings, r.Runtimes); err != nil {
		return err
	}
	r.extensionsStarted = true
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
	if r.extensionsStarted {
		errs = append(errs,
			CloseBindings(r.Bindings, bindingNames(r.Bindings)),
			StopRuntimes(ctx, r.Runtimes, runtimeNames(r.Runtimes)),
		)
		r.extensionsStarted = false
	}
	errs = append(errs,
		CloseProviders(r.Providers),
		closeDatastore(r.Datastore),
		closeResultSecretManager(r.SecretManager),
	)
	if r.Telemetry != nil {
		errs = append(errs, r.Telemetry.Shutdown(ctx))
	}
	r.closed = true
	return errors.Join(errs...)
}

func Bootstrap(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) (*Result, error) {
	sm, err := buildSecretManager(cfg, factories)
	if err != nil {
		return nil, err
	}
	closeSM := true
	defer func() {
		if closeSM {
			if c, ok := sm.(interface{ Close() error }); ok {
				_ = c.Close()
			}
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
	if encKey == nil {
		slog.Warn("no encryption key configured; stored secrets will not be encrypted")
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
	deps.Egress = newEgressDeps(cfg)

	// Expose the underlying *sql.DB and dialect for optional use by
	// provider factories (e.g. MCP OAuth registration storage).
	type sqlDBAccessor interface{ RawDB() any }
	type sqlDialectAccessor interface{ RawDialect() any }
	if acc, ok := ds.(sqlDBAccessor); ok {
		deps.SQLDB = acc.RawDB()
	}
	if acc, ok := ds.(sqlDialectAccessor); ok {
		deps.SQLDialect = acc.RawDialect()
	}

	providers, providersReady, connAuthResolver, err := buildProviders(ctx, cfg, factories, deps)
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

	connMap := BuildConnectionMap(cfg)
	sharedInvoker := invocation.NewBroker(providers, ds,
		invocation.WithConnectionMapper(connMap),
		invocation.WithConnectionAuth(lazyRefreshers(providersReady, connAuthResolver)),
	)
	wireCredentialResolver(&deps.Egress, sm)
	audit := core.AuditSink(invocation.NewSlogAuditSink(nil))

	extensions, err := buildExtensions(ctx, cfg, factories, sharedInvoker, sharedInvoker, audit, deps.Egress)
	if err != nil {
		return nil, err
	}
	shutdownExtensions := true
	defer func() {
		if shutdownExtensions {
			_ = extensions.Shutdown(context.Background())
		}
	}()

	closeProviders = false
	shutdownExtensions = false
	closeSM = false
	shutdownTelemetry = false
	return &Result{
		Auth:             auth,
		Datastore:        ds,
		Providers:        providers,
		ProvidersReady:   providersReady,
		ConnectionAuth:   connAuthResolver,
		Runtimes:         extensions.Runtimes,
		Bindings:         extensions.Bindings,
		Invoker:          sharedInvoker,
		CapabilityLister: sharedInvoker,
		AuditSink:        audit,
		SecretManager:    sm,
		Telemetry:        tp,
		Egress:           deps.Egress,
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

func startRuntimes(ctx context.Context, runtimes *registry.PluginMap[core.Runtime]) error {
	if runtimes == nil {
		return nil
	}

	var started []string
	for _, name := range runtimeNames(runtimes) {
		rt, err := runtimes.Get(name)
		if err != nil {
			return errors.Join(
				fmt.Errorf("getting runtime %q: %w", name, err),
				StopRuntimes(ctx, runtimes, started),
			)
		}
		if err := rt.Start(ctx); err != nil {
			return errors.Join(
				fmt.Errorf("starting runtime %q: %w", name, err),
				StopRuntimes(ctx, runtimes, started),
			)
		}
		started = append(started, name)
	}

	return nil
}

func startBindings(ctx context.Context, bindings *registry.PluginMap[core.Binding], runtimes *registry.PluginMap[core.Runtime]) error {
	if bindings == nil {
		return nil
	}

	var started []string
	for _, name := range bindingNames(bindings) {
		binding, err := bindings.Get(name)
		if err != nil {
			return errors.Join(
				fmt.Errorf("getting binding %q: %w", name, err),
				CloseBindings(bindings, started),
				StopRuntimes(ctx, runtimes, runtimeNames(runtimes)),
			)
		}
		if err := binding.Start(ctx); err != nil {
			return errors.Join(
				fmt.Errorf("starting binding %q: %w", name, err),
				CloseBindings(bindings, started),
				StopRuntimes(ctx, runtimes, runtimeNames(runtimes)),
			)
		}
		started = append(started, name)
	}

	return nil
}

func closeDatastore(ds core.Datastore) error {
	if ds == nil {
		return nil
	}
	return ds.Close()
}

func closeResultSecretManager(sm core.SecretManager) error {
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
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if err := resolveStringFields(&rt, resolve); err != nil {
			return err
		}
		if err := resolveYAMLNode(&rt.Config, resolve); err != nil {
			return err
		}
		if rt.Plugin != nil {
			if err := resolveYAMLNode(&rt.Plugin.Config, resolve); err != nil {
				return err
			}
		}
		cfg.Runtimes[name] = rt
	}
	for name := range cfg.Bindings {
		b := cfg.Bindings[name]
		if err := resolveStringFields(&b, resolve); err != nil {
			return err
		}
		if err := resolveYAMLNode(&b.Config, resolve); err != nil {
			return err
		}
		cfg.Bindings[name] = b
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
		slog.Info("loaded builtin provider", "provider", builtin.Name(), "operations", len(builtin.ListOperations()))
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
			result, err := buildProvider(ctx, name, intgDef, factories, deps, regStore)
			if err != nil {
				slog.Warn("skipping provider", "provider", name, "error", err)
				return
			}
			if err := reg.Providers.Register(name, result.Provider); err != nil {
				if c, ok := result.Provider.(interface{ Close() error }); ok {
					_ = c.Close()
				}
				slog.Warn("registering provider failed", "provider", name, "error", err)
				return
			}
			if len(result.ConnectionAuth) > 0 {
				connMu.Lock()
				connAuth[name] = result.ConnectionAuth
				connMu.Unlock()
			}
			slog.Info("loaded provider", "provider", name, "operations", len(result.Provider.ListOperations()))
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

func buildProvider(ctx context.Context, name string, intg config.IntegrationDef, factories *FactoryRegistry, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	if intg.Plugin == nil {
		return nil, fmt.Errorf("integration %q has no plugin defined", name)
	}

	if intg.Plugin.IsInline() {
		return buildInlineProvider(ctx, name, intg, deps, regStore)
	}

	if intg.Plugin.IsDeclarative {
		return buildDeclarativeProvider(name, intg, deps, regStore)
	}

	pluginProv, pluginConfig, err := buildPluginProvider(ctx, name, intg)
	if err != nil {
		return nil, err
	}
	applyPluginIcon(pluginProv, intg)

	restricted, err := applyAllowedOperations(name, intg, pluginProv)
	if err != nil {
		if c, ok := pluginProv.(interface{ Close() error }); ok {
			_ = c.Close()
		}
		return nil, err
	}

	buildResult := &ProviderBuildResult{Provider: restricted}
	attachPluginOAuth(name, intg, pluginConfig, deps, buildResult)
	return buildResult, nil
}

func buildInlineProvider(ctx context.Context, name string, intg config.IntegrationDef, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	manifest, err := config.InlineToManifest(name, intg.Plugin)
	if err != nil {
		return nil, fmt.Errorf("convert inline plugin %q to manifest: %w", name, err)
	}
	if intg.DisplayName != "" {
		manifest.DisplayName = intg.DisplayName
	}
	if intg.Description != "" {
		manifest.Description = intg.Description
	}
	if manifest.Provider.IsSpecLoaded() {
		return buildSpecLoadedProvider(ctx, name, intg, manifest, deps, regStore)
	}
	prov, err := pluginhost.NewDeclarativeProvider(manifest, nil)
	if err != nil {
		return nil, fmt.Errorf("create inline provider %q: %w", name, err)
	}
	applyPluginIcon(prov, intg)

	restricted, err := applyAllowedOperations(name, intg, prov)
	if err != nil {
		return nil, err
	}

	result := &ProviderBuildResult{Provider: restricted}
	if err := attachManifestOAuth(name, intg, manifest, deps, regStore, result); err != nil {
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

func buildSpecLoadedProvider(ctx context.Context, name string, intg config.IntegrationDef, manifest *pluginmanifestv1.Manifest, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	mp := manifest.Provider

	allowedOps := convertAllowedOperations(mp.AllowedOperations)
	conn := pluginConnectionDef(intg.Plugin)

	switch {
	case mp.OpenAPI != "":
		def, err := openapi.LoadDefinition(ctx, name, mp.OpenAPI, allowedOps)
		if err != nil {
			return nil, fmt.Errorf("load openapi spec for %q: %w", name, err)
		}
		if mp.BaseURL != "" {
			def.BaseURL = mp.BaseURL
		}
		applyManifestResponseMapping(def, mp)
		provider.ApplyDisplayOverrides(def, intg)
		prov, err := provider.Build(def, conn, nil, mcpOAuthBuildOpts(conn, mp, regStore, deps)...)
		if err != nil {
			return nil, fmt.Errorf("build openapi provider %q: %w", name, err)
		}
		return specLoadedResult(name, intg, manifest, prov, deps, regStore)

	case mp.GraphQLURL != "":
		def, err := graphqlupstream.LoadDefinition(ctx, name, mp.GraphQLURL, allowedOps)
		if err != nil {
			return nil, fmt.Errorf("load graphql schema for %q: %w", name, err)
		}
		if mp.BaseURL != "" {
			def.BaseURL = mp.BaseURL
		}
		applyManifestResponseMapping(def, mp)
		provider.ApplyDisplayOverrides(def, intg)
		prov, err := provider.Build(def, conn, nil, mcpOAuthBuildOpts(conn, mp, regStore, deps)...)
		if err != nil {
			return nil, fmt.Errorf("build graphql provider %q: %w", name, err)
		}
		return specLoadedResult(name, intg, manifest, prov, deps, regStore)

	case mp.MCPURL != "":
		connMode := core.ConnectionMode(conn.Mode)
		if connMode == "" {
			connMode = core.ConnectionModeUser
		}
		up, err := mcpupstream.New(ctx, name, mp.MCPURL, connMode, deps.Egress.Resolver)
		if err != nil {
			return nil, fmt.Errorf("create mcp upstream for %q: %w", name, err)
		}
		if allowedOps != nil {
			if err := up.FilterOperations(allowedOps); err != nil {
				_ = up.Close()
				return nil, fmt.Errorf("filter mcp operations for %q: %w", name, err)
			}
		}
		result, resultErr := specLoadedResult(name, intg, manifest, up, deps, regStore)
		if resultErr != nil {
			_ = up.Close()
			return nil, resultErr
		}
		return result, nil

	default:
		return nil, fmt.Errorf("inline spec-loaded provider %q has no spec URL", name)
	}
}

func specLoadedResult(name string, intg config.IntegrationDef, manifest *pluginmanifestv1.Manifest, prov core.Provider, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	applyPluginIcon(prov, intg)
	result := &ProviderBuildResult{Provider: prov}
	if err := attachManifestOAuth(name, intg, manifest, deps, regStore, result); err != nil {
		return nil, err
	}
	return result, nil
}

func convertAllowedOperations(ops map[string]*pluginmanifestv1.ManifestOperationOverride) map[string]*config.OperationOverride {
	if ops == nil {
		return nil
	}
	result := make(map[string]*config.OperationOverride, len(ops))
	for k, v := range ops {
		if v == nil {
			result[k] = nil
			continue
		}
		result[k] = &config.OperationOverride{
			Alias:       v.Alias,
			Description: v.Description,
		}
	}
	return result
}

func pluginConnectionDef(plugin *config.PluginDef) config.ConnectionDef {
	conn := config.ConnectionDef{}
	if plugin == nil {
		return conn
	}
	if plugin.Auth != nil {
		conn.Auth = *plugin.Auth
	}
	conn.Params = plugin.ConnectionParams
	return conn
}

func buildDeclarativeProvider(name string, intg config.IntegrationDef, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	if intg.Plugin == nil || intg.Plugin.ResolvedManifestPath == "" {
		return nil, fmt.Errorf("declarative provider %q has no resolved manifest path", name)
	}
	_, manifest, err := pluginpkg.ReadManifestFile(intg.Plugin.ResolvedManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest for declarative provider %q: %w", name, err)
	}
	prov, err := pluginhost.NewDeclarativeProvider(manifest, nil)
	if err != nil {
		return nil, fmt.Errorf("create declarative provider %q: %w", name, err)
	}
	applyPluginIcon(prov, intg)

	restricted, err := applyAllowedOperations(name, intg, prov)
	if err != nil {
		return nil, err
	}
	result := &ProviderBuildResult{Provider: restricted}
	if err := attachManifestOAuth(name, intg, manifest, deps, regStore, result); err != nil {
		return nil, err
	}
	return result, nil
}

func applyAllowedOperations(name string, intg config.IntegrationDef, pluginProv core.Provider) (core.Provider, error) {
	if intg.Plugin.AllowedOperations != nil && len(intg.Plugin.AllowedOperations) == 0 {
		return nil, fmt.Errorf("integration %q plugin.allowed_operations cannot be empty; omit the field to allow all", name)
	}
	if len(intg.Plugin.AllowedOperations) == 0 {
		return pluginProv, nil
	}

	provOps := make(map[string]struct{}, len(pluginProv.ListOperations()))
	for _, op := range pluginProv.ListOperations() {
		provOps[op.Name] = struct{}{}
	}

	opsMap := make(map[string]string, len(intg.Plugin.AllowedOperations))
	descs := make(map[string]string)
	exposedNames := make(map[string]string, len(intg.Plugin.AllowedOperations))
	for opName, override := range intg.Plugin.AllowedOperations {
		if _, ok := provOps[opName]; !ok {
			return nil, fmt.Errorf("integration %q plugin.allowed_operations references unknown operation %q", name, opName)
		}
		exposed := opName
		if override != nil && override.Alias != "" {
			exposed = override.Alias
			opsMap[exposed] = opName
		} else {
			opsMap[opName] = ""
		}
		if existing, ok := exposedNames[exposed]; ok {
			return nil, fmt.Errorf("integration %q plugin: alias collision: %q and %q both resolve to %q", name, existing, opName, exposed)
		}
		exposedNames[exposed] = opName
		if override != nil && override.Description != "" {
			descs[exposed] = override.Description
		}
	}
	var opts []integration.RestrictedOption
	if len(descs) > 0 {
		opts = append(opts, integration.WithDescriptions(descs))
	}
	return integration.NewRestricted(pluginProv, opsMap, opts...), nil
}

func buildPluginProvider(ctx context.Context, name string, intg config.IntegrationDef) (core.Provider, map[string]any, error) {
	pluginConfig, err := config.NodeToMap(intg.Plugin.Config)
	if err != nil {
		return nil, nil, fmt.Errorf("decode plugin config for %q: %w", name, err)
	}
	prov, err := pluginhost.NewExecutableProvider(ctx, pluginhost.ExecConfig{
		Command: intg.Plugin.Command,
		Args:    intg.Plugin.Args,
		Env:     intg.Plugin.Env,
		Name:    name,
		Config:  pluginConfig,
	})
	if err != nil {
		return nil, nil, err
	}
	return prov, pluginConfig, nil
}

func applyPluginIcon(prov core.Provider, intg config.IntegrationDef) {
	if intg.IconFile == "" {
		return
	}
	svg, err := provider.ReadIconFile(intg.IconFile)
	if err != nil {
		slog.Warn("could not read plugin icon_file", "path", intg.IconFile, "error", err)
		return
	}
	if svg == "" {
		return
	}
	type iconSetter interface{ SetIconSVG(string) }
	if setter, ok := prov.(iconSetter); ok {
		setter.SetIconSVG(svg)
	}
}

func attachManifestOAuth(name string, intg config.IntegrationDef, manifest *pluginmanifestv1.Manifest, deps Deps, regStore *lazyRegStore, result *ProviderBuildResult) error {
	if manifest.Provider == nil || manifest.Provider.Auth == nil {
		return nil
	}

	switch manifest.Provider.Auth.Type {
	case pluginmanifestv1.AuthTypeOAuth2:
		pluginConfig, err := config.NodeToMap(intg.Plugin.Config)
		if err != nil {
			return fmt.Errorf("decode plugin config for %q: %w", name, err)
		}
		authHandler, err := buildOAuthHandlerFromManifest(manifest, pluginConfig, deps)
		if err != nil {
			slog.Warn("cannot build oauth handler from manifest", "provider", name, "error", err)
			return nil
		}
		if authHandler != nil {
			result.ConnectionAuth = map[string]OAuthHandler{
				config.PluginConnectionName: authHandler,
			}
		}

	case pluginmanifestv1.AuthTypeMCPOAuth:
		mcpURL := manifest.Provider.MCPURL
		if mcpURL == "" && intg.Plugin != nil {
			mcpURL = intg.Plugin.MCPURL
		}
		if mcpURL == "" {
			slog.Warn("mcp_oauth auth requires mcp_url", "provider", name)
			return nil
		}
		conn := pluginConnectionDef(intg.Plugin)
		handler := buildMCPOAuthHandler(conn, mcpURL, regStore.get(), deps)
		result.ConnectionAuth = map[string]OAuthHandler{
			config.PluginConnectionName: handler,
		}
	}

	return nil
}

func attachPluginOAuth(name string, intg config.IntegrationDef, pluginConfig map[string]any, deps Deps, result *ProviderBuildResult) {
	if intg.Plugin == nil || intg.Plugin.ResolvedManifestPath == "" {
		return
	}
	authHandler, err := buildPluginOAuthHandler(intg, pluginConfig, deps)
	if err != nil {
		slog.Warn("cannot build oauth handler from manifest", "provider", name, "error", err)
		return
	}
	if authHandler == nil {
		return
	}
	if result.ConnectionAuth == nil {
		result.ConnectionAuth = make(map[string]OAuthHandler)
	}
	result.ConnectionAuth[config.PluginConnectionName] = authHandler
}

func buildPluginOAuthHandler(intg config.IntegrationDef, pluginConfig map[string]any, deps Deps) (OAuthHandler, error) {
	if intg.Plugin == nil || intg.Plugin.ResolvedManifestPath == "" {
		return nil, nil
	}
	_, manifest, err := pluginpkg.ReadManifestFile(intg.Plugin.ResolvedManifestPath)
	if err != nil {
		return nil, err
	}
	return buildOAuthHandlerFromManifest(manifest, pluginConfig, deps)
}

func buildOAuthHandlerFromManifest(manifest *pluginmanifestv1.Manifest, pluginConfig map[string]any, deps Deps) (OAuthHandler, error) {
	if manifest.Provider == nil || manifest.Provider.Auth == nil || manifest.Provider.Auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
		return nil, nil
	}
	auth := manifest.Provider.Auth

	clientID := auth.ClientID
	clientSecret := auth.ClientSecret
	if id, _ := pluginConfig["client_id"].(string); id != "" {
		clientID = id
	}
	if sec, _ := pluginConfig["client_secret"].(string); sec != "" {
		clientSecret = sec
	}
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required for oauth2 auth (set in plugin.auth or plugin.config)")
	}

	redirectURL := deps.BaseURL + config.IntegrationCallbackPath

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
		RedirectURL:         redirectURL,
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

	handler := oauth.NewUpstream(oauthCfg)
	return WrapUpstreamHandler(handler), nil
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

func applyManifestResponseMapping(def *provider.Definition, mp *pluginmanifestv1.Provider) {
	if mp.ResponseMapping == nil {
		return
	}
	rm := &provider.ResponseMappingDef{
		DataPath: mp.ResponseMapping.DataPath,
	}
	if mp.ResponseMapping.Pagination != nil {
		rm.Pagination = &provider.PaginationMappingDef{
			HasMorePath: mp.ResponseMapping.Pagination.HasMorePath,
			CursorPath:  mp.ResponseMapping.Pagination.CursorPath,
		}
	}
	def.ResponseMapping = rm
}

func BuildConnectionMap(cfg *config.Config) invocation.ConnectionMap {
	m := make(invocation.ConnectionMap, len(cfg.Integrations))
	for name := range cfg.Integrations {
		m[name] = config.PluginConnectionName
	}
	return m
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

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/core/integration"
	"github.com/valon-technologies/gestalt/internal/composite"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/oauth"
	"github.com/valon-technologies/gestalt/internal/pluginapi"
	"github.com/valon-technologies/gestalt/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/internal/provider"
	"github.com/valon-technologies/gestalt/internal/registry"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
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

// ProviderBuildResult is the return value of a ProviderFactory. It carries
// the constructed provider and an OAuth handler for each named connection
// that uses oauth2 or mcp_oauth auth.
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
type ProviderFactory func(ctx context.Context, name string, intg config.IntegrationDef, deps Deps) (*ProviderBuildResult, error)
type SecretManagerFactory func(node yaml.Node) (core.SecretManager, error)
type TelemetryFactory func(node yaml.Node) (core.TelemetryProvider, error)

type FactoryRegistry struct {
	Auth            map[string]AuthFactory
	Datastores      map[string]DatastoreFactory
	Providers       map[string]ProviderFactory
	DefaultProvider ProviderFactory
	Secrets         map[string]SecretManagerFactory
	Telemetry       map[string]TelemetryFactory
	Runtimes        map[string]RuntimeFactory
	Bindings        map[string]BindingFactory
	Builtins        []core.Provider
}

func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{
		Auth:       make(map[string]AuthFactory),
		Datastores: make(map[string]DatastoreFactory),
		Providers:  make(map[string]ProviderFactory),
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

	deps := Deps{
		EncryptionKey: crypto.DeriveKey(cfg.Server.EncryptionKey),
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
		for cname := range intg.Connections {
			conn := intg.Connections[cname]
			if err := resolveStringFields(&conn, resolve); err != nil {
				return err
			}
			intg.Connections[cname] = conn
		}
		if intg.API != nil {
			if err := resolveStringFields(intg.API, resolve); err != nil {
				return err
			}
		}
		if intg.MCP != nil {
			if err := resolveStringFields(intg.MCP, resolve); err != nil {
				return err
			}
		}
		if intg.Plugin != nil {
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

func buildProviders(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (*registry.PluginMap[core.Provider], <-chan struct{}, func() map[string]map[string]OAuthHandler, error) {
	reg := registry.New()
	connAuth := make(map[string]map[string]OAuthHandler)
	var connMu sync.Mutex

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
		if err := validateProviderBuildAvailable(name, intgDef, factories); err != nil {
			close(ready)
			return nil, nil, nil, fmt.Errorf("bootstrap: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := buildProvider(ctx, name, intgDef, factories, deps)
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

func buildProvider(ctx context.Context, name string, intg config.IntegrationDef, factories *FactoryRegistry, deps Deps) (*ProviderBuildResult, error) {
	if intg.Plugin != nil {
		if intg.Plugin.IsDeclarative {
			return buildDeclarativeProvider(name, intg, deps)
		}
		pluginProv, pluginConfig, err := buildPluginProvider(ctx, name, intg)
		if err != nil {
			return nil, err
		}

		restricted, err := applyAllowedOperations(name, intg, pluginProv)
		if err != nil {
			if c, ok := pluginProv.(interface{ Close() error }); ok {
				_ = c.Close()
			}
			return nil, err
		}

		if intg.MCP != nil {
			return buildPluginMCPProvider(ctx, name, intg, restricted, pluginConfig, factories, deps)
		}

		buildResult := &ProviderBuildResult{Provider: restricted}
		attachPluginOAuth(name, intg, pluginConfig, deps, buildResult)
		return buildResult, nil
	}

	factory, err := providerFactoryForName(name, factories)
	if err != nil {
		return nil, err
	}
	return factory(ctx, name, intg, deps)
}

func buildDeclarativeProvider(name string, intg config.IntegrationDef, deps Deps) (*ProviderBuildResult, error) {
	if intg.Plugin == nil || intg.Plugin.ResolvedManifestPath == "" {
		return nil, fmt.Errorf("declarative provider %q has no resolved manifest path", name)
	}
	_, manifest, err := pluginpkg.ReadManifestFile(intg.Plugin.ResolvedManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest for declarative provider %q: %w", name, err)
	}
	prov, err := pluginapi.NewDeclarativeProvider(manifest, nil)
	if err != nil {
		return nil, fmt.Errorf("create declarative provider %q: %w", name, err)
	}

	restricted, err := applyAllowedOperations(name, intg, prov)
	if err != nil {
		return nil, err
	}
	result := &ProviderBuildResult{Provider: restricted}

	pluginConfig, err := config.NodeToMap(intg.Plugin.Config)
	if err != nil {
		return nil, fmt.Errorf("decode plugin config for %q: %w", name, err)
	}
	authHandler, err := buildOAuthHandlerFromManifest(manifest, pluginConfig, deps)
	if err != nil {
		slog.Warn("cannot build oauth handler from manifest", "provider", name, "error", err)
	} else if authHandler != nil {
		result.ConnectionAuth = map[string]OAuthHandler{
			config.PluginConnectionName: authHandler,
		}
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
	prov, err := pluginapi.NewExecutableProvider(ctx, pluginapi.ExecConfig{
		Command:      intg.Plugin.Command,
		Args:         intg.Plugin.Args,
		Env:          intg.Plugin.Env,
		Name:         name,
		Config:       pluginConfig,
		AllowedHosts: intg.Plugin.AllowedHosts,
	})
	if err != nil {
		return nil, nil, err
	}
	return prov, pluginConfig, nil
}

func buildPluginMCPProvider(ctx context.Context, name string, intg config.IntegrationDef, pluginProv core.Provider, pluginConfig map[string]any, factories *FactoryRegistry, deps Deps) (*ProviderBuildResult, error) {
	cleanupPlugin := true
	defer func() {
		if cleanupPlugin {
			if c, ok := pluginProv.(interface{ Close() error }); ok {
				_ = c.Close()
			}
		}
	}()

	factory, err := providerFactoryForName(name, factories)
	if err != nil {
		return nil, fmt.Errorf("building mcp surface for plugin+mcp %q: %w", name, err)
	}

	mcpResult, err := factory(ctx, name, intg, deps)
	if err != nil {
		return nil, fmt.Errorf("building mcp surface for plugin+mcp %q: %w", name, err)
	}

	mcpUp, ok := mcpResult.Provider.(composite.MCPUpstream)
	if !ok {
		if c, ok := mcpResult.Provider.(interface{ Close() error }); ok {
			_ = c.Close()
		}
		return nil, fmt.Errorf("plugin+mcp %q: provider factory returned %T which does not implement MCPUpstream", name, mcpResult.Provider)
	}
	composed := composite.New(name, pluginProv, mcpUp)

	result := &ProviderBuildResult{
		Provider:       composed,
		ConnectionAuth: mcpResult.ConnectionAuth,
	}
	attachPluginOAuth(name, intg, pluginConfig, deps, result)

	cleanupPlugin = false
	return result, nil
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

	clientID, _ := pluginConfig["client_id"].(string)
	clientSecret, _ := pluginConfig["client_secret"].(string)
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("plugin.config must provide client_id and client_secret for oauth2 auth")
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
		ScopeSeparator:      auth.ScopeSeparator,
		TokenExchange:       tokenExchange,
		AuthorizationParams: auth.AuthorizationParams,
	}
	if auth.ClientAuth == "header" {
		oauthCfg.ClientAuthMethod = oauth.ClientAuthHeader
	}

	handler := oauth.NewUpstream(oauthCfg)
	return WrapUpstreamHandler(handler), nil
}

func providerFactoryForName(name string, factories *FactoryRegistry) (ProviderFactory, error) {
	factory, ok := factories.Providers[name]
	if !ok {
		factory = factories.DefaultProvider
	}
	if factory == nil {
		return nil, fmt.Errorf("no provider factory for %q and no default factory registered", name)
	}
	return factory, nil
}

func validateProviderBuildAvailable(name string, intg config.IntegrationDef, factories *FactoryRegistry) error {
	if intg.Plugin != nil && intg.MCP == nil {
		return nil
	}
	_, err := providerFactoryForName(name, factories)
	return err
}

// BuildConnectionMap returns a map from integration name to its default
// connection name. Used by the broker and server to resolve connections.
// For hybrid integrations, the default is PluginConnectionName; the MCP and
// server handlers pass per-tool connection names from the surface config.
func BuildConnectionMap(cfg *config.Config) invocation.ConnectionMap {
	m := make(invocation.ConnectionMap, len(cfg.Integrations))
	for name, intg := range cfg.Integrations {
		switch {
		case intg.Plugin != nil:
			m[name] = config.PluginConnectionName
		case intg.API != nil:
			m[name] = intg.API.Connection
		case intg.MCP != nil:
			m[name] = intg.MCP.Connection
		}
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

// BuildResultWithOAuth wraps a provider in a ProviderBuildResult and builds an
// OAuth handler for the API connection when applicable. Named provider factories
// (BigQuery, Jira, etc.) use this to avoid duplicating the connection auth
// construction logic.
func BuildResultWithOAuth(prov core.Provider, def *provider.Definition, intg config.IntegrationDef, conn config.ConnectionDef) *ProviderBuildResult {
	result := &ProviderBuildResult{Provider: prov}
	if intg.API == nil {
		return result
	}
	switch conn.Auth.Type {
	case "manual", "api_key":
		return result
	case "":
		if conn.Auth.AuthorizationURL == "" && conn.Auth.ClientID == "" {
			return result
		}
	}
	upstream, err := provider.BuildOAuthUpstream(def, conn, def.BaseURL, nil)
	if err != nil {
		slog.Warn("cannot build oauth handler for connection", "provider", prov.Name(), "connection", intg.API.Connection, "error", err)
		return result
	}
	result.ConnectionAuth = map[string]OAuthHandler{
		intg.API.Connection: WrapUpstreamHandler(upstream),
	}
	return result
}

// ResolveAPIConnection returns the ConnectionDef referenced by the
// integration's API surface. Named provider factories use this to extract
// auth configuration from the V1 manifest.
func ResolveAPIConnection(intg config.IntegrationDef) (config.ConnectionDef, error) {
	if intg.API == nil {
		return config.ConnectionDef{}, fmt.Errorf("integration has no api surface")
	}
	conn, ok := intg.Connections[intg.API.Connection]
	if !ok {
		return config.ConnectionDef{}, fmt.Errorf("api.connection %q not found in connections", intg.API.Connection)
	}
	return conn, nil
}

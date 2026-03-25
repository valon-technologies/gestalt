package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/internal/composite"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/pluginapi"
	"github.com/valon-technologies/gestalt/internal/registry"
	"gopkg.in/yaml.v3"
)

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
type ProviderFactory func(ctx context.Context, name string, intg config.IntegrationDef, deps Deps) (core.Provider, error)
type SecretManagerFactory func(node yaml.Node) (core.SecretManager, error)
type BindingDeps struct {
	Invoker invocation.Invoker
	Egress  EgressDeps
}

type RuntimeDeps struct {
	Invoker          invocation.Invoker
	CapabilityLister invocation.CapabilityLister
	Egress           EgressDeps
}

type RuntimeFactory func(ctx context.Context, name string, cfg config.RuntimeDef, deps RuntimeDeps) (core.Runtime, error)
type BindingFactory func(ctx context.Context, name string, cfg config.BindingDef, deps BindingDeps) (core.Binding, error)

type FactoryRegistry struct {
	Auth            map[string]AuthFactory
	Datastores      map[string]DatastoreFactory
	Providers       map[string]ProviderFactory
	DefaultProvider ProviderFactory
	Secrets         map[string]SecretManagerFactory
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
		Runtimes:   make(map[string]RuntimeFactory),
		Bindings:   make(map[string]BindingFactory),
	}
}

type Result struct {
	Auth             core.AuthProvider
	Datastore        core.Datastore
	Providers        *registry.PluginMap[core.Provider]
	ProvidersReady   <-chan struct{}
	Runtimes         *registry.PluginMap[core.Runtime]
	Bindings         *registry.PluginMap[core.Binding]
	Invoker          invocation.Invoker
	CapabilityLister invocation.CapabilityLister
	AuditSink        core.AuditSink
	SecretManager    core.SecretManager
	Egress           EgressDeps
	DevMode          bool
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
	deps.Egress = newEgressDeps(cfg, ds)

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

	providers, providersReady, err := buildProviders(ctx, cfg, factories, deps)
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

	sharedInvoker := invocation.NewBroker(providers, ds)
	audit := core.AuditSink(invocation.LogAuditSink{})

	runtimes, err := buildRuntimes(ctx, cfg, factories, sharedInvoker, sharedInvoker, audit, deps.Egress)
	if err != nil {
		return nil, err
	}
	stopRuntimes := true
	defer func() {
		if stopRuntimes {
			_ = StopRuntimes(context.Background(), runtimes, runtimeNames(runtimes))
		}
	}()

	bindings, err := buildBindings(ctx, cfg, factories, sharedInvoker, sharedInvoker, audit, deps.Egress)
	if err != nil {
		return nil, err
	}

	closeProviders = false
	stopRuntimes = false
	closeSM = false
	return &Result{
		Auth:             auth,
		Datastore:        ds,
		Providers:        providers,
		ProvidersReady:   providersReady,
		Runtimes:         runtimes,
		Bindings:         bindings,
		Invoker:          sharedInvoker,
		CapabilityLister: sharedInvoker,
		AuditSink:        audit,
		SecretManager:    sm,
		Egress:           deps.Egress,
		DevMode:          cfg.Server.DevMode,
	}, nil
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
		cfg.Integrations[name] = intg
	}
	for name := range cfg.AuthProfiles {
		profile := cfg.AuthProfiles[name]
		if err := resolveStringFields(&profile, resolve); err != nil {
			return err
		}
		cfg.AuthProfiles[name] = profile
	}

	// Skip cfg.Secrets.Config to avoid self-referential resolution.
	if err := resolveYAMLNode(&cfg.Auth.Config, resolve); err != nil {
		return err
	}
	if err := resolveYAMLNode(&cfg.Datastore.Config, resolve); err != nil {
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

func buildProviders(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (*registry.PluginMap[core.Provider], <-chan struct{}, error) {
	reg := registry.New()

	for _, builtin := range factories.Builtins {
		if err := reg.Providers.Register(builtin.Name(), builtin); errors.Is(err, core.ErrAlreadyRegistered) {
			continue
		} else if err != nil {
			return nil, nil, fmt.Errorf("bootstrap: registering builtin %q: %w", builtin.Name(), err)
		}
		log.Printf("loaded builtin provider %s (%d operations)", builtin.Name(), len(builtin.ListOperations()))
	}

	ready := make(chan struct{})
	if len(cfg.Integrations) == 0 {
		close(ready)
		return &reg.Providers, ready, nil
	}

	var wg sync.WaitGroup
	for name := range cfg.Integrations {
		intgDef := cfg.Integrations[name]
		if !providerBuildAvailable(name, intgDef, factories) {
			close(ready)
			return nil, nil, fmt.Errorf("bootstrap: no provider factory for %q and no default factory registered", name)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			prov, err := buildProvider(ctx, name, intgDef, factories, deps)
			if err != nil {
				log.Printf("WARNING: skipping provider %q: %v", name, err)
				return
			}
			if err := reg.Providers.Register(name, prov); err != nil {
				if c, ok := prov.(interface{ Close() error }); ok {
					_ = c.Close()
				}
				log.Printf("WARNING: registering provider %q: %v", name, err)
				return
			}
			log.Printf("loaded provider %s (%d operations)", name, len(prov.ListOperations()))
		}()
	}
	go func() {
		wg.Wait()
		close(ready)
	}()

	return &reg.Providers, ready, nil
}

func buildRuntimes(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, egressDeps EgressDeps) (*registry.PluginMap[core.Runtime], error) {
	if len(cfg.Runtimes) == 0 {
		return nil, nil
	}

	runtimes := registry.NewRuntimeMap()

	for name := range cfg.Runtimes {
		def := cfg.Runtimes[name]
		deps := runtimeDepsForProviders(name, invoker, lister, def.Providers, audit, egressDeps)
		rt, err := buildRuntime(ctx, name, def, factories, deps)
		if err != nil {
			_ = StopRuntimes(context.Background(), runtimes, runtimes.List())
			return nil, fmt.Errorf("bootstrap: runtime %q: %w", name, err)
		}

		if err := runtimes.Register(name, rt); err != nil {
			_ = rt.Stop(context.Background())
			_ = StopRuntimes(context.Background(), runtimes, runtimes.List())
			return nil, fmt.Errorf("bootstrap: registering runtime %q: %w", name, err)
		}
		log.Printf("loaded runtime %s (type=%s, providers=%v)", name, def.Type, def.Providers)
	}

	return runtimes, nil
}

func buildBindings(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, egressDeps EgressDeps) (*registry.PluginMap[core.Binding], error) {
	if len(cfg.Bindings) == 0 {
		return nil, nil
	}

	bindings := registry.NewBindingMap()

	for name := range cfg.Bindings {
		def := cfg.Bindings[name]
		factory, ok := factories.Bindings[def.Type]
		if !ok {
			_ = CloseBindings(bindings, bindings.List())
			return nil, fmt.Errorf("bootstrap: unknown binding type %q for binding %q", def.Type, name)
		}

		deps := bindingDepsForProviders(name, invoker, lister, def.Providers, audit, egressDeps)
		binding, err := factory(ctx, name, def, deps)
		if err != nil {
			_ = CloseBindings(bindings, bindings.List())
			return nil, fmt.Errorf("bootstrap: binding %q: %w", name, err)
		}

		if err := bindings.Register(name, binding); err != nil {
			_ = binding.Close()
			_ = CloseBindings(bindings, bindings.List())
			return nil, fmt.Errorf("bootstrap: registering binding %q: %w", name, err)
		}
		log.Printf("loaded binding %s (type=%s, providers=%v)", name, def.Type, def.Providers)
	}

	return bindings, nil
}

func buildProvider(ctx context.Context, name string, intg config.IntegrationDef, factories *FactoryRegistry, deps Deps) (core.Provider, error) {
	if intg.Plugin != nil {
		mode := intg.Plugin.Mode
		if mode == "" {
			mode = config.PluginModeReplace
		}

		if mode == config.PluginModeOverlay {
			baseIntg := intg
			baseIntg.Plugin = nil
			factory, ok := factories.Providers[name]
			if !ok {
				factory = factories.DefaultProvider
			}
			if factory == nil {
				return nil, fmt.Errorf("no provider factory for overlay base %q", name)
			}
			baseProv, err := factory(ctx, name, baseIntg, deps)
			if err != nil {
				return nil, fmt.Errorf("building overlay base: %w", err)
			}
			overlayProv, err := pluginapi.NewExecutableProvider(ctx, pluginapi.ExecConfig{
				Command: intg.Plugin.Command,
				Args:    intg.Plugin.Args,
				Env:     intg.Plugin.Env,
			})
			if err != nil {
				if c, ok := baseProv.(interface{ Close() error }); ok {
					_ = c.Close()
				}
				return nil, fmt.Errorf("building overlay plugin: %w", err)
			}
			return composite.NewOverlay(name, baseProv, overlayProv), nil
		}

		return pluginapi.NewExecutableProvider(ctx, pluginapi.ExecConfig{
			Command: intg.Plugin.Command,
			Args:    intg.Plugin.Args,
			Env:     intg.Plugin.Env,
		})
	}

	factory, ok := factories.Providers[name]
	if !ok {
		factory = factories.DefaultProvider
	}
	if factory == nil {
		return nil, fmt.Errorf("no provider factory for %q and no default factory registered", name)
	}
	return factory(ctx, name, intg, deps)
}

func providerBuildAvailable(name string, intg config.IntegrationDef, factories *FactoryRegistry) bool {
	if intg.Plugin != nil {
		mode := intg.Plugin.Mode
		if mode == "" {
			mode = config.PluginModeReplace
		}
		if mode == config.PluginModeOverlay {
			if _, ok := factories.Providers[name]; ok {
				return true
			}
			return factories.DefaultProvider != nil
		}
		return true
	}
	if _, ok := factories.Providers[name]; ok {
		return true
	}
	return factories.DefaultProvider != nil
}

func buildRuntime(ctx context.Context, name string, cfg config.RuntimeDef, factories *FactoryRegistry, deps RuntimeDeps) (core.Runtime, error) {
	if cfg.Plugin != nil {
		m, err := nodeToMap(cfg.Config)
		if err != nil {
			return nil, fmt.Errorf("decode runtime config: %w", err)
		}
		return pluginapi.NewExecutableRuntime(ctx, name, pluginapi.ExecConfig{
			Command: cfg.Plugin.Command,
			Args:    cfg.Plugin.Args,
			Env:     cfg.Plugin.Env,
		}, m, deps.Invoker, deps.CapabilityLister)
	}

	factory, ok := factories.Runtimes[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("unknown runtime type %q", cfg.Type)
	}
	return factory(ctx, name, cfg, deps)
}

func nodeToMap(node yaml.Node) (map[string]any, error) {
	if node.Kind == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := node.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func runtimeNames(runtimes *registry.PluginMap[core.Runtime]) []string {
	if runtimes == nil {
		return nil
	}
	return runtimes.List()
}

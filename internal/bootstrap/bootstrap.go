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
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/registry"
	"gopkg.in/yaml.v3"
)

type Deps struct {
	EncryptionKey []byte
	BaseURL       string
	SecretManager core.SecretManager
}

type AuthFactory func(node yaml.Node, deps Deps) (core.AuthProvider, error)
type DatastoreFactory func(node yaml.Node, deps Deps) (core.Datastore, error)
type ProviderFactory func(ctx context.Context, name string, intg config.IntegrationDef, deps Deps) (core.Provider, error)
type SecretManagerFactory func(node yaml.Node) (core.SecretManager, error)
type BindingDeps struct {
	Invoker  invocation.Invoker
	Runtimes *registry.PluginMap[core.Runtime]
}

type RuntimeDeps struct {
	Invoker          invocation.Invoker
	CapabilityLister invocation.CapabilityLister
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
	Runtimes         *registry.PluginMap[core.Runtime]
	Bindings         *registry.PluginMap[core.Binding]
	Invoker          invocation.Invoker
	CapabilityLister invocation.CapabilityLister
	AuditSink        core.AuditSink
	SecretManager    core.SecretManager
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

	providers, err := buildProviders(ctx, cfg, factories, deps)
	if err != nil {
		return nil, err
	}

	sharedInvoker := invocation.NewBroker(providers, ds)
	audit := core.AuditSink(invocation.LogAuditSink{})

	runtimes, err := buildRuntimes(ctx, cfg, factories, sharedInvoker, sharedInvoker, audit)
	if err != nil {
		return nil, err
	}
	if runtimes == nil {
		runtimes = registry.NewRuntimeMap()
	}

	bindings, err := buildBindings(ctx, cfg, factories, sharedInvoker, sharedInvoker, audit, runtimes)
	if err != nil {
		return nil, err
	}

	closeSM = false
	return &Result{
		Auth:             auth,
		Datastore:        ds,
		Providers:        providers,
		Runtimes:         runtimes,
		Bindings:         bindings,
		Invoker:          sharedInvoker,
		CapabilityLister: sharedInvoker,
		AuditSink:        audit,
		SecretManager:    sm,
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

func buildProviders(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (*registry.PluginMap[core.Provider], error) {
	providers := make(map[string]core.Provider, len(cfg.Integrations))
	var mu sync.Mutex

	if len(cfg.Integrations) > 0 {
		var wg sync.WaitGroup
		for name := range cfg.Integrations {
			intgDef := cfg.Integrations[name]
			factory, ok := factories.Providers[name]
			if !ok {
				factory = factories.DefaultProvider
			}
			if factory == nil {
				return nil, fmt.Errorf("bootstrap: no provider factory for %q and no default factory registered", name)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				prov, err := factory(ctx, name, intgDef, deps)
				if err != nil {
					log.Printf("WARNING: skipping provider %q: %v", name, err)
					return
				}
				mu.Lock()
				providers[name] = prov
				mu.Unlock()
			}()
		}
		wg.Wait()
	}

	reg := registry.New()
	for name, prov := range providers {
		if err := reg.Providers.Register(name, prov); err != nil {
			return nil, fmt.Errorf("bootstrap: registering provider %q: %w", name, err)
		}
		log.Printf("loaded provider %s (%d operations)", name, len(prov.ListOperations()))
	}

	for _, builtin := range factories.Builtins {
		if err := reg.Providers.Register(builtin.Name(), builtin); errors.Is(err, core.ErrAlreadyRegistered) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("bootstrap: registering builtin %q: %w", builtin.Name(), err)
		}
		log.Printf("loaded builtin provider %s (%d operations)", builtin.Name(), len(builtin.ListOperations()))
	}

	return &reg.Providers, nil
}

func buildRuntimes(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink) (*registry.PluginMap[core.Runtime], error) {
	if len(cfg.Runtimes) == 0 {
		return nil, nil
	}

	runtimes := registry.NewRuntimeMap()

	for name := range cfg.Runtimes {
		def := cfg.Runtimes[name]
		factory, ok := factories.Runtimes[def.Type]
		if !ok {
			return nil, fmt.Errorf("bootstrap: unknown runtime type %q for runtime %q", def.Type, name)
		}

		deps := runtimeDepsForProviders(name, invoker, lister, def.Providers, audit)
		rt, err := factory(ctx, name, def, deps)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: runtime %q: %w", name, err)
		}

		if err := runtimes.Register(name, rt); err != nil {
			return nil, fmt.Errorf("bootstrap: registering runtime %q: %w", name, err)
		}
		log.Printf("loaded runtime %s (type=%s, providers=%v)", name, def.Type, def.Providers)
	}

	return runtimes, nil
}

func buildBindings(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, runtimes *registry.PluginMap[core.Runtime]) (*registry.PluginMap[core.Binding], error) {
	if len(cfg.Bindings) == 0 {
		return nil, nil
	}

	bindings := registry.NewBindingMap()

	for name := range cfg.Bindings {
		def := cfg.Bindings[name]
		factory, ok := factories.Bindings[def.Type]
		if !ok {
			return nil, fmt.Errorf("bootstrap: unknown binding type %q for binding %q", def.Type, name)
		}

		deps := bindingDepsForProviders(name, invoker, lister, def.Providers, audit, runtimes)
		binding, err := factory(ctx, name, def, deps)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: binding %q: %w", name, err)
		}

		if err := bindings.Register(name, binding); err != nil {
			return nil, fmt.Errorf("bootstrap: registering binding %q: %w", name, err)
		}
		log.Printf("loaded binding %s (type=%s, providers=%v)", name, def.Type, def.Providers)
	}

	return bindings, nil
}

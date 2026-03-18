package bootstrap

import (
	"context"
	"fmt"
	"log"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/core/crypto"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/internal/registry"
	"gopkg.in/yaml.v3"
)

type Deps struct {
	EncryptionKey []byte
	BaseURL       string
}

type AuthFactory func(node yaml.Node, deps Deps) (core.AuthProvider, error)
type DatastoreFactory func(node yaml.Node, deps Deps) (core.Datastore, error)
type ProviderFactory func(ctx context.Context, name string, intg config.IntegrationDef, deps Deps) (core.Provider, error)

type FactoryRegistry struct {
	Auth            map[string]AuthFactory
	Datastores      map[string]DatastoreFactory
	Providers       map[string]ProviderFactory
	DefaultProvider ProviderFactory
}

func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{
		Auth:       make(map[string]AuthFactory),
		Datastores: make(map[string]DatastoreFactory),
		Providers:  make(map[string]ProviderFactory),
	}
}

type Result struct {
	Auth      core.AuthProvider
	Datastore core.Datastore
	Providers *registry.PluginMap[core.Provider]
	DevMode   bool
}

func Bootstrap(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) (*Result, error) {
	deps := Deps{
		EncryptionKey: crypto.DeriveKey(cfg.Server.EncryptionKey),
		BaseURL:       cfg.Server.BaseURL,
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

	return &Result{
		Auth:      auth,
		Datastore: ds,
		Providers: providers,
		DevMode:   cfg.Server.DevMode,
	}, nil
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
	if len(cfg.Integrations) == 0 {
		reg := registry.New()
		return &reg.Providers, nil
	}

	providers := make(map[string]core.Provider, len(cfg.Integrations))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	for name := range cfg.Integrations {
		intgDef := cfg.Integrations[name]
		factory, ok := factories.Providers[name]
		if !ok {
			factory = factories.DefaultProvider
		}
		if factory == nil {
			return nil, fmt.Errorf("bootstrap: no provider factory for %q and no default factory registered", name)
		}
		g.Go(func() error {
			prov, err := factory(ctx, name, intgDef, deps)
			if err != nil {
				return fmt.Errorf("provider %q: %w", name, err)
			}
			mu.Lock()
			providers[name] = prov
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}

	reg := registry.New()
	for name, prov := range providers {
		if err := reg.Providers.Register(name, prov); err != nil {
			return nil, fmt.Errorf("bootstrap: registering provider %q: %w", name, err)
		}
		log.Printf("loaded provider %s (%d operations)", name, len(prov.ListOperations()))
	}
	return &reg.Providers, nil
}

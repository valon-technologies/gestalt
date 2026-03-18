package bootstrap

import (
	"fmt"

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
type IntegrationFactory func(intg config.IntegrationDef, deps Deps) (core.Integration, error)

type FactoryRegistry struct {
	Auth         map[string]AuthFactory
	Datastores   map[string]DatastoreFactory
	Integrations map[string]IntegrationFactory
}

func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{
		Auth:         make(map[string]AuthFactory),
		Datastores:   make(map[string]DatastoreFactory),
		Integrations: make(map[string]IntegrationFactory),
	}
}

type Result struct {
	Auth         core.AuthProvider
	Datastore    core.Datastore
	Integrations *registry.PluginMap[core.Integration]
	DevMode      bool
}

func Bootstrap(cfg *config.Config, factories *FactoryRegistry) (*Result, error) {
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

	var integrations *registry.PluginMap[core.Integration]
	if len(factories.Integrations) > 0 {
		integrations, err = buildIntegrations(cfg, factories, deps)
		if err != nil {
			return nil, err
		}
	}

	return &Result{
		Auth:         auth,
		Datastore:    ds,
		Integrations: integrations,
		DevMode:      cfg.Server.DevMode,
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

func buildIntegrations(cfg *config.Config, factories *FactoryRegistry, deps Deps) (*registry.PluginMap[core.Integration], error) {
	reg := registry.New()

	for name := range cfg.Integrations {
		intgDef := cfg.Integrations[name]
		factory, ok := factories.Integrations[name]
		if !ok {
			return nil, fmt.Errorf("bootstrap: unknown integration %q", name)
		}
		intg, err := factory(intgDef, deps)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: integration %q: %w", name, err)
		}

		if err := reg.Integrations.Register(name, intg); err != nil {
			return nil, fmt.Errorf("bootstrap: registering integration %q: %w", name, err)
		}
	}
	return &reg.Integrations, nil
}

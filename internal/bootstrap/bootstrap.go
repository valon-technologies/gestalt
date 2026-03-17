package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/internal/integration"
	"github.com/valon-technologies/toolshed/internal/registry"
	"gopkg.in/yaml.v3"
)

type integrationMeta struct {
	AllowedOperations []string `yaml:"allowed_operations"`
}

type Deps struct {
	EncryptionKey []byte
}

type AuthFactory func(node yaml.Node, deps Deps) (core.AuthProvider, error)
type DatastoreFactory func(node yaml.Node, deps Deps) (core.Datastore, error)
type IntegrationFactory func(node yaml.Node, deps Deps) (core.Integration, error)

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
		EncryptionKey: deriveKey(cfg.Server.EncryptionKey),
	}

	auth, err := buildAuth(cfg, factories, deps)
	if err != nil {
		return nil, err
	}

	ds, err := buildDatastore(cfg, factories, deps)
	if err != nil {
		return nil, err
	}

	integrations, err := buildIntegrations(cfg, factories, deps)
	if err != nil {
		return nil, err
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

	names := make([]string, 0, len(cfg.Integrations))
	for name := range cfg.Integrations {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		node := cfg.Integrations[name]
		factory, ok := factories.Integrations[name]
		if !ok {
			return nil, fmt.Errorf("bootstrap: unknown integration %q", name)
		}
		intg, err := factory(node, deps)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: integration %q: %w", name, err)
		}

		var meta integrationMeta
		if err := node.Decode(&meta); err != nil {
			return nil, fmt.Errorf("bootstrap: integration %q meta: %w", name, err)
		}

		if meta.AllowedOperations != nil && len(meta.AllowedOperations) == 0 {
			return nil, fmt.Errorf("bootstrap: integration %q: allowed_operations cannot be empty; omit the field to allow all operations", name)
		}

		if allowed := meta.AllowedOperations; len(allowed) > 0 {
			if !isWildcard(allowed) {
				opSet := make(map[string]struct{})
				for _, op := range intg.ListOperations() {
					opSet[op.Name] = struct{}{}
				}
				for _, opName := range allowed {
					if _, ok := opSet[opName]; !ok {
						return nil, fmt.Errorf("bootstrap: integration %q: allowed_operations contains unknown operation %q", name, opName)
					}
				}
				intg = integration.NewRestricted(intg, allowed)
			}
		}

		if err := reg.Integrations.Register(name, intg); err != nil {
			return nil, fmt.Errorf("bootstrap: registering integration %q: %w", name, err)
		}
	}
	return &reg.Integrations, nil
}

// isWildcard returns true when the allowlist is exactly ["*"].
func isWildcard(ops []string) bool {
	return len(ops) == 1 && ops[0] == "*"
}

// deriveKey converts the encryption_key string to a 32-byte key for AES-256-GCM.
// If the string is 64 hex characters it is hex-decoded directly; otherwise it is
// SHA-256 hashed.
func deriveKey(s string) []byte {
	if s == "" {
		return nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b
	}
	h := sha256.Sum256([]byte(s))
	return h[:]
}

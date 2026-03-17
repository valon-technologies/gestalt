package bootstrap_test

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/internal/config"
	"gopkg.in/yaml.v3"
)

func stubAuthFactory(name string) bootstrap.AuthFactory {
	return func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
		return &coretesting.StubAuthProvider{N: name}, nil
	}
}

func stubDatastoreFactory() bootstrap.DatastoreFactory {
	return func(yaml.Node, bootstrap.Deps) (core.Datastore, error) {
		return &coretesting.StubDatastore{}, nil
	}
}

func stubIntegrationFactory(name string) bootstrap.IntegrationFactory {
	return func(yaml.Node, bootstrap.Deps) (core.Integration, error) {
		return &coretesting.StubIntegration{N: name}, nil
	}
}

func validConfig() *config.Config {
	return &config.Config{
		Auth:      config.AuthConfig{Provider: "test-auth"},
		Datastore: config.DatastoreConfig{Provider: "test-store"},
		Integrations: map[string]yaml.Node{
			"alpha": {},
		},
		Server: config.ServerConfig{
			Port:          8080,
			EncryptionKey: "test-key",
		},
	}
}

func validFactories() *bootstrap.FactoryRegistry {
	f := bootstrap.NewFactoryRegistry()
	f.Auth["test-auth"] = stubAuthFactory("test-auth")
	f.Datastores["test-store"] = stubDatastoreFactory()
	f.Integrations["alpha"] = stubIntegrationFactory("alpha")
	return f
}

func TestBootstrap(t *testing.T) {
	t.Parallel()

	result, err := bootstrap.Bootstrap(validConfig(), validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if result.Auth == nil {
		t.Fatal("Auth is nil")
	}
	if result.Auth.Name() != "test-auth" {
		t.Errorf("Auth.Name: got %q, want %q", result.Auth.Name(), "test-auth")
	}
	if result.Datastore == nil {
		t.Fatal("Datastore is nil")
	}
	names := result.Integrations.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("Integrations.List: got %v, want [alpha]", names)
	}
}

func TestBootstrapMultipleIntegrations(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Integrations["beta"] = yaml.Node{}

	factories := validFactories()
	factories.Integrations["beta"] = stubIntegrationFactory("beta")

	result, err := bootstrap.Bootstrap(cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	names := result.Integrations.List()
	if len(names) != 2 {
		t.Fatalf("Integrations.List: got %d items, want 2", len(names))
	}
}

func TestBootstrapNoIntegrations(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Integrations = nil

	result, err := bootstrap.Bootstrap(cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if len(result.Integrations.List()) != 0 {
		t.Errorf("expected empty integrations, got %v", result.Integrations.List())
	}
}

func TestBootstrapDevMode(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Server.DevMode = true

	result, err := bootstrap.Bootstrap(cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !result.DevMode {
		t.Error("DevMode: got false, want true")
	}
}

func TestBootstrapUnknownProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{
			name:   "unknown auth provider",
			mutate: func(c *config.Config) { c.Auth.Provider = "unknown" },
		},
		{
			name:   "unknown datastore",
			mutate: func(c *config.Config) { c.Datastore.Provider = "unknown" },
		},
		{
			name:   "unknown integration",
			mutate: func(c *config.Config) { c.Integrations["unknown"] = yaml.Node{} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			tc.mutate(cfg)
			_, err := bootstrap.Bootstrap(cfg, validFactories())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestBootstrapFactoryError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*bootstrap.FactoryRegistry)
	}{
		{
			name: "auth factory error",
			mutate: func(f *bootstrap.FactoryRegistry) {
				f.Auth["test-auth"] = func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
					return nil, fmt.Errorf("auth broke")
				}
			},
		},
		{
			name: "datastore factory error",
			mutate: func(f *bootstrap.FactoryRegistry) {
				f.Datastores["test-store"] = func(yaml.Node, bootstrap.Deps) (core.Datastore, error) {
					return nil, fmt.Errorf("datastore broke")
				}
			},
		},
		{
			name: "integration factory error",
			mutate: func(f *bootstrap.FactoryRegistry) {
				f.Integrations["alpha"] = func(yaml.Node, bootstrap.Deps) (core.Integration, error) {
					return nil, fmt.Errorf("integration broke")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			factories := validFactories()
			tc.mutate(factories)
			_, err := bootstrap.Bootstrap(validConfig(), factories)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestBootstrapEncryptionKeyDerivation(t *testing.T) {
	t.Parallel()

	t.Run("passphrase produces 32-byte key", func(t *testing.T) {
		t.Parallel()

		var receivedKey []byte
		factories := validFactories()
		factories.Auth["test-auth"] = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
			receivedKey = deps.EncryptionKey
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		cfg := validConfig()
		cfg.Server.EncryptionKey = "my-passphrase"

		if _, err := bootstrap.Bootstrap(cfg, factories); err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		if len(receivedKey) != 32 {
			t.Errorf("key length: got %d, want 32", len(receivedKey))
		}
	})

	t.Run("hex key is decoded directly", func(t *testing.T) {
		t.Parallel()

		want := make([]byte, 32)
		for i := range want {
			want[i] = byte(i)
		}
		hexKey := hex.EncodeToString(want)

		var receivedKey []byte
		factories := validFactories()
		factories.Auth["test-auth"] = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
			receivedKey = deps.EncryptionKey
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		cfg := validConfig()
		cfg.Server.EncryptionKey = hexKey

		if _, err := bootstrap.Bootstrap(cfg, factories); err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		if hex.EncodeToString(receivedKey) != hexKey {
			t.Errorf("hex key not decoded: got %x, want %x", receivedKey, want)
		}
	})

	t.Run("same passphrase produces same key", func(t *testing.T) {
		t.Parallel()

		var keys [][]byte
		for i := 0; i < 2; i++ {
			factories := validFactories()
			factories.Auth["test-auth"] = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
				keys = append(keys, deps.EncryptionKey)
				return &coretesting.StubAuthProvider{N: "test-auth"}, nil
			}
			cfg := validConfig()
			cfg.Server.EncryptionKey = "deterministic"
			if _, err := bootstrap.Bootstrap(cfg, factories); err != nil {
				t.Fatalf("Bootstrap: %v", err)
			}
		}
		if hex.EncodeToString(keys[0]) != hex.EncodeToString(keys[1]) {
			t.Error("key derivation is not deterministic")
		}
	})
}

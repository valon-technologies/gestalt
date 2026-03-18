package bootstrap_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

func stubProviderFactory(name string) bootstrap.ProviderFactory {
	return func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		return &coretesting.StubIntegration{N: name}, nil
	}
}

type stubIntegrationWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubIntegrationWithOps) ListOperations() []core.Operation {
	return s.ops
}

func stubProviderFactoryWithOps(name string, ops []core.Operation) bootstrap.ProviderFactory {
	return func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		return &stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: name},
			ops:             ops,
		}, nil
	}
}

func validConfig() *config.Config {
	return &config.Config{
		Auth:      config.AuthConfig{Provider: "test-auth"},
		Datastore: config.DatastoreConfig{Provider: "test-store"},
		Integrations: map[string]config.IntegrationDef{
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
	f.Providers["alpha"] = stubProviderFactory("alpha")
	return f
}

func TestBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	result, err := bootstrap.Bootstrap(ctx, validConfig(), validFactories())
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
	names := result.Providers.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("Providers.List: got %v, want [alpha]", names)
	}
}

func TestBootstrapMultipleProviders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Integrations["beta"] = config.IntegrationDef{}

	factories := validFactories()
	factories.Providers["beta"] = stubProviderFactory("beta")

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	names := result.Providers.List()
	if len(names) != 2 {
		t.Fatalf("Providers.List: got %d items, want 2", len(names))
	}
}

func TestBootstrapNoIntegrations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Integrations = nil

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if got := result.Providers.List(); len(got) != 0 {
		t.Errorf("expected empty providers, got %v", got)
	}
}

func TestBootstrapDevMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Server.DevMode = true

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !result.DevMode {
		t.Error("DevMode: got false, want true")
	}
}

func TestBootstrapDefaultProviderFactory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	factories := bootstrap.NewFactoryRegistry()
	factories.Auth["test-auth"] = stubAuthFactory("test-auth")
	factories.Datastores["test-store"] = stubDatastoreFactory()
	factories.DefaultProvider = stubProviderFactory("default-alpha")

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	names := result.Providers.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("Providers.List: got %v, want [alpha]", names)
	}
}

func TestBootstrapNamedOverridesDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	factories := bootstrap.NewFactoryRegistry()
	factories.Auth["test-auth"] = stubAuthFactory("test-auth")
	factories.Datastores["test-store"] = stubDatastoreFactory()
	factories.DefaultProvider = func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		return nil, fmt.Errorf("should not be called")
	}
	factories.Providers["alpha"] = stubProviderFactory("alpha")

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	names := result.Providers.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("Providers.List: got %v, want [alpha]", names)
	}
}

func TestBootstrapUnknownProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

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
			name: "no factory for integration",
			mutate: func(c *config.Config) {
				c.Integrations["unknown"] = config.IntegrationDef{}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			tc.mutate(cfg)
			_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestBootstrapFactoryError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

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
			name: "provider factory error",
			mutate: func(f *bootstrap.FactoryRegistry) {
				f.Providers["alpha"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
					return nil, fmt.Errorf("provider broke")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			factories := validFactories()
			tc.mutate(factories)
			_, err := bootstrap.Bootstrap(ctx, validConfig(), factories)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestBootstrapEncryptionKeyDerivation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

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

		if _, err := bootstrap.Bootstrap(ctx, cfg, factories); err != nil {
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

		if _, err := bootstrap.Bootstrap(ctx, cfg, factories); err != nil {
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
			if _, err := bootstrap.Bootstrap(ctx, cfg, factories); err != nil {
				t.Fatalf("Bootstrap: %v", err)
			}
		}
		if hex.EncodeToString(keys[0]) != hex.EncodeToString(keys[1]) {
			t.Error("key derivation is not deterministic")
		}
	})
}

func TestBootstrapBaseURL(t *testing.T) {
	t.Parallel()

	var receivedBaseURL string
	var receivedRedirectURL string
	factories := validFactories()
	factories.Auth["test-auth"] = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
		receivedBaseURL = deps.BaseURL
		return &coretesting.StubAuthProvider{N: "test-auth"}, nil
	}
	factories.Providers["alpha"] = func(_ context.Context, _ string, def config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		receivedRedirectURL = def.RedirectURL
		return &coretesting.StubIntegration{N: "alpha"}, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfgYAML := `
auth:
  provider: test-auth
datastore:
  provider: test-store
server:
  base_url: https://toolshed.example.com
  encryption_key: test-key
integrations:
  alpha:
    client_id: cid
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	if _, err := bootstrap.Bootstrap(context.Background(), cfg, factories); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	if receivedBaseURL != "https://toolshed.example.com" {
		t.Errorf("auth factory deps.BaseURL = %q, want %q", receivedBaseURL, "https://toolshed.example.com")
	}
	want := "https://toolshed.example.com" + config.IntegrationCallbackPath
	if receivedRedirectURL != want {
		t.Errorf("integration factory RedirectURL = %q, want %q", receivedRedirectURL, want)
	}
}

func TestBootstrapAllowedOperations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	allOps := []core.Operation{
		{Name: "list_channels"},
		{Name: "send_message"},
		{Name: "delete_message"},
	}

	cfg := validConfig()
	factories := validFactories()
	factories.Providers["alpha"] = stubProviderFactoryWithOps("alpha", allOps[:2])

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	prov, err := result.Providers.Get("alpha")
	if err != nil {
		t.Fatalf("provider 'alpha' not found: %v", err)
	}
	if len(prov.ListOperations()) != 2 {
		t.Fatalf("ListOperations: got %d ops, want 2", len(prov.ListOperations()))
	}
}

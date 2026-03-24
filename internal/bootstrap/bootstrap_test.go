package bootstrap_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	echoruntime "github.com/valon-technologies/gestalt/plugins/runtimes/echo"
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

func stubRuntimeFactory(name string, stopFn func(context.Context) error) bootstrap.RuntimeFactory {
	return func(_ context.Context, _ string, _ config.RuntimeDef, _ bootstrap.RuntimeDeps) (core.Runtime, error) {
		return &coretesting.StubRuntime{N: name, StopFn: stopFn}, nil
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

func stubSecretManagerFactory() bootstrap.SecretManagerFactory {
	return func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{}, nil
	}
}

func validConfig() *config.Config {
	return &config.Config{
		Auth:      config.AuthConfig{Provider: "test-auth"},
		Datastore: config.DatastoreConfig{Provider: "test-store"},
		Secrets:   config.SecretsConfig{Provider: "test-secrets"},
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
	f.Secrets["test-secrets"] = stubSecretManagerFactory()
	return f
}

func TestBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	result, err := bootstrap.Bootstrap(ctx, validConfig(), validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if result.Auth == nil {
		t.Fatal("Auth is nil")
	}
	if result.Auth.Name() != "test-auth" {
		t.Errorf("Auth.Name: got %q, want %q", result.Auth.Name(), "test-auth")
	}
	if result.Datastore == nil {
		t.Fatal("Datastore is nil")
	}
	if result.Invoker == nil {
		t.Fatal("Invoker is nil")
	}
	if result.CapabilityLister == nil {
		t.Fatal("CapabilityLister is nil")
	}
	invoker, ok := result.Invoker.(*invocation.Broker)
	if !ok {
		t.Fatalf("Invoker should be *invocation.Broker, got %T", result.Invoker)
	}
	lister, ok := result.CapabilityLister.(*invocation.Broker)
	if !ok {
		t.Fatalf("CapabilityLister should be *invocation.Broker, got %T", result.CapabilityLister)
	}
	if invoker != lister {
		t.Fatal("expected shared invoker and capability lister to be the same instance")
	}
	<-result.ProvidersReady
	names := result.Providers.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("Providers.List: got %v, want [alpha]", names)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	if _, err := bootstrap.Validate(context.Background(), validConfig(), validFactories()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateStopsConstructedRuntimes(t *testing.T) {
	t.Parallel()

	stopped := false

	cfg := validConfig()
	cfg.Runtimes = map[string]config.RuntimeDef{
		"echo": {Type: "test-runtime"},
	}

	factories := validFactories()
	factories.Runtimes["test-runtime"] = stubRuntimeFactory("echo", func(context.Context) error {
		stopped = true
		return nil
	})

	if _, err := bootstrap.Validate(context.Background(), cfg, factories); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !stopped {
		t.Fatal("expected Validate to stop constructed runtimes")
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
	<-result.ProvidersReady
	<-result.ProvidersReady
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
	<-result.ProvidersReady
	<-result.ProvidersReady
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
	<-result.ProvidersReady
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
	factories.Secrets["test-secrets"] = stubSecretManagerFactory()

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	<-result.ProvidersReady
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
	factories.Secrets["test-secrets"] = stubSecretManagerFactory()

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	<-result.ProvidersReady
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
			name:   "unknown secrets provider",
			mutate: func(c *config.Config) { c.Secrets.Provider = "unknown" },
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

func TestBootstrapProviderErrorSkipsProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Integrations["beta"] = config.IntegrationDef{}

	factories := validFactories()
	factories.Providers["beta"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		return nil, fmt.Errorf("provider broke")
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap should succeed when a provider fails: %v", err)
	}
	<-result.ProvidersReady
	<-result.ProvidersReady
	names := result.Providers.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("Providers.List: got %v, want [alpha] (beta should be skipped)", names)
	}
}

func TestValidateProviderErrorFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Integrations["beta"] = config.IntegrationDef{}

	factories := validFactories()
	factories.Providers["beta"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		return nil, fmt.Errorf("provider broke")
	}

	_, err := bootstrap.Validate(ctx, cfg, factories)
	if err == nil {
		t.Fatal("expected Validate to fail when a provider fails")
	}
	if !strings.Contains(err.Error(), `integration "beta": provider broke`) {
		t.Fatalf("unexpected validation error: %v", err)
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

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
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

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
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
			result, err := bootstrap.Bootstrap(ctx, cfg, factories)
			if err != nil {
				t.Fatalf("Bootstrap: %v", err)
			}
			<-result.ProvidersReady
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
	// config.Load defaults secrets.provider to "env"
	factories.Secrets["env"] = stubSecretManagerFactory()

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfgYAML := `
auth:
  provider: test-auth
datastore:
  provider: test-store
server:
  base_url: https://gestalt.example.com
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

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	if receivedBaseURL != "https://gestalt.example.com" {
		t.Errorf("auth factory deps.BaseURL = %q, want %q", receivedBaseURL, "https://gestalt.example.com")
	}
	want := "https://gestalt.example.com" + config.IntegrationCallbackPath
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
	<-result.ProvidersReady

	<-result.ProvidersReady
	prov, err := result.Providers.Get("alpha")
	if err != nil {
		t.Fatalf("provider 'alpha' not found: %v", err)
	}
	if len(prov.ListOperations()) != 2 {
		t.Fatalf("ListOperations: got %d ops, want 2", len(prov.ListOperations()))
	}
}

func TestBootstrapSecretResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("resolves secret:// in encryption key", func(t *testing.T) {
		t.Parallel()

		var receivedKey []byte
		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"enc-key": "resolved-passphrase"},
			}, nil
		}
		factories.Auth["test-auth"] = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
			receivedKey = deps.EncryptionKey
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		cfg := validConfig()
		cfg.Server.EncryptionKey = "secret://enc-key"

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if len(receivedKey) != 32 {
			t.Errorf("key length: got %d, want 32", len(receivedKey))
		}
	})

	t.Run("resolves secret:// in integration client_secret", func(t *testing.T) {
		t.Parallel()

		var receivedDef config.IntegrationDef
		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"slack-secret": "resolved-secret"},
			}, nil
		}
		factories.Providers["alpha"] = func(_ context.Context, _ string, intg config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
			receivedDef = intg
			return &coretesting.StubIntegration{N: "alpha"}, nil
		}

		cfg := validConfig()
		intg := cfg.Integrations["alpha"]
		intg.ClientSecret = "secret://slack-secret"
		cfg.Integrations["alpha"] = intg

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if receivedDef.ClientSecret != "resolved-secret" {
			t.Errorf("ClientSecret: got %q, want %q", receivedDef.ClientSecret, "resolved-secret")
		}
	})

	t.Run("leaves non-secret values unchanged", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Server.EncryptionKey = "plain-passphrase"

		result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if result.Auth == nil {
			t.Fatal("Auth is nil")
		}
	})

	t.Run("error on unresolvable secret", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Server.EncryptionKey = "secret://missing-key"

		_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "missing-key") {
			t.Errorf("error should mention secret name: %v", err)
		}
	})

	t.Run("error on empty resolved value", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"empty-secret": ""},
			}, nil
		}

		cfg := validConfig()
		cfg.Server.EncryptionKey = "secret://empty-secret"

		_, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "empty value") {
			t.Errorf("error should mention empty value: %v", err)
		}
	})

	t.Run("resolves secret:// in yaml.Node auth config", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"auth-secret": "resolved-auth-secret"},
			}, nil
		}

		var receivedNode yaml.Node
		factories.Auth["test-auth"] = func(node yaml.Node, _ bootstrap.Deps) (core.AuthProvider, error) {
			receivedNode = node
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		cfg := validConfig()
		// Build a yaml.Node mapping with a secret:// value.
		cfg.Auth.Config = yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "client_secret", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: "secret://auth-secret", Tag: "!!str"},
			},
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		// The resolved node should have the value replaced.
		var decoded map[string]string
		if err := receivedNode.Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded["client_secret"] != "resolved-auth-secret" {
			t.Errorf("client_secret: got %q, want %q", decoded["client_secret"], "resolved-auth-secret")
		}
	})

	t.Run("result includes SecretManager", func(t *testing.T) {
		t.Parallel()

		result, err := bootstrap.Bootstrap(ctx, validConfig(), validFactories())
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if result.SecretManager == nil {
			t.Fatal("SecretManager is nil")
		}
	})

	t.Run("secrets factory error", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return nil, fmt.Errorf("secrets broke")
		}

		_, err := bootstrap.Bootstrap(ctx, validConfig(), factories)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "secrets broke") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("resolves secret:// in map[string]string fields", func(t *testing.T) {
		t.Parallel()

		var receivedDef config.IntegrationDef
		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"api-key": "resolved-key"},
			}, nil
		}
		factories.Providers["alpha"] = func(_ context.Context, _ string, intg config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
			receivedDef = intg
			return &coretesting.StubIntegration{N: "alpha"}, nil
		}

		cfg := validConfig()
		intg := cfg.Integrations["alpha"]
		intg.Headers = map[string]string{"Authorization": "secret://api-key"}
		cfg.Integrations["alpha"] = intg

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if receivedDef.Headers["Authorization"] != "resolved-key" {
			t.Errorf("Headers[Authorization]: got %q, want %q", receivedDef.Headers["Authorization"], "resolved-key")
		}
	})

	t.Run("resolves secret:// in []string fields", func(t *testing.T) {
		t.Parallel()

		var receivedDef config.IntegrationDef
		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"meta-val": "resolved-meta"},
			}, nil
		}
		factories.Providers["alpha"] = func(_ context.Context, _ string, intg config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
			receivedDef = intg
			return &coretesting.StubIntegration{N: "alpha"}, nil
		}

		cfg := validConfig()
		intg := cfg.Integrations["alpha"]
		intg.Auth.TokenMetadata = []string{"secret://meta-val"}
		cfg.Integrations["alpha"] = intg

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if len(receivedDef.Auth.TokenMetadata) != 1 || receivedDef.Auth.TokenMetadata[0] != "resolved-meta" {
			t.Errorf("Auth.TokenMetadata: got %v, want [resolved-meta]", receivedDef.Auth.TokenMetadata)
		}
	})
}

func TestBootstrapWithRuntimes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Runtimes = map[string]config.RuntimeDef{
		"my-echo": {
			Type:      "echo",
			Providers: []string{"alpha"},
		},
	}

	factories := validFactories()
	factories.Runtimes["echo"] = echoruntime.Factory

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if result.Runtimes == nil {
		t.Fatal("expected Runtimes to be non-nil")
	}
	names := result.Runtimes.List()
	if len(names) != 1 || names[0] != "my-echo" {
		t.Fatalf("Runtimes.List: got %v, want [my-echo]", names)
	}
}

func TestBootstrapNoRuntimes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	result, err := bootstrap.Bootstrap(ctx, validConfig(), validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if result.Runtimes != nil {
		t.Fatalf("expected Runtimes to be nil, got %v", result.Runtimes.List())
	}
}

func TestBootstrapUnknownRuntimeType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Runtimes = map[string]config.RuntimeDef{
		"bad": {Type: "nonexistent"},
	}

	_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err == nil {
		t.Fatal("expected error for unknown runtime type")
	}
}

func TestBootstrapWithBindings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Bindings = map[string]config.BindingDef{
		"my-webhook": {Type: "webhook"},
	}

	factories := validFactories()
	factories.Bindings["webhook"] = func(_ context.Context, name string, _ config.BindingDef, _ bootstrap.BindingDeps) (core.Binding, error) {
		return &coretesting.StubBinding{N: name, K: core.BindingTrigger}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if result.Bindings == nil {
		t.Fatal("expected Bindings to be non-nil")
	}
	names := result.Bindings.List()
	if len(names) != 1 || names[0] != "my-webhook" {
		t.Fatalf("Bindings.List: got %v, want [my-webhook]", names)
	}
}

func TestBootstrapNoBindings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	result, err := bootstrap.Bootstrap(ctx, validConfig(), validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if result.Bindings != nil {
		t.Fatalf("expected Bindings to be nil, got %v", result.Bindings.List())
	}
}

func TestBootstrapUnknownBindingType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Bindings = map[string]config.BindingDef{
		"bad": {Type: "nonexistent"},
	}

	_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err == nil {
		t.Fatal("expected error for unknown binding type")
	}
}

func TestBootstrapBindingWithProviders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Integrations["beta"] = config.IntegrationDef{}
	cfg.Bindings = map[string]config.BindingDef{
		"my-webhook": {
			Type:      "webhook",
			Providers: []string{"alpha"},
		},
	}

	factories := validFactories()
	factories.Providers["beta"] = stubProviderFactory("beta")

	var receivedDeps bootstrap.BindingDeps
	factories.Bindings["webhook"] = func(_ context.Context, name string, _ config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
		receivedDeps = deps
		return &coretesting.StubBinding{N: name, K: core.BindingTrigger}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if result.Bindings == nil {
		t.Fatal("expected Bindings to be non-nil")
	}

	if receivedDeps.Invoker == nil {
		t.Fatal("expected binding deps to carry an invoker")
	}
	if receivedDeps.Invoker == result.Invoker {
		t.Fatal("expected binding deps to be scoped rather than reusing the shared bootstrap invoker directly")
	}
}

package bootstrap_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	telemetrynoop "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/noop"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"gopkg.in/yaml.v3"
)

func stubAuthFactory(name string) bootstrap.AuthFactory {
	return func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
		return &coretesting.StubAuthProvider{N: name}, nil
	}
}

func stubSecretManagerFactory() bootstrap.SecretManagerFactory {
	return func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{}, nil
	}
}

func stubTelemetryFactory() bootstrap.TelemetryFactory {
	return func(yaml.Node) (core.TelemetryProvider, error) {
		return telemetrynoop.New(), nil
	}
}

type closableAuthProvider struct {
	*coretesting.StubAuthProvider
	closed *atomic.Bool
}

func (p *closableAuthProvider) Close() error {
	p.closed.Store(true)
	return nil
}

func stubIndexedDBFactory() bootstrap.IndexedDBFactory {
	return func(yaml.Node) (indexeddb.IndexedDB, error) {
		return &coretesting.StubIndexedDB{}, nil
	}
}

func validConfig() *config.Config {
	return &config.Config{
		Auth: config.AuthConfig{
			Provider: &config.ProviderDef{Source: &config.PluginSourceDef{Ref: "github.com/valon-technologies/gestalt-providers/auth/oidc", Version: "0.0.1-alpha.1"}},
			Config:   yaml.Node{Kind: yaml.MappingNode},
		},
		IndexedDB: config.IndexedDBConfig("test"),
		IndexedDBs: map[string]config.IndexedDBDef{
			"test": {Provider: &config.ProviderDef{Source: &config.PluginSourceDef{Path: "stub"}}},
		},
		Secrets:   config.SecretsConfig{Builtin: "test-secrets"},
		Telemetry: config.TelemetryConfig{Builtin: "test-telemetry"},
		Plugins:   map[string]config.PluginDef{},
		Server: config.ServerConfig{
			Public:        config.ListenerConfig{Port: 8080},
			EncryptionKey: "test-key",
		},
	}
}

func validFactories() *bootstrap.FactoryRegistry {
	f := bootstrap.NewFactoryRegistry()
	f.Auth = stubAuthFactory("test-auth")
	f.IndexedDB = stubIndexedDBFactory()
	f.Secrets["test-secrets"] = stubSecretManagerFactory()
	f.Telemetry["test-telemetry"] = stubTelemetryFactory()
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
	if result.Services == nil {
		t.Fatal("Datastore is nil")
	}
	if result.Telemetry == nil {
		t.Fatal("Telemetry is nil")
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
}

func TestResultCloseClosesAuthProvider(t *testing.T) {
	t.Parallel()

	closed := &atomic.Bool{}
	factories := validFactories()
	factories.Auth = func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
		return &closableAuthProvider{
			StubAuthProvider: &coretesting.StubAuthProvider{N: "test-auth"},
			closed:           closed,
		}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), validConfig(), factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err := result.Close(context.Background()); err != nil {
		t.Fatalf("Result.Close: %v", err)
	}
	if !closed.Load() {
		t.Fatal("auth provider was not closed")
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	t.Run("baseline", func(t *testing.T) {
		t.Parallel()

		if _, err := bootstrap.Validate(context.Background(), validConfig(), validFactories()); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
}

func TestBootstrapNoIntegrations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Plugins = nil

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if got := result.Providers.List(); len(got) != 0 {
		t.Errorf("expected empty providers, got %v", got)
	}
}

func TestBootstrap_ReusesPreparedComponentRuntimeConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()

	authRuntime, err := config.BuildComponentRuntimeConfigNode("auth", "auth", cfg.Auth.Provider, yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "clientId"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "prepared-auth"},
		},
	})
	if err != nil {
		t.Fatalf("BuildComponentRuntimeConfigNode(auth): %v", err)
	}
	cfg.Auth.Config = authRuntime

	var gotAuthNode yaml.Node
	factories := validFactories()
	factories.Auth = func(node yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
		gotAuthNode = node
		return &coretesting.StubAuthProvider{N: "test-auth"}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })

	authMap, err := config.NodeToMap(gotAuthNode)
	if err != nil {
		t.Fatalf("NodeToMap(auth): %v", err)
	}
	authConfig, ok := authMap["config"].(map[string]any)
	if !ok {
		t.Fatalf("auth runtime config = %#v", authMap["config"])
	}
	if _, nested := authConfig["config"]; nested {
		t.Fatalf("auth config was rewrapped: %#v", authConfig)
	}
	if authConfig["clientId"] != "prepared-auth" {
		t.Fatalf("auth config = %#v", authConfig)
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
				f.Auth = func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
					return nil, fmt.Errorf("auth broke")
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
		factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
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
		factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
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
			factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
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
		factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
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
		factories.Auth = func(node yaml.Node, _ bootstrap.Deps) (core.AuthProvider, error) {
			receivedNode = node
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		cfg := validConfig()
		cfg.Auth.Config = yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "clientSecret", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: "secret://auth-secret", Tag: "!!str"},
			},
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		var decoded struct {
			Source *config.PluginSourceDef `yaml:"source"`
			Config map[string]string       `yaml:"config"`
		}
		if err := receivedNode.Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.Source == nil || decoded.Source.Ref != "github.com/valon-technologies/gestalt-providers/auth/oidc" {
			t.Fatalf("source = %+v", decoded.Source)
		}
		if decoded.Config["clientSecret"] != "resolved-auth-secret" {
			t.Errorf("clientSecret: got %q, want %q", decoded.Config["clientSecret"], "resolved-auth-secret")
		}
	})

	t.Run("passes top-level provider selection to auth factory", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Auth.Provider = &config.ProviderDef{Source: &config.PluginSourceDef{Ref: "github.com/valon-technologies/gestalt-providers/auth/oidc", Version: "0.0.1-alpha.1"}}
		cfg.Auth.Config = yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "issuerUrl", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: "https://issuer.example.test", Tag: "!!str"},
			},
		}

		var authNode yaml.Node
		factories := validFactories()
		factories.Auth = func(node yaml.Node, _ bootstrap.Deps) (core.AuthProvider, error) {
			authNode = node
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		var authCfg struct {
			Source *config.PluginSourceDef `yaml:"source"`
			Config map[string]string       `yaml:"config"`
		}
		if err := authNode.Decode(&authCfg); err != nil {
			t.Fatalf("decode auth node: %v", err)
		}
		if authCfg.Source == nil || authCfg.Source.Ref != "github.com/valon-technologies/gestalt-providers/auth/oidc" {
			t.Fatalf("auth source = %+v", authCfg.Source)
		}
		if authCfg.Config["issuerUrl"] != "https://issuer.example.test" {
			t.Fatalf("auth config = %+v", authCfg.Config)
		}
	})

	t.Run("omits auth when auth provider is unset", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Auth.Provider = nil

		var authFactoryCalled atomic.Bool
		factories := validFactories()
		factories.Auth = func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
			authFactoryCalled.Store(true)
			return &coretesting.StubAuthProvider{N: "unexpected"}, nil
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if result.Auth != nil {
			t.Fatalf("Auth = %T, want nil", result.Auth)
		}
		if authFactoryCalled.Load() {
			t.Fatal("auth factory was called")
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
}

func TestBootstrapDisabledComponents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("disabled telemetry uses noop", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Telemetry = config.TelemetryConfig{Disabled: true}

		factories := validFactories()
		factories.Telemetry["noop"] = telemetrynoop.Factory

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if result.Telemetry == nil {
			t.Fatal("Telemetry is nil")
		}
		if err := result.Close(context.Background()); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	t.Run("disabled secrets returns not found", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Secrets = config.SecretsConfig{Disabled: true}

		result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if result.SecretManager == nil {
			t.Fatal("SecretManager is nil")
		}
		_, getErr := result.SecretManager.GetSecret(ctx, "any-key")
		if getErr == nil {
			t.Fatal("expected error from disabled secret manager")
		}
		if !strings.Contains(getErr.Error(), "disabled") {
			t.Fatalf("error should mention disabled: %v", getErr)
		}
		if err := result.Close(context.Background()); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
}

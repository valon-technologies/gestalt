package bootstrap_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	telemetrynoop "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/noop"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
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

func pluginIntegration() config.IntegrationDef {
	return config.IntegrationDef{
		Plugin: &config.PluginDef{
			BaseURL: "https://api.example.test",
			Operations: []config.InlineOperationDef{
				{Name: "list_items", Method: "GET", Path: "/items"},
			},
		},
	}
}

func validConfig() *config.Config {
	return &config.Config{
		Auth:      config.AuthConfig{Provider: "test-auth"},
		Datastore: config.DatastoreConfig{Provider: "test-store"},
		Secrets:   config.SecretsConfig{Provider: "test-secrets"},
		Telemetry: config.TelemetryConfig{Provider: "test-telemetry"},
		Integrations: map[string]config.IntegrationDef{
			"alpha": pluginIntegration(),
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
	if result.Datastore == nil {
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

func TestValidate(t *testing.T) {
	t.Parallel()

	t.Run("baseline", func(t *testing.T) {
		t.Parallel()

		if _, err := bootstrap.Validate(context.Background(), validConfig(), validFactories()); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("allows resolved manifest local overrides", func(t *testing.T) {
		t.Parallel()

		specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, map[string]any{
				"openapi": "3.0.0",
				"info":    map[string]string{"title": "Reports API"},
				"servers": []any{map[string]string{"url": "https://api.example.test"}},
				"paths": map[string]any{
					"/reports": map[string]any{
						"get": map[string]any{
							"operationId": "list_reports",
							"summary":     "List reports",
						},
					},
				},
			})
		}))
		testutil.CloseOnCleanup(t, specSrv)

		plugin := &config.PluginDef{
			Source:            "github.com/acme/plugins/reports",
			Version:           "1.0.0",
			IsDeclarative:     true,
			OpenAPIConnection: "reports",
			ResponseMapping: &config.ResponseMappingDef{
				DataPath: "results.items",
			},
			Connections: map[string]*config.ConnectionDef{
				"reports": {
					Mode: "user",
					Auth: config.ConnectionAuthDef{Type: pluginmanifestv1.AuthTypeManual},
				},
			},
			ResolvedManifest: &pluginmanifestv1.Manifest{
				Kinds: []string{pluginmanifestv1.KindProvider},
				Provider: &pluginmanifestv1.Provider{
					OpenAPI: specSrv.URL,
				},
			},
		}

		cfg := validConfig()
		cfg.Integrations["reports"] = config.IntegrationDef{
			Plugin: plugin,
		}

		if _, err := bootstrap.Validate(context.Background(), cfg, validFactories()); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("rejects undeclared resolved manifest connection override", func(t *testing.T) {
		t.Parallel()

		specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, map[string]any{
				"openapi": "3.0.0",
				"info":    map[string]string{"title": "Reports API"},
				"servers": []any{map[string]string{"url": "https://api.example.test"}},
				"paths": map[string]any{
					"/reports": map[string]any{
						"get": map[string]any{
							"operationId": "list_reports",
							"summary":     "List reports",
						},
					},
				},
			})
		}))
		testutil.CloseOnCleanup(t, specSrv)

		cfg := validConfig()
		cfg.Integrations["reports"] = config.IntegrationDef{
			Plugin: &config.PluginDef{
				Source:            "github.com/acme/plugins/reports",
				Version:           "1.0.0",
				IsDeclarative:     true,
				OpenAPIConnection: "reports",
				ResolvedManifest: &pluginmanifestv1.Manifest{
					Kinds: []string{pluginmanifestv1.KindProvider},
					Provider: &pluginmanifestv1.Provider{
						OpenAPI: specSrv.URL,
					},
				},
			},
		}

		_, err := bootstrap.Validate(context.Background(), cfg, validFactories())
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), `openapi_connection references undeclared connection "reports"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateRejectsInvalidManagedParameters(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Integrations["alpha"] = config.IntegrationDef{
		Plugin: &config.PluginDef{
			BaseURL: "https://api.example.test",
			Operations: []config.InlineOperationDef{
				{Name: "list_items", Method: "GET", Path: "/items"},
			},
			ManagedParameters: []config.ManagedParameterDef{
				{
					In:    "query",
					Name:  "api_version",
					Value: "2026-04-01",
				},
			},
		},
	}

	err := config.ValidateStructure(cfg)
	if err == nil {
		t.Fatal("expected invalid managed parameters")
	}
	if !strings.Contains(err.Error(), `managed_parameters[0].in "query" must be "header" or "path"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBootstrapMultipleProviders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Integrations["beta"] = pluginIntegration()

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
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
	if got := result.Providers.List(); len(got) != 0 {
		t.Errorf("expected empty providers, got %v", got)
	}
}

func TestGatewayMode_NoBindingsRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Bindings = nil

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if result.Invoker == nil {
		t.Fatal("expected Invoker to be non-nil")
	}
	if result.CapabilityLister == nil {
		t.Fatal("expected CapabilityLister to be non-nil")
	}
	<-result.ProvidersReady
	names := result.Providers.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("Providers.List: got %v, want [alpha]", names)
	}
	if result.Bindings != nil {
		t.Error("expected Bindings to be nil")
	}
}

func TestPlatformMode_BindingsUseScopedInvoker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	doOp := config.InlineOperationDef{Name: "do", Method: "POST", Path: "/do"}
	cfg.Integrations["alpha"] = config.IntegrationDef{
		Plugin: &config.PluginDef{
			BaseURL:    "https://api.test",
			Operations: []config.InlineOperationDef{doOp},
		},
	}
	cfg.Integrations["beta"] = config.IntegrationDef{
		Plugin: &config.PluginDef{
			BaseURL:    "https://api.test",
			Operations: []config.InlineOperationDef{doOp},
		},
	}

	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {
			Type:      "test-binding",
			Providers: []string{"beta"},
		},
	}

	factories := validFactories()

	var bindingDeps bootstrap.BindingDeps
	factories.Bindings["test-binding"] = func(_ context.Context, name string, _ config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
		bindingDeps = deps
		return &coretesting.StubBinding{N: name}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if result.AuditSink == nil {
		t.Fatal("expected AuditSink to be non-nil")
	}

	if bindingDeps.Invoker == nil {
		t.Fatal("expected binding deps to carry an invoker")
	}

	_, err = bindingDeps.Invoker.Invoke(ctx, &principal.Principal{}, "alpha", "", "do", nil)
	if err == nil || !strings.Contains(err.Error(), "not available in this scope") {
		t.Fatalf("expected scoped binding invoker to reject alpha, got %v", err)
	}

	_, err = bindingDeps.Invoker.Invoke(ctx, &principal.Principal{}, "beta", "", "do", nil)
	if err != nil && strings.Contains(err.Error(), "not available in this scope") {
		t.Fatalf("expected scoped binding invoker to allow beta, but got scope error: %v", err)
	}
}

func TestBootstrapPluginOnlySkipsFactory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Integrations["alpha"] = config.IntegrationDef{
		Plugin: &config.PluginDef{
			Command: "echo",
		},
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap should succeed for plugin-only: %v", err)
	}
	<-result.ProvidersReady
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
		return &coretesting.StubBinding{N: name}, nil
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

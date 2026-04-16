package bootstrap_test

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	telemetrynoop "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/noop"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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

type trackedIndexedDB struct {
	*coretesting.StubIndexedDB
	closed *atomic.Int32
}

func (t *trackedIndexedDB) Close() error {
	if t.closed != nil {
		t.closed.Add(1)
	}
	return nil
}

func validConfig() *config.Config {
	return &config.Config{
		Plugins: map[string]*config.ProviderEntry{},
		Providers: config.ProvidersConfig{
			Auth: map[string]*config.ProviderEntry{
				"default": {
					Source: config.ProviderSource{Ref: "github.com/valon-technologies/gestalt-providers/auth/oidc", Version: "0.0.1-alpha.1"},
					Config: yaml.Node{Kind: yaml.MappingNode},
				},
			},
			Secrets: map[string]*config.ProviderEntry{
				"default": {Source: config.ProviderSource{Builtin: "test-secrets"}},
			},
			Telemetry: map[string]*config.ProviderEntry{
				"default": {Source: config.ProviderSource{Builtin: "test-telemetry"}},
			},
			IndexedDB: map[string]*config.ProviderEntry{
				"test": {Source: config.ProviderSource{Path: "stub"}},
			},
		},
		Server: config.ServerConfig{
			Public:        config.ListenerConfig{Port: 8080},
			EncryptionKey: "test-key",
			Providers:     config.ServerProvidersConfig{IndexedDB: "test"},
		},
	}
}

func selectedAuthEntry(t *testing.T, cfg *config.Config) *config.ProviderEntry {
	t.Helper()
	_, entry, err := cfg.SelectedAuthProvider()
	if err != nil {
		t.Fatalf("SelectedAuthProvider: %v", err)
	}
	return entry
}

func validFactories() *bootstrap.FactoryRegistry {
	f := bootstrap.NewFactoryRegistry()
	f.Auth = stubAuthFactory("test-auth")
	f.IndexedDB = stubIndexedDBFactory()
	f.Secrets["test-secrets"] = stubSecretManagerFactory()
	f.Telemetry["test-telemetry"] = stubTelemetryFactory()
	return f
}

func transportSecretRef(name string) string {
	return config.EncodeSecretRefTransport(config.SecretRef{
		Provider: "default",
		Name:     name,
	})
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

	t.Run("invoker uses resolved REST connections", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name           string
			restConnection string
			specAuth       *providermanifestv1.ProviderAuth
			connections    map[string]*providermanifestv1.ManifestConnectionDef
			tokenConn      string
			tokenValue     string
			wantAuth       string
			wantAPIKey     string
		}{
			{
				name: "single named connection is inferred as default",
				connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"default": {
						Auth: &providermanifestv1.ProviderAuth{
							Type:             providermanifestv1.AuthTypeOAuth2,
							ClientID:         "client-id",
							ClientSecret:     "client-secret",
							AuthorizationURL: "https://example.com/authorize",
							TokenURL:         "https://example.com/token",
						},
					},
				},
				tokenConn: "default",
			},
			{
				name:           "explicit REST connection is used for invoke",
				restConnection: "workspace",
				connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"workspace": {
						Auth: &providermanifestv1.ProviderAuth{
							Type:             providermanifestv1.AuthTypeOAuth2,
							ClientID:         "client-id",
							ClientSecret:     "client-secret",
							AuthorizationURL: "https://example.com/authorize",
							TokenURL:         "https://example.com/token",
						},
					},
					"backup": {
						Auth: &providermanifestv1.ProviderAuth{
							Type:             providermanifestv1.AuthTypeOAuth2,
							ClientID:         "client-id",
							ClientSecret:     "client-secret",
							AuthorizationURL: "https://example.com/authorize",
							TokenURL:         "https://example.com/token",
						},
					},
				},
				tokenConn: "workspace",
			},
			{
				name: "declarative auth mapping basic preserves derived authorization header",
				specAuth: &providermanifestv1.ProviderAuth{
					Type: providermanifestv1.AuthTypeManual,
					AuthMapping: &providermanifestv1.AuthMapping{
						Basic: &providermanifestv1.BasicAuthMapping{
							Username: providermanifestv1.AuthValue{
								ValueFrom: &providermanifestv1.AuthValueFrom{
									CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "username"},
								},
							},
							Password: providermanifestv1.AuthValue{
								ValueFrom: &providermanifestv1.AuthValueFrom{
									CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "password"},
								},
							},
						},
					},
				},
				connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"default": {Mode: providermanifestv1.ConnectionModeUser},
				},
				tokenConn:  "default",
				tokenValue: `{"username":"alice","password":"secret"}`,
				wantAuth:   "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret")),
			},
			{
				name: "declarative auth mapping headers preserves derived upstream header",
				specAuth: &providermanifestv1.ProviderAuth{
					Type: providermanifestv1.AuthTypeManual,
					AuthMapping: &providermanifestv1.AuthMapping{
						Headers: map[string]providermanifestv1.AuthValue{
							"X-API-Key": {
								ValueFrom: &providermanifestv1.AuthValueFrom{
									CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "api_key"},
								},
							},
						},
					},
				},
				connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"default": {Mode: providermanifestv1.ConnectionModeUser},
				},
				tokenConn:  "default",
				tokenValue: `{"api_key":"secret-key"}`,
				wantAPIKey: "secret-key",
			},
			{
				name:     "auth none still forwards bearer token when connection mode is user",
				specAuth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"workspace": {Mode: providermanifestv1.ConnectionModeUser},
				},
				restConnection: "workspace",
				tokenConn:      "workspace",
				wantAuth:       "Bearer workspace-access-token",
			},
		}

		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var authHeader atomic.Value
				var apiKeyHeader atomic.Value
				var requestPath atomic.Value
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					authHeader.Store(r.Header.Get("Authorization"))
					apiKeyHeader.Store(r.Header.Get("X-API-Key"))
					requestPath.Store(r.URL.Path)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"ok":true}`))
				}))
				defer srv.Close()

				cfg := validConfig()
				cfg.Plugins = map[string]*config.ProviderEntry{
					"slack": {
						ResolvedManifest: &providermanifestv1.Manifest{
							Spec: &providermanifestv1.Spec{
								Auth: tc.specAuth,
								Surfaces: &providermanifestv1.ProviderSurfaces{
									REST: &providermanifestv1.RESTSurface{
										BaseURL:    srv.URL,
										Connection: tc.restConnection,
										Operations: []providermanifestv1.ProviderOperation{
											{Name: "users.list", Method: http.MethodGet, Path: "/users"},
										},
									},
								},
								Connections: tc.connections,
							},
						},
					},
				}

				result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
				if err != nil {
					t.Fatalf("Bootstrap: %v", err)
				}
				t.Cleanup(func() { _ = result.Close(context.Background()) })
				<-result.ProvidersReady

				user, err := result.Services.Users.FindOrCreateUser(ctx, "hugh@test.com")
				if err != nil {
					t.Fatalf("FindOrCreateUser: %v", err)
				}
				tokenValue := tc.tokenConn + "-access-token"
				if tc.tokenValue != "" {
					tokenValue = tc.tokenValue
				}
				if err := result.Services.Tokens.StoreToken(ctx, &core.IntegrationToken{
					UserID:       user.ID,
					Integration:  "slack",
					Connection:   tc.tokenConn,
					Instance:     "default",
					AccessToken:  tokenValue,
					RefreshToken: "refresh-token",
				}); err != nil {
					t.Fatalf("StoreToken: %v", err)
				}

				principal := &principal.Principal{
					UserID: user.ID,
					Source: principal.SourceSession,
					Scopes: []string{"slack"},
				}
				got, err := result.Invoker.Invoke(ctx, principal, "slack", "", "users.list", nil)
				if err != nil {
					t.Fatalf("Invoke: %v", err)
				}
				if got.Status != http.StatusOK {
					t.Fatalf("status = %d, want %d", got.Status, http.StatusOK)
				}
				if gotPath, _ := requestPath.Load().(string); gotPath != "/users" {
					t.Fatalf("path = %q, want %q", gotPath, "/users")
				}
				wantAuth := "Bearer " + tokenValue
				if tc.wantAuth != "" || tc.specAuth != nil {
					wantAuth = tc.wantAuth
				}
				if gotAuth, _ := authHeader.Load().(string); gotAuth != wantAuth {
					t.Fatalf("Authorization = %q, want %q", gotAuth, wantAuth)
				}
				if gotAPIKey, _ := apiKeyHeader.Load().(string); gotAPIKey != tc.wantAPIKey {
					t.Fatalf("X-API-Key = %q, want %q", gotAPIKey, tc.wantAPIKey)
				}
			})
		}
	})
}

func TestBootstrapPassesConfiguredS3ResourceNamesToProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.S3 = map[string]*config.ProviderEntry{
		"archive": {Source: config.ProviderSource{Path: "stub"}},
		"main":    {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	seen := make(map[string]struct{}, len(cfg.Providers.S3))
	factories.S3 = func(node yaml.Node) (s3store.Client, error) {
		var runtime struct {
			Name string `yaml:"name"`
		}
		if err := node.Decode(&runtime); err != nil {
			return nil, err
		}
		seen[runtime.Name] = struct{}{}
		return &coretesting.StubS3{}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(seen) != 2 {
		t.Fatalf("seen S3 runtime names = %v, want 2 entries", seen)
	}
	for _, name := range []string{"archive", "main"} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing S3 runtime name %q in %v", name, seen)
		}
	}
}

func TestBootstrapS3BuildFailureClosesIndexedDBsOnce(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["archive"] = &config.ProviderEntry{
		Source: config.ProviderSource{Path: "stub"},
	}
	cfg.Providers.S3 = map[string]*config.ProviderEntry{
		"assets": {Source: config.ProviderSource{Path: "stub"}},
	}

	var selectedClosed atomic.Int32
	var extraClosed atomic.Int32
	var indexeddbBuilds atomic.Int32

	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) {
		switch indexeddbBuilds.Add(1) {
		case 1:
			return &trackedIndexedDB{
				StubIndexedDB: &coretesting.StubIndexedDB{},
				closed:        &selectedClosed,
			}, nil
		case 2:
			return &trackedIndexedDB{
				StubIndexedDB: &coretesting.StubIndexedDB{},
				closed:        &extraClosed,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected indexeddb build #%d", indexeddbBuilds.Load())
		}
	}
	factories.S3 = func(yaml.Node) (s3store.Client, error) {
		return nil, fmt.Errorf("boom")
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil {
		t.Fatal("Bootstrap: expected error, got nil")
	}
	if !strings.Contains(err.Error(), `bootstrap: s3 from resource "assets": s3 provider: boom`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := selectedClosed.Load(); got != 1 {
		t.Fatalf("selected indexeddb close count = %d, want 1", got)
	}
	if got := extraClosed.Load(); got != 1 {
		t.Fatalf("extra indexeddb close count = %d, want 1", got)
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

	t.Run("rejects invalid plugin invokes dependency", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Plugins = map[string]*config.ProviderEntry{
			"caller": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "caller"},
					Spec:       &providermanifestv1.Spec{},
				},
				Invokes: []config.PluginInvocationDependency{
					{Plugin: "missing", Operation: "ping"},
				},
			},
		}

		_, err := bootstrap.Validate(context.Background(), cfg, validFactories())
		if err == nil || !strings.Contains(err.Error(), `plugins.caller.invokes[0] references unknown plugin "missing"`) {
			t.Fatalf("Validate error = %v, want unknown plugin invokes error", err)
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

	authRuntime, err := config.BuildComponentRuntimeConfigNode("auth", "auth", selectedAuthEntry(t, cfg), yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "clientId"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "prepared-auth"},
		},
	})
	if err != nil {
		t.Fatalf("BuildComponentRuntimeConfigNode(auth): %v", err)
	}
	selectedAuthEntry(t, cfg).Config = authRuntime

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

	t.Run("resolves config secret ref in encryption key", func(t *testing.T) {
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
		cfg.Server.EncryptionKey = transportSecretRef("enc-key")

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
		cfg.Server.EncryptionKey = transportSecretRef("missing-key")

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
		cfg.Server.EncryptionKey = transportSecretRef("empty-secret")

		_, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "empty value") {
			t.Errorf("error should mention empty value: %v", err)
		}
	})

	t.Run("resolves config secret ref in yaml.Node auth config", func(t *testing.T) {
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
		selectedAuthEntry(t, cfg).Config = yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "clientSecret", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: transportSecretRef("auth-secret"), Tag: "!!str"},
			},
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		var decoded struct {
			Source *config.ProviderSource `yaml:"source"`
			Config map[string]string      `yaml:"config"`
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

	t.Run("resolves config secret ref in yaml.Node indexeddb config", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"indexeddb-dsn": "mysql://resolved-dsn"},
			}, nil
		}

		var receivedNode yaml.Node
		factories.IndexedDB = func(node yaml.Node) (indexeddb.IndexedDB, error) {
			receivedNode = node
			return &coretesting.StubIndexedDB{}, nil
		}

		cfg := validConfig()
		ds := cfg.Providers.IndexedDB["test"]
		ds.Config = yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "dsn", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: transportSecretRef("indexeddb-dsn"), Tag: "!!str"},
			},
		}
		cfg.Providers.IndexedDB["test"] = ds

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		var decoded struct {
			Source *config.ProviderEntry `yaml:"provider"`
			Config map[string]string     `yaml:"config"`
		}
		if err := receivedNode.Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.Config["dsn"] != "mysql://resolved-dsn" {
			t.Errorf("dsn: got %q, want %q", decoded.Config["dsn"], "mysql://resolved-dsn")
		}
	})

	t.Run("resolves config secret ref in yaml.Node s3 config", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"s3-token": "resolved-s3-token"},
			}, nil
		}

		var receivedNode yaml.Node
		factories.S3 = func(node yaml.Node) (s3store.Client, error) {
			receivedNode = node
			return &coretesting.StubS3{}, nil
		}

		cfg := validConfig()
		cfg.Providers.S3 = map[string]*config.ProviderEntry{
			"assets": {
				Source: config.ProviderSource{Path: "stub"},
				Config: yaml.Node{
					Kind: yaml.MappingNode,
					Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Value: "token", Tag: "!!str"},
						{Kind: yaml.ScalarNode, Value: transportSecretRef("s3-token"), Tag: "!!str"},
					},
				},
			},
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		var decoded struct {
			Config map[string]string `yaml:"config"`
		}
		if err := receivedNode.Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.Config["token"] != "resolved-s3-token" {
			t.Errorf("token: got %q, want %q", decoded.Config["token"], "resolved-s3-token")
		}
	})

	t.Run("resolves config secret ref in workload tokens", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"workload-token": "gst_wld_resolved-workload-token"},
			}, nil
		}
		factories.Builtins = []core.Provider{
			&coretesting.StubIntegration{N: "weather", ConnMode: core.ConnectionModeNone},
		}

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Workloads: map[string]config.WorkloadDef{
				"triage-bot": {
					Token: transportSecretRef("workload-token"),
					Providers: map[string]config.WorkloadProviderDef{
						"weather": {Allow: []string{"forecast"}},
					},
				},
			},
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		if result.Authorizer == nil {
			t.Fatal("Authorizer is nil")
		}
		if _, ok := result.Authorizer.ResolveWorkloadToken("gst_wld_resolved-workload-token"); !ok {
			t.Fatal("expected resolved workload token to authenticate")
		}
	})

	t.Run("ignores secret refs inside secrets provider config", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"enc-key": "resolved-passphrase"},
			}, nil
		}

		cfg := validConfig()
		cfg.Providers.Secrets["default"] = &config.ProviderEntry{
			Source: config.ProviderSource{Builtin: "test-secrets"},
			Config: yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "prefix", Tag: "!!str"},
					{Kind: yaml.ScalarNode, Value: transportSecretRef("ignored-provider-secret"), Tag: "!!str"},
				},
			},
		}
		cfg.Server.EncryptionKey = config.EncodeSecretRefTransport(config.SecretRef{
			Provider: "default",
			Name:     "enc-key",
		})

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
	})

	t.Run("requires configured provider for programmatic config refs", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		delete(cfg.Providers.Secrets, "default")
		cfg.Server.EncryptionKey = config.EncodeSecretRefTransport(config.SecretRef{
			Provider: "env",
			Name:     "GESTALT_ENCRYPTION_KEY",
		})

		_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), `unknown secrets provider "env"`) {
			t.Fatalf("expected unknown provider error, got %v", err)
		}
	})

	t.Run("configured secrets provider without source errors with config key", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Providers.Secrets["default"] = &config.ProviderEntry{}

		_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), `secrets provider "default" has no source`) {
			t.Fatalf("expected missing source error, got %v", err)
		}
	})

	t.Run("configured builtin secrets provider errors keep config key", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Providers.Secrets["default"] = &config.ProviderEntry{
			Source: config.ProviderSource{Builtin: "missing-builtin"},
		}

		_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), `secrets provider "default" references unknown builtin "missing-builtin"`) {
			t.Fatalf("expected config-key builtin error, got %v", err)
		}
	})

	t.Run("passes top-level provider selection to auth factory", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Providers.Auth = map[string]*config.ProviderEntry{
			"secondary": {Source: config.ProviderSource{Ref: "github.com/valon-technologies/gestalt-providers/auth/oidc", Version: "0.0.1-alpha.1"}},
		}
		cfg.Server.Providers.Auth = "secondary"
		cfg.Providers.Auth["secondary"].Config = yaml.Node{
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
			Source *config.ProviderSource `yaml:"source"`
			Config map[string]string      `yaml:"config"`
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
		cfg.Providers.Auth = nil
		cfg.Server.Providers.Auth = ""

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

func TestBootstrapWorkloadAuthorizationRejectsEitherProvider(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Authorization = config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot-token",
				Providers: map[string]config.WorkloadProviderDef{
					"svc": {Allow: []string{"run"}},
				},
			},
		},
	}

	factories := validFactories()
	factories.Builtins = []core.Provider{
		&coretesting.StubIntegration{N: "svc", ConnMode: core.ConnectionModeEither},
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `unsupported connection mode "either"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

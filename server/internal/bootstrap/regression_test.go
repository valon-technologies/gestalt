package bootstrap_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestGatewayMode_NoRuntimesOrBindingsRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Runtimes = nil
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
	invoker, ok := result.Invoker.(*invocation.Broker)
	if !ok {
		t.Fatalf("expected Invoker to be *invocation.Broker, got %T", result.Invoker)
	}
	lister, ok := result.CapabilityLister.(*invocation.Broker)
	if !ok {
		t.Fatalf("expected CapabilityLister to be *invocation.Broker, got %T", result.CapabilityLister)
	}
	if invoker != lister {
		t.Fatal("expected Invoker and CapabilityLister to reference the same shared instance")
	}
	<-result.ProvidersReady
	names := result.Providers.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("Providers.List: got %v, want [alpha]", names)
	}
	if result.Runtimes != nil {
		t.Error("expected Runtimes to be nil")
	}
	if result.Bindings != nil {
		t.Error("expected Bindings to be nil")
	}
}

func TestPlatformMode_BindingsAndRuntimesWithSafetyLayer(t *testing.T) {
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

	cfg.Runtimes = map[string]config.RuntimeDef{
		"my-runtime": {
			Type:      "echo",
			Providers: []string{"alpha"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {
			Type:      "test-binding",
			Providers: []string{"beta"},
		},
	}

	factories := validFactories()

	var runtimeDeps bootstrap.RuntimeDeps
	factories.Runtimes["echo"] = func(_ context.Context, name string, _ config.RuntimeDef, deps bootstrap.RuntimeDeps) (core.Runtime, error) {
		runtimeDeps = deps
		return &coretesting.StubRuntime{N: name}, nil
	}

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

	if runtimeDeps.Invoker == nil {
		t.Fatal("expected runtime invoker to be non-nil")
	}
	if runtimeDeps.CapabilityLister == nil {
		t.Fatal("expected runtime capability lister to be non-nil")
	}
	if any(runtimeDeps.Invoker) != any(runtimeDeps.CapabilityLister) {
		t.Fatal("expected runtime invoker and capability lister to be the same scoped instance")
	}

	rtCaps := runtimeDeps.CapabilityLister.ListCapabilities()
	for _, cap := range rtCaps {
		if cap.Provider != "alpha" {
			t.Errorf("runtime broker should only see alpha, got %q", cap.Provider)
		}
	}

	if bindingDeps.Invoker == nil {
		t.Fatal("expected binding deps to carry an invoker")
	}
	if bindingDeps.Invoker == result.Invoker {
		t.Fatal("expected binding deps to be scoped, not the shared invoker")
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

func TestEgressPolicyWiredThroughBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		DefaultAction: "deny",
		Policies: []config.EgressPolicyRule{
			{Action: "allow", Provider: "alpha", PathPrefix: "/v1/public"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {
			Type:      "test-binding",
			Providers: []string{"alpha"},
		},
	}

	factories := validFactories()

	var receivedEgress bootstrap.EgressDeps
	factories.Bindings["test-binding"] = func(_ context.Context, name string, _ config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
		receivedEgress = deps.Egress
		return &coretesting.StubBinding{N: name}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	if receivedEgress.Resolver == nil {
		t.Fatal("expected egress resolver to be wired into binding deps")
	}
	if receivedEgress.Resolver.Policy == nil {
		t.Fatal("expected policy enforcer to be set")
	}

	resolve := func(path string) error {
		_, err := receivedEgress.Resolver.Resolve(ctx, egress.ResolutionInput{
			Target: egress.Target{Provider: "alpha", Method: http.MethodGet, Host: "api.test", Path: path},
		})
		return err
	}

	if err := resolve("/v1/public/items"); err != nil {
		t.Fatalf("allowed path should pass: %v", err)
	}
	if err := resolve("/v1/admin/users"); err == nil {
		t.Fatal("default-deny should block unmatched path")
	}
}

func TestEgressCredentialGrantWiredThroughBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{Host: "api.test", SecretRef: "test-key"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {
			Type:      "test-binding",
			Providers: []string{"alpha"},
		},
	}

	factories := validFactories()

	var receivedEgress bootstrap.EgressDeps
	factories.Bindings["test-binding"] = func(_ context.Context, name string, _ config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
		receivedEgress = deps.Egress
		return &coretesting.StubBinding{N: name}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	if receivedEgress.Resolver == nil {
		t.Fatal("expected egress resolver to be wired")
	}
	if receivedEgress.Resolver.Credentials == nil {
		t.Fatal("expected credential resolver to be wired when grants are configured")
	}
}

func TestSecretBackedCredentialGrant_ConfigGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const secretName = "vendor-api-key"
	const secretValue = "sk-test-secret-abc123"

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{
				Host:      "api.vendor.test",
				SecretRef: secretName,
				AuthStyle: "bearer",
			},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {Type: "test-binding"},
	}

	factories := validFactories()
	factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{
			Secrets: map[string]string{secretName: secretValue},
		}, nil
	}

	var receivedEgress bootstrap.EgressDeps
	factories.Bindings["test-binding"] = func(_ context.Context, name string, _ config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
		receivedEgress = deps.Egress
		return &coretesting.StubBinding{N: name}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	if receivedEgress.Resolver == nil || receivedEgress.Resolver.Credentials == nil {
		t.Fatal("expected credential resolver to be wired")
	}

	agentCtx := egress.WithSubject(ctx, egress.Subject{Kind: egress.SubjectUser, ID: "user-1"})
	resolution, err := receivedEgress.Resolver.Resolve(agentCtx, egress.ResolutionInput{
		Target: egress.Target{Method: http.MethodGet, Host: "api.vendor.test", Path: "/v1/data"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	const wantAuth = "Bearer " + secretValue
	if resolution.Credential.Authorization != wantAuth {
		t.Fatalf("got Authorization %q, want %q", resolution.Credential.Authorization, wantAuth)
	}
}

func TestSecretBackedCredentialGrant_MultiTenantHostMatching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		secretNameShop1 = "shop-1-key"
		secretNameShop2 = "shop-2-key"
		secretShop1     = "sk-shop-one"
		secretShop2     = "sk-shop-two"
		hostShop1       = "shop-1.example.com"
		hostShop2       = "shop-2.example.com"
	)

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{Host: hostShop1, SecretRef: secretNameShop1, AuthStyle: "raw"},
			{Host: hostShop2, SecretRef: secretNameShop2, AuthStyle: "raw"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {Type: "test-binding"},
	}

	factories := validFactories()
	factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{
			Secrets: map[string]string{
				secretNameShop1: secretShop1,
				secretNameShop2: secretShop2,
			},
		}, nil
	}

	var receivedEgress bootstrap.EgressDeps
	factories.Bindings["test-binding"] = func(_ context.Context, name string, _ config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
		receivedEgress = deps.Egress
		return &coretesting.StubBinding{N: name}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	agentCtx := egress.WithSubject(ctx, egress.Subject{Kind: egress.SubjectUser, ID: "user-1"})

	resolve := func(host string) string {
		t.Helper()
		res, err := receivedEgress.Resolver.Resolve(agentCtx, egress.ResolutionInput{
			Target: egress.Target{Method: http.MethodGet, Host: host, Path: "/api/orders"},
		})
		if err != nil {
			t.Fatalf("Resolve(%s): %v", host, err)
		}
		return res.Credential.Authorization
	}

	if auth := resolve(hostShop1); auth != secretShop1 {
		t.Fatalf("shop-1: got %q, want %q", auth, secretShop1)
	}
	if auth := resolve(hostShop2); auth != secretShop2 {
		t.Fatalf("shop-2: got %q, want %q", auth, secretShop2)
	}
}

func TestSecretRefNotEagerlyResolvedByBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const secretRefValue = "my-key"

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{Host: "api.test", SecretRef: secretRefValue},
		},
	}

	factories := validFactories()

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	if len(result.Egress.CredentialGrants) != 1 {
		t.Fatalf("expected 1 credential grant, got %d", len(result.Egress.CredentialGrants))
	}
	if result.Egress.CredentialGrants[0].SecretRef != secretRefValue {
		t.Fatalf("secret_ref was modified by bootstrap: got %q, want %q",
			result.Egress.CredentialGrants[0].SecretRef, secretRefValue)
	}
}

func TestSecretBackedGrant_SecretURIPrefixStripped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		secretName  = "prefixed-key"
		secretValue = "sk-with-prefix"
	)

	cfg := validConfig()
	cfg.Egress = config.EgressConfig{
		Credentials: []config.EgressCredentialGrant{
			{Host: "api.prefix.test", SecretRef: "secret://" + secretName, AuthStyle: "bearer"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {Type: "test-binding"},
	}

	factories := validFactories()
	factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{
			Secrets: map[string]string{secretName: secretValue},
		}, nil
	}

	var receivedEgress bootstrap.EgressDeps
	factories.Bindings["test-binding"] = func(_ context.Context, name string, _ config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
		receivedEgress = deps.Egress
		return &coretesting.StubBinding{N: name}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	agentCtx := egress.WithSubject(ctx, egress.Subject{Kind: egress.SubjectUser, ID: "user-1"})
	resolution, err := receivedEgress.Resolver.Resolve(agentCtx, egress.ResolutionInput{
		Target: egress.Target{Method: http.MethodGet, Host: "api.prefix.test", Path: "/v1"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	const wantAuth = "Bearer " + secretValue
	if resolution.Credential.Authorization != wantAuth {
		t.Fatalf("got Authorization %q, want %q", resolution.Credential.Authorization, wantAuth)
	}
}

func TestBootstrap_InlineProviderStaticHeadersReachUpstream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const headerName = "X-Static-Version"
	const headerValue = "2026-02-09"

	cases := []struct {
		name   string
		plugin func(apiURL string, specURL string) *config.PluginDef
	}{
		{
			name: "spec_loaded_openapi",
			plugin: func(_ string, specURL string) *config.PluginDef {
				return &config.PluginDef{
					OpenAPI:           specURL,
					OpenAPIConnection: config.PluginConnectionName,
					Headers: map[string]string{
						headerName: headerValue,
					},
					Connections: map[string]*config.ConnectionDef{
						config.PluginConnectionName: {
							Mode: "none",
							Auth: config.ConnectionAuthDef{Type: "none"},
						},
					},
				}
			},
		},
		{
			name: "declarative",
			plugin: func(apiURL string, _ string) *config.PluginDef {
				return &config.PluginDef{
					BaseURL: apiURL,
					Headers: map[string]string{
						headerName: headerValue,
					},
					Auth: &config.ConnectionAuthDef{Type: "none"},
					Operations: []config.InlineOperationDef{
						{
							Name:        "list_items",
							Description: "List items",
							Method:      http.MethodGet,
							Path:        "/items",
						},
					},
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotHeader := make(chan string, 1)
			apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHeader <- r.Header.Get(headerName)
				writeTestJSON(w, map[string]any{"ok": true})
			}))
			testutil.CloseOnCleanup(t, apiSrv)

			specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(w, map[string]any{
					"openapi": "3.0.0",
					"info":    map[string]string{"title": "Test API"},
					"servers": []any{map[string]string{"url": apiSrv.URL}},
					"paths": map[string]any{
						"/items": map[string]any{
							"get": map[string]any{
								"operationId": "list_items",
								"summary":     "List items",
							},
						},
					},
				})
			}))
			testutil.CloseOnCleanup(t, specSrv)

			cfg := validConfig()
			cfg.Integrations = map[string]config.IntegrationDef{
				"sample": {
					Plugin: tc.plugin(apiSrv.URL, specSrv.URL),
				},
			}

			factories := validFactories()
			factories.Datastores["test-store"] = func(yaml.Node, bootstrap.Deps) (core.Datastore, error) {
				return &coretesting.StubDatastore{
					FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
						return &core.User{ID: "u1", Email: email}, nil
					},
					TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
						return &core.IntegrationToken{AccessToken: ""}, nil
					},
				}, nil
			}

			result, err := bootstrap.Bootstrap(ctx, cfg, factories)
			if err != nil {
				t.Fatalf("Bootstrap: %v", err)
			}
			<-result.ProvidersReady

			p := &principal.Principal{Identity: &core.UserIdentity{Email: "tester@example.com"}}
			if _, err := result.Invoker.Invoke(ctx, p, "sample", "", "list_items", nil); err != nil {
				t.Fatalf("Invoke: %v", err)
			}

			select {
			case got := <-gotHeader:
				if got != headerValue {
					t.Fatalf("%s = %q, want %q", headerName, got, headerValue)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for upstream request")
			}
		})
	}
}

func TestBootstrap_InlineMCPProviderStaticHeadersReachUpstream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const headerName = "X-Static-Version"
	const headerValue = "2026-02-09"

	mcpSrv := mcpserver.NewMCPServer("test-remote", "1.0.0")
	mcpSrv.AddTool(
		mcpgo.NewTool("list_items", mcpgo.WithDescription("List items")),
		func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("ok"), nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(
		mcpSrv,
		mcpserver.WithStateLess(true),
	)

	var requestCount atomic.Int32
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(headerName); got != headerValue {
			http.Error(w, "missing static header", http.StatusUnauthorized)
			return
		}
		requestCount.Add(1)
		handler.ServeHTTP(w, r)
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"sample": {
			Plugin: &config.PluginDef{
				MCPURL:        apiSrv.URL,
				MCPConnection: config.PluginConnectionName,
				Headers: map[string]string{
					headerName: headerValue,
				},
				Connections: map[string]*config.ConnectionDef{
					config.PluginConnectionName: {
						Mode: string(core.ConnectionModeNone),
						Auth: config.ConnectionAuthDef{Type: "none"},
					},
				},
			},
		},
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	prov, err := result.Providers.Get("sample")
	if err != nil {
		t.Fatalf("Providers.Get(sample): %v", err)
	}

	sessionProv, ok := prov.(core.SessionCatalogProvider)
	if !ok {
		t.Fatalf("provider does not implement SessionCatalogProvider: %T", prov)
	}
	cat, err := sessionProv.CatalogForRequest(ctx, "")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if len(cat.Operations) != 1 || cat.Operations[0].ID != "list_items" {
		t.Fatalf("unexpected catalog operations: %+v", cat.Operations)
	}

	type callToolProvider interface {
		CallTool(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error)
	}

	caller, ok := prov.(callToolProvider)
	if !ok {
		t.Fatalf("provider does not support CallTool: %T", prov)
	}
	resultTool, err := caller.CallTool(ctx, "list_items", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if resultTool.IsError {
		t.Fatalf("unexpected tool error: %+v", resultTool.Content)
	}
	if requestCount.Load() == 0 {
		t.Fatal("expected at least one MCP request to reach the upstream")
	}
}

func TestBootstrap_ConfigHeadersOverrideManifestHeaders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		headerName    = "X-Static-Version"
		manifestValue = "from-manifest"
		configValue   = "from-config"
	)

	gotHeader := make(chan string, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader <- r.Header.Get(headerName)
		writeTestJSON(w, map[string]any{"ok": true})
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/sample",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL: apiSrv.URL,
			Headers: map[string]string{
				"x-static-version": manifestValue,
			},
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:   "list_items",
					Method: http.MethodGet,
					Path:   "/items",
				},
			},
		},
	}
	manifestData, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "plugin.json")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"sample": {
			Plugin: &config.PluginDef{
				IsDeclarative:        true,
				ResolvedManifestPath: manifestPath,
				Headers: map[string]string{
					headerName: configValue,
				},
			},
		},
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	prov, err := result.Providers.Get("sample")
	if err != nil {
		t.Fatalf("Providers.Get: %v", err)
	}

	execResult, err := prov.Execute(ctx, "list_items", nil, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if execResult.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", execResult.Status, http.StatusOK)
	}

	select {
	case got := <-gotHeader:
		if got != configValue {
			t.Fatalf("%s = %q, want %q", headerName, got, configValue)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}
}

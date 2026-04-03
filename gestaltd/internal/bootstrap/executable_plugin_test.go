package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestExecutableSDKExampleProviderReceivesStartConfig(t *testing.T) {
	t.Parallel()

	bin := buildExampleProviderBinary(t)
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"example": {
				Plugin: &config.PluginDef{
					Command: bin,
					Config: mustNode(t, map[string]any{
						"greeting": "Hello from config",
					}),
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("example")
	if err != nil {
		t.Fatalf("providers.Get(example): %v", err)
	}

	result, err := prov.Execute(context.Background(), "greet", map[string]any{"name": "Gestalt"}, "")
	if err != nil {
		t.Fatalf("Execute(greet): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("greet status = %d", result.Status)
	}
	if result.Body != `{"message":"Hello from config, Gestalt!"}` {
		t.Fatalf("greet body = %q", result.Body)
	}

	result, err = prov.Execute(context.Background(), "status", nil, "")
	if err != nil {
		t.Fatalf("Execute(status): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status status = %d", result.Status)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
		t.Fatalf("json.Unmarshal(status): %v", err)
	}
	if got["name"] != "example" {
		t.Fatalf("status.name = %q", got["name"])
	}
	if got["greeting"] != "Hello from config" {
		t.Fatalf("status.greeting = %q", got["greeting"])
	}
}

func buildEchoPluginBinary(t *testing.T) string {
	t.Helper()
	if sharedEchoPluginBin == "" {
		t.Fatal("shared echo plugin binary not initialized")
	}
	return sharedEchoPluginBin
}

func buildExampleProviderBinary(t *testing.T) string {
	t.Helper()
	if sharedExampleProviderBin == "" {
		t.Fatal("shared example provider binary not initialized")
	}
	return sharedExampleProviderBin
}

func mustNode(t *testing.T, value any) yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		t.Fatalf("node.Encode: %v", err)
	}
	return node
}

func TestPluginManifestOAuthWiresConnectionAuth(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/echo",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth: &pluginmanifestv1.ProviderAuth{
				Type:             pluginmanifestv1.AuthTypeOAuth2,
				AuthorizationURL: "https://example.com/authorize",
				TokenURL:         "https://example.com/token",
				Scopes:           []string{"read", "write"},
			},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider", SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider"},
		},
	}
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echoauth": {
				Plugin: &config.PluginDef{
					Command: bin,
					Args:    []string{"provider"},
					Config: mustNode(t, map[string]any{
						"client_id":     "test-client-id",
						"client_secret": "test-client-secret",
					}),
					ResolvedManifest: manifest,
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, connAuth, err := buildProvidersStrict(
		context.Background(), cfg, factories,
		Deps{BaseURL: "https://gestalt.example.com"},
	)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echoauth")
	if err != nil {
		t.Fatalf("providers.Get(echoauth): %v", err)
	}
	if cat := prov.Catalog(); cat == nil || len(cat.Operations) == 0 {
		t.Fatal("expected at least one operation from the echo provider")
	}

	handlers, ok := connAuth["echoauth"]
	if !ok {
		t.Fatal("expected connection auth entry for echoauth")
	}
	handler, ok := handlers[config.PluginConnectionName]
	if !ok {
		t.Fatalf("expected handler for connection %q", config.PluginConnectionName)
	}
	if handler.AuthorizationBaseURL() != "https://example.com/authorize" {
		t.Fatalf("authorization URL = %q, want %q", handler.AuthorizationBaseURL(), "https://example.com/authorize")
	}
	if handler.TokenURL() != "https://example.com/token" {
		t.Fatalf("token URL = %q, want %q", handler.TokenURL(), "https://example.com/token")
	}
}

func TestPluginManifestNoAuthSkipsConnectionAuth(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	manifest := &pluginmanifestv1.Manifest{
		Source:   "github.com/acme/plugins/echo",
		Version:  "1.0.0",
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
		Artifacts: []pluginmanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider", SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider"},
		},
	}
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echonoauth": {
				Plugin: &config.PluginDef{
					Command:          bin,
					Args:             []string{"provider"},
					ResolvedManifest: manifest,
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, connAuth, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	if _, ok := connAuth["echonoauth"]; ok {
		t.Fatal("expected no connection auth for plugin without oauth2 auth")
	}
}

func TestPluginProcessEnvIsolation(t *testing.T) {
	t.Parallel()
	bin := buildEchoPluginBinary(t)

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echoext": {
				Plugin: &config.PluginDef{
					Command: bin,
					Args:    []string{"provider"},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": "USER"}, "")
	if err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Found {
		t.Fatalf("plugin process should not see USER, but got %q", env.Value)
	}

	result, err = prov.Execute(context.Background(), "read_env", map[string]any{"name": "PATH"}, "")
	if err != nil {
		t.Fatalf("Execute read_env PATH: %v", err)
	}
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatal("plugin process should see PATH")
	}
}

func TestHybridPluginMergesCommandAndOpenAPI(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Hybrid Test API"},
		"servers": []any{map[string]string{"url": "https://api.hybrid.example/v1"}},
		"paths": map[string]any{
			"/messages": map[string]any{
				"get": map[string]any{
					"operationId": "list_messages",
					"summary":     "List messages",
				},
			},
			"/messages/{id}": map[string]any{
				"get": map[string]any{
					"operationId": "get_message",
					"summary":     "Get a message by ID",
					"parameters": []any{
						map[string]any{
							"name":     "id",
							"in":       "path",
							"required": true,
							"schema":   map[string]string{"type": "string"},
						},
					},
				},
			},
		},
	}
	specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(spec)
	}))
	testutil.CloseOnCleanup(t, specSrv)

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"hybrid": {
				Plugin: &config.PluginDef{
					Command: bin,
					Args:    []string{"provider"},
					OpenAPI: specSrv.URL,
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("hybrid")
	if err != nil {
		t.Fatalf("providers.Get(hybrid): %v", err)
	}

	cat := prov.Catalog()
	if cat == nil {
		t.Fatal("expected hybrid provider catalog")
	}
	opNames := make(map[string]bool, len(cat.Operations))
	for _, op := range cat.Operations {
		opNames[op.ID] = true
	}

	if !opNames["echo"] {
		t.Error("expected plugin operation 'echo' to be present")
	}
	if !opNames["list_messages"] {
		t.Error("expected spec operation 'list_messages' to be present")
	}
	if !opNames["get_message"] {
		t.Error("expected spec operation 'get_message' to be present")
	}

	result, err := prov.Execute(context.Background(), "echo", map[string]any{"msg": "hello"}, "")
	if err != nil {
		t.Fatalf("Execute(echo): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("echo status = %d, want %d", result.Status, http.StatusOK)
	}
}

func TestHybridPluginMergesCommandAndDeclarativeREST(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	gotPath := make(chan string, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/hybrid",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL: apiSrv.URL,
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:        "list_items",
					Description: "List items",
					Method:      http.MethodGet,
					Path:        "/items",
				},
				{
					Name:        "get_item",
					Description: "Get item",
					Method:      http.MethodGet,
					Path:        "/items/{id}",
					Parameters: []pluginmanifestv1.ProviderParameter{
						{
							Name:     "id",
							Type:     "string",
							In:       "path",
							Required: true,
						},
					},
				},
			},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider",
				SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider",
			},
		},
	}
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"hybrid": {
				Plugin: &config.PluginDef{
					Command:          bin,
					Args:             []string{"provider"},
					ResolvedManifest: manifest,
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("hybrid")
	if err != nil {
		t.Fatalf("providers.Get(hybrid): %v", err)
	}

	cat := prov.Catalog()
	if cat == nil {
		t.Fatal("expected hybrid provider catalog")
	}
	ops := make(map[string]catalog.CatalogOperation, len(cat.Operations))
	for _, op := range cat.Operations {
		ops[op.ID] = op
	}

	if op, ok := ops["echo"]; !ok {
		t.Fatal("expected plugin operation 'echo' to be present")
	} else if op.Transport != catalog.TransportPlugin {
		t.Fatalf("echo transport = %q, want %q", op.Transport, catalog.TransportPlugin)
	}
	if op, ok := ops["list_items"]; !ok {
		t.Fatal("expected declarative REST operation 'list_items' to be present")
	} else if op.Transport != catalog.TransportREST {
		t.Fatalf("list_items transport = %q, want %q", op.Transport, catalog.TransportREST)
	}
	if _, ok := ops["get_item"]; !ok {
		t.Fatal("expected declarative REST operation 'get_item' to be present")
	}

	result, err := prov.Execute(context.Background(), "list_items", nil, "")
	if err != nil {
		t.Fatalf("Execute(list_items): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("list_items status = %d, want %d", result.Status, http.StatusOK)
	}

	select {
	case got := <-gotPath:
		if got != "/items" {
			t.Fatalf("path = %q, want %q", got, "/items")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}
}

func TestHybridPluginUsesManifestStaticHeadersForSpecSurface(t *testing.T) {
	t.Parallel()

	const (
		headerName  = "X-Static-Version"
		headerValue = "2026-02-09"
	)

	bin := buildEchoPluginBinary(t)

	gotHeader := make(chan string, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader <- r.Header.Get(headerName)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"openapi": "3.0.0",
			"info":    map[string]string{"title": "Hybrid Test API"},
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

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/hybrid",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: specSrv.URL,
			Headers: map[string]string{
				headerName: headerValue,
			},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider",
				SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider",
			},
		},
	}
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"hybrid": {
				Plugin: &config.PluginDef{
					Command:          bin,
					Args:             []string{"provider"},
					ResolvedManifest: manifest,
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("hybrid")
	if err != nil {
		t.Fatalf("providers.Get(hybrid): %v", err)
	}

	result, err := prov.Execute(context.Background(), "list_items", nil, "")
	if err != nil {
		t.Fatalf("Execute(list_items): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	select {
	case got := <-gotHeader:
		if got != headerValue {
			t.Fatalf("%s = %q, want %q", headerName, got, headerValue)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}
}

func TestHybridPluginUsesManifestManagedParametersForSpecSurface(t *testing.T) {
	t.Parallel()

	const (
		headerName  = "Intercom-Version"
		headerValue = "2.11"
	)

	bin := buildEchoPluginBinary(t)

	gotHeader := make(chan string, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader <- r.Header.Get(headerName)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"openapi": "3.0.0",
			"info":    map[string]string{"title": "Hybrid Managed Parameters Test API"},
			"servers": []any{map[string]string{"url": apiSrv.URL}},
			"paths": map[string]any{
				"/items": map[string]any{
					"get": map[string]any{
						"operationId": "list_items",
						"summary":     "List items",
						"parameters": []any{
							map[string]any{
								"name":     headerName,
								"in":       "header",
								"required": true,
								"schema":   map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		})
	}))
	testutil.CloseOnCleanup(t, specSrv)

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/hybrid",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: specSrv.URL,
			ManagedParameters: []pluginmanifestv1.ManagedParameter{
				{
					In:    "header",
					Name:  "intercom-version",
					Value: headerValue,
				},
			},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider",
				SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider",
			},
		},
	}
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"hybrid": {
				Plugin: &config.PluginDef{
					Command:          bin,
					Args:             []string{"provider"},
					ResolvedManifest: manifest,
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("hybrid")
	if err != nil {
		t.Fatalf("providers.Get(hybrid): %v", err)
	}

	cat := prov.Catalog()
	if cat == nil {
		t.Fatalf("unexpected catalog: %+v", cat)
	}
	foundListItems := false
	for i := range cat.Operations {
		if cat.Operations[i].ID == "list_items" {
			foundListItems = true
			if got := cat.Operations[i].Parameters; len(got) != 0 {
				t.Fatalf("catalog params = %+v, want none", got)
			}
			break
		}
	}
	if !foundListItems {
		t.Fatalf("expected list_items operation in catalog: %+v", cat.Operations)
	}

	result, err := prov.Execute(context.Background(), "list_items", nil, "")
	if err != nil {
		t.Fatalf("Execute(list_items): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	select {
	case got := <-gotHeader:
		if got != headerValue {
			t.Fatalf("%s = %q, want %q", headerName, got, headerValue)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}
}

func TestHybridPluginUsesManifestManagedPathParametersForSpecSurface(t *testing.T) {
	t.Parallel()

	const managedAccountID = "acct-managed"

	bin := buildEchoPluginBinary(t)

	gotPath := make(chan string, 1)
	gotPageSize := make(chan string, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path
		gotPageSize <- r.URL.Query().Get("page_size")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"openapi": "3.0.0",
			"info":    map[string]string{"title": "Hybrid Managed Path Parameters Test API"},
			"servers": []any{map[string]string{"url": apiSrv.URL}},
			"paths": map[string]any{
				"/accounts/{account_id}/items": map[string]any{
					"get": map[string]any{
						"operationId": "list_items",
						"summary":     "List items",
						"parameters": []any{
							map[string]any{
								"name":     "account_id",
								"in":       "path",
								"required": true,
								"schema":   map[string]any{"type": "string"},
							},
							map[string]any{
								"name":   "page_size",
								"in":     "query",
								"schema": map[string]any{"type": "integer"},
							},
						},
					},
				},
			},
		})
	}))
	testutil.CloseOnCleanup(t, specSrv)

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/hybrid",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: specSrv.URL,
			ManagedParameters: []pluginmanifestv1.ManagedParameter{
				{
					In:    "path",
					Name:  "account_id",
					Value: managedAccountID,
				},
			},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider",
				SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: "artifacts/" + runtime.GOOS + "/" + runtime.GOARCH + "/provider",
			},
		},
	}
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"hybrid": {
				Plugin: &config.PluginDef{
					Command:          bin,
					Args:             []string{"provider"},
					ResolvedManifest: manifest,
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("hybrid")
	if err != nil {
		t.Fatalf("providers.Get(hybrid): %v", err)
	}

	cat := prov.Catalog()
	if cat == nil {
		t.Fatalf("unexpected catalog: %+v", cat)
	}
	foundListItems := false
	for i := range cat.Operations {
		if cat.Operations[i].ID == "list_items" {
			foundListItems = true
			if got := cat.Operations[i].Path; got != "/accounts/acct-managed/items" {
				t.Fatalf("catalog path = %q, want %q", got, "/accounts/acct-managed/items")
			}
			if got := cat.Operations[i].Parameters; len(got) != 1 || got[0].Name != "page_size" {
				t.Fatalf("catalog params = %+v, want only page_size", got)
			}
			break
		}
	}
	if !foundListItems {
		t.Fatalf("expected list_items operation in catalog: %+v", cat.Operations)
	}

	result, err := prov.Execute(context.Background(), "list_items", map[string]any{"page_size": 25}, "")
	if err != nil {
		t.Fatalf("Execute(list_items): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}

	select {
	case got := <-gotPath:
		if got != "/accounts/acct-managed/items" {
			t.Fatalf("path = %q, want %q", got, "/accounts/acct-managed/items")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}

	select {
	case got := <-gotPageSize:
		if got != "25" {
			t.Fatalf("page_size = %q, want %q", got, "25")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream query param")
	}
}

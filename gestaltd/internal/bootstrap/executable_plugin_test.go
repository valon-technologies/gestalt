package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestExecutableSDKExampleProviderReceivesStartConfig(t *testing.T) {
	t.Parallel()

	bin := buildExampleProviderBinary(t)
	manifestRoot := exampleProviderRoot(t)
	manifest := newExecutableManifest("Example Provider", "A minimal example provider built with the public SDK")
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"example": {
				Plugin: &config.PluginDef{
					Command:              bin,
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "plugin.yaml"),
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
	if prov.DisplayName() != "Example Provider" {
		t.Fatalf("DisplayName = %q", prov.DisplayName())
	}
	if prov.Description() != "A minimal example provider built with the public SDK" {
		t.Fatalf("Description = %q", prov.Description())
	}
	cat := prov.Catalog()
	if cat == nil || len(cat.Operations) != 3 {
		t.Fatalf("unexpected catalog: %+v", cat)
	}
	if cat.DisplayName != "Example Provider" || cat.Description != "A minimal example provider built with the public SDK" {
		t.Fatalf("unexpected catalog metadata: %+v", cat)
	}
	if cat.Operations[0].Transport != catalog.TransportPlugin {
		t.Fatalf("unexpected catalog transport: %+v", cat.Operations[0])
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

func TestPythonSourcePluginFallsBackWithoutGoOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("source-plugin fallback fixture is POSIX-only")
	}

	bin := buildExampleProviderBinary(t)
	root := t.TempDir()
	manifest := &pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/python-source",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Python Source",
		Description: "Python source provider fixture",
		Kinds:       []string{pluginmanifestv1.KindPlugin},
		Plugin: &pluginmanifestv1.Plugin{
			Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
		},
	}
	manifestData, err := pluginpkg.EncodeSourceManifestFormat(manifest, pluginpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	manifestPath := filepath.Join(root, "plugin.yaml")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.yaml): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
plugin = "provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}
	catalogData, err := yaml.Marshal(&catalog.Catalog{
		Name: "python-source",
		Operations: []catalog.CatalogOperation{
			{ID: "greet", Method: http.MethodPost},
			{ID: "status", Method: http.MethodGet},
		},
	})
	if err != nil {
		t.Fatalf("yaml.Marshal(catalog): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, pluginpkg.StaticCatalogFile), catalogData, 0o644); err != nil {
		t.Fatalf("WriteFile(catalog.yaml): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".venv", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.venv/bin): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".venv", "bin", "python"), []byte("#!/bin/sh\nset -eu\nexec "+strconv.Quote(bin)+"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(.venv/bin/python): %v", err)
	}

	t.Setenv("PATH", t.TempDir())

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"python-source": {
				Plugin: &config.PluginDef{
					ResolvedManifest:     manifest,
					ResolvedManifestPath: manifestPath,
					Config: mustNode(t, map[string]any{
						"greeting": "Hi",
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

	prov, err := providers.Get("python-source")
	if err != nil {
		t.Fatalf("providers.Get(python-source): %v", err)
	}

	result, err := prov.Execute(context.Background(), "greet", map[string]any{"name": "Ada"}, "")
	if err != nil {
		t.Fatalf("Execute(greet): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("greet status = %d, want %d", result.Status, http.StatusOK)
	}
	if result.Body != `{"message":"Hi, Ada!"}` {
		t.Fatalf("greet body = %q", result.Body)
	}
}

func TestExecutableSDKExampleProviderAppliesConfigMetadataOverrides(t *testing.T) {
	t.Parallel()

	const iconSVG = `<svg viewBox="0 0 10 10"><rect x="1" y="1" width="8" height="8"/></svg>`

	bin := buildExampleProviderBinary(t)
	iconPath := t.TempDir() + "/override.svg"
	if err := os.WriteFile(iconPath, []byte(iconSVG), 0o644); err != nil {
		t.Fatalf("WriteFile(icon): %v", err)
	}

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name:        "example",
		DisplayName: "Catalog Display",
		Description: "Catalog Description",
		Operations: []catalog.CatalogOperation{
			{ID: "status", Method: http.MethodGet},
		},
	})
	manifest := newExecutableManifest("Manifest Display", "Manifest Description")

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"example": {
				DisplayName: "Config Display",
				Description: "Config Description",
				IconFile:    iconPath,
				Plugin: &config.PluginDef{
					Command:              bin,
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "plugin.yaml"),
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
	if prov.DisplayName() != "Config Display" {
		t.Fatalf("DisplayName = %q, want %q", prov.DisplayName(), "Config Display")
	}
	if prov.Description() != "Config Description" {
		t.Fatalf("Description = %q, want %q", prov.Description(), "Config Description")
	}

	cat := prov.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
	}
	if cat.DisplayName != "Config Display" {
		t.Fatalf("catalog DisplayName = %q, want %q", cat.DisplayName, "Config Display")
	}
	if cat.Description != "Config Description" {
		t.Fatalf("catalog Description = %q, want %q", cat.Description, "Config Description")
	}
	if cat.IconSVG != iconSVG {
		t.Fatalf("catalog IconSVG = %q, want %q", cat.IconSVG, iconSVG)
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

func exampleProviderRoot(t *testing.T) string {
	t.Helper()
	return testutil.ExampleProviderPluginPath(t)
}

func mustNode(t *testing.T, value any) yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		t.Fatalf("node.Encode: %v", err)
	}
	return node
}

func writeStaticCatalog(t *testing.T, cat *catalog.Catalog) string {
	t.Helper()
	data, err := yaml.Marshal(cat)
	if err != nil {
		t.Fatalf("yaml.Marshal(catalog): %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, pluginpkg.StaticCatalogFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(catalog): %v", err)
	}
	return dir
}

func newExecutableManifest(displayName, description string) *pluginmanifestv1.Manifest {
	return &pluginmanifestv1.Manifest{
		Source:      "github.com/acme/plugins/test",
		Version:     "1.0.0",
		Kinds:       []string{pluginmanifestv1.KindPlugin},
		DisplayName: displayName,
		Description: description,
		Plugin:      &pluginmanifestv1.Plugin{},
	}
}

func TestPluginManifestOAuthWiresConnectionAuth(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoauth",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	manifest.Plugin.Auth = &pluginmanifestv1.ProviderAuth{
		Type:             pluginmanifestv1.AuthTypeOAuth2,
		AuthorizationURL: "https://example.com/authorize",
		TokenURL:         "https://example.com/token",
		Scopes:           []string{"read", "write"},
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
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "plugin.yaml"),
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

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echonoauth",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echonoauth": {
				Plugin: &config.PluginDef{
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "plugin.yaml"),
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

func TestPluginManifestNamedOAuthKeepsProviderTokenMode(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoauth",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echoauth": {
				Plugin: &config.PluginDef{
					Command:           bin,
					Args:              []string{"provider"},
					Source:            &config.PluginSourceDef{Ref: "github.com/acme/plugins/test", Version: "1.0.0"},
					DefaultConnection: "workspace",
					Connections: map[string]*config.ConnectionDef{
						"workspace": {
							Auth: config.ConnectionAuthDef{
								Type:             pluginmanifestv1.AuthTypeOAuth2,
								AuthorizationURL: "https://example.com/authorize",
								TokenURL:         "https://example.com/token",
							},
						},
					},
					Config: mustNode(t, map[string]any{
						"client_id":     "test-client-id",
						"client_secret": "test-client-secret",
					}),
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "plugin.yaml"),
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(
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
	if prov.ConnectionMode() != core.ConnectionModeUser {
		t.Fatalf("ConnectionMode = %q, want %q", prov.ConnectionMode(), core.ConnectionModeUser)
	}
}

func TestPluginProcessEnvIsolation(t *testing.T) {
	t.Parallel()
	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echoext": {
				Plugin: &config.PluginDef{
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "plugin.yaml"),
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

func TestExecutablePluginRequiresManifest(t *testing.T) {
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
	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err == nil {
		t.Fatal("expected buildProvidersStrict to reject executable plugin without manifest")
	}
	if got := err.Error(); got != `bootstrap: provider validation failed: integration "echoext": integration "echoext" must resolve to a provider manifest` {
		t.Fatalf("unexpected error: %v", err)
	}
}

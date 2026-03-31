package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

type runtimeOutput struct {
	Name            string         `json:"name"`
	CapabilityCount int            `json:"capability_count"`
	Capabilities    []string       `json:"capabilities"`
	ProbeStatus     int            `json:"probe_status"`
	ProbeBody       string         `json:"probe_body"`
	Config          map[string]any `json:"config"`
}

func TestExecutableProviderAndRuntimePlugins(t *testing.T) {
	t.Parallel()
	bin := buildEchoPluginBinary(t)
	outputFile := filepath.Join(t.TempDir(), "runtime-output.json")

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echoext": {
				Plugin: &config.PluginDef{
					Command: bin,
					Args:    []string{"provider"},
				},
			},
		},
		Runtimes: map[string]config.RuntimeDef{
			"echoextrt": {
				Providers: []string{"echoext"},
				Config: mustNode(t, map[string]any{
					"output_file":     outputFile,
					"probe_provider":  "echoext",
					"probe_operation": "echo",
					"probe_params": map[string]any{
						"message": "from runtime",
					},
				}),
				Plugin: &config.PluginDef{
					Command: bin,
					Args:    []string{"runtime"},
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

	broker := invocation.NewBroker(providers, nil)
	runtimes, err := buildRuntimes(context.Background(), cfg, factories, broker, broker, core.AuditSink(invocation.NewSlogAuditSink(nil)), EgressDeps{})
	if err != nil {
		t.Fatalf("buildRuntimes: %v", err)
	}
	defer func() { _ = StopRuntimes(context.Background(), runtimes, runtimes.List()) }()

	rt, err := runtimes.Get("echoextrt")
	if err != nil {
		t.Fatalf("runtimes.Get: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("runtime.Start: %v", err)
	}

	got := readRuntimeOutput(t, outputFile)

	if got.Name != "echoextrt" {
		t.Fatalf("runtime output name = %q", got.Name)
	}
	if got.CapabilityCount != 4 {
		t.Fatalf("runtime output capability_count = %d", got.CapabilityCount)
	}
	if got.ProbeStatus != http.StatusOK {
		t.Fatalf("runtime output probe_status = %d", got.ProbeStatus)
	}
	if got.ProbeBody != `{"message":"from runtime"}` {
		t.Fatalf("runtime output probe_body = %q", got.ProbeBody)
	}
}

func TestExecutableRuntimeCapabilities_OmitsMCPPassthroughOnlyCatalogOperations(t *testing.T) {
	t.Parallel()
	t.Skip("requires old factory-based provider model")

	bin := buildEchoPluginBinary(t)
	outputFile := filepath.Join(t.TempDir(), "runtime-output.json")

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"search": {Plugin: &config.PluginDef{BaseURL: "https://api.test", Operations: []config.InlineOperationDef{{Name: "op", Method: "GET", Path: "/op"}}}},
		},
		Runtimes: map[string]config.RuntimeDef{
			"echoextrt": {
				Providers: []string{"search"},
				Config: mustNode(t, map[string]any{
					"output_file": outputFile,
				}),
				Plugin: &config.PluginDef{
					Command: bin,
					Args:    []string{"runtime"},
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

	broker := invocation.NewBroker(providers, nil)
	runtimes, err := buildRuntimes(context.Background(), cfg, factories, broker, broker, core.AuditSink(invocation.NewSlogAuditSink(nil)), EgressDeps{})
	if err != nil {
		t.Fatalf("buildRuntimes: %v", err)
	}
	defer func() { _ = StopRuntimes(context.Background(), runtimes, runtimes.List()) }()

	rt, err := runtimes.Get("echoextrt")
	if err != nil {
		t.Fatalf("runtimes.Get: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("runtime.Start: %v", err)
	}

	got := readRuntimeOutput(t, outputFile)
	if got.CapabilityCount != 1 {
		t.Fatalf("runtime output capability_count = %d, want 1", got.CapabilityCount)
	}
	if !slices.Equal(got.Capabilities, []string{"search.search_workspace"}) {
		t.Fatalf("runtime output capabilities = %v, want [search.search_workspace]", got.Capabilities)
	}
}

func TestExecutableRuntimeCapabilities_ExposeOnlyInvokableCatalogOperations(t *testing.T) {
	t.Parallel()
	t.Skip("requires old factory-based provider model")

	bin := buildEchoPluginBinary(t)
	outputFile := filepath.Join(t.TempDir(), "runtime-output.json")

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"workspace": {Plugin: &config.PluginDef{BaseURL: "https://api.test", Operations: []config.InlineOperationDef{{Name: "op", Method: "GET", Path: "/op"}}}},
		},
		Runtimes: map[string]config.RuntimeDef{
			"echoextrt": {
				Providers: []string{"workspace"},
				Config: mustNode(t, map[string]any{
					"output_file":     outputFile,
					"probe_provider":  "workspace",
					"probe_operation": "fetch_record",
					"probe_params": map[string]any{
						"id": "abc123",
					},
				}),
				Plugin: &config.PluginDef{
					Command: bin,
					Args:    []string{"runtime"},
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

	broker := invocation.NewBroker(providers, nil)
	runtimes, err := buildRuntimes(context.Background(), cfg, factories, broker, broker, core.AuditSink(invocation.NewSlogAuditSink(nil)), EgressDeps{})
	if err != nil {
		t.Fatalf("buildRuntimes: %v", err)
	}
	defer func() { _ = StopRuntimes(context.Background(), runtimes, runtimes.List()) }()

	rt, err := runtimes.Get("echoextrt")
	if err != nil {
		t.Fatalf("runtimes.Get: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("runtime.Start: %v", err)
	}

	got := readRuntimeOutput(t, outputFile)
	if got.CapabilityCount != 2 {
		t.Fatalf("runtime output capability_count = %d, want 2", got.CapabilityCount)
	}
	if !slices.Equal(got.Capabilities, []string{"workspace.fetch_record", "workspace.search_workspace"}) {
		t.Fatalf("runtime output capabilities = %v, want [workspace.fetch_record workspace.search_workspace]", got.Capabilities)
	}
	if got.ProbeStatus != http.StatusOK {
		t.Fatalf("runtime output probe_status = %d, want %d", got.ProbeStatus, http.StatusOK)
	}
	if got.ProbeBody != `{"operation":"fetch_record","params":{"id":"abc123"}}` {
		t.Fatalf("runtime output probe_body = %q", got.ProbeBody)
	}
}

func TestExecutableRuntimeCapabilities_FilterMethodlessFallbackOperations(t *testing.T) {
	t.Parallel()
	t.Skip("requires old factory-based provider model")

	bin := buildEchoPluginBinary(t)
	outputFile := filepath.Join(t.TempDir(), "runtime-output.json")

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"alpha": {Plugin: &config.PluginDef{BaseURL: "https://api.test", Operations: []config.InlineOperationDef{{Name: "op", Method: "GET", Path: "/op"}}}},
		},
		Runtimes: map[string]config.RuntimeDef{
			"echoextrt": {
				Providers: []string{"alpha"},
				Config: mustNode(t, map[string]any{
					"output_file": outputFile,
				}),
				Plugin: &config.PluginDef{
					Command: bin,
					Args:    []string{"runtime"},
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

	broker := invocation.NewBroker(providers, nil)
	runtimes, err := buildRuntimes(context.Background(), cfg, factories, broker, broker, core.AuditSink(invocation.NewSlogAuditSink(nil)), EgressDeps{})
	if err != nil {
		t.Fatalf("buildRuntimes: %v", err)
	}
	defer func() { _ = StopRuntimes(context.Background(), runtimes, runtimes.List()) }()

	rt, err := runtimes.Get("echoextrt")
	if err != nil {
		t.Fatalf("runtimes.Get: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("runtime.Start: %v", err)
	}

	got := readRuntimeOutput(t, outputFile)
	if got.CapabilityCount != 2 {
		t.Fatalf("runtime output capability_count = %d, want 2", got.CapabilityCount)
	}
	if !slices.Equal(got.Capabilities, []string{"alpha.rest_op", "alpha.hidden_op"}) {
		t.Fatalf("runtime output capabilities = %v, want [alpha.rest_op alpha.hidden_op]", got.Capabilities)
	}
}

func TestExecutableRuntimeReceivesPluginConfig(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	outputFile := filepath.Join(t.TempDir(), "runtime-output.json")

	cfg := &config.Config{
		Runtimes: map[string]config.RuntimeDef{
			"echoextrt": {
				Config: mustNode(t, map[string]any{
					"output_file":  outputFile,
					"plugin_only":  "from runtime config",
					"runtime_only": "from runtime config",
				}),
				Plugin: &config.PluginDef{
					Command: bin,
					Args:    []string{"runtime"},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	broker := invocation.NewBroker(nil, nil)
	runtimes, err := buildRuntimes(context.Background(), cfg, factories, broker, broker, core.AuditSink(invocation.NewSlogAuditSink(nil)), EgressDeps{})
	if err != nil {
		t.Fatalf("buildRuntimes: %v", err)
	}
	defer func() { _ = StopRuntimes(context.Background(), runtimes, runtimes.List()) }()

	rt, err := runtimes.Get("echoextrt")
	if err != nil {
		t.Fatalf("runtimes.Get: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("runtime.Start: %v", err)
	}

	got := readRuntimeOutput(t, outputFile)
	if got.Config["runtime_only"] != "from runtime config" {
		t.Fatalf("runtime config missing runtime_only: %+v", got.Config)
	}
	if got.Config["plugin_only"] != "from runtime config" {
		t.Fatalf("runtime config missing plugin_only: %+v", got.Config)
	}
}

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

	bin := filepath.Join(t.TempDir(), "gestalt-plugin-echo")
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./internal/testplugins/echo")
	cmd.Dir = filepath.Join(root, "server")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build plugin binary: %v\n%s", err, out)
	}
	return bin
}

func buildExampleProviderBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "provider-go")
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = filepath.Join(root, "examples", "plugins", "provider-go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build example provider: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func mustNode(t *testing.T, value any) yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		t.Fatalf("node.Encode: %v", err)
	}
	return node
}

func readRuntimeOutput(t *testing.T, path string) runtimeOutput {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}

	var got runtimeOutput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return got
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
	manifestData, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "plugin.json")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
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
					ResolvedManifestPath: manifestPath,
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
	if len(prov.ListOperations()) == 0 {
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
	manifestData, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "plugin.json")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echonoauth": {
				Plugin: &config.PluginDef{
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifestPath: manifestPath,
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

func TestDeclarativeProvider_ConfigHeadersOverrideManifestHeadersCaseInsensitively(t *testing.T) {
	t.Parallel()

	const (
		headerName    = "X-Static-Version"
		manifestValue = "from-manifest"
		configValue   = "from-config"
	)

	gotHeader := make(chan string, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader <- r.Header.Get(headerName)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
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

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"sample": {
				Plugin: &config.PluginDef{
					IsDeclarative:        true,
					ResolvedManifestPath: manifestPath,
					Headers: map[string]string{
						headerName: configValue,
					},
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

	prov, err := providers.Get("sample")
	if err != nil {
		t.Fatalf("providers.Get(sample): %v", err)
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
		if got != configValue {
			t.Fatalf("%s = %q, want %q", headerName, got, configValue)
		}
	default:
		t.Fatal("expected upstream request")
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

	ops := prov.ListOperations()
	opNames := make(map[string]bool, len(ops))
	for _, op := range ops {
		opNames[op.Name] = true
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

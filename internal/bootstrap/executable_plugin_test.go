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
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
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

type catalogProviderWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
	cat *catalog.Catalog
}

func (p *catalogProviderWithOps) ListOperations() []core.Operation {
	return slices.Clone(p.ops)
}

func (p *catalogProviderWithOps) Catalog() *catalog.Catalog {
	if p.cat == nil {
		return nil
	}
	return p.cat.Clone()
}

func TestExecutableProviderAndRuntimePlugins(t *testing.T) {
	t.Parallel()
	bin := buildEchoPluginBinary(t)
	outputFile := filepath.Join(t.TempDir(), "runtime-output.json")

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echoext": {
				Plugin: &config.ExecutablePluginDef{
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
				Plugin: &config.ExecutablePluginDef{
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
	runtimes, err := buildRuntimes(context.Background(), cfg, factories, broker, broker, core.AuditSink(invocation.LogAuditSink{}), EgressDeps{})
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
	if got.CapabilityCount != 3 {
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

	bin := buildEchoPluginBinary(t)
	outputFile := filepath.Join(t.TempDir(), "runtime-output.json")

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"search": {},
		},
		Runtimes: map[string]config.RuntimeDef{
			"echoextrt": {
				Providers: []string{"search"},
				Config: mustNode(t, map[string]any{
					"output_file": outputFile,
				}),
				Plugin: &config.ExecutablePluginDef{
					Command: bin,
					Args:    []string{"runtime"},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	factories.Providers["search"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ Deps) (*ProviderBuildResult, error) {
		return &ProviderBuildResult{Provider: &catalogProviderWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "search",
				ConnMode: core.ConnectionModeNone,
			},
			ops: []core.Operation{
				{Name: "search_workspace", Description: "Search workspace", Method: http.MethodPost},
			},
			cat: &catalog.Catalog{
				Name: "search",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "search_workspace",
						Description: "Search workspace",
						Transport:   catalog.TransportMCPPassthrough,
					},
				},
			},
		}}, nil
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	broker := invocation.NewBroker(providers, nil)
	runtimes, err := buildRuntimes(context.Background(), cfg, factories, broker, broker, core.AuditSink(invocation.LogAuditSink{}), EgressDeps{})
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

	bin := buildEchoPluginBinary(t)
	outputFile := filepath.Join(t.TempDir(), "runtime-output.json")

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"workspace": {},
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
				Plugin: &config.ExecutablePluginDef{
					Command: bin,
					Args:    []string{"runtime"},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	factories.Providers["workspace"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ Deps) (*ProviderBuildResult, error) {
		return &ProviderBuildResult{Provider: &catalogProviderWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "workspace",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(_ context.Context, operation string, params map[string]any, _ string) (*core.OperationResult, error) {
					body, err := json.Marshal(map[string]any{
						"operation": operation,
						"params":    params,
					})
					if err != nil {
						return nil, err
					}
					return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
				},
			},
			ops: []core.Operation{
				{Name: "fetch_record", Description: "Fetch record", Method: http.MethodGet},
				{Name: "search_workspace", Description: "Search workspace", Method: http.MethodPost},
			},
			cat: &catalog.Catalog{
				Name: "workspace",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "fetch_record",
						Description: "Fetch record",
						Method:      http.MethodGet,
						Path:        "/records/{id}",
						Transport:   catalog.TransportREST,
					},
					{
						ID:          "search_workspace",
						Description: "Search workspace",
						Transport:   catalog.TransportMCPPassthrough,
					},
				},
			},
		}}, nil
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	broker := invocation.NewBroker(providers, nil)
	runtimes, err := buildRuntimes(context.Background(), cfg, factories, broker, broker, core.AuditSink(invocation.LogAuditSink{}), EgressDeps{})
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

	bin := buildEchoPluginBinary(t)
	outputFile := filepath.Join(t.TempDir(), "runtime-output.json")

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"alpha": {},
		},
		Runtimes: map[string]config.RuntimeDef{
			"echoextrt": {
				Providers: []string{"alpha"},
				Config: mustNode(t, map[string]any{
					"output_file": outputFile,
				}),
				Plugin: &config.ExecutablePluginDef{
					Command: bin,
					Args:    []string{"runtime"},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	factories.Providers["alpha"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ Deps) (*ProviderBuildResult, error) {
		return &ProviderBuildResult{Provider: &catalogProviderWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "alpha",
				ConnMode: core.ConnectionModeNone,
			},
			ops: []core.Operation{
				{Name: "rest_op", Description: "REST op", Method: http.MethodPost},
				{Name: "hidden_op", Description: "Hidden op"},
			},
		}}, nil
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	broker := invocation.NewBroker(providers, nil)
	runtimes, err := buildRuntimes(context.Background(), cfg, factories, broker, broker, core.AuditSink(invocation.LogAuditSink{}), EgressDeps{})
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
				Plugin: &config.ExecutablePluginDef{
					Command: bin,
					Args:    []string{"runtime"},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	broker := invocation.NewBroker(nil, nil)
	runtimes, err := buildRuntimes(context.Background(), cfg, factories, broker, broker, core.AuditSink(invocation.LogAuditSink{}), EgressDeps{})
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
				Plugin: &config.ExecutablePluginDef{
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

func TestExecutableProviderAcceptsMinOnlyProtocolVersion(t *testing.T) {
	t.Parallel()

	bin := buildMinProtocolProviderBinary(t)
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"minproto": {
				Plugin: &config.ExecutablePluginDef{
					Command: bin,
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

	prov, err := providers.Get("minproto")
	if err != nil {
		t.Fatalf("providers.Get(minproto): %v", err)
	}
	if prov.Name() != "minproto" {
		t.Fatalf("provider name = %q", prov.Name())
	}
	if len(prov.ListOperations()) != 1 || prov.ListOperations()[0].Name != "ping" {
		t.Fatalf("operations = %+v", prov.ListOperations())
	}
}

func buildEchoPluginBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "gestalt-plugin-echo")
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./internal/testplugins/echo")
	cmd.Dir = root
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

func buildMinProtocolProviderBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "minproto-provider")
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./internal/testplugins/min_protocol_provider")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build min protocol provider: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
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
		SchemaVersion: pluginmanifestv1.SchemaVersion2,
		Source:        "github.com/acme/plugins/echo",
		Version:       "1.0.0",
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
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
				Plugin: &config.ExecutablePluginDef{
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
		SchemaVersion: pluginmanifestv1.SchemaVersion2,
		Source:        "github.com/acme/plugins/echo",
		Version:       "1.0.0",
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
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
			"echonoauth": {
				Plugin: &config.ExecutablePluginDef{
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

func TestPluginProcessEnvIsolation(t *testing.T) {
	t.Parallel()
	bin := buildEchoPluginBinary(t)

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echoext": {
				Plugin: &config.ExecutablePluginDef{
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

func TestPluginProcessReceivesProviderHostSocket(t *testing.T) {
	t.Parallel()
	bin := buildEchoPluginBinary(t)

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echoext": {
				Plugin: &config.ExecutablePluginDef{
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

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": "GESTALT_PROVIDER_HOST_SOCKET"}, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatal("plugin process should receive GESTALT_PROVIDER_HOST_SOCKET")
	}
}

func TestPluginProxyHTTPRejectsHTTP(t *testing.T) {
	t.Parallel()
	bin := buildEchoPluginBinary(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echoext": {
				Plugin: &config.ExecutablePluginDef{
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

	_, err = prov.Execute(context.Background(), "proxy_fetch", map[string]any{
		"url": upstream.URL + "/test",
	}, "test-token")
	if err == nil {
		t.Fatal("expected ProxyHTTP to reject http:// URL")
	}
	if !strings.Contains(err.Error(), "only https is permitted") {
		t.Fatalf("expected https rejection, got: %v", err)
	}
}

func TestPluginProxyHTTPBlocksPrivateIPs(t *testing.T) {
	t.Parallel()
	bin := buildEchoPluginBinary(t)

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"echoext": {
				Plugin: &config.ExecutablePluginDef{
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

	_, err = prov.Execute(context.Background(), "proxy_fetch", map[string]any{
		"url": upstream.URL + "/data",
	}, "test-token")
	if err == nil {
		t.Fatal("expected ProxyHTTP to reject loopback IP")
	}
	if !strings.Contains(err.Error(), "private/reserved") {
		t.Fatalf("expected private IP rejection, got: %v", err)
	}
}

package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"gopkg.in/yaml.v3"
)

type runtimeOutput struct {
	Name            string   `json:"name"`
	CapabilityCount int      `json:"capability_count"`
	Capabilities    []string `json:"capabilities"`
	ProbeStatus     int      `json:"probe_status"`
	ProbeBody       string   `json:"probe_body"`
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
				Plugin: &config.ExecutablePluginDef{
					Command: bin,
					Args:    []string{"runtime"},
				},
				Config: mustNode(t, map[string]any{
					"output_file":     outputFile,
					"probe_provider":  "echoext",
					"probe_operation": "echo",
					"probe_params": map[string]any{
						"message": "from runtime",
					},
				}),
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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
	if got.CapabilityCount != 1 {
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
				Plugin: &config.ExecutablePluginDef{
					Command: bin,
					Args:    []string{"runtime"},
				},
				Config: mustNode(t, map[string]any{
					"output_file": outputFile,
				}),
			},
		},
	}

	factories := NewFactoryRegistry()
	factories.Providers["search"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ Deps) (core.Provider, error) {
		return &catalogProviderWithOps{
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
		}, nil
	}

	providers, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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
				Plugin: &config.ExecutablePluginDef{
					Command: bin,
					Args:    []string{"runtime"},
				},
				Config: mustNode(t, map[string]any{
					"output_file":     outputFile,
					"probe_provider":  "workspace",
					"probe_operation": "fetch_record",
					"probe_params": map[string]any{
						"id": "abc123",
					},
				}),
			},
		},
	}

	factories := NewFactoryRegistry()
	factories.Providers["workspace"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ Deps) (core.Provider, error) {
		return &catalogProviderWithOps{
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
						Transport:   catalog.TransportHTTP,
					},
					{
						ID:          "search_workspace",
						Description: "Search workspace",
						Transport:   catalog.TransportMCPPassthrough,
					},
				},
			},
		}, nil
	}

	providers, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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
				Plugin: &config.ExecutablePluginDef{
					Command: bin,
					Args:    []string{"runtime"},
				},
				Config: mustNode(t, map[string]any{
					"output_file": outputFile,
				}),
			},
		},
	}

	factories := NewFactoryRegistry()
	factories.Providers["alpha"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ Deps) (core.Provider, error) {
		return &catalogProviderWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "alpha",
				ConnMode: core.ConnectionModeNone,
			},
			ops: []core.Operation{
				{Name: "http_op", Description: "HTTP op", Method: http.MethodPost},
				{Name: "hidden_op", Description: "Hidden op"},
			},
		}, nil
	}

	providers, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
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
	if !slices.Equal(got.Capabilities, []string{"alpha.http_op", "alpha.hidden_op"}) {
		t.Fatalf("runtime output capabilities = %v, want [alpha.http_op alpha.hidden_op]", got.Capabilities)
	}
}

func buildEchoPluginBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "gestalt-plugin-echo")
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/gestalt-plugin-echo")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build plugin binary: %v\n%s", err, out)
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

package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"gopkg.in/yaml.v3"
)

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

	var got struct {
		Name            string `json:"name"`
		CapabilityCount int    `json:"capability_count"`
		ProbeStatus     int    `json:"probe_status"`
		ProbeBody       string `json:"probe_body"`
	}
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", outputFile, err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Name != "echoextrt" {
		t.Fatalf("runtime output name = %q", got.Name)
	}
	if got.CapabilityCount != 1 {
		t.Fatalf("runtime output capability_count = %d", got.CapabilityCount)
	}
	if got.ProbeStatus != 200 {
		t.Fatalf("runtime output probe_status = %d", got.ProbeStatus)
	}
	if got.ProbeBody != `{"message":"from runtime"}` {
		t.Fatalf("runtime output probe_body = %q", got.ProbeBody)
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

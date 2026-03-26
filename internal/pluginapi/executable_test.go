package pluginapi

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/principal"
)

type recordingInvoker struct {
	called    bool
	provider  string
	operation string
	params    map[string]any
	principal *principal.Principal
}

func (i *recordingInvoker) Invoke(_ context.Context, p *principal.Principal, providerName, _ string, operation string, params map[string]any) (*core.OperationResult, error) {
	i.called = true
	i.provider = providerName
	i.operation = operation
	i.params = params
	i.principal = p
	return &core.OperationResult{Status: 203, Body: `{"ok":true}`}, nil
}

type staticCapabilityLister struct {
	caps []core.Capability
}

func (l staticCapabilityLister) ListCapabilities() []core.Capability {
	return slices.Clone(l.caps)
}

func TestNewExecutableProviderRoundTrip(t *testing.T) {
	prov, err := NewExecutableProvider(context.Background(), ExecConfig{
		Command: buildExampleProviderBinary(t),
		Name:    "fixture-instance",
		Config:  map[string]any{"greeting": "Hello"},
		Mode:    "overlay",
	})
	if err != nil {
		t.Fatalf("NewExecutableProvider: %v", err)
	}
	t.Cleanup(func() {
		if closer, ok := prov.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})

	if prov.Name() != "example" || prov.DisplayName() != "Example Provider" {
		t.Fatalf("unexpected provider metadata: name=%q display=%q", prov.Name(), prov.DisplayName())
	}

	if got := sortedOperationNames(prov.ListOperations()); !slices.Equal(got, []string{"echo", "greet", "status"}) {
		t.Fatalf("operations = %v, want [echo greet status]", got)
	}

	statusResult, err := prov.Execute(context.Background(), "status", nil, "")
	if err != nil {
		t.Fatalf("Execute(status): %v", err)
	}
	var statusBody map[string]any
	if err := json.Unmarshal([]byte(statusResult.Body), &statusBody); err != nil {
		t.Fatalf("Unmarshal(status): %v", err)
	}
	if statusBody["name"] != "fixture-instance" || statusBody["mode"] != "overlay" || statusBody["greeting"] != "Hello" {
		t.Fatalf("unexpected status body: %+v", statusBody)
	}

	greetResult, err := prov.Execute(context.Background(), "greet", map[string]any{"name": "Reader"}, "")
	if err != nil {
		t.Fatalf("Execute(greet): %v", err)
	}
	var greetBody map[string]any
	if err := json.Unmarshal([]byte(greetResult.Body), &greetBody); err != nil {
		t.Fatalf("Unmarshal(greet): %v", err)
	}
	if greetBody["message"] != "Hello, Reader!" {
		t.Fatalf("unexpected greet body: %+v", greetBody)
	}
}

func TestNewExecutableRuntimeRoundTrip(t *testing.T) {
	outputFile := filepath.Join(t.TempDir(), "runtime-output.json")
	invoker := &recordingInvoker{}
	lister := staticCapabilityLister{
		caps: []core.Capability{
			{Provider: "utility", Operation: "inspect"},
			{Provider: "utility", Operation: "probe"},
		},
	}

	rt, err := NewExecutableRuntime(context.Background(), "fixture-runtime", ExecConfig{
		Command: buildEchoPluginBinary(t),
		Args:    []string{"runtime"},
		Config: map[string]any{
			"output_file":     outputFile,
			"probe_provider":  "utility",
			"probe_operation": "probe",
			"probe_params": map[string]any{
				"message": "hello",
			},
		},
	}, invoker, lister)
	if err != nil {
		t.Fatalf("NewExecutableRuntime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop(context.Background()) })

	if rt.Name() != "fixture-runtime" {
		t.Fatalf("unexpected runtime name: %q", rt.Name())
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	record := readJSONFile(t, outputFile)
	if record["name"] != "fixture-runtime" || record["capability_count"] != float64(2) {
		t.Fatalf("unexpected runtime record: %+v", record)
	}
	if record["probe_status"] != float64(203) || record["probe_body"] != `{"ok":true}` {
		t.Fatalf("unexpected probe result: %+v", record)
	}
	caps, ok := record["capabilities"].([]any)
	if !ok || len(caps) != 2 {
		t.Fatalf("unexpected capabilities: %+v", record["capabilities"])
	}
	if caps[0] != "utility.inspect" || caps[1] != "utility.probe" {
		t.Fatalf("unexpected capabilities ordering: %+v", caps)
	}

	if !invoker.called || invoker.provider != "utility" || invoker.operation != "probe" {
		t.Fatalf("unexpected invoker call: %+v", invoker)
	}
	if invoker.params["message"] != "hello" {
		t.Fatalf("unexpected probe params: %+v", invoker.params)
	}

	if err := rt.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func sortedOperationNames(ops []core.Operation) []string {
	names := make([]string, 0, len(ops))
	for _, op := range ops {
		names = append(names, op.Name)
	}
	slices.Sort(names)
	return names
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

func buildEchoPluginBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "gestalt-plugin-echo")
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/gestalt-plugin-echo")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build echo plugin: %v\n%s", err, out)
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

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal(%s): %v", path, err)
	}
	return out
}

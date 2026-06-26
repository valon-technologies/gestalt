package providermanifestv1_test

import (
	"encoding/json"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestSourceRunJSONRoundTrip(t *testing.T) {
	t.Parallel()

	raw := `{"command":["bun","run","dev"],"workdir":"ui","env":{"FOO":"bar"}}`
	var run providermanifestv1.SourceRun
	if err := json.Unmarshal([]byte(raw), &run); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(run.Command) != 3 || run.Command[0] != "bun" || run.Command[1] != "run" || run.Command[2] != "dev" {
		t.Fatalf("command = %#v", run.Command)
	}
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var roundTrip providermanifestv1.SourceRun
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("round-trip Unmarshal: %v", err)
	}
	if roundTrip.Workdir != "ui" || roundTrip.Env["FOO"] != "bar" {
		t.Fatalf("roundTrip = %#v", roundTrip)
	}
}

func TestSourceRunJSONRejectsSequenceForm(t *testing.T) {
	t.Parallel()

	var run providermanifestv1.SourceRun
	if err := json.Unmarshal([]byte(`["bun","run","dev"]`), &run); err == nil {
		t.Fatal("want sequence-form rejection")
	}
}

func TestSourceRunJSONAcceptsReadyTimeout(t *testing.T) {
	t.Parallel()

	var run providermanifestv1.SourceRun
	if err := json.Unmarshal([]byte(`{"command":["bun","run","dev"],"readyTimeout":"30s"}`), &run); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if run.ReadyTimeout != "30s" {
		t.Fatalf("readyTimeout = %q, want 30s", run.ReadyTimeout)
	}
}

func TestSourceRunJSONRequiresCommand(t *testing.T) {
	t.Parallel()

	var run providermanifestv1.SourceRun
	if err := json.Unmarshal([]byte(`{"workdir":"ui"}`), &run); err == nil {
		t.Fatal("want missing command rejection")
	}
}

func TestSourceRunYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	raw := `
command:
  - bun
  - run
  - dev
workdir: ui
env:
  FOO: bar
`
	var run providermanifestv1.SourceRun
	if err := yaml.Unmarshal([]byte(raw), &run); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	encoded, err := yaml.Marshal(run)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var roundTrip providermanifestv1.SourceRun
	if err := yaml.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("round-trip Unmarshal: %v", err)
	}
	if roundTrip.Workdir != "ui" || roundTrip.Env["FOO"] != "bar" {
		t.Fatalf("roundTrip = %#v", roundTrip)
	}
}

func TestSourceRunYAMLRejectsSequenceForm(t *testing.T) {
	t.Parallel()

	var run providermanifestv1.SourceRun
	if err := yaml.Unmarshal([]byte(`[bun, run, dev]`), &run); err == nil {
		t.Fatal("want sequence-form rejection")
	}
}

func TestSourceRunYAMLAcceptsReadyTimeout(t *testing.T) {
	t.Parallel()

	var run providermanifestv1.SourceRun
	if err := yaml.Unmarshal([]byte(`
command: [bun, run, dev]
readyTimeout: 30s
`), &run); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if run.ReadyTimeout != "30s" {
		t.Fatalf("readyTimeout = %q, want 30s", run.ReadyTimeout)
	}
}

func TestSourceRunJSONRejectsUnknownField(t *testing.T) {
	t.Parallel()

	var run providermanifestv1.SourceRun
	if err := json.Unmarshal([]byte(`{"command":["bun","run","dev"],"unexpected":"value"}`), &run); err == nil {
		t.Fatal("want unknown field rejection")
	}
}

func TestSourceRunYAMLRejectsUnknownField(t *testing.T) {
	t.Parallel()

	var run providermanifestv1.SourceRun
	if err := yaml.Unmarshal([]byte(`
command: [bun, run, dev]
unexpected: value
`), &run); err == nil {
		t.Fatal("want unknown field rejection")
	}
}

func TestSourceRunYAMLRequiresCommand(t *testing.T) {
	t.Parallel()

	var run providermanifestv1.SourceRun
	if err := yaml.Unmarshal([]byte(`workdir: ui`), &run); err == nil {
		t.Fatal("want missing command rejection")
	}
}

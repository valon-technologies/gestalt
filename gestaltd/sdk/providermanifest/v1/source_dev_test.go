package providermanifestv1_test

import (
	"encoding/json"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestSourceDevJSONRoundTrip(t *testing.T) {
	t.Parallel()

	raw := `{"command":["npm","run","dev"],"workdir":"ui","readyTimeout":"30s","env":{"FOO":"bar"}}`
	var dev providermanifestv1.SourceDev
	if err := json.Unmarshal([]byte(raw), &dev); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	out, err := json.Marshal(dev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var roundTrip providermanifestv1.SourceDev
	if err := json.Unmarshal(out, &roundTrip); err != nil {
		t.Fatalf("round-trip Unmarshal: %v", err)
	}
	if len(roundTrip.Command) != 3 || roundTrip.Workdir != "ui" || roundTrip.ReadyTimeout != "30s" || roundTrip.Env["FOO"] != "bar" {
		t.Fatalf("round-trip dev = %#v", roundTrip)
	}
}

func TestSourceDevJSONRejectsUnknownField(t *testing.T) {
	t.Parallel()

	var dev providermanifestv1.SourceDev
	if err := json.Unmarshal([]byte(`{"command":["npm"],"extra":true}`), &dev); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestSourceDevJSONRequiresCommand(t *testing.T) {
	t.Parallel()

	var dev providermanifestv1.SourceDev
	if err := json.Unmarshal([]byte(`{"workdir":"."}`), &dev); err == nil {
		t.Fatal("expected missing command error")
	}
}

func TestSourceDevYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	raw := `
command:
  - npm
  - run
  - dev
workdir: ui
readyTimeout: 30s
env:
  FOO: bar
`
	var dev providermanifestv1.SourceDev
	if err := yaml.Unmarshal([]byte(raw), &dev); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(dev.Command) != 3 || dev.Workdir != "ui" || dev.ReadyTimeout != "30s" || dev.Env["FOO"] != "bar" {
		t.Fatalf("decoded dev = %#v", dev)
	}
}

func TestSourceDevYAMLRejectsUnknownField(t *testing.T) {
	t.Parallel()

	var node yaml.Node
	if err := yaml.Unmarshal([]byte("command: [npm]\nextra: true\n"), &node); err != nil {
		t.Fatalf("Unmarshal node: %v", err)
	}
	var dev providermanifestv1.SourceDev
	if err := dev.UnmarshalYAML(&node); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestSourceDevYAMLRequiresCommand(t *testing.T) {
	t.Parallel()

	var node yaml.Node
	if err := yaml.Unmarshal([]byte("workdir: .\n"), &node); err != nil {
		t.Fatalf("Unmarshal node: %v", err)
	}
	var dev providermanifestv1.SourceDev
	if err := dev.UnmarshalYAML(&node); err == nil {
		t.Fatal("expected missing command error")
	}
}

package providermanifestv1_test

import (
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestSourceRunParsesRoleUI(t *testing.T) {
	t.Parallel()

	const manifest = `
kind: app
source: github.com/acme/apps/demo
version: 0.0.1
run:
  - command: [uv, run, provider.py, --serve]
  - command: [bun, run, dev]
    workdir: ui
    role: ui
spec:
  connections:
    default:
      auth:
        type: none
`
	var got providermanifestv1.Manifest
	if err := yaml.Unmarshal([]byte(manifest), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	commands := got.Run.PhaseCommands()
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(commands))
	}
	if commands[0].Role != "" {
		t.Fatalf("provider role = %q, want empty", commands[0].Role)
	}
	if commands[1].Role != providermanifestv1.SourceRunRoleUI {
		t.Fatalf("ui role = %q, want %q", commands[1].Role, providermanifestv1.SourceRunRoleUI)
	}
}

func TestSourceRunRejectsUnsupportedRole(t *testing.T) {
	t.Parallel()

	const manifest = `
kind: app
source: github.com/acme/apps/demo
version: 0.0.1
run:
  - command: [bun, run, dev]
    role: backend
spec: {}
`
	var got providermanifestv1.Manifest
	err := yaml.Unmarshal([]byte(manifest), &got)
	if err == nil || !strings.Contains(err.Error(), `role "backend" is not supported`) {
		t.Fatalf("Unmarshal error = %v, want unsupported role", err)
	}
}

func TestSourceRunRoundTripsRoleUI(t *testing.T) {
	t.Parallel()

	original := `
kind: app
source: github.com/acme/apps/demo
version: 0.0.1
run:
  - command: [uv, run, provider.py, --serve]
  - command: [bun, run, dev]
    role: ui
spec: {}
`
	var manifest providermanifestv1.Manifest
	if err := yaml.Unmarshal([]byte(original), &manifest); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	encoded, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var roundTripped providermanifestv1.Manifest
	if err := yaml.Unmarshal(encoded, &roundTripped); err != nil {
		t.Fatalf("round trip: %v\n%s", err, encoded)
	}
	commands := roundTripped.Run.PhaseCommands()
	if len(commands) != 2 || commands[1].Role != providermanifestv1.SourceRunRoleUI {
		t.Fatalf("roundTripped commands = %#v", commands)
	}
}

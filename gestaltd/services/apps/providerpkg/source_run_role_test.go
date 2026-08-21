package providerpkg

import (
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestSourceUIRunCommandsAndRemotePreviewValidation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeManifest := func(name, body string) string {
		t.Helper()
		return mustWriteManifestData(t, dir, name, []byte(body))
	}

	t.Run("returns ui commands", func(t *testing.T) {
		t.Parallel()
		manifestPath := writeManifest("ui-only.yaml", `
kind: app
source: github.com/acme/apps/demo
version: 0.0.1
run:
  - command: [uv, run, provider.py, --serve]
  - command: [bun, run, dev]
    role: ui
spec: {}
`)
		ui, err := SourceUIRunCommands(manifestPath)
		if err != nil {
			t.Fatalf("SourceUIRunCommands: %v", err)
		}
		if len(ui) != 1 || ui[0].Command[0] != "bun" {
			t.Fatalf("ui commands = %#v", ui)
		}
	})

	t.Run("validate accepts exactly one ui command", func(t *testing.T) {
		t.Parallel()
		manifestPath := writeManifest("valid.yaml", `
kind: app
source: github.com/acme/apps/demo
version: 0.0.1
run:
  - command: [uv, run, provider.py, --serve]
  - command: [bun, run, dev]
    role: ui
spec: {}
`)
		if err := ValidateRemotePreviewUIRunTarget(manifestPath); err != nil {
			t.Fatalf("ValidateRemotePreviewUIRunTarget: %v", err)
		}
	})

	t.Run("validate rejects missing ui command", func(t *testing.T) {
		t.Parallel()
		manifestPath := writeManifest("missing-ui.yaml", `
kind: app
source: github.com/acme/apps/demo
version: 0.0.1
run:
  command: [uv, run, provider.py, --serve]
spec: {}
`)
		err := ValidateRemotePreviewUIRunTarget(manifestPath)
		if err == nil || !strings.Contains(err.Error(), "role: ui") {
			t.Fatalf("error = %v, want missing ui role", err)
		}
	})

	t.Run("validate rejects multiple ui commands", func(t *testing.T) {
		t.Parallel()
		manifestPath := writeManifest("multi-ui.yaml", `
kind: app
source: github.com/acme/apps/demo
version: 0.0.1
run:
  - command: [bun, run, dev]
    role: ui
  - command: [npm, run, dev]
    role: ui
spec: {}
`)
		err := ValidateRemotePreviewUIRunTarget(manifestPath)
		if err == nil || !strings.Contains(err.Error(), errRemotePreviewUICommandCount) {
			t.Fatalf("error = %v, want multiple ui rejection", err)
		}
	})
}

func TestResolvedCommandPreservesRole(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/acme/apps/demo",
		Version: "0.0.1",
		Run: &providermanifestv1.SourceRun{
			Commands: []providermanifestv1.SourcePhaseCommand{
				{Command: []string{"bun", "run", "dev"}, Role: providermanifestv1.SourceRunRoleUI},
			},
		},
	}
	run := EffectiveSourceRun(manifest)
	if run == nil || len(run.Commands) != 1 || run.Commands[0].Role != providermanifestv1.SourceRunRoleUI {
		t.Fatalf("run = %#v", run)
	}
}

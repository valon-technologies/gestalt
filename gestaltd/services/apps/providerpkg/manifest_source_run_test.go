package providerpkg

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestSourceManifestPrepareBuildAndRunCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: app
source: github.com/acme/apps/uv-source
version: 0.0.1-alpha.1
build: [uv, sync, --frozen, --no-install-project]
run:
  command: [uv, run, --frozen, provider.py, --serve]
spec:
  connections:
    default:
      auth:
        type: none
`))

	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile: %v", err)
	}
	if manifest.Build == nil || !manifest.Build.PrepareOnly {
		t.Fatalf("build = %#v, want prepare-only build", manifest.Build)
	}
	if got := strings.Join(manifest.Build.Command, " "); got != "uv sync --frozen --no-install-project" {
		t.Fatalf("build.command = %q", got)
	}
	if got := strings.Join(manifest.Run.Command, " "); got != "uv run --frozen provider.py --serve" {
		t.Fatalf("run.command = %q", got)
	}

	cloned, err := cloneManifest(manifest)
	if err != nil {
		t.Fatalf("cloneManifest: %v", err)
	}
	if cloned.Build == nil || !cloned.Build.PrepareOnly {
		t.Fatalf("cloned build = %#v, want prepare-only build", cloned.Build)
	}

	encoded, err := EncodeSourceManifestFormat(cloned, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	roundTripped, err := DecodeSourceManifestFormat(encoded, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("DecodeSourceManifestFormat(round trip): %v\n%s", err, encoded)
	}
	if roundTripped.Build == nil || !roundTripped.Build.PrepareOnly {
		t.Fatalf("round-tripped build = %#v, want prepare-only build", roundTripped.Build)
	}

	if _, _, err := ReadManifestFile(manifestPath); err == nil || !strings.Contains(err.Error(), "build metadata is only allowed in source manifests") {
		t.Fatalf("ReadManifestFile error = %v, want package build rejection", err)
	}
}

func TestSourceManifestParsesInstallPhase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: app
source: github.com/acme/apps/install-phase
version: 0.0.1-alpha.1
install:
  command: [uv, sync, --frozen]
  inputs: [uv.lock]
build:
  command: [uv, run, python, -m, gestalt._build]
  inputs: [provider.py]
spec:
  connections:
    default:
      auth:
        type: none
`))

	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile: %v", err)
	}
	if manifest.Install == nil {
		t.Fatalf("install = nil, want parsed install phase")
	}
	if got := strings.Join(manifest.Install.Command, " "); got != "uv sync --frozen" {
		t.Fatalf("install.command = %q, want %q", got, "uv sync --frozen")
	}
	if len(manifest.Install.Inputs) != 1 || manifest.Install.Inputs[0] != "uv.lock" {
		t.Fatalf("install.inputs = %#v, want [uv.lock]", manifest.Install.Inputs)
	}

	cloned, err := cloneManifest(manifest)
	if err != nil {
		t.Fatalf("cloneManifest: %v", err)
	}
	encoded, err := EncodeSourceManifestFormat(cloned, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	roundTripped, err := DecodeSourceManifestFormat(encoded, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("DecodeSourceManifestFormat(round trip): %v\n%s", err, encoded)
	}
	if roundTripped.Install == nil || strings.Join(roundTripped.Install.Command, " ") != "uv sync --frozen" {
		t.Fatalf("round-tripped install = %#v", roundTripped.Install)
	}

	if _, _, err := ReadManifestFile(manifestPath); err == nil || !strings.Contains(err.Error(), "is only allowed in source manifests") {
		t.Fatalf("ReadManifestFile error = %v, want source-only rejection", err)
	}
}

func TestSourceManifestRejectsSequenceFormInstall(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: app
source: github.com/acme/apps/sequence-install
version: 0.0.1-alpha.1
install: [uv, sync, --frozen]
build:
  command: [uv, run, python, -m, gestalt._build]
spec:
  connections:
    default:
      auth:
        type: none
`))

	_, _, err := ReadSourceManifestFile(manifestPath)
	if err == nil || !strings.Contains(err.Error(), "install[0] must be a sequence or mapping") {
		t.Fatalf("ReadSourceManifestFile error = %v, want install sequence rejection", err)
	}
}

func TestPrepareSourceManifest_RunsInstallBeforeRunCatalog(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsOS {
		t.Skip("install marker fixture uses POSIX shell")
	}

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "install.sh"), []byte("#!/bin/sh\nset -eu\nprintf 'ok\n' > install-ran.txt\n"), 0o755)
	mustWriteFile(t, filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\nset -eu\ntest -f install-ran.txt\nif [ -n \"${GESTALT_APP_WRITE_CATALOG:-}\" ]; then\n  printf 'name: provider\\noperations:\\n  - id: echo\\n    method: POST\\n' > \"$GESTALT_APP_WRITE_CATALOG\"\nfi\n"), 0o755)
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/acme/apps/run-only-install",
		Version: "0.0.1-alpha.1",
		Install: &providermanifestv1.SourceInstall{
			Command: []string{"sh", "./install.sh"},
			Inputs:  []string{"install.sh"},
		},
		Run:  sourceRunCommand("sh", "./run.sh"),
		Spec: &providermanifestv1.Spec{},
	}))

	if _, _, err := PrepareSourceManifest(manifestPath); err != nil {
		t.Fatalf("PrepareSourceManifest: %v (install should run before run/catalog)", err)
	}
}

func TestSourceManifestRoundTripsMixedPhaseLists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	original := []byte(`
kind: app
source: github.com/acme/apps/mixed-phases
version: 0.0.1-alpha.1
install:
  - [uv, sync]
  - {command: [bun, install], workdir: ui, inputs: [bun.lock]}
build:
  - [go, build]
  - {command: [bun, run, build], workdir: ui}
run:
  command: [uv, run, provider.py, --serve]
spec:
  connections:
    default:
      auth:
        type: none
`)
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", original)

	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile: %v", err)
	}
	if len(manifest.Install.PhaseCommands()) != 2 || len(manifest.Build.PhaseCommands()) != 2 {
		t.Fatalf("install/build phase commands = %#v / %#v", manifest.Install, manifest.Build)
	}

	encoded, err := EncodeSourceManifestFormat(manifest, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	roundTripped, err := DecodeSourceManifestFormat(encoded, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("DecodeSourceManifestFormat: %v\n%s", err, encoded)
	}
	if len(roundTripped.Install.PhaseCommands()) != 2 || len(roundTripped.Build.PhaseCommands()) != 2 {
		t.Fatalf("round-tripped phases = %#v / %#v", roundTripped.Install, roundTripped.Build)
	}
}

func TestSourceRunCommand_RejectsMultipleRunEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: app
source: github.com/test/apps/multi-run
version: 0.0.1-alpha.1
run:
  - [sh, -c, "true"]
  - [sh, -c, "true"]
spec: {}
`))
	_, err := SourceRunCommand(manifestPath)
	if err == nil || !strings.Contains(err.Error(), errMultipleRunCommandsRequireDevMode) {
		t.Fatalf("SourceRunCommand err = %v", err)
	}
	if _, _, err := PrepareSourceManifest(manifestPath); err == nil || !strings.Contains(err.Error(), errMultipleRunCommandsRequireDevMode) {
		t.Fatalf("PrepareSourceManifest err = %v", err)
	}
	if _, _, err := PrepareSourceManifestForExecution(manifestPath, SourceBuildOptions{}); err != nil && strings.Contains(err.Error(), errMultipleRunCommandsRequireDevMode) {
		t.Fatalf("PrepareSourceManifestForExecution err = %v, want execution prepare to allow multiple runs", err)
	}
	if _, err := StageSourcePreparedInstallDir(manifestPath, filepath.Join(t.TempDir(), "prepared"), StageSourcePreparedInstallOptions{
		Kind: providermanifestv1.KindApp,
	}); err == nil || !strings.Contains(err.Error(), errMultipleRunCommandsRequireDevMode) {
		t.Fatalf("StageSourcePreparedInstallDir err = %v", err)
	}
}

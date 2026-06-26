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
	if err == nil || !strings.Contains(err.Error(), "install must be a mapping") {
		t.Fatalf("ReadSourceManifestFile error = %v, want install must be a mapping", err)
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

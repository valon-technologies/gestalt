package providerpkg

import (
	"strings"
	"testing"
)

func TestSourceManifestPrepareBuildAndRunCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: app
source: github.com/acme/apps/uv-source
version: 0.0.1-alpha.1
build: [uv, sync, --frozen, --no-install-project]
run: [uv, run, --frozen, provider.py, --serve]
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
	if strings.Join(manifest.Run, " ") != "uv run --frozen provider.py --serve" {
		t.Fatalf("run = %#v", manifest.Run)
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

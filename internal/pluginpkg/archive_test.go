package pluginpkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreatePackageFromDirAndReadManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "artifacts", "darwin", "arm64"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "artifacts", "darwin", "arm64", "provider"), []byte("provider"), 0755); err != nil {
		t.Fatalf("WriteFile(provider): %v", err)
	}
	manifest := `{
  "schema_version": 1,
  "id": "acme/provider",
  "version": "0.1.0",
  "kinds": ["provider"],
  "provider": {
    "protocol": { "min": 1, "max": 1 }
  },
  "artifacts": [
    {
      "os": "darwin",
      "arch": "arm64",
      "path": "artifacts/darwin/arm64/provider",
      "sha256": "` + sha256Hex("provider") + `"
    }
  ],
  "entrypoints": {
    "provider": {
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(src, ManifestFile), []byte(manifest), 0644); err != nil {
		t.Fatalf("WriteFile(plugin.json): %v", err)
	}

	archivePath := filepath.Join(dir, "acme-provider-0.1.0.tar.gz")
	if err := CreatePackageFromDir(src, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	data, parsed, err := ReadPackageManifest(archivePath)
	if err != nil {
		t.Fatalf("ReadPackageManifest: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected manifest bytes")
	}
	if parsed.ID != "acme/provider" {
		t.Fatalf("unexpected id %q", parsed.ID)
	}
}

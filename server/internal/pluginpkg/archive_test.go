package pluginpkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageRoundTripWithIconFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir, manifest := mustWriteProviderPackageDir(t, root, "github.com/acme/plugins/provider", "0.1.0", "provider")
	manifest.IconFile = "assets/icon.svg"
	mustWriteManifest(t, sourceDir, manifest)

	iconContent := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="16" height="16"/></svg>`)
	mustWriteFile(t, filepath.Join(sourceDir, "assets", "icon.svg"), iconContent, 0644)

	if _, err := ValidatePackageDir(sourceDir); err != nil {
		t.Fatalf("ValidatePackageDir: %v", err)
	}

	archivePath := filepath.Join(root, "plugin.tar.gz")
	if err := CreatePackageFromDir(sourceDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	extractDir := filepath.Join(root, "extracted")
	if err := ExtractPackage(archivePath, extractDir); err != nil {
		t.Fatalf("ExtractPackage: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(extractDir, "assets", "icon.svg"))
	if err != nil {
		t.Fatalf("icon file missing after extract: %v", err)
	}
	if string(got) != string(iconContent) {
		t.Fatalf("icon content mismatch: got %q", got)
	}

	extracted, err := ValidatePackageDir(extractDir)
	if err != nil {
		t.Fatalf("ValidatePackageDir on extracted: %v", err)
	}
	if extracted.IconFile != "assets/icon.svg" {
		t.Fatalf("extracted manifest IconFile = %q, want %q", extracted.IconFile, "assets/icon.svg")
	}
}

func TestValidatePackageDirIconFileMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir, manifest := mustWriteProviderPackageDir(t, root, "github.com/acme/plugins/provider", "0.2.0", "provider2")
	manifest.IconFile = "assets/icon.svg"
	mustWriteManifest(t, sourceDir, manifest)

	_, err := ValidatePackageDir(sourceDir)
	if err == nil {
		t.Fatal("expected error when icon file is missing")
	}
}

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
  "source": "github.com/acme/plugins/provider",
  "version": "0.1.0",
  "kinds": ["provider"],
  "provider": {},
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
	if parsed.Source != "github.com/acme/plugins/provider" {
		t.Fatalf("unexpected source %q", parsed.Source)
	}
}

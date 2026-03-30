package pluginpkg

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestFromPath_DirectoryManifestFileAndArchive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourceDir, manifest := mustWriteProviderPackageDir(t, dir, "github.com/acme/plugins/provider", "0.1.0", "provider")
	archivePath := filepath.Join(dir, "acme-provider-0.1.0.tar.gz")
	if err := CreatePackageFromDir(sourceDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		wantPath string
	}{
		{
			name:     "directory",
			input:    sourceDir,
			wantPath: filepath.Join(sourceDir, ManifestFile),
		},
		{
			name:     "manifest file",
			input:    filepath.Join(sourceDir, ManifestFile),
			wantPath: filepath.Join(sourceDir, ManifestFile),
		},
		{
			name:     "archive",
			input:    archivePath,
			wantPath: archivePath,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, parsed, gotPath, err := LoadManifestFromPath(tc.input)
			if err != nil {
				t.Fatalf("LoadManifestFromPath(%q): %v", tc.input, err)
			}
			if len(data) == 0 {
				t.Fatal("expected manifest bytes")
			}
			if gotPath != tc.wantPath {
				t.Fatalf("path = %q, want %q", gotPath, tc.wantPath)
			}
			if !ManifestEqual(parsed, manifest) {
				t.Fatalf("unexpected manifest: %+v", parsed)
			}
		})
	}
}

func TestValidatePackageDirRejectsMissingProviderSchema(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourceDir, manifest := mustWriteProviderPackageDir(t, dir, "github.com/acme/plugins/provider", "0.1.0", "provider")
	manifest.Provider.ConfigSchemaPath = "schemas/config.schema.json"
	mustWriteManifest(t, sourceDir, manifest)

	_, err := ValidatePackageDir(sourceDir)
	if err == nil {
		t.Fatal("expected missing provider schema error")
	}
	if !strings.Contains(err.Error(), "validate provider config schema") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadArchiveEntryRejectsDuplicateEntry(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "duplicate-entry.tar.gz")
	mustCreateArchive(t, archivePath,
		archiveTestFile{name: "dup.txt", data: []byte("first"), mode: 0644},
		archiveTestFile{name: "dup.txt", data: []byte("second"), mode: 0644},
	)

	_, err := ReadArchiveEntry(archivePath, "dup.txt")
	if err == nil {
		t.Fatal("expected duplicate archive entry error")
	}
	if !strings.Contains(err.Error(), "appears more than once") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractPackageRejectsEscapingEntry(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "escaping-entry.tar.gz")
	mustCreateArchive(t, archivePath,
		archiveTestFile{name: "../evil", data: []byte("oops"), mode: 0644},
	)

	err := ExtractPackage(archivePath, filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("expected escaping archive entry error")
	}
	if !strings.Contains(err.Error(), "escapes the package root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

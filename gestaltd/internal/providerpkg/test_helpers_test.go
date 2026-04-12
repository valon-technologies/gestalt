package providerpkg

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type archiveTestFile struct {
	name string
	data []byte
	mode int64
}

const (
	testArtifactOS   = "linux"
	testArtifactArch = "amd64"
)

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func artifactPathFor(osName, arch, binary string) string {
	return filepath.ToSlash(filepath.Join("artifacts", osName, arch, binary))
}

func testArtifactPath(binary string) string {
	return artifactPathFor(testArtifactOS, testArtifactArch, binary)
}

func unknownSiblingArtifactPath(artifactPath string) string {
	base := path.Base(artifactPath)
	ext := path.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return path.Join(path.Dir(artifactPath), name+"-missing"+ext)
}

func newProviderManifest(source, version, artifactPath, digest string) *providermanifestv1.Manifest {
	return &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  source,
		Version: version,
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:     testArtifactOS,
				Arch:   testArtifactArch,
				Path:   artifactPath,
				SHA256: digest,
			},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}
}

func mustWriteFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func mustWriteManifest(t *testing.T, dir string, manifest *providermanifestv1.Manifest) []byte {
	t.Helper()

	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	mustWriteFile(t, filepath.Join(dir, ManifestFile), data, 0644)
	mustWriteStaticCatalog(t, dir, manifest)
	return data
}

func mustProviderManifest(source, version, osName, arch, artifactPath, sha string) *providermanifestv1.Manifest {
	return &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  source,
		Version: version,
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:     osName,
				Arch:   arch,
				Path:   artifactPath,
				SHA256: sha,
			},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}
}

func mustManifestJSON(t *testing.T, manifest *providermanifestv1.Manifest) []byte {
	t.Helper()
	data, err := EncodeSourceManifestFormat(manifest, ManifestFormatJSON)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat(JSON): %v", err)
	}
	return data
}

func mustManifestYAML(t *testing.T, manifest *providermanifestv1.Manifest) []byte {
	t.Helper()
	data, err := EncodeSourceManifestFormat(manifest, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat(YAML): %v", err)
	}
	return data
}

func mustRawManifestJSON(t *testing.T, manifest *providermanifestv1.Manifest) []byte {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent: %v", err)
	}
	return append(data, '\n')
}

func mustWriteManifestData(t *testing.T, dir, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	mustWriteFile(t, path, data, 0644)
	format := ManifestFormatJSON
	if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
		format = ManifestFormatYAML
	}
	manifest, err := DecodeManifestFormat(data, format)
	if err != nil {
		return path
	}
	mustWriteStaticCatalog(t, dir, manifest)
	return path
}

func mustWriteStaticCatalog(t *testing.T, dir string, manifest *providermanifestv1.Manifest) {
	t.Helper()
	if manifest == nil || manifest.Spec == nil {
		return
	}
	mustWriteFile(t, filepath.Join(dir, StaticCatalogFile), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644)
}

func mustCreateArchive(t *testing.T, archivePath string, files ...archiveTestFile) {
	t.Helper()

	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Create(%q): %v", archivePath, err)
	}
	defer func() {
		if err := out.Close(); err != nil {
			t.Fatalf("close archive: %v", err)
		}
	}()

	gzw := gzip.NewWriter(out)
	defer func() {
		if err := gzw.Close(); err != nil {
			t.Fatalf("close gzip: %v", err)
		}
	}()

	tw := tar.NewWriter(gzw)
	defer func() {
		if err := tw.Close(); err != nil {
			t.Fatalf("close tar: %v", err)
		}
	}()

	for _, file := range files {
		hdr := &tar.Header{Name: file.name, Mode: file.mode, Size: int64(len(file.data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%q): %v", file.name, err)
		}
		if _, err := tw.Write(file.data); err != nil {
			t.Fatalf("Write(%q): %v", file.name, err)
		}
	}
}

func mustWriteProviderPackageDir(t *testing.T, root, source, version, content string) (string, *providermanifestv1.Manifest) {
	t.Helper()

	sourceDir := filepath.Join(root, "src")
	artifactPath := testArtifactPath("provider")
	mustWriteFile(t, filepath.Join(sourceDir, filepath.FromSlash(artifactPath)), []byte(content), 0755)

	manifest := newProviderManifest(source, version, artifactPath, sha256Hex(content))
	mustWriteManifest(t, sourceDir, manifest)
	return sourceDir, manifest
}

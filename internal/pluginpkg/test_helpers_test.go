package pluginpkg

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/manifest/v1"
)

type archiveTestFile struct {
	name string
	data []byte
	mode int64
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func currentArtifactPath(binary string) string {
	return filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, binary))
}

func newProviderManifest(source, version, artifactPath, digest string) *pluginmanifestv1.Manifest {
	return &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		Source:        source,
		Version:       version,
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactPath,
				SHA256: digest,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: artifactPath},
		},
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

func mustWriteManifest(t *testing.T, dir string, manifest *pluginmanifestv1.Manifest) []byte {
	t.Helper()

	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	mustWriteFile(t, filepath.Join(dir, ManifestFile), data, 0644)
	return data
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

func mustWriteProviderPackageDir(t *testing.T, root, source, version, content string) (string, *pluginmanifestv1.Manifest) {
	t.Helper()

	sourceDir := filepath.Join(root, "src")
	artifactPath := currentArtifactPath("provider")
	mustWriteFile(t, filepath.Join(sourceDir, filepath.FromSlash(artifactPath)), []byte(content), 0755)

	manifest := newProviderManifest(source, version, artifactPath, sha256Hex(content))
	mustWriteManifest(t, sourceDir, manifest)
	return sourceDir, manifest
}

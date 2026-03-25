package pluginstore

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	pluginpkg "github.com/valon-technologies/gestalt/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

func TestRootForConfigPath(t *testing.T) {
	t.Parallel()

	got := RootForConfigPath("/tmp/project/configs/gestalt.yaml")
	want := filepath.Join("/tmp/project/configs", ".gestalt", "plugins")
	if got != want {
		t.Fatalf("RootForConfigPath = %q, want %q", got, want)
	}
}

func TestParseRef(t *testing.T) {
	t.Parallel()

	ref, err := ParseRef("acme/provider@0.1.0")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	if ref.Publisher != "acme" || ref.Name != "provider" || ref.Version != "0.1.0" {
		t.Fatalf("unexpected ref: %+v", ref)
	}

	for _, raw := range []string{"", "acme/provider", "acme@0.1.0", " acme/provider@0.1.0 ", "acme/provider@", "acme//provider@0.1.0"} {
		if _, err := ParseRef(raw); err == nil {
			t.Fatalf("expected ParseRef(%q) to fail", raw)
		}
	}
}

func TestInstallAndList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configs", "gestalt.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8080\n"), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	store := New(cfgPath)
	pkg1 := mustBuildPackage(t, dir, "acme/provider", "0.1.0", "hello from acme")
	pkg2 := mustBuildPackage(t, dir, "beta/provider", "1.2.3", "hello from beta")

	installed1, err := store.Install(pkg1)
	if err != nil {
		t.Fatalf("Install pkg1: %v", err)
	}
	if installed1.Ref.String() != "acme/provider@0.1.0" {
		t.Fatalf("installed ref = %q", installed1.Ref.String())
	}
	if installed1.Manifest == nil || installed1.Manifest.ID != "acme/provider" {
		t.Fatalf("unexpected installed manifest: %+v", installed1.Manifest)
	}
	if installed1.ManifestPath != filepath.Join(store.Root(), "acme", "provider", "0.1.0", pluginpkg.ManifestFile) {
		t.Fatalf("ManifestPath = %q", installed1.ManifestPath)
	}
	if installed1.ExecutablePath != filepath.Join(store.Root(), "acme", "provider", "0.1.0", "artifacts", runtime.GOOS, runtime.GOARCH, "provider") {
		t.Fatalf("ExecutablePath = %q", installed1.ExecutablePath)
	}
	data, err := os.ReadFile(installed1.ExecutablePath)
	if err != nil {
		t.Fatalf("ReadFile executable: %v", err)
	}
	if string(data) != "hello from acme" {
		t.Fatalf("unexpected executable content: %q", data)
	}

	installed2, err := store.Install(pkg2)
	if err != nil {
		t.Fatalf("Install pkg2: %v", err)
	}
	if installed2.Ref.String() != "beta/provider@1.2.3" {
		t.Fatalf("installed ref = %q", installed2.Ref.String())
	}

	listed, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("List returned %d items, want 2", len(listed))
	}
	if listed[0].Ref.String() != "acme/provider@0.1.0" || listed[1].Ref.String() != "beta/provider@1.2.3" {
		t.Fatalf("unexpected list order: %s, %s", listed[0].Ref.String(), listed[1].Ref.String())
	}

	resolved, err := store.Resolve(installed1.Ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.ExecutablePath != installed1.ExecutablePath {
		t.Fatalf("resolved executable path = %q, want %q", resolved.ExecutablePath, installed1.ExecutablePath)
	}
}

func TestInstallRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8080\n"), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	store := New(cfgPath)

	pkg := mustBuildMismatchPackage(t, dir, "acme/provider", "0.1.0", "hello", strings.Repeat("deadbeef", 8))
	_, err := store.Install(pkg)
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
	if !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallRejectsUnsafeManifestVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8080\n"), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	store := New(cfgPath)

	pkg := mustBuildPackage(t, dir, "acme/provider", "../evil", "hello")
	_, err := store.Install(pkg)
	if err == nil {
		t.Fatal("expected invalid manifest version error")
	}
	if !strings.Contains(err.Error(), "valid plugin ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallRejectsDuplicateArtifactEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8080\n"), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	store := New(cfgPath)

	pkg := mustBuildPackageWithDuplicateArtifact(t, dir, "acme/provider", "0.1.0", "good", "evil")
	_, err := store.Install(pkg)
	if err == nil {
		t.Fatal("expected duplicate artifact entry error")
	}
	if !strings.Contains(err.Error(), "appears more than once") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustBuildPackage(t *testing.T, dir, id, version, content string) string {
	t.Helper()
	return mustBuildPackageWithDigest(t, dir, id, version, content, "")
}

func mustBuildPackageWithDigest(t *testing.T, dir, id, version, content, digestOverride string) string {
	t.Helper()

	source := filepath.Join(dir, strings.NewReplacer("/", "-", "@", "-", ".", "_").Replace(id+"-"+version))
	if err := os.MkdirAll(filepath.Join(source, "artifacts", runtime.GOOS, runtime.GOARCH), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	artifactPath := filepath.Join(source, "artifacts", runtime.GOOS, runtime.GOARCH, "provider")
	if err := os.WriteFile(artifactPath, []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	sum := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(sum[:])
	if digestOverride != "" {
		digest = digestOverride
	}
	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		ID:            id,
		Version:       version,
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider")),
				SHA256: digest,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider")),
			},
		},
	}
	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, pluginpkg.ManifestFile), manifestBytes, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	archivePath := filepath.Join(dir, filepath.Base(source)+".tar.gz")
	if err := pluginpkg.CreatePackageFromDir(source, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	return archivePath
}

func mustBuildMismatchPackage(t *testing.T, dir, id, version, content, digest string) string {
	t.Helper()

	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		ID:            id,
		Version:       version,
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider")),
				SHA256: digest,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider")),
			},
		},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent manifest: %v", err)
	}
	manifestBytes = append(manifestBytes, '\n')

	archivePath := filepath.Join(dir, "mismatch.tar.gz")
	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Create archive: %v", err)
	}
	gzw := gzip.NewWriter(out)
	tw := tar.NewWriter(gzw)

	writeFile := func(name string, data []byte, mode int64) {
		hdr := &tar.Header{Name: name, Mode: mode, Size: int64(len(data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", name, err)
		}
		if _, err := io.Copy(tw, bytes.NewReader(data)); err != nil {
			t.Fatalf("Write file %s: %v", name, err)
		}
	}
	writeFile(pluginpkg.ManifestFile, manifestBytes, 0644)
	writeFile(filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider")), []byte(content), 0755)

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return archivePath
}

func mustBuildPackageWithDuplicateArtifact(t *testing.T, dir, id, version, firstContent, secondContent string) string {
	t.Helper()

	artifactName := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	sum := sha256.Sum256([]byte(firstContent))
	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		ID:            id,
		Version:       version,
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactName,
				SHA256: hex.EncodeToString(sum[:]),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: artifactName,
			},
		},
	}
	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}

	archivePath := filepath.Join(dir, "duplicate-artifact.tar.gz")
	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Create archive: %v", err)
	}
	gzw := gzip.NewWriter(out)
	tw := tar.NewWriter(gzw)

	writeFile := func(name string, data []byte, mode int64) {
		hdr := &tar.Header{Name: name, Mode: mode, Size: int64(len(data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", name, err)
		}
		if _, err := io.Copy(tw, bytes.NewReader(data)); err != nil {
			t.Fatalf("Write file %s: %v", name, err)
		}
	}
	writeFile(pluginpkg.ManifestFile, manifestBytes, 0644)
	writeFile(artifactName, []byte(firstContent), 0755)
	writeFile(artifactName, []byte(secondContent), 0755)

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return archivePath
}

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

	pluginpkg "github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

const testCatalogYAML = "name: provider\noperations:\n  - id: echo\n    method: POST\n"

func TestInstall(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pkg1 := mustBuildPackage(t, dir, "github.com/testowner/plugins/provider", "0.0.1-alpha.1", "hello from testowner")
	pkg2 := mustBuildPackage(t, dir, "github.com/beta/plugins/provider", "1.2.3", "hello from beta")

	dest1 := filepath.Join(dir, "plugins", "integration_example")
	installed1, err := Install(pkg1, dest1)
	if err != nil {
		t.Fatalf("Install pkg1: %v", err)
	}
	if installed1.Source != "github.com/testowner/plugins/provider" {
		t.Fatalf("installed source = %q", installed1.Source)
	}
	if installed1.Manifest == nil || installed1.Manifest.Source != "github.com/testowner/plugins/provider" {
		t.Fatalf("unexpected installed manifest: %+v", installed1.Manifest)
	}
	if installed1.Root != dest1 {
		t.Fatalf("Root = %q, want %q", installed1.Root, dest1)
	}
	if installed1.ManifestPath != filepath.Join(dest1, pluginpkg.ManifestFile) {
		t.Fatalf("ManifestPath = %q", installed1.ManifestPath)
	}
	if installed1.ExecutablePath != filepath.Join(dest1, "artifacts", runtime.GOOS, runtime.GOARCH, "provider") {
		t.Fatalf("ExecutablePath = %q", installed1.ExecutablePath)
	}
	data, err := os.ReadFile(installed1.ExecutablePath)
	if err != nil {
		t.Fatalf("ReadFile executable: %v", err)
	}
	if string(data) != "hello from testowner" {
		t.Fatalf("unexpected executable content: %q", data)
	}

	dest2 := filepath.Join(dir, "plugins", "integration_beta")
	installed2, err := Install(pkg2, dest2)
	if err != nil {
		t.Fatalf("Install pkg2: %v", err)
	}
	if installed2.Source != "github.com/beta/plugins/provider" {
		t.Fatalf("installed source = %q", installed2.Source)
	}
}

func TestExecutablePathForManifestPrefersProviderEntrypoint(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	providerArtifact := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	authArtifact := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth"))
	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/testowner/plugins/multi-kind",
		Version: "0.0.1-alpha.1",
		Kinds:   []string{pluginmanifestv1.KindAuth, pluginmanifestv1.KindProvider},
		Auth:    &pluginmanifestv1.AuthMetadata{},
		Provider: &pluginmanifestv1.Provider{
			Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Auth:     &pluginmanifestv1.Entrypoint{ArtifactPath: authArtifact},
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: providerArtifact},
		},
	}

	executablePath, err := executablePathForManifest(root, manifest)
	if err != nil {
		t.Fatalf("executablePathForManifest: %v", err)
	}
	if executablePath != filepath.Join(root, filepath.FromSlash(providerArtifact)) {
		t.Fatalf("ExecutablePath = %q, want %q", executablePath, filepath.Join(root, filepath.FromSlash(providerArtifact)))
	}
}

func TestInstallRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pkg := mustBuildMismatchPackage(t, dir, "github.com/testowner/plugins/provider", "0.0.1-alpha.1", "hello", strings.Repeat("deadbeef", 8))
	dest := filepath.Join(dir, "plugins", "integration_example")
	_, err := Install(pkg, dest)
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
	if !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallRejectsDuplicateArtifactEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pkg := mustBuildPackageWithDuplicateArtifact(t, dir, "github.com/testowner/plugins/provider", "0.0.1-alpha.1", "good", "evil")
	dest := filepath.Join(dir, "plugins", "integration_example")
	_, err := Install(pkg, dest)
	if err == nil {
		t.Fatal("expected duplicate artifact entry error")
	}
	if !strings.Contains(err.Error(), "appears more than once") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallFromDirValidatesAndCopies(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcDir := mustBuildPluginDir(t, dir, "github.com/testowner/plugins/provider", "0.0.1-alpha.1", "dir-content", "")

	dest := filepath.Join(dir, "plugins", "integration_example")
	installed, err := InstallFromDir(srcDir, dest)
	if err != nil {
		t.Fatalf("InstallFromDir: %v", err)
	}
	if installed.Source != "github.com/testowner/plugins/provider" {
		t.Fatalf("source = %q", installed.Source)
	}
	data, err := os.ReadFile(installed.ExecutablePath)
	if err != nil {
		t.Fatalf("ReadFile executable: %v", err)
	}
	if string(data) != "dir-content" {
		t.Fatalf("executable content = %q", data)
	}
	if _, err := os.Stat(installed.ManifestPath); err != nil {
		t.Fatalf("manifest not copied: %v", err)
	}
}

func TestInstallFromDirCopiesSchema(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcDir := mustBuildPluginDir(t, dir, "github.com/testowner/plugins/provider", "0.2.0", "binary", `{"type":"object"}`)

	dest := filepath.Join(dir, "plugins", "integration_example")
	installed, err := InstallFromDir(srcDir, dest)
	if err != nil {
		t.Fatalf("InstallFromDir: %v", err)
	}

	schemaPath := filepath.Join(installed.Root, "schemas", "config.schema.json")
	if _, err := os.Stat(schemaPath); err != nil {
		t.Fatalf("schema not copied: %v", err)
	}
}

func TestInstallFromDirRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcDir := mustBuildPluginDirWithDigest(t, dir, "github.com/testowner/plugins/provider", "0.3.0", "real-content", strings.Repeat("deadbeef", 8))
	dest := filepath.Join(dir, "plugins", "integration_example")
	_, err := InstallFromDir(srcDir, dest)
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
	if !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallExtractsToDestDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pkg := mustBuildV2Package(t, dir, "github.com/testorg/testrepo/testplugin", "1.0.0", "v2-binary")

	dest := filepath.Join(dir, "plugins", "integration_beta")
	installed, err := Install(pkg, dest)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if installed.Source != "github.com/testorg/testrepo/testplugin" {
		t.Fatalf("source = %q", installed.Source)
	}
	if installed.Root != dest {
		t.Fatalf("install root = %q, want %q", installed.Root, dest)
	}
	data, err := os.ReadFile(installed.ExecutablePath)
	if err != nil {
		t.Fatalf("ReadFile executable: %v", err)
	}
	if string(data) != "v2-binary" {
		t.Fatalf("executable content = %q", data)
	}
}

func TestInstallFromDirExtractsToDestDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcDir := mustBuildV2PluginDir(t, dir, "github.com/testorg/testrepo/testplugin", "0.5.0", "dir-v2-binary")

	dest := filepath.Join(dir, "plugins", "integration_beta")
	installed, err := InstallFromDir(srcDir, dest)
	if err != nil {
		t.Fatalf("InstallFromDir: %v", err)
	}
	if installed.Source != "github.com/testorg/testrepo/testplugin" {
		t.Fatalf("source = %q", installed.Source)
	}
	if installed.Root != dest {
		t.Fatalf("install root = %q, want %q", installed.Root, dest)
	}
	data, err := os.ReadFile(installed.ExecutablePath)
	if err != nil {
		t.Fatalf("ReadFile executable: %v", err)
	}
	if string(data) != "dir-v2-binary" {
		t.Fatalf("executable content = %q", data)
	}
}

func TestInstallFromDirCopiesManifestAndArtifact(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcDir := mustBuildPluginDir(t, dir, "github.com/testowner/plugins/fullcopy", "0.0.1-alpha.1", "test-binary", "")

	dest := filepath.Join(dir, "plugins", "integration_fullcopy")
	installed, err := InstallFromDir(srcDir, dest)
	if err != nil {
		t.Fatalf("InstallFromDir: %v", err)
	}

	if installed.ManifestPath == "" {
		t.Fatal("ManifestPath is empty")
	}
	if _, err := os.Stat(installed.ManifestPath); err != nil {
		t.Fatalf("manifest file missing: %v", err)
	}
	if installed.ExecutablePath == "" {
		t.Fatal("ExecutablePath is empty")
	}
	if _, err := os.Stat(installed.ExecutablePath); err != nil {
		t.Fatalf("executable file missing: %v", err)
	}

	data, err := os.ReadFile(installed.ExecutablePath)
	if err != nil {
		t.Fatalf("ReadFile executable: %v", err)
	}
	if string(data) != "test-binary" {
		t.Fatalf("executable content = %q", data)
	}
}

func TestInstallFromDirSetsSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcDir := mustBuildV2PluginDir(t, dir, "github.com/test-org/test-repo/test-plugin", "1.0.0", "v2-install-test")

	dest := filepath.Join(dir, "plugins", "integration_test")
	installed, err := InstallFromDir(srcDir, dest)
	if err != nil {
		t.Fatalf("InstallFromDir: %v", err)
	}
	if installed.Source != "github.com/test-org/test-repo/test-plugin" {
		t.Fatalf("Source = %q", installed.Source)
	}
	if installed.ExecutablePath == "" {
		t.Fatal("ExecutablePath is empty")
	}
}

func TestInstall_ArchiveArtifactDigestVerified(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pkg := mustBuildPackage(t, dir, "github.com/testowner/plugins/digcheck", "0.0.1-alpha.1", "correct-content")

	dest := filepath.Join(dir, "plugins", "integration_digcheck")
	installed, err := Install(pkg, dest)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if installed.SHA256 == "" {
		t.Fatal("SHA256 is empty")
	}
	sum := sha256.Sum256([]byte("correct-content"))
	if installed.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("SHA256 = %q, want %q", installed.SHA256, hex.EncodeToString(sum[:]))
	}
}

func TestInstallRejectsNonLinuxLibcArtifact(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcDir := mustBuildV2PluginDir(t, dir, "github.com/testowner/plugins/provider", "0.4.0", "bad-artifact")
	manifestPath := filepath.Join(srcDir, pluginpkg.ManifestFile)
	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile: %v", err)
	}

	oldPath := manifest.Artifacts[0].Path
	newPath := "artifacts/darwin/arm64/provider"
	manifest.Artifacts[0].OS = "darwin"
	manifest.Artifacts[0].Arch = "arm64"
	manifest.Artifacts[0].Path = newPath
	manifest.Entrypoints.Provider.ArtifactPath = newPath

	oldArtifactPath := filepath.Join(srcDir, filepath.FromSlash(oldPath))
	newArtifactPath := filepath.Join(srcDir, filepath.FromSlash(newPath))
	if err := os.MkdirAll(filepath.Dir(newArtifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.Rename(oldArtifactPath, newArtifactPath); err != nil {
		t.Fatalf("Rename artifact: %v", err)
	}

	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	var manifestDoc map[string]any
	if err := json.Unmarshal(manifestBytes, &manifestDoc); err != nil {
		t.Fatalf("json.Unmarshal manifest: %v", err)
	}
	artifacts, ok := manifestDoc["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("manifest artifacts = %#v, want one artifact", manifestDoc["artifacts"])
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("artifact doc = %T, want map[string]any", artifacts[0])
	}
	artifact["libc"] = pluginpkg.LinuxLibCGLibC
	manifestBytes, err = json.Marshal(manifestDoc)
	if err != nil {
		t.Fatalf("json.Marshal manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	pkg := filepath.Join(dir, "invalid-libc.tar.gz")
	out, err := os.Create(pkg)
	if err != nil {
		t.Fatalf("Create archive: %v", err)
	}
	gzw := gzip.NewWriter(out)
	tw := tar.NewWriter(gzw)
	writeFile := func(name string, data []byte, mode int64) {
		t.Helper()
		hdr := &tar.Header{Name: name, Mode: mode, Size: int64(len(data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", name, err)
		}
		if _, err := io.Copy(tw, bytes.NewReader(data)); err != nil {
			t.Fatalf("Write file %s: %v", name, err)
		}
	}
	writeFile(pluginpkg.ManifestFile, manifestBytes, 0o644)
	writeFile("catalog.yaml", []byte(testCatalogYAML), 0o644)
	artifactData, err := os.ReadFile(newArtifactPath)
	if err != nil {
		t.Fatalf("ReadFile artifact: %v", err)
	}
	writeFile(newPath, artifactData, 0o755)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	_, err = Install(pkg, filepath.Join(dir, "plugins", "integration_invalid_libc"))
	if err == nil {
		t.Fatal("expected invalid non-linux libc artifact error")
	}
	if !strings.Contains(err.Error(), `only supported for linux artifacts`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallPrefersCurrentLinuxLibcArtifact(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("linux libc-specific artifact selection only applies on linux hosts")
	}
	currentLibC := pluginpkg.CurrentRuntimeLibC()
	if currentLibC == "" {
		t.Skip("linux libc detection unavailable on this host")
	}

	otherLibC := pluginpkg.LinuxLibCGLibC
	if currentLibC == pluginpkg.LinuxLibCGLibC {
		otherLibC = pluginpkg.LinuxLibCMusl
	}

	dir := t.TempDir()
	srcDir := mustBuildV2PluginDir(t, dir, "github.com/testowner/plugins/provider", "0.5.0", "exact-libc-provider")
	manifestPath := filepath.Join(srcDir, pluginpkg.ManifestFile)
	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile: %v", err)
	}

	exactPath := manifest.Artifacts[0].Path
	manifest.Artifacts[0].LibC = currentLibC

	appendArtifact := func(path, libc, content string) {
		t.Helper()
		sum := sha256.Sum256([]byte(content))
		manifest.Artifacts = append(manifest.Artifacts, pluginmanifestv1.Artifact{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			LibC:   libc,
			Path:   path,
			SHA256: hex.EncodeToString(sum[:]),
		})
		filePath := filepath.Join(srcDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatalf("MkdirAll artifact dir: %v", err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0o755); err != nil {
			t.Fatalf("WriteFile artifact: %v", err)
		}
	}

	appendArtifact(filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider-generic")), "", "generic-provider")
	appendArtifact(filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider-"+otherLibC)), otherLibC, "other-libc-provider")

	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	pkg := filepath.Join(dir, "exact-libc.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(srcDir, pkg); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	installed, err := Install(pkg, filepath.Join(dir, "plugins", "integration_exact_libc"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if installed.Artifact == nil {
		t.Fatal("Artifact is nil")
	}
	if installed.Artifact.Path != exactPath {
		t.Fatalf("Artifact.Path = %q, want %q", installed.Artifact.Path, exactPath)
	}
	if installed.ExecutablePath != filepath.Join(installed.Root, filepath.FromSlash(exactPath)) {
		t.Fatalf("ExecutablePath = %q, want %q", installed.ExecutablePath, filepath.Join(installed.Root, filepath.FromSlash(exactPath)))
	}
	data, err := os.ReadFile(installed.ExecutablePath)
	if err != nil {
		t.Fatalf("ReadFile executable: %v", err)
	}
	if string(data) != "exact-libc-provider" {
		t.Fatalf("executable content = %q, want %q", data, "exact-libc-provider")
	}
}

func mustBuildPluginDir(t *testing.T, dir, source, version, content, schema string) string {
	t.Helper()

	srcDir := filepath.Join(dir, strings.NewReplacer("/", "-", "@", "-", ".", "_").Replace(source+"-"+version)+"-dir")
	if err := os.MkdirAll(filepath.Join(srcDir, "artifacts", runtime.GOOS, runtime.GOARCH), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "artifacts", runtime.GOOS, runtime.GOARCH, "provider"), []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	sum := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(sum[:])

	var schemaPath string
	if schema != "" {
		schemaPath = "schemas/config.schema.json"
		if err := os.MkdirAll(filepath.Join(srcDir, "schemas"), 0755); err != nil {
			t.Fatalf("MkdirAll schemas: %v", err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, "schemas", "config.schema.json"), []byte(schema), 0644); err != nil {
			t.Fatalf("WriteFile schema: %v", err)
		}
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			ConfigSchemaPath: schemaPath,
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
	if err := os.WriteFile(filepath.Join(srcDir, pluginpkg.ManifestFile), manifestBytes, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte(testCatalogYAML), 0644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}
	return srcDir
}

func mustBuildPluginDirWithDigest(t *testing.T, dir, source, version, content, digestOverride string) string {
	t.Helper()

	srcDir := filepath.Join(dir, strings.NewReplacer("/", "-", "@", "-", ".", "_").Replace(source+"-"+version)+"-dir-bad")
	if err := os.MkdirAll(filepath.Join(srcDir, "artifacts", runtime.GOOS, runtime.GOARCH), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "artifacts", runtime.GOOS, runtime.GOARCH, "provider"), []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:   source,
		Version:  version,
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider")),
				SHA256: digestOverride,
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
	if err := os.WriteFile(filepath.Join(srcDir, pluginpkg.ManifestFile), manifestBytes, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte(testCatalogYAML), 0644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}
	return srcDir
}

func mustBuildPackage(t *testing.T, dir, source, version, content string) string {
	t.Helper()
	return mustBuildPackageWithDigest(t, dir, source, version, content, "")
}

func mustBuildPackageWithDigest(t *testing.T, dir, source, version, content, digestOverride string) string {
	t.Helper()

	srcDir := filepath.Join(dir, strings.NewReplacer("/", "-", "@", "-", ".", "_").Replace(source+"-"+version))
	if err := os.MkdirAll(filepath.Join(srcDir, "artifacts", runtime.GOOS, runtime.GOARCH), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	artifactPath := filepath.Join(srcDir, "artifacts", runtime.GOOS, runtime.GOARCH, "provider")
	if err := os.WriteFile(artifactPath, []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	sum := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(sum[:])
	if digestOverride != "" {
		digest = digestOverride
	}
	manifest := &pluginmanifestv1.Manifest{
		Source:   source,
		Version:  version,
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
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
	if err := os.WriteFile(filepath.Join(srcDir, pluginpkg.ManifestFile), manifestBytes, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte(testCatalogYAML), 0644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}

	archivePath := filepath.Join(dir, filepath.Base(srcDir)+".tar.gz")
	if err := pluginpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	return archivePath
}

func mustBuildMismatchPackage(t *testing.T, dir, source, version, content, digest string) string {
	t.Helper()

	manifest := &pluginmanifestv1.Manifest{
		Source:   source,
		Version:  version,
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
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
	writeFile("catalog.yaml", []byte(testCatalogYAML), 0644)
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

func mustBuildPackageWithDuplicateArtifact(t *testing.T, dir, source, version, firstContent, secondContent string) string {
	t.Helper()

	artifactName := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	sum := sha256.Sum256([]byte(firstContent))
	manifest := &pluginmanifestv1.Manifest{
		Source:   source,
		Version:  version,
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
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
	writeFile("catalog.yaml", []byte(testCatalogYAML), 0644)
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

func newV2Manifest(source, version, content string) *pluginmanifestv1.Manifest {
	artifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	sum := sha256.Sum256([]byte(content))
	return &pluginmanifestv1.Manifest{
		Source:   source,
		Version:  version,
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactPath,
				SHA256: hex.EncodeToString(sum[:]),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: artifactPath,
			},
		},
	}
}

func mustBuildV2Package(t *testing.T, dir, source, version, content string) string {
	t.Helper()

	safeName := strings.NewReplacer("/", "-", ".", "_").Replace(source + "-" + version)
	sourceDir := filepath.Join(dir, safeName)
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	if err := os.MkdirAll(filepath.Join(sourceDir, filepath.Dir(filepath.FromSlash(artifactRel))), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, filepath.FromSlash(artifactRel)), []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}

	manifest := newV2Manifest(source, version, content)
	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, pluginpkg.ManifestFile), manifestBytes, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "catalog.yaml"), []byte(testCatalogYAML), 0644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}

	archivePath := filepath.Join(dir, safeName+".tar.gz")
	if err := pluginpkg.CreatePackageFromDir(sourceDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	return archivePath
}

func mustBuildV2PluginDir(t *testing.T, dir, source, version, content string) string {
	t.Helper()

	safeName := strings.NewReplacer("/", "-", ".", "_").Replace(source+"-"+version) + "-dir"
	sourceDir := filepath.Join(dir, safeName)
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	if err := os.MkdirAll(filepath.Join(sourceDir, filepath.Dir(filepath.FromSlash(artifactRel))), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, filepath.FromSlash(artifactRel)), []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}

	manifest := newV2Manifest(source, version, content)
	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, pluginpkg.ManifestFile), manifestBytes, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "catalog.yaml"), []byte(testCatalogYAML), 0644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}
	return sourceDir
}

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

func TestE2EBundleArchiveAndValidate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")

	out, err := exec.Command(gestaltdBin, "plugin", "package", "--input", pluginDir, "--output", archivePath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd plugin package: %v\n%s", err, out)
	}

	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", 0)
	bundleDir := filepath.Join(dir, "bundle")
	out, err = exec.Command(gestaltdBin, "bundle", "--config", cfgPath, "--output", bundleDir).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd bundle: %v\n%s", err, out)
	}

	bundledCfg := filepath.Join(bundleDir, "config.yaml")
	lockPath := filepath.Join(bundleDir, initLockfileName)
	lock, err := readLockfile(lockPath)
	if err != nil {
		t.Fatalf("readLockfile: %v", err)
	}
	entry, ok := lock.Plugins[lockPluginKey("integration", "example")]
	if !ok {
		t.Fatalf("lockfile missing plugin entry: %+v", lock.Plugins)
	}
	if entry.SourceDigest == "" {
		t.Fatal("expected non-empty SourceDigest for archive package")
	}
	if entry.Package == "" {
		t.Fatal("expected non-empty Package in lockfile entry")
	}

	out, err = exec.Command(gestaltdBin, "validate", "--config", bundledCfg).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate: %v\n%s", err, out)
	}
}

func TestE2EBundleDirectoryPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	cfgPath := writeE2EConfigForDir(t, dir, pluginDir)
	bundleDir := filepath.Join(dir, "bundle")

	out, err := exec.Command(gestaltdBin, "bundle", "--config", cfgPath, "--output", bundleDir).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd bundle: %v\n%s", err, out)
	}

	lockPath := filepath.Join(bundleDir, initLockfileName)
	lock, err := readLockfile(lockPath)
	if err != nil {
		t.Fatalf("readLockfile: %v", err)
	}
	entry, ok := lock.Plugins[lockPluginKey("integration", "example")]
	if !ok {
		t.Fatalf("lockfile missing plugin entry: %+v", lock.Plugins)
	}
	if entry.SourceDigest == "" {
		t.Fatal("expected non-empty SourceDigest for directory package")
	}
}

func TestE2EBundleHTTPSPackage(t *testing.T) { //nolint:paralleltest // mutates http.DefaultTransport

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(pluginDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	}))
	defer ts.Close()

	savedTransport := http.DefaultTransport
	http.DefaultTransport = ts.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = savedTransport })

	cfgPath := writeE2EConfig(t, dir, ts.URL+"/plugin.tar.gz", 0)
	bundleDir := filepath.Join(dir, "bundle")
	if err := run([]string{"bundle", "--config", cfgPath, "--output", bundleDir}); err != nil {
		t.Fatalf("run bundle: %v", err)
	}

	lockPath := filepath.Join(bundleDir, initLockfileName)
	lock, err := readLockfile(lockPath)
	if err != nil {
		t.Fatalf("readLockfile: %v", err)
	}
	entry, ok := lock.Plugins[lockPluginKey("integration", "example")]
	if !ok {
		t.Fatalf("lockfile missing plugin entry: %+v", lock.Plugins)
	}
	if entry.SourceDigest != "" {
		t.Fatalf("expected empty SourceDigest for HTTPS package, got %q", entry.SourceDigest)
	}
}

func TestE2EBundleServeLockedGoldenPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")

	out, err := exec.Command(gestaltdBin, "plugin", "package", "--input", pluginDir, "--output", archivePath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd plugin package: %v\n%s", err, out)
	}

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", port)
	bundleDir := filepath.Join(dir, "bundle")

	out, err = exec.Command(gestaltdBin, "bundle", "--config", cfgPath, "--output", bundleDir).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd bundle: %v\n%s", err, out)
	}

	bundledCfg := filepath.Join(bundleDir, "config.yaml")
	cmd := exec.Command(gestaltdBin, "serve", "--locked", "--config", bundledCfg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForReady(t, baseURL, 30*time.Second)

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/example/echo", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("invoke returned %d: %s", resp.StatusCode, body)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if result["echo"] != "hello" {
		t.Fatalf("expected echo=hello, got %v", result)
	}
}

func TestE2EBareGestaltdAutoInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(pluginDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", port)

	cmd := exec.Command(gestaltdBin, "--config", cfgPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForHealth(t, baseURL, 20*time.Second)
}

func TestE2EValidateNonMutating(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(pluginDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	cfgPath := writeE2EConfig(t, dir, "plugin.tar.gz", 0)
	lockPath := filepath.Join(dir, initLockfileName)

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail without bundle, output: %s", out)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("expected no lockfile after non-mutating validate")
	}

	out, err = exec.Command(gestaltdBin, "validate", "--init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate --init: %v\n%s", err, out)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lockfile after validate --init: %v", err)
	}
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EValidateRejectsUnknownYAMLField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: local
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  encryption_key: test-key
integrations:
  example:
    plugin:
      command: /tmp/provider
      bogus: true
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail for unknown field, output: %s", out)
	}
	if !strings.Contains(string(out), "bogus") || !strings.Contains(string(out), "parsing config YAML") {
		t.Fatalf("expected output to mention unknown field and YAML parsing, got: %s", out)
	}
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EDefaultStartRejectsUnknownYAMLField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: local
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  encryption_key: test-key
  typo: true
integrations:
  example:
    plugin:
      command: /tmp/provider
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected default start command to fail for unknown field, output: %s", out)
	}
	if !strings.Contains(string(out), "typo") || !strings.Contains(string(out), "parsing config YAML") {
		t.Fatalf("expected output to mention unknown field and YAML parsing, got: %s", out)
	}
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EValidateRejectsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`{{{invalid yaml`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail for malformed YAML, output: %s", out)
	}
	if !strings.Contains(string(out), "parsing config YAML") {
		t.Fatalf("expected output to mention YAML parsing failure, got: %s", out)
	}
}

func setupPluginDir(t *testing.T, baseDir string) string {
	t.Helper()
	return setupPluginDirWithVersion(t, baseDir, "0.1.0")
}

func setupPluginDirWithVersion(t *testing.T, baseDir, version string) string {
	t.Helper()

	pluginDir := filepath.Join(baseDir, "plugin-src")
	artifactRel := pluginArtifactRel()
	artifactAbs := filepath.Join(pluginDir, filepath.FromSlash(artifactRel))

	if err := os.MkdirAll(filepath.Dir(artifactAbs), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := copyFile(pluginBin, artifactAbs); err != nil {
		t.Fatalf("copy plugin binary: %v", err)
	}

	writeManifest(t, pluginDir, version)
	return pluginDir
}

func writeManifest(t *testing.T, pluginDir, version string) {
	t.Helper()

	artifactRel := pluginArtifactRel()
	artifactAbs := filepath.Join(pluginDir, filepath.FromSlash(artifactRel))

	digest, err := fileSHA256(artifactAbs)
	if err != nil {
		t.Fatalf("compute artifact digest: %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		ID:            "test/provider",
		Version:       version,
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactRel,
				SHA256: digest,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: artifactRel,
			},
		},
	}
	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, pluginpkg.ManifestFile), data, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func pluginArtifactRel() string {
	return filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeE2EConfig(t *testing.T, dir, packageRef string, port int) string {
	t.Helper()

	if port == 0 {
		port = 18080
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: %d
  encryption_key: test-e2e-key
integrations:
  example:
    plugin:
      package: %s
`, filepath.Join(dir, "gestalt.db"), port, packageRef)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func writeE2EConfigForDir(t *testing.T, dir, pluginDir string) string {
	t.Helper()
	return writeE2EConfig(t, dir, pluginDir, 0)
}

var nextTestPort atomic.Int32 // zero value; first allocation returns 19100

func allocateTestPort(t *testing.T) int {
	t.Helper()
	return int(nextTestPort.Add(1)) + 19099
}

func waitForHealth(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	waitForEndpoint(t, baseURL+"/health", timeout)
}

func waitForReady(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	waitForEndpoint(t, baseURL+"/ready", timeout)
}

func waitForEndpoint(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s did not return 200 within %s", url, timeout)
}

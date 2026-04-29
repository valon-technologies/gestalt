package daemon

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

var (
	gestaltdBin            string
	pluginBin              string
	indexedDBBin           string
	externalCredentialsBin string
	gestaltCLIBin          string
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "gestaltd-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}

	gestaltdBin = filepath.Join(tmpDir, "gestaltd")
	pluginBin = filepath.Join(tmpDir, "provider")
	indexedDBBin = filepath.Join(tmpDir, "indexeddb-provider")
	externalCredentialsBin = filepath.Join(tmpDir, "external-credentials-provider")
	indexedDBSrcDir := filepath.Join(filepath.Dir(testutil.MustExampleProviderPluginPath()), "provider-go-indexeddb")
	externalCredentialsSrcDir, err := writeExternalCredentialsProviderFixture(tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write external credentials fixture: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	wg.Add(4)
	go func() {
		defer wg.Done()
		errs[0] = buildTarget(".", "github.com/valon-technologies/gestalt/server/cmd/gestaltd", gestaltdBin)
	}()
	go func() {
		defer wg.Done()
		errs[1] = providerpkg.BuildGoProviderBinary(testutil.MustExampleProviderPluginPath(), pluginBin, "provider-go", runtime.GOOS, runtime.GOARCH)
	}()
	go func() {
		defer wg.Done()
		errs[2] = providerpkg.BuildGoComponentBinary(indexedDBSrcDir, indexedDBBin, "indexeddb", runtime.GOOS, runtime.GOARCH)
	}()
	go func() {
		defer wg.Done()
		errs[3] = providerpkg.BuildGoComponentBinary(externalCredentialsSrcDir, externalCredentialsBin, string(providermanifestv1.KindExternalCredentials), runtime.GOOS, runtime.GOARCH)
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			fmt.Fprintf(os.Stderr, "build %d: %v\n", i, err)
			_ = os.RemoveAll(tmpDir)
			os.Exit(1)
		}
	}

	defaultProvidersDir, err := writeDefaultProvidersDir(tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write default providers dir: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}
	if err := os.Setenv("GESTALT_PROVIDERS_DIR", defaultProvidersDir); err != nil {
		fmt.Fprintf(os.Stderr, "set GESTALT_PROVIDERS_DIR: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	gestaltCLIBin = buildGestaltCLI()

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func buildGestaltCLI() string {
	if _, err := exec.LookPath("cargo"); err != nil {
		return ""
	}

	repoRoot := filepath.Dir(filepath.Dir(testutil.MustExampleProviderPluginPath()))
	for {
		if _, err := os.Stat(filepath.Join(repoRoot, "gestalt", "Cargo.toml")); err == nil {
			break
		}
		parent := filepath.Dir(repoRoot)
		if parent == repoRoot {
			return ""
		}
		repoRoot = parent
	}

	workspaceDir := filepath.Join(repoRoot, "gestalt")
	cmd := exec.Command("cargo", "build", "-p", "gestalt", "--release")
	cmd.Dir = workspaceDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build gestalt CLI: %v (skipping CLI tests)\n", err)
		return ""
	}

	builtBin := filepath.Join(workspaceDir, "target", "release", "gestalt")
	if _, err := os.Stat(builtBin); err != nil {
		fmt.Fprintf(os.Stderr, "gestalt CLI binary not found at %s\n", builtBin)
		return ""
	}
	return builtBin
}

func buildTarget(dir, target, output string) error {
	return runGo(dir, "build", "-o", output, target)
}

func runGo(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeExternalCredentialsProviderFixture(baseDir string) (string, error) {
	fixtureDir := filepath.Join(baseDir, "external-credentials-fixture")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		return "", err
	}

	exampleDir := testutil.MustExampleProviderPluginPath()
	goModPath := filepath.Join(exampleDir, "go.mod")
	goSumPath := filepath.Join(exampleDir, "go.sum")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}
	root := filepath.Clean(filepath.Join(exampleDir, "..", "..", "..", "..", ".."))
	replaced := strings.Replace(string(goMod), "module github.com/valon-technologies/gestalt/testdata/provider-go", "module github.com/valon-technologies/gestalt/testdata/provider-go-externalcredentials", 1)
	replaced = strings.Replace(replaced, "replace github.com/valon-technologies/gestalt/sdk/go => ../../../../../sdk/go", "replace github.com/valon-technologies/gestalt/sdk/go => "+filepath.Join(root, "sdk", "go"), 1)
	if err := os.WriteFile(filepath.Join(fixtureDir, "go.mod"), []byte(replaced), 0o644); err != nil {
		return "", err
	}

	goSum, err := os.ReadFile(goSumPath)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "go.sum"), goSum, 0o644); err != nil {
		return "", err
	}

	if err := os.WriteFile(filepath.Join(fixtureDir, "external_credentials.go"), []byte(testutil.GeneratedExternalCredentialPackageSource()), 0o644); err != nil {
		return "", err
	}

	return fixtureDir, nil
}

func writeDefaultProvidersDir(baseDir string) (string, error) {
	providersDir := filepath.Join(baseDir, "providers")
	if err := os.MkdirAll(providersDir, 0o755); err != nil {
		return "", err
	}

	if err := writeComponentProviderDir(filepath.Join(providersDir, "indexeddb", "relationaldb"), indexedDBBin, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindIndexedDB,
		Source:      "github.com/test/providers/indexeddb-relationaldb",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Relational IndexedDB",
		Spec:        &providermanifestv1.Spec{},
	}); err != nil {
		return "", err
	}

	if err := writeComponentProviderDir(filepath.Join(providersDir, "external_credentials", "default"), externalCredentialsBin, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindExternalCredentials,
		Source:      "github.com/test/providers/external-credentials-default",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Default External Credentials",
		Spec:        &providermanifestv1.Spec{},
	}); err != nil {
		return "", err
	}
	if err := writeLocalProviderReleaseMetadata(filepath.Join(providersDir, "external_credentials", "default")); err != nil {
		return "", err
	}

	uiDir := filepath.Join(providersDir, "ui", "default")
	distDir := filepath.Join(uiDir, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte(`<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <title>Default Gestalt UI</title>
  </head>
  <body>
    <div id="app">Default Gestalt UI</div>
  </body>
</html>
`), 0o644); err != nil {
		return "", err
	}
	if err := writeManifest(filepath.Join(uiDir, "manifest.yaml"), &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/test/ui/default",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Default Gestalt UI",
		Spec:        &providermanifestv1.Spec{AssetRoot: "dist"},
	}); err != nil {
		return "", err
	}

	return providersDir, nil
}

func writeComponentProviderDir(dir, binaryPath string, manifest *providermanifestv1.Manifest) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, filepath.Base(binaryPath))
	if err := os.WriteFile(dest, data, 0o755); err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	manifestCopy := *manifest
	manifestCopy.Artifacts = []providermanifestv1.Artifact{{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		Path:   filepath.Base(dest),
		SHA256: fmt.Sprintf("%x", sum[:]),
	}}
	manifestCopy.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: filepath.Base(dest)}
	return writeManifest(filepath.Join(dir, "manifest.yaml"), &manifestCopy)
}

func writeManifest(path string, manifest *providermanifestv1.Manifest) error {
	data, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeLocalProviderReleaseMetadata(dir string) error {
	manifestPath := filepath.Join(dir, "manifest.yaml")
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return err
	}

	archivePath := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-provider.tar.gz")
	if err := providerpkg.CreatePackageFromDir(dir, archivePath); err != nil {
		return err
	}
	digest, err := providerpkg.ArchiveDigest(archivePath)
	if err != nil {
		return err
	}

	metadata := map[string]any{
		"schema":        "gestaltd-provider-release",
		"schemaVersion": 1,
		"package":       manifest.Source,
		"kind":          manifest.Kind,
		"version":       manifest.Version,
		"runtime":       "executable",
		"artifacts": map[string]any{
			providerpkg.CurrentPlatformString(): map[string]any{
				"path":   filepath.ToSlash(filepath.Join("..", filepath.Base(archivePath))),
				"sha256": digest,
			},
		},
	}
	data, err := yaml.Marshal(metadata)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "provider-release.yaml"), data, 0o644)
}

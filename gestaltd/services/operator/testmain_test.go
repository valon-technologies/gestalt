package operator

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"gopkg.in/yaml.v3"
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "operator-external-credentials-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}

	sourceDir, err := writeExternalCredentialsProviderFixture(tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write external credentials fixture: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	binaryPath := filepath.Join(tmpDir, "external-credentials-provider")
	if err := providerpkg.BuildGoComponentBinary(sourceDir, binaryPath, string(providermanifestv1.KindExternalCredentials), runtime.GOOS, runtime.GOARCH); err != nil {
		fmt.Fprintf(os.Stderr, "build external credentials fixture: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	providersDir, err := writeDefaultProvidersDir(tmpDir, binaryPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write providers dir: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	if err := os.Setenv("GESTALT_PROVIDERS_DIR", providersDir); err != nil {
		fmt.Fprintf(os.Stderr, "set GESTALT_PROVIDERS_DIR: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
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

func writeDefaultProvidersDir(baseDir, binaryPath string) (string, error) {
	providersDir := filepath.Join(baseDir, "providers")
	dir := filepath.Join(providersDir, "external_credentials", "default")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, filepath.Base(binaryPath))
	if err := os.WriteFile(dest, data, 0o755); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)

	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindExternalCredentials,
		Source:      "github.com/test/providers/external-credentials-default",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Default External Credentials",
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   filepath.Base(dest),
			SHA256: fmt.Sprintf("%x", sum[:]),
		}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: filepath.Base(dest)},
	}
	manifestData, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), manifestData, 0o644); err != nil {
		return "", err
	}
	if err := writeProviderReleaseMetadata(dir, manifest); err != nil {
		return "", err
	}
	return providersDir, nil
}

func writeProviderReleaseMetadata(dir string, manifest *providermanifestv1.Manifest) error {
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

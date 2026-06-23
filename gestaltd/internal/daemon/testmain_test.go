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

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/staticvalidation"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"gopkg.in/yaml.v3"
)

var (
	gestaltdBin            string
	appBin                 string
	indexedDBBin           string
	externalCredentialsBin string
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "gestaltd-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}

	gestaltdBin = filepath.Join(tmpDir, "gestaltd")
	appBin = filepath.Join(tmpDir, "provider")
	indexedDBBin = filepath.Join(tmpDir, "indexeddb-provider")
	externalCredentialsBin = filepath.Join(tmpDir, "external-credentials-provider")
	indexedDBSrcDir := filepath.Join(filepath.Dir(testutil.MustExampleProviderAppPath()), "provider-go-indexeddb")
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
		errs[1] = buildGoFixtureBinary(testutil.MustExampleProviderAppPath(), appBin, "github.com/valon-technologies/gestalt/testdata/provider-go", `gestalt.ServeProvider(ctx, providerpkg.New(), providerpkg.Router.WithName("provider-go"))`)
	}()
	go func() {
		defer wg.Done()
		errs[2] = buildGoFixtureBinary(indexedDBSrcDir, indexedDBBin, "github.com/valon-technologies/gestalt/testdata/provider-go-indexeddb", "gestalt.ServeIndexedDBProvider(ctx, providerpkg.New())")
	}()
	go func() {
		defer wg.Done()
		errs[3] = buildGoFixtureBinary(externalCredentialsSrcDir, externalCredentialsBin, "github.com/valon-technologies/gestalt/testdata/provider-go-externalcredentials", "gestalt.ServeExternalCredentialProvider(ctx, providerpkg.New())")
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

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func buildTarget(dir, target, output string) error {
	return runGo(dir, "build", "-o", output, target)
}

func buildGoFixtureBinary(srcDir, output, importPath, serveCall string) error {
	buildDir, err := os.MkdirTemp(filepath.Dir(output), "go-provider-fixture-*")
	if err != nil {
		return err
	}
	if err := copyTestFixtureTree(srcDir, buildDir); err != nil {
		return err
	}
	goModPath := filepath.Join(buildDir, "go.mod")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		return err
	}
	root := filepath.Clean(filepath.Join(testutil.MustExampleProviderPluginPath(), "..", "..", "..", "..", ".."))
	replaced := strings.Replace(string(goMod), "replace github.com/valon-technologies/gestalt/sdk/go => ../../../../../sdk/go", "replace github.com/valon-technologies/gestalt/sdk/go => "+filepath.Join(root, "sdk", "go"), 1)
	replaced = strings.Replace(replaced, "replace github.com/valon-technologies/gestalt/server/rpc => ../../../../../gestaltd/rpc", "replace github.com/valon-technologies/gestalt/server/rpc => "+filepath.Join(root, "gestaltd", "rpc"), 1)
	if err := os.WriteFile(goModPath, []byte(replaced), 0o644); err != nil {
		return err
	}
	mainDir := filepath.Join(buildDir, "cmd", "provider")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		return err
	}
	mainSource := fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	providerpkg %q
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := %s; err != nil {
		fmt.Fprintf(os.Stderr, "error: %%v\n", err)
		os.Exit(1)
	}
}
`, importPath, serveCall)
	if err := os.WriteFile(filepath.Join(mainDir, "main.go"), []byte(mainSource), 0o644); err != nil {
		return err
	}
	return buildTarget(buildDir, "./cmd/provider", output)
}

func copyTestFixtureTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
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
	replaced = strings.Replace(replaced, "replace github.com/valon-technologies/gestalt/server/rpc => ../../../../../gestaltd/rpc", "replace github.com/valon-technologies/gestalt/server/rpc => "+filepath.Join(root, "gestaltd", "rpc"), 1)
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

	if err := os.WriteFile(filepath.Join(fixtureDir, "externalcredentials.go"), []byte(testutil.GeneratedExternalCredentialPackageSource()), 0o644); err != nil {
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
		Source:      "github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Relational IndexedDB",
		Spec:        &providermanifestv1.Spec{},
	}); err != nil {
		return "", err
	}

	if err := writeComponentProviderDir(filepath.Join(providersDir, "externalcredentials", "default"), externalCredentialsBin, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindExternalCredentials,
		Source:      "github.com/test/providers/external-credentials-default",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Default External Credentials",
		Spec:        &providermanifestv1.Spec{},
	}); err != nil {
		return "", err
	}
	if err := writeLocalProviderReleaseMetadata(filepath.Join(providersDir, "externalcredentials", "default")); err != nil {
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
	if err := os.WriteFile(filepath.Join(uiDir, "build.sh"), []byte("mkdir -p dist\nprintf '<html>Default Gestalt UI</html>\\n' > dist/index.html\n"), 0o755); err != nil {
		return "", err
	}
	if err := writeManifest(filepath.Join(uiDir, "manifest.yaml"), &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/test/ui/default",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Default Gestalt UI",
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: "dist"},
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
	manifestData, err := providerpkg.EncodeManifestFormat(&manifestCopy, providerpkg.ManifestFormatYAML)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.yaml"), manifestData, 0o644)
}

func writeManifest(path string, manifest *providermanifestv1.Manifest) error {
	data, err := encodeTestManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeLocalProviderReleaseMetadata(dir string) error {
	manifestPath := filepath.Join(dir, "manifest.yaml")
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		_, manifest, err = providerpkg.ReadManifestFile(manifestPath)
		if err != nil {
			return err
		}
	} else {
		if manifest.Build != nil && !manifest.Build.PrepareOnly {
			if sourceOutput, err := providerpkg.SourceBuildOutputPath(manifest, runtime.GOOS); err == nil {
				srcPath := filepath.Join(dir, filepath.FromSlash(sourceOutput))
				if _, err := os.Stat(srcPath); err != nil {
					if err := providerpkg.RunSourceBuild(manifestPath, manifest, providerpkg.SourceBuildOptions{}); err != nil {
						return fmt.Errorf("run source build: %w", err)
					}
				}
			}
		}
		manifestCopy := *manifest
		manifestCopy.Build = nil
		manifestCopy.Artifacts = nil
		name, err := providerpkg.SourceNameFromManifest(&manifestCopy)
		if err != nil {
			return err
		}
		packageEntrypoint := providerpkg.PackageExecutablePath(name, runtime.GOOS)
		if sourceOutput, err := providerpkg.SourceBuildOutputPath(manifest, runtime.GOOS); err == nil {
			srcPath := filepath.Join(dir, filepath.FromSlash(sourceOutput))
			dstPath := filepath.Join(dir, filepath.FromSlash(packageEntrypoint))
			if data, err := os.ReadFile(srcPath); err == nil {
				if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(dstPath, data, 0o755); err != nil {
					return err
				}
			}
		}
		artifactPath := packageEntrypoint
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(artifactPath))); err != nil {
			return fmt.Errorf("package artifact %q: %w", artifactPath, err)
		}
		digest, err := providerpkg.FileSHA256(filepath.Join(dir, filepath.FromSlash(artifactPath)))
		if err != nil {
			return err
		}
		manifestCopy.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: artifactPath}
		manifestCopy.Artifacts = []providermanifestv1.Artifact{{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   artifactPath,
			SHA256: digest,
		}}
		manifestData, err := providerpkg.EncodeManifestFormat(&manifestCopy, providerpkg.ManifestFormatYAML)
		if err != nil {
			return err
		}
		if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
			return err
		}
		manifest = &manifestCopy
	}

	archivePath := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-provider.tar.gz")
	if err := providerpkg.CreatePackageFromDir(dir, archivePath); err != nil {
		return err
	}
	digest, err := providerpkg.ArchiveDigest(archivePath)
	if err != nil {
		return err
	}

	staticManifest, err := staticvalidation.ProjectManifest(manifest, "", true)
	if err != nil {
		return err
	}
	var staticCatalog *catalog.Catalog
	if catalogData, err := os.ReadFile(filepath.Join(dir, providerpkg.StaticCatalogFile)); err == nil {
		var cat catalog.Catalog
		if err := yaml.Unmarshal(catalogData, &cat); err != nil {
			return err
		}
		staticCatalog = &cat
	}

	metadata := providerrelease.Metadata{
		Schema:        providerrelease.SchemaName,
		SchemaVersion: providerrelease.SchemaVersion,
		Package:       manifest.Source,
		Kind:          manifest.Kind,
		Version:       manifest.Version,
		Runtime:       providerrelease.RuntimeForManifest(manifest.Kind, staticManifest),
		Artifacts: providerrelease.Artifacts{
			providerpkg.CurrentPlatformString(): {
				Path:   filepath.ToSlash(filepath.Join("..", filepath.Base(archivePath))),
				SHA256: digest,
			},
		},
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: staticManifest,
			Catalog:  staticCatalog,
		},
	}
	if staticCatalog == nil && providerrelease.CatalogSessionModeAllowed(manifest.Kind, staticManifest) {
		metadata.StaticValidation.CatalogSessionOnly = true
	}
	if err := providerrelease.ValidateMetadata(&metadata); err != nil {
		return err
	}
	data, err := yaml.Marshal(metadata)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, providerrelease.MetadataFile), data, 0o644)
}

package daemon

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func setupAppDir(t *testing.T, baseDir string) string {
	t.Helper()
	return setupAppDirWithVersion(t, baseDir, "0.0.1-alpha.1")
}

func localAppSourceRunCommand(artifactRel string) *providermanifestv1.SourceRun {
	return &providermanifestv1.SourceRun{
		Command: []string{"sh", "-c", "sh ./build.sh && ./" + artifactRel},
	}
}

func setupAppDirWithVersion(t *testing.T, baseDir, version string) string {
	t.Helper()

	appDir := filepath.Join(baseDir, "app-src")
	testutil.CopyExampleProviderApp(t, appDir)
	artifactRel := ".gestaltd/bin/provider"
	writeGoAppBuildFixture(t, appDir, "github.com/valon-technologies/gestalt/testdata/provider-go", "example", artifactRel)
	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/test/apps/provider",
		Version:     version,
		DisplayName: "Example Provider",
		Description: "A minimal example provider built with the public SDK",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Run:        localAppSourceRunCommand(artifactRel),
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	}
	writeManifestFile(t, appDir, manifest)
	return appDir
}

func setAppManifestSource(t *testing.T, appDir, source string) {
	t.Helper()

	manifestPath := componentProviderManifestPath(t, appDir)
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", manifestPath, err)
	}
	manifest.Source = source
	writeManifestFile(t, appDir, manifest)
}

func setUIManifestSource(t *testing.T, manifestPath, source string) {
	t.Helper()

	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", manifestPath, err)
	}
	manifest.Source = source
	writeManifestFile(t, filepath.Dir(manifestPath), manifest)
}

func setupAuthProviderDir(t *testing.T, baseDir, name string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "auth", name)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}
	writeTestFile(t, providerDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/providers/auth/"+name)), 0o644)
	writeTestFile(t, providerDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, providerDir, "auth.go", []byte(authProviderSource(name)), 0o644)
	artifactRel := ".gestaltd/bin/" + name
	writeGoComponentBuildFixture(t, providerDir, "example.com/providers/auth/"+name, providermanifestv1.KindIdentity, artifactRel)
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindIdentity,
		Source:      "github.com/test/providers/auth/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Test Auth " + name,
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "auth.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return providerDir
}

func writeGoComponentBuildFixture(t *testing.T, providerDir, importPath, kind, artifactRel string) {
	t.Helper()

	serveCall := goComponentServeCallForTest(t, kind)
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
	writeTestFile(t, providerDir, filepath.Join("cmd", "provider", "main.go"), []byte(mainSource), 0o644)
	buildScript := fmt.Sprintf("mkdir -p %q\ngo build -o %q ./cmd/provider\n", filepath.ToSlash(filepath.Dir(artifactRel)), artifactRel)
	writeTestFile(t, providerDir, "build.sh", []byte(buildScript), 0o755)
}

func writeGoAppBuildFixture(t *testing.T, providerDir, importPath, appName, artifactRel string) {
	t.Helper()

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
	if err := gestalt.ServeProvider(ctx, providerpkg.New(), providerpkg.Router.WithName(%q)); err != nil {
		fmt.Fprintf(os.Stderr, "error: %%v\n", err)
		os.Exit(1)
	}
}
`, importPath, appName)
	writeTestFile(t, providerDir, filepath.Join("cmd", "provider", "main.go"), []byte(mainSource), 0o644)
	buildScript := fmt.Sprintf("mkdir -p %q\ngo build -o %q ./cmd/provider\n", filepath.ToSlash(filepath.Dir(artifactRel)), artifactRel)
	writeTestFile(t, providerDir, "build.sh", []byte(buildScript), 0o755)
}

func goComponentServeCallForTest(t *testing.T, kind string) string {
	t.Helper()
	switch providermanifestv1.NormalizeKind(kind) {
	case providermanifestv1.KindIdentity:
		return "gestalt.ServeIdentityProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindAuthorization:
		return "gestalt.ServeAuthorizationProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindCache:
		return "gestalt.ServeCacheProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindWorkflow:
		return "gestalt.ServeWorkflowProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindExternalCredentials:
		return "gestalt.ServeExternalCredentialProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindSecrets:
		return "gestalt.ServeSecretsProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindIndexedDB:
		return "gestalt.ServeIndexedDBProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindS3:
		return "gestalt.ServeS3Provider(ctx, providerpkg.New())"
	case providermanifestv1.KindAgent:
		return "gestalt.ServeAgentProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindRuntime:
		return "gestalt.ServeRuntimeProvider(ctx, providerpkg.New())"
	default:
		t.Fatalf("unsupported Go component fixture kind %q", kind)
		return ""
	}
}

func authProviderSource(name string) string {
	source := testutil.GeneratedAuthPackageSource()
	displayName := name
	if name != "" {
		displayName = strings.ToUpper(name[:1]) + name[1:]
	}
	source = strings.Replace(source, `Name:        "generated-auth"`, fmt.Sprintf(`Name:        %q`, name), 1)
	source = strings.Replace(source, `DisplayName: "Generated Auth"`, fmt.Sprintf(`DisplayName: %q`, displayName), 1)
	return source
}

func componentProviderManifestPath(t *testing.T, providerDir string) string {
	t.Helper()

	manifestPath, err := providerpkg.FindManifestFile(providerDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", providerDir, err)
	}
	return manifestPath
}

func writeManifestFile(t *testing.T, appDir string, manifest *providermanifestv1.Manifest) {
	t.Helper()
	data, err := encodeTestManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func setupIndexedDBProviderDir(t *testing.T, baseDir string) string {
	t.Helper()
	return setupIndexedDBProviderDirForInstance(t, baseDir, "inmem")
}

func setupIndexedDBProviderDirForInstance(t *testing.T, baseDir, instanceName string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "indexeddb-provider")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}

	data, err := os.ReadFile(indexedDBBin)
	if err != nil {
		t.Fatalf("read indexeddb binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "indexeddb-staging"), data, 0o755); err != nil {
		t.Fatalf("write indexeddb staging binary: %v", err)
	}
	buildOutput := ".gestaltd/bin/relationaldb"
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\ncp indexeddb-staging %s\nchmod +x %s\n", buildOutput, buildOutput)
	if err := os.WriteFile(filepath.Join(providerDir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("write indexeddb build script: %v", err)
	}
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindIndexedDB,
		Source:      "github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Relational IndexedDB",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh", "indexeddb-staging"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: buildOutput},
	})
	_ = instanceName
	return providerDir
}

func setupExternalCredentialsProviderDir(t *testing.T, baseDir string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "external-credentials-provider")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}

	data, err := os.ReadFile(externalCredentialsBin)
	if err != nil {
		t.Fatalf("read external credentials binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "external-credentials-staging"), data, 0o755); err != nil {
		t.Fatalf("write external credentials staging binary: %v", err)
	}
	buildOutput := ".gestaltd/bin/external-credentials-default"
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\ncp external-credentials-staging %s\nchmod +x %s\n", buildOutput, buildOutput)
	if err := os.WriteFile(filepath.Join(providerDir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("write external credentials build script: %v", err)
	}
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindExternalCredentials,
		Source:      "github.com/test/providers/external-credentials-default",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Default External Credentials",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh", "external-credentials-staging"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: buildOutput},
	})
	return providerDir
}

type mountedUITestConfig struct {
	Name         string
	Path         string
	ManifestPath string
}

func setupMountedUIDir(t *testing.T, baseDir string) *mountedUITestConfig {
	t.Helper()
	return setupMountedUIDirWithRoutes(t, baseDir, nil)
}

func setupMountedUIDirWithRoutes(t *testing.T, baseDir string, _ any) *mountedUITestConfig {
	t.Helper()

	return setupMountedUIDirAt(t, filepath.Join(baseDir, "mounted-ui"))
}

func setupMountedUIDirAt(t *testing.T, uiDir string) *mountedUITestConfig {
	t.Helper()

	distDir := filepath.Join(uiDir, "dist")
	assetsDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", assetsDir, err)
	}

	writeTestFile(t, uiDir, filepath.Join("dist", "index.html"), []byte(`<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <title>Roadmap Review UI</title>
  </head>
  <body>
    <div id="app">Roadmap Review UI</div>
    <script type="module" src="assets/app.js"></script>
  </body>
</html>
`), 0o644)
	writeTestFile(t, uiDir, filepath.Join("dist", "assets", "app.js"), []byte(`window.__ROADMAP_REVIEW_UI__ = "ready";
`), 0o644)
	writeTestFile(t, uiDir, "build.sh", []byte("mkdir -p dist/assets\nprintf '<html>Roadmap Review UI</html>\\n' > dist/index.html\nprintf 'window.__ROADMAP_REVIEW_UI__ = \"ready\";\\n' > dist/assets/app.js\n"), 0o755)
	writeManifestFile(t, uiDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/test/ui/roadmap-review",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Roadmap Review UI",
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Spec: &providermanifestv1.Spec{
			AssetRoot: "dist",
		},
	})

	return &mountedUITestConfig{
		Name:         "roadmap_review",
		Path:         "/create-customer-roadmap-review",
		ManifestPath: filepath.Join(uiDir, "manifest.yaml"),
	}
}

func writeServeConfig(t *testing.T, dir string, port int, mountedUI *mountedUITestConfig) string {
	t.Helper()

	indexedDBDir := setupIndexedDBProviderDir(t, dir)
	indexedDBManifest := componentProviderManifestPath(t, indexedDBDir)
	appDir := setupAppDir(t, dir)
	appManifest, err := providerpkg.FindManifestFile(appDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", appDir, err)
	}
	uiBlock := ""
	if mountedUI != nil {
		uiBlock = fmt.Sprintf(`  ui:
    %s:
      source:
        path: %q
      path: %s
`, mountedUI.Name, mountedUI.ManifestPath, mountedUI.Path)
	}

	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v8
server:
  baseUrl: %s
  public:
    port: %d
  encryptionKey: test-serve-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
%sapps:
  example:
    source:
      path: %s
`, e2eLoopbackBaseURL(port), port, indexedDBManifest, uiBlock, appManifest)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func e2eLoopbackBaseURL(port int) string {
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort("127.0.0.1", fmt.Sprint(port)),
	}).String()
}

func TestRunLockSyncLocalProviders(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "gestalt.lock.json")
	artifactsDir := filepath.Join(dir, "artifacts")
	cfgPath := writeServeConfig(t, dir, 0, nil)

	if err := runLock([]string{"--config", cfgPath, "--lockfile", lockPath}); err != nil {
		t.Fatalf("runLock: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lockfile at %s: %v", lockPath, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gestaltd")); !os.IsNotExist(err) {
		t.Fatalf("lock should not prepare final artifacts, got err=%v", err)
	}
	if err := runLock([]string{"--check", "--config", cfgPath, "--lockfile", lockPath}); err != nil {
		t.Fatalf("runLock --check: %v", err)
	}

	err := runSync([]string{"--locked", "--check", "--config", cfgPath, "--lockfile", lockPath, "--artifacts-dir", artifactsDir})
	if err == nil {
		t.Fatal("runSync --check before sync unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "artifacts would be materialized") {
		t.Fatalf("sync --check error should report artifacts would be materialized, got: %v", err)
	}

	if err := runSync([]string{"--locked", "--config", cfgPath, "--lockfile", lockPath, "--artifacts-dir", artifactsDir}); err != nil {
		t.Fatalf("runSync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(artifactsDir, "providers", "example")); !os.IsNotExist(err) {
		t.Fatalf("local source-run apps should not materialize provider artifacts, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(artifactsDir, "indexeddb", "inmem")); err != nil {
		t.Fatalf("expected synced indexeddb artifact: %v", err)
	}

	if err := runSync([]string{"--locked", "--check", "--config", cfgPath, "--lockfile", lockPath, "--artifacts-dir", artifactsDir}); err != nil {
		t.Fatalf("runSync --check after sync: %v", err)
	}
}

func TestRunLockWritesOverrideLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)
	lockPath := filepath.Join(dir, "state", "local", "gestalt.lock.json")

	if err := runLock([]string{"--config", cfgPath, "--lockfile", lockPath}); err != nil {
		t.Fatalf("runLock with --lockfile: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected override lockfile at %s: %v", lockPath, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gestalt.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("default lockfile should not be written, got err=%v", err)
	}
}

func TestRunLockSyncLayeredConfigs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath, overridePath, lockPath, artifactsDir := writeLayeredE2EConfigs(t, dir, 0)

	if err := runLock([]string{"--config", basePath, "--config", overridePath, "--lockfile", lockPath}); err != nil {
		t.Fatalf("runLock with layered configs: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file at %s: %v", lockPath, err)
	}
	if err := runSync([]string{"--locked", "--config", basePath, "--config", overridePath, "--lockfile", lockPath, "--artifacts-dir", artifactsDir}); err != nil {
		t.Fatalf("runSync with layered configs: %v", err)
	}

	env, err := setupBootstrapWithConfigPaths([]string{basePath, overridePath}, lockPath, artifactsDir, true, false)
	if err != nil {
		t.Fatalf("setupBootstrapWithConfigPaths locked layered configs: %v", err)
	}
	defer env.Close()
	if got := env.Config.Server.Providers.Identity; got != "local" {
		t.Fatalf("Server.Providers.Identity = %q, want local", got)
	}
	if _, auth, err := env.Config.SelectedIdentityProvider(); err != nil || auth == nil {
		t.Fatalf("SelectedIdentityProvider = (%#v, %v), want local auth provider", auth, err)
	}
}

func TestRunServeLockedUsesOverrideLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)
	lockPath := filepath.Join(dir, "state", "locked-serve", "gestalt.lock.json")
	if err := runLock([]string{"--config", cfgPath, "--lockfile", lockPath}); err != nil {
		t.Fatalf("runLock with --lockfile: %v", err)
	}
	artifactsDir := filepath.Join(dir, "artifacts")
	if err := runSync([]string{"--locked", "--config", cfgPath, "--lockfile", lockPath, "--artifacts-dir", artifactsDir}); err != nil {
		t.Fatalf("runSync with --lockfile: %v", err)
	}

	env, err := setupBootstrapWithConfigPaths([]string{cfgPath}, lockPath, artifactsDir, true, false)
	if err != nil {
		t.Fatalf("setupBootstrapWithConfigPaths locked with --lockfile: %v", err)
	}
	defer env.Close()
	if env.Config.Apps["example"] == nil {
		t.Fatal(`Apps["example"] = nil`)
	}
	if _, err := os.Stat(filepath.Join(dir, "gestalt.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("default lockfile should not be written, got err=%v", err)
	}
}

func writeLayeredE2EConfigs(t *testing.T, dir string, port int) (string, string, string, string) {
	t.Helper()

	deployDir := filepath.Join(dir, "deploy")
	overrideDir := filepath.Join(deployDir, "overrides")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", overrideDir, err)
	}

	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	externalCredentialsManifest := componentProviderManifestPath(t, setupExternalCredentialsProviderDir(t, dir))
	appManifest := componentProviderManifestPath(t, setupAppDir(t, dir))
	authManifest := componentProviderManifestPath(t, setupAuthProviderDir(t, dir, "local"))

	indexedDBRel, err := filepath.Rel(deployDir, indexedDBManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(indexeddb): %v", err)
	}
	appRel, err := filepath.Rel(deployDir, appManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(app): %v", err)
	}
	authRel, err := filepath.Rel(overrideDir, authManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(auth): %v", err)
	}

	basePath := filepath.Join(deployDir, "base.yaml")
	overridePath := filepath.Join(overrideDir, "local.yaml")
	baseCfg := fmt.Sprintf(`apiVersion: gestaltd.config/v8
server:
  baseUrl: %s
  public:
    port: %d
  encryptionKey: test-layered-e2e-key
  providers:
    indexeddb: inmem
    externalCredentials: default
providers:
  externalCredentials:
    default:
      source:
        path: %s
  indexeddb:
    inmem:
      source:
        path: %s
apps:
  example:
    source:
      path: %s
`, e2eLoopbackBaseURL(port), port, filepath.ToSlash(externalCredentialsManifest), filepath.ToSlash(indexedDBRel), filepath.ToSlash(appRel))
	overrideCfg := fmt.Sprintf(`apiVersion: gestaltd.config/v8
server:
  providers:
    identity: local
  artifactsDir: ../artifacts/local
providers:
  identity:
    local:
      source:
        path: %s
`, filepath.ToSlash(authRel))

	if err := os.WriteFile(basePath, []byte(baseCfg), 0o644); err != nil {
		t.Fatalf("write base config: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte(overrideCfg), 0o644); err != nil {
		t.Fatalf("write override config: %v", err)
	}

	return basePath, overridePath, filepath.Join(deployDir, "gestalt.lock.json"), filepath.Join(deployDir, "artifacts", "local")
}

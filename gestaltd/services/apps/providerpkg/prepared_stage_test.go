package providerpkg

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestStageSourcePreparedInstallDir_BuildsHostBinaryWhenSourcePackageExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const source = "github.com/test/apps/provider"
	buildOutputRel := sourceBuildOutputRel(t, source, runtime.GOOS)
	stagedEntrypoint := stagedExecutableRel("prepared-stage-test", runtime.GOOS)
	buildScript := `mkdir -p .gestaltd/bin
cat > ` + buildOutputRel + ` <<'SH'
#!/bin/sh
if [ -n "$GESTALT_APP_WRITE_CATALOG" ]; then
  printf 'name: provider\noperations:\n  - id: echo\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
SH
chmod +x ` + buildOutputRel + `
`
	mustWriteFile(t, filepath.Join(root, "build.sh"), []byte(buildScript), 0o755)
	mustWriteFile(t, filepath.Join(root, "catalog.sh"), []byte(`#!/bin/sh
set -eu
if [ -n "${GESTALT_APP_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: run_catalog\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
`), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      source,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Example Provider",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Run: sourceRunCommand("./catalog.sh"),
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	staged, err := StageSourcePreparedInstallDir(manifestPath, stagingDir, StageSourcePreparedInstallOptions{
		Kind:    providermanifestv1.KindApp,
		AppName: "prepared-stage-test",
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	})
	if err != nil {
		t.Fatalf("StageSourcePreparedInstallDir: %v", err)
	}

	if staged.Manifest == nil || staged.Manifest.Entrypoint == nil || staged.Manifest.Entrypoint.ArtifactPath != stagedEntrypoint {
		var entrypoint any
		if staged.Manifest != nil {
			entrypoint = staged.Manifest.Entrypoint
		}
		t.Fatalf("staged manifest entrypoint = %#v, want artifact path %q", entrypoint, stagedEntrypoint)
	}
	if staged.Manifest.Build != nil || staged.Manifest.Run != nil {
		t.Fatalf("staged manifest retained source execution metadata: build=%#v run=%#v", staged.Manifest.Build, staged.Manifest.Run)
	}

	stagedBinaryPath := filepath.Join(stagingDir, filepath.FromSlash(stagedEntrypoint))
	data, err := os.ReadFile(stagedBinaryPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", stagedBinaryPath, err)
	}
	if !strings.Contains(string(data), "GESTALT_APP_WRITE_CATALOG") {
		t.Fatalf("staged binary did not come from declared build command")
	}

	catalogData, err := os.ReadFile(filepath.Join(stagingDir, StaticCatalogFile))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", filepath.Join(stagingDir, StaticCatalogFile), err)
	}
	if !strings.Contains(string(catalogData), "echo") || strings.Contains(string(catalogData), "run_catalog") {
		t.Fatalf("staged catalog should come from packaged entrypoint, got: %s", catalogData)
	}
}

func TestStageSourcePreparedInstallDir_ReplacesRunGeneratedCatalog(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const source = "github.com/test/apps/provider"
	buildOutputRel := sourceBuildOutputRel(t, source, runtime.GOOS)
	buildScript := `mkdir -p .gestaltd/bin
cat > ` + buildOutputRel + ` <<'SH'
#!/bin/sh
if [ -n "$GESTALT_APP_WRITE_CATALOG" ]; then
  printf 'name: provider\noperations:\n  - id: packaged_catalog\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
SH
chmod +x ` + buildOutputRel + `
`
	mustWriteFile(t, filepath.Join(root, "build.sh"), []byte(buildScript), 0o755)
	mustWriteFile(t, filepath.Join(root, "run.sh"), []byte(`#!/bin/sh
set -eu
if [ -n "${GESTALT_APP_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: run_catalog\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
`), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  source,
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Run: sourceRunCommand("./run.sh"),
	}))

	if _, _, err := PrepareSourceManifest(manifestPath); err != nil {
		t.Fatalf("PrepareSourceManifest: %v", err)
	}
	sourceCatalog, err := os.ReadFile(filepath.Join(root, StaticCatalogFile))
	if err != nil {
		t.Fatalf("ReadFile(source catalog): %v", err)
	}
	if !strings.Contains(string(sourceCatalog), "run_catalog") {
		t.Fatalf("source catalog = %s, want run-generated catalog", sourceCatalog)
	}

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	if _, err := StageSourcePreparedInstallDir(manifestPath, stagingDir, StageSourcePreparedInstallOptions{
		Kind:    providermanifestv1.KindApp,
		AppName: "prepared-stage-test",
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	}); err != nil {
		t.Fatalf("StageSourcePreparedInstallDir: %v", err)
	}

	stagedCatalog, err := os.ReadFile(filepath.Join(stagingDir, StaticCatalogFile))
	if err != nil {
		t.Fatalf("ReadFile(staged catalog): %v", err)
	}
	if !strings.Contains(string(stagedCatalog), "packaged_catalog") || strings.Contains(string(stagedCatalog), "run_catalog") {
		t.Fatalf("staged catalog should be regenerated from packaged entrypoint, got: %s", stagedCatalog)
	}
}

func TestValidateExplicitRunPackaging_AllowsManifestBackedRunOnlyPlugin(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/test/apps/manifest-backed",
		Version: "0.0.1-alpha.1",
		Run:     sourceRunCommand("npm", "run", "dev"),
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://api.example.com",
					Operations: []providermanifestv1.ProviderOperation{{
						Name:   "get_status",
						Method: "GET",
						Path:   "/status",
					}},
				},
			},
		},
	}
	if ReleaseRequiresBuild(manifest) {
		t.Fatal("manifest-backed app should not require a release build")
	}
	if err := ValidateExplicitRunPackaging(t.TempDir(), manifest); err != nil {
		t.Fatalf("ValidateExplicitRunPackaging: %v", err)
	}
}

func TestStageSourcePreparedInstallDir_BuildsDeclaredGoProvider(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const source = "github.com/test/providers/cache/local-cache"
	const configuredName = "configured-cache"
	mustWriteFile(t, filepath.Join(root, "go.mod"), []byte(testutil.GeneratedProviderModuleSource(t, "example.com/implicit-cache")), 0o644)
	mustWriteFile(t, filepath.Join(root, "go.sum"), testutil.GeneratedProviderModuleSum(t), 0o644)
	mustWriteFile(t, filepath.Join(root, "provider.go"), []byte(testutil.GeneratedCachePackageSource()), 0o644)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindCache,
		Source:      source,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Declared Cache",
		Spec:        &providermanifestv1.Spec{},
		Install: &providermanifestv1.SourceInstall{
			Command: []string{"go", "mod", "download"},
		},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"go", "run", "github.com/valon-technologies/gestalt/sdk/go/cmd/gestalt", "build"},
		},
		Run: sourceRunCommand("go", "run", "github.com/valon-technologies/gestalt/sdk/go/cmd/gestalt", "run"),
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	staged, err := StageSourcePreparedInstallDir(manifestPath, stagingDir, StageSourcePreparedInstallOptions{
		Kind:    providermanifestv1.KindCache,
		AppName: configuredName,
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	})
	if err != nil {
		t.Fatalf("StageSourcePreparedInstallDir: %v", err)
	}

	stagedEntrypoint := stagedExecutableRel(configuredName, runtime.GOOS)
	if staged.Manifest == nil || staged.Manifest.Entrypoint == nil || staged.Manifest.Entrypoint.ArtifactPath != stagedEntrypoint {
		var entrypoint any
		if staged.Manifest != nil {
			entrypoint = staged.Manifest.Entrypoint
		}
		t.Fatalf("staged manifest entrypoint = %#v, want artifact path %q", entrypoint, stagedEntrypoint)
	}
	if staged.Manifest.Build != nil || staged.Manifest.Run != nil {
		t.Fatalf("staged manifest retained source execution metadata: build=%#v run=%#v", staged.Manifest.Build, staged.Manifest.Run)
	}
	if _, err := os.Stat(filepath.Join(stagingDir, filepath.FromSlash(stagedEntrypoint))); err != nil {
		t.Fatalf("staged declared Go executable: %v", err)
	}
}

func TestValidatePreparedInstallDeclaredBuild_RequiresArtifactWhenReleaseBuildRequired(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindAgent,
		Source:  "github.com/test/providers/example-agent",
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
		},
	}
	err := validatePreparedInstallDeclaredBuild(root, manifest, providermanifestv1.KindAgent)
	if err == nil || !strings.Contains(err.Error(), "declare object-form build.command") {
		t.Fatalf("validatePreparedInstallDeclaredBuild err = %v, want missing declared build error", err)
	}
}

func TestSourceRunCommand(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsOS {
		t.Skip("fake uv fixture uses POSIX shell")
	}

	tests := []struct {
		name string
		run  func(t *testing.T, root, fakeUVPath, logPath string)
	}{
		{
			name: "source execution uses run instead of entrypoint",
			run: func(t *testing.T, root, fakeUVPath, logPath string) {
				mustWriteFile(t, filepath.Join(root, "provider.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
				manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
					Kind:    providermanifestv1.KindApp,
					Source:  "github.com/test/apps/uv-provider",
					Version: "0.0.1-alpha.1",
					Build: &providermanifestv1.SourceBuild{
						Command:     []string{fakeUVPath, "sync", "--frozen", "--no-install-project"},
						PrepareOnly: true,
					},
					Run:  sourceRunCommand(fakeUVPath, "run", "--frozen", "./provider.sh", "--dev"),
					Spec: &providermanifestv1.Spec{},
				}))

				execution, err := SourceManifestExecution(manifestPath, providermanifestv1.KindApp, SourceBuildOptions{})
				if err != nil {
					t.Fatalf("SourceManifestExecution: %v", err)
				}
				if execution.Command != fakeUVPath {
					t.Fatalf("execution.Command = %q, want %q", execution.Command, fakeUVPath)
				}
				wantArgs := []string{"run", "--frozen", "./provider.sh", "--dev"}
				if !slices.Equal(execution.Args, wantArgs) {
					t.Fatalf("execution.Args = %#v, want %#v", execution.Args, wantArgs)
				}
				if execution.Workdir != root {
					t.Fatalf("execution.Workdir = %q, want %q", execution.Workdir, root)
				}
				assertLogContains(t, logPath, root+"|sync --frozen --no-install-project")
			},
		},
		{
			name: "prepare runs before static catalog generation",
			run: func(t *testing.T, root, fakeUVPath, logPath string) {
				mustWriteFile(t, filepath.Join(root, "provider.sh"), []byte(`#!/bin/sh
set -eu
if [ -n "${GESTALT_APP_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: uv_catalog\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
`), 0o755)
				manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
					Kind:    providermanifestv1.KindApp,
					Source:  "github.com/test/apps/uv-provider",
					Version: "0.0.1-alpha.1",
					Build: &providermanifestv1.SourceBuild{
						Command:     []string{fakeUVPath, "sync", "--frozen", "--no-install-project"},
						PrepareOnly: true,
					},
					Run:  sourceRunCommand(fakeUVPath, "run", "--frozen", "./provider.sh"),
					Spec: &providermanifestv1.Spec{},
				}))

				if _, _, err := PrepareSourceManifest(manifestPath); err != nil {
					t.Fatalf("PrepareSourceManifest: %v", err)
				}

				catalogData, err := os.ReadFile(filepath.Join(root, StaticCatalogFile))
				if err != nil {
					t.Fatalf("ReadFile(%s): %v", filepath.Join(root, StaticCatalogFile), err)
				}
				if !strings.Contains(string(catalogData), "uv_catalog") {
					t.Fatalf("catalog = %q, want generated catalog", catalogData)
				}

				logText := readLog(t, logPath)
				syncLine := root + "|sync --frozen --no-install-project"
				runLine := root + "|run --frozen ./provider.sh"
				syncIdx := strings.Index(logText, syncLine)
				runIdx := strings.Index(logText, runLine)
				if syncIdx < 0 || runIdx < 0 || syncIdx > runIdx {
					t.Fatalf("uv log = %q, want sync before catalog run", logText)
				}
			},
		},
		{
			name: "local execution skips output build",
			run: func(t *testing.T, root, fakeUVPath, logPath string) {
				mustWriteFile(t, filepath.Join(root, "provider.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
				mustWriteFile(t, filepath.Join(root, "fail-build.sh"), []byte("#!/bin/sh\nexit 42\n"), 0o755)
				manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
					Kind:    providermanifestv1.KindApp,
					Source:  "github.com/test/apps/uv-provider",
					Version: "0.0.1-alpha.1",
					Build: &providermanifestv1.SourceBuild{
						Command: []string{"./fail-build.sh"},
					},
					Run:  sourceRunCommand("./provider.sh"),
					Spec: &providermanifestv1.Spec{},
				}))

				execution, err := SourceManifestExecution(manifestPath, providermanifestv1.KindApp, SourceBuildOptions{})
				if err != nil {
					t.Fatalf("SourceManifestExecution: %v", err)
				}
				if execution.Command != "./provider.sh" || len(execution.Args) != 0 {
					t.Fatalf("execution = %#v, want explicit run command", execution)
				}
				if _, err := os.Stat(logPath); err == nil {
					t.Fatalf("unexpected fake uv log; output build or prep command ran")
				} else if !os.IsNotExist(err) {
					t.Fatalf("stat fake uv log: %v", err)
				}
			},
		},
		{
			name: "run-only app does not need SDK metadata",
			run: func(t *testing.T, root, fakeUVPath, logPath string) {
				mustWriteFile(t, filepath.Join(root, "provider.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
				manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
					Kind:    providermanifestv1.KindApp,
					Source:  "github.com/test/apps/run-only",
					Version: "0.0.1-alpha.1",
					Run:     sourceRunCommand("./provider.sh"),
					Spec:    &providermanifestv1.Spec{},
				}))

				execution, err := SourceManifestExecution(manifestPath, providermanifestv1.KindApp, SourceBuildOptions{})
				if err != nil {
					t.Fatalf("SourceManifestExecution: %v", err)
				}
				if execution.Command != "./provider.sh" || len(execution.Args) != 0 {
					t.Fatalf("execution = %#v, want explicit run command", execution)
				}
				if _, err := os.Stat(logPath); err == nil {
					t.Fatalf("unexpected fake uv log; SDK detection should not run commands")
				} else if !os.IsNotExist(err) {
					t.Fatalf("stat fake uv log: %v", err)
				}
			},
		},
		{
			name: "run-only component provider",
			run: func(t *testing.T, root, fakeUVPath, logPath string) {
				mustWriteFile(t, filepath.Join(root, "auth.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
				manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
					Kind:    providermanifestv1.KindIdentity,
					Source:  "github.com/test/providers/auth",
					Version: "0.0.1-alpha.1",
					Run:     sourceRunCommand("./auth.sh", "--serve"),
					Spec:    &providermanifestv1.Spec{},
				}))

				execution, err := SourceManifestExecution(manifestPath, providermanifestv1.KindIdentity, SourceBuildOptions{})
				if err != nil {
					t.Fatalf("SourceManifestExecution: %v", err)
				}
				if execution.Command != "./auth.sh" || !slices.Equal(execution.Args, []string{"--serve"}) {
					t.Fatalf("execution = %#v, want explicit run command", execution)
				}
				if _, err := os.Stat(logPath); err == nil {
					t.Fatalf("unexpected fake uv log; SDK detection should not run commands")
				} else if !os.IsNotExist(err) {
					t.Fatalf("stat fake uv log: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			fakeUVPath, logPath := writeFakeUV(t, root)
			tc.run(t, root, fakeUVPath, logPath)
		})
	}
}

func TestStageSourcePreparedInstallDir_GeneratesCatalogBeforeTargetBuild(t *testing.T) {
	t.Parallel()

	targetGOOS, targetGOARCH := "darwin", "amd64"
	if runtime.GOOS == targetGOOS && runtime.GOARCH == targetGOARCH {
		targetGOOS, targetGOARCH = "linux", "amd64"
	}

	root := t.TempDir()
	const source = "github.com/test/apps/provider"
	buildOutputRel := sourceBuildOutputRel(t, source, targetGOOS)
	hostBuildOutputRel := sourceBuildOutputRel(t, source, runtime.GOOS)
	hostPlatform := runtime.GOOS + "/" + runtime.GOARCH
	buildScript := `set -eu
mkdir -p .gestaltd/bin
if [ "${GOOS:-}/` + "${GOARCH:-}" + `" = "` + hostPlatform + `" ]; then
  cat > ` + hostBuildOutputRel + ` <<'SH'
#!/bin/sh
if [ -n "$GESTALT_APP_WRITE_CATALOG" ]; then
  printf 'name: provider\noperations:\n  - id: host_catalog\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
SH
  chmod +x ` + hostBuildOutputRel + `
else
  cat > ` + buildOutputRel + ` <<'SH'
#!/bin/sh
if [ -n "$GESTALT_APP_WRITE_CATALOG" ]; then
  echo "target artifact should not generate catalogs" >&2
  exit 42
fi
echo target artifact
SH
  chmod +x ` + buildOutputRel + `
fi
`
	mustWriteFile(t, filepath.Join(root, "build.sh"), []byte(buildScript), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      source,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Example Provider",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	appName := "prepared-stage-test"
	_, err := StageSourcePreparedInstallDir(manifestPath, stagingDir, StageSourcePreparedInstallOptions{
		Kind:    providermanifestv1.KindApp,
		AppName: appName,
		GOOS:    targetGOOS,
		GOARCH:  targetGOARCH,
	})
	if err != nil {
		t.Fatalf("StageSourcePreparedInstallDir: %v", err)
	}

	catalogData, err := os.ReadFile(filepath.Join(stagingDir, StaticCatalogFile))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", filepath.Join(stagingDir, StaticCatalogFile), err)
	}
	if !strings.Contains(string(catalogData), "host_catalog") {
		t.Fatalf("staged catalog was not generated by host artifact: %s", catalogData)
	}

	stagedBinary, err := os.ReadFile(filepath.Join(stagingDir, filepath.FromSlash(stagedExecutableRel(appName, targetGOOS))))
	if err != nil {
		t.Fatalf("ReadFile(staged binary): %v", err)
	}
	if !strings.Contains(string(stagedBinary), "target artifact") {
		t.Fatalf("staged binary did not come from target build: %s", stagedBinary)
	}
}

func TestSourcePackagingPreparation_ReusesHostWorkAndStaticAssetsAcrossTargets(t *testing.T) {
	t.Parallel()

	targetGOOS, targetGOARCH := "linux", "amd64"
	if runtime.GOOS == targetGOOS && runtime.GOARCH == targetGOARCH {
		targetGOOS, targetGOARCH = "darwin", "arm64"
	}

	root := t.TempDir()
	const source = "github.com/test/apps/provider"
	hostOutputRel := sourceBuildOutputRel(t, source, runtime.GOOS)
	targetOutputRel := sourceBuildOutputRel(t, source, targetGOOS)
	logPath := filepath.Join(root, "phases.log")
	buildScript := `#!/bin/sh
set -eu
printf 'build:%s\n' "$GESTALT_TARGET_PLATFORM" >> ` + shellQuote(logPath) + `
mkdir -p .gestaltd/bin
if [ "$GESTALT_TARGET_OS" = "` + targetGOOS + `" ]; then
  output=` + shellQuote(targetOutputRel) + `
else
  output=` + shellQuote(hostOutputRel) + `
fi
cat > "$output" <<'SH'
#!/bin/sh
if [ -n "${GESTALT_APP_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: packaged_catalog\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
SH
printf '# target:%s\n' "$GESTALT_TARGET_PLATFORM" >> "$output"
chmod +x "$output"
`
	uiScript := `#!/bin/sh
set -eu
printf 'ui:%s\n' "$GESTALT_TARGET_PLATFORM" >> ` + shellQuote(logPath) + `
mkdir -p "$GESTALT_BUILD_STATIC"
printf '<html>%s</html>\n' "$GESTALT_TARGET_PLATFORM" > "$GESTALT_BUILD_STATIC/index.html"
`
	mustWriteFile(t, filepath.Join(root, "install.sh"), []byte("#!/bin/sh\nprintf 'install\\n' >> "+shellQuote(logPath)+"\n"), 0o755)
	mustWriteFile(t, filepath.Join(root, "build.sh"), []byte(buildScript), 0o755)
	mustWriteFile(t, filepath.Join(root, "ui.sh"), []byte(uiScript), 0o755)
	mustWriteFile(t, filepath.Join(root, "run.sh"), []byte(`#!/bin/sh
if [ -n "${GESTALT_APP_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: run_catalog\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
`), 0o755)
	mustWriteFile(t, filepath.Join(root, StaticCatalogFile), []byte("name: provider\noperations:\n  - id: stale_catalog\n    method: POST\n"), 0o644)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  source,
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Install: &providermanifestv1.SourceInstall{
			Command: []string{"sh", "./install.sh"},
		},
		Build: &providermanifestv1.SourceBuild{
			Commands: []providermanifestv1.SourcePhaseCommand{
				{Command: []string{"sh", "./build.sh"}},
				{Command: []string{"sh", "./ui.sh"}, Workdir: "."},
			},
		},
		Run: sourceRunCommand("./run.sh"),
	}))

	preparation, err := PrepareSourcePackaging(manifestPath, CommandOutput{})
	if err != nil {
		t.Fatalf("PrepareSourcePackaging: %v", err)
	}
	defer func() {
		if err := preparation.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	targetStage := filepath.Join(t.TempDir(), "target")
	if _, err := StageSourcePreparedInstallDir(manifestPath, targetStage, StageSourcePreparedInstallOptions{
		AppName:     "prepared-stage-test",
		GOOS:        targetGOOS,
		GOARCH:      targetGOARCH,
		Preparation: preparation,
	}); err != nil {
		t.Fatalf("stage target: %v", err)
	}
	hostStage := filepath.Join(t.TempDir(), "host")
	if _, err := StageSourcePreparedInstallDir(manifestPath, hostStage, StageSourcePreparedInstallOptions{
		AppName:     "prepared-stage-test",
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		Preparation: preparation,
	}); err != nil {
		t.Fatalf("stage host: %v", err)
	}

	logText := readLog(t, logPath)
	if strings.Count(logText, "install\n") != 1 {
		t.Fatalf("phase log = %q, want one install", logText)
	}
	expectedHostBuilds := 1
	hostTargetOpts := SourceBuildOptions{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if HostLibC() != effectiveTargetLibC(hostTargetOpts) {
		// Linux catalog generation uses a host-libc executable, while the
		// packaged host target defaults to musl and needs its own build.
		expectedHostBuilds = 2
	}
	if got, want := strings.Count(logText, "build:"), expectedHostBuilds+1; got != want {
		t.Fatalf("phase log = %q, build count = %d, want %d", logText, got, want)
	}
	if got := strings.Count(logText, "build:"+runtime.GOOS+"/"+runtime.GOARCH+"\n"); got != expectedHostBuilds {
		t.Fatalf("phase log = %q, host build count = %d, want %d", logText, got, expectedHostBuilds)
	}
	if strings.Count(logText, "build:"+targetGOOS+"/"+targetGOARCH+"\n") != 1 {
		t.Fatalf("phase log = %q, want exactly one target build", logText)
	}
	if strings.Count(logText, "ui:") != 1 {
		t.Fatalf("phase log = %q, want one shared UI build", logText)
	}

	targetBinary := mustReadFile(t, filepath.Join(targetStage, filepath.FromSlash(stagedExecutableRel("prepared-stage-test", targetGOOS))))
	if !strings.Contains(string(targetBinary), "# target:"+targetGOOS+"/"+targetGOARCH) {
		t.Fatalf("target executable = %q, want target platform marker", targetBinary)
	}
	hostBinary := mustReadFile(t, filepath.Join(hostStage, filepath.FromSlash(stagedExecutableRel("prepared-stage-test", runtime.GOOS))))
	if !strings.Contains(string(hostBinary), "# target:"+runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("host executable = %q, want host platform marker", hostBinary)
	}

	targetCatalog := mustReadFile(t, filepath.Join(targetStage, StaticCatalogFile))
	hostCatalog := mustReadFile(t, filepath.Join(hostStage, StaticCatalogFile))
	if !slices.Equal(targetCatalog, hostCatalog) || !strings.Contains(string(targetCatalog), "packaged_catalog") {
		t.Fatalf("staged catalogs differ or are stale: target=%q host=%q", targetCatalog, hostCatalog)
	}
	targetStatic := mustReadFile(t, filepath.Join(targetStage, "static", "index.html"))
	hostStatic := mustReadFile(t, filepath.Join(hostStage, "static", "index.html"))
	if !slices.Equal(targetStatic, hostStatic) || !strings.Contains(string(targetStatic), runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("staged static assets differ or are not host-prepared: target=%q host=%q", targetStatic, hostStatic)
	}
}

func TestStageSourcePreparedInstallDir_RunOnlyFailsReleasePackaging(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "provider.sh"), []byte(`#!/bin/sh
set -eu
if [ -n "${GESTALT_APP_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: local_only\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
`), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/test/apps/run-only",
		Version: "0.0.1-alpha.1",
		Run:     sourceRunCommand("./provider.sh"),
		Spec:    &providermanifestv1.Spec{},
	}))

	_, err := StageSourcePreparedInstallDir(manifestPath, filepath.Join(t.TempDir(), "prepared"), StageSourcePreparedInstallOptions{
		Kind:    providermanifestv1.KindApp,
		AppName: "run-only",
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	})
	if err == nil || !strings.Contains(err.Error(), "run is local-only and cannot be packaged") {
		t.Fatalf("StageSourcePreparedInstallDir error = %v, want local-only run release error", err)
	}
}

func writeFakeUV(t *testing.T, root string) (string, string) {
	t.Helper()

	logPath := filepath.Join(root, "uv.log")
	uvPath := filepath.Join(root, "uv")
	mustWriteFile(t, uvPath, []byte(`#!/bin/sh
set -eu
printf '%s|%s\n' "$PWD" "$*" >> `+shellQuote(logPath)+`
if [ "$#" -gt 0 ] && [ "$1" = "sync" ]; then
  exit 0
fi
if [ "$#" -gt 0 ] && [ "$1" = "run" ]; then
  shift
  while [ "$#" -gt 0 ] && [ "$1" = "--frozen" ]; do
    shift
  done
  exec "$@"
fi
echo "unexpected uv invocation: $*" >&2
exit 64
`), 0o755)
	return uvPath, logPath
}

func assertLogContains(t *testing.T, logPath, want string) {
	t.Helper()

	if got := readLog(t, logPath); !strings.Contains(got, want) {
		t.Fatalf("uv log = %q, want to contain %q", got, want)
	}
}

func readLog(t *testing.T, logPath string) string {
	t.Helper()

	logData := mustReadFile(t, logPath)
	return string(logData)
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return data
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func TestStagePreparedInstallDir_CopiesGeneratedCatalog(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const source = "github.com/test/apps/provider"
	buildOutputRel := sourceBuildOutputRel(t, source, runtime.GOOS)
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(buildOutputRel)), []byte(`#!/bin/sh
if [ -n "$GESTALT_APP_WRITE_CATALOG" ]; then
  printf 'name: provider\noperations:\n  - id: echo\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
`), 0o755)

	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      source,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Example Provider",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "-c", "exit 0"},
		},
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	_, err := StagePreparedInstallDir(manifestPath, stagingDir, StagePreparedInstallOptions{
		AppName: "prepared-stage-test",
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	})
	if err != nil {
		t.Fatalf("StagePreparedInstallDir: %v", err)
	}

	catalogData, err := os.ReadFile(filepath.Join(stagingDir, StaticCatalogFile))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", filepath.Join(stagingDir, StaticCatalogFile), err)
	}
	if len(catalogData) == 0 {
		t.Fatal("staged catalog.yaml is empty")
	}
}

func TestStageSourcePreparedInstallDir_RunsInstallBeforeBuild(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const source = "github.com/test/apps/install-phase"
	buildOutputRel := sourceBuildOutputRel(t, source, runtime.GOOS)
	stagedEntrypoint := stagedExecutableRel("install-phase-test", runtime.GOOS)
	installScript := `#!/bin/sh
set -eu
printf 'install-marker\n' > install-ran.txt
`
	buildScript := `#!/bin/sh
set -eu
test -f install-ran.txt
mkdir -p .gestaltd/bin
cat > ` + buildOutputRel + ` <<'SH'
#!/bin/sh
if [ -n "$GESTALT_APP_WRITE_CATALOG" ]; then
  printf 'name: provider\noperations:\n  - id: echo\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
SH
chmod +x ` + buildOutputRel + `
`
	mustWriteFile(t, filepath.Join(root, "install.sh"), []byte(installScript), 0o755)
	mustWriteFile(t, filepath.Join(root, "build.sh"), []byte(buildScript), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      source,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Install Phase",
		Spec:        &providermanifestv1.Spec{},
		Install: &providermanifestv1.SourceInstall{
			Command: []string{"sh", "./install.sh"},
			Inputs:  []string{"install.sh"},
		},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh", "install-ran.txt"},
		},
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	staged, err := StageSourcePreparedInstallDir(manifestPath, stagingDir, StageSourcePreparedInstallOptions{
		Kind:    providermanifestv1.KindApp,
		AppName: "install-phase-test",
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	})
	if err != nil {
		t.Fatalf("StageSourcePreparedInstallDir: %v", err)
	}

	if staged.Manifest == nil || staged.Manifest.Install != nil {
		t.Fatalf("staged manifest should strip install: %#v", staged.Manifest)
	}

	stagedBinaryPath := filepath.Join(stagingDir, filepath.FromSlash(stagedEntrypoint))
	if _, err := os.Stat(stagedBinaryPath); err != nil {
		t.Fatalf("staged binary not produced: %v", err)
	}
}

func TestRunSourceInstall_NoopWithoutInstall(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/test/apps/no-install",
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "-c", "exit 0"},
		},
	}))
	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile: %v", err)
	}
	if err := RunSourceInstall(manifestPath, manifest, SourceBuildOptions{}); err != nil {
		t.Fatalf("RunSourceInstall with nil install should be a no-op, got: %v", err)
	}
}

func TestSourceManifestExecution_RunsInstallBeforeBuild(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsOS {
		t.Skip("install marker fixture uses POSIX shell")
	}

	root := t.TempDir()
	const source = "github.com/test/apps/exec-install-phase"
	buildOutputRel := sourceBuildOutputRel(t, source, runtime.GOOS)
	installScript := `#!/bin/sh
set -eu
printf 'install-marker\n' > install-ran.txt
`
	buildScript := `#!/bin/sh
set -eu
test -f install-ran.txt
mkdir -p .gestaltd/bin
printf '#!/bin/sh\nexit 0\n' > ` + buildOutputRel + `
chmod +x ` + buildOutputRel + `
`
	mustWriteFile(t, filepath.Join(root, "install.sh"), []byte(installScript), 0o755)
	mustWriteFile(t, filepath.Join(root, "build.sh"), []byte(buildScript), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  source,
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Install: &providermanifestv1.SourceInstall{
			Command: []string{"sh", "./install.sh"},
			Inputs:  []string{"install.sh"},
		},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh", "install-ran.txt"},
		},
	}))

	if _, err := SourceManifestExecution(manifestPath, providermanifestv1.KindApp, SourceBuildOptions{}); err != nil {
		t.Fatalf("SourceManifestExecution: %v (install should run before build)", err)
	}
}

func TestStageSourcePreparedInstallDir_StagesStaticBundle(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == windowsOS {
		t.Skip("POSIX shell fixture")
	}

	root := t.TempDir()
	const source = "github.com/test/apps/static-frontend"
	buildOutputRel := sourceBuildOutputRel(t, source, runtime.GOOS)
	mustWriteFile(t, filepath.Join(root, "build.sh"), []byte(`#!/bin/sh
set -eu
out="`+buildOutputRel+`"
mkdir -p "$(dirname "$out")"
cat > "$out" <<'SH'
#!/bin/sh
if [ -n "$GESTALT_APP_WRITE_CATALOG" ]; then
  printf 'name: provider\noperations:\n  - id: echo\n    method: POST\n' > "$GESTALT_APP_WRITE_CATALOG"
fi
SH
chmod +x "$out"
mkdir -p "$GESTALT_BUILD_STATIC"
printf '<html>alpha</html>\n' > "$GESTALT_BUILD_STATIC/index.html"
`), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", []byte(`
kind: app
source: github.com/test/apps/static-frontend
version: 0.0.1-alpha.1
build:
  - [sh, ./build.sh]
run:
  command: [sh, -c, "true"]
spec: {}
`))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	staged, err := StageSourcePreparedInstallDir(manifestPath, stagingDir, StageSourcePreparedInstallOptions{
		Kind:    providermanifestv1.KindApp,
		AppName: "alpha",
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	})
	if err != nil {
		t.Fatalf("StageSourcePreparedInstallDir: %v", err)
	}
	indexPath := filepath.Join(stagingDir, "static", "index.html")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", indexPath, err)
	}
	if !strings.Contains(string(data), "alpha") {
		t.Fatalf("static index = %q", data)
	}
	if staged.Manifest == nil || staged.Manifest.Spec == nil || staged.Manifest.Spec.AssetRoot != "static" {
		t.Fatalf("staged manifest spec.assetRoot = %#v, want static", staged.Manifest)
	}
}

func TestStageSourcePreparedInstallDir_SerialBuildAbortOnFailure(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == windowsOS {
		t.Skip("POSIX shell fixture")
	}

	root := t.TempDir()
	logPath := filepath.Join(root, "build.log")
	mustWriteFile(t, filepath.Join(root, "step.sh"), []byte(`#!/bin/sh
set -eu
step="$1"
echo "$step" >> build.log
if [ "$step" = "two" ] && [ "${FAIL_STEP_TWO:-}" = "1" ]; then
  exit 1
fi
`), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", []byte(`
kind: app
source: github.com/test/apps/serial-build
version: 0.0.1-alpha.1
build:
  - [sh, ./step.sh, one]
  - [sh, ./step.sh, two]
  - [sh, ./step.sh, three]
run:
  command: [sh, -c, "true"]
spec: {}
`))
	if err := os.Setenv("FAIL_STEP_TWO", "1"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("FAIL_STEP_TWO") })

	_, err := StageSourcePreparedInstallDir(manifestPath, filepath.Join(t.TempDir(), "prepared"), StageSourcePreparedInstallOptions{
		Kind: providermanifestv1.KindApp,
	})
	if err == nil {
		t.Fatal("StageSourcePreparedInstallDir: want failure on step two")
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(build.log): %v", err)
	}
	got := string(logData)
	if !strings.Contains(got, "one") || strings.Contains(got, "three") {
		t.Fatalf("build.log = %q, want one without three after abort", got)
	}
}

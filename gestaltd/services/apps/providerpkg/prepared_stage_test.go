package providerpkg

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestStageSourcePreparedInstallDir_BuildsHostBinaryWhenSourcePackageExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	artifactPath := ".gestalt/build/provider"
	buildScript := `mkdir -p .gestalt/build
cat > .gestalt/build/provider <<'SH'
#!/bin/sh
if [ -n "$GESTALT_PLUGIN_WRITE_CATALOG" ]; then
  printf 'name: provider\noperations:\n  - id: echo\n    method: POST\n' > "$GESTALT_PLUGIN_WRITE_CATALOG"
fi
SH
chmod +x .gestalt/build/provider
`
	mustWriteFile(t, filepath.Join(root, "build.sh"), []byte(buildScript), 0o755)
	mustWriteFile(t, filepath.Join(root, "catalog.sh"), []byte(`#!/bin/sh
set -eu
if [ -n "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: run_catalog\n    method: POST\n' > "$GESTALT_PLUGIN_WRITE_CATALOG"
fi
`), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/test/apps/provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Example Provider",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Run:        []string{"./catalog.sh"},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	staged, err := StageSourcePreparedInstallDir(manifestPath, stagingDir, StageSourcePreparedInstallOptions{
		Kind:       providermanifestv1.KindApp,
		AppName: "prepared-stage-test",
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
	})
	if err != nil {
		t.Fatalf("StageSourcePreparedInstallDir: %v", err)
	}

	if staged.Manifest == nil || staged.Manifest.Entrypoint == nil || staged.Manifest.Entrypoint.ArtifactPath != artifactPath {
		var entrypoint any
		if staged.Manifest != nil {
			entrypoint = staged.Manifest.Entrypoint
		}
		t.Fatalf("staged manifest entrypoint = %#v, want artifact path %q", entrypoint, artifactPath)
	}
	if staged.Manifest.Build != nil || staged.Manifest.Run != nil {
		t.Fatalf("staged manifest retained source execution metadata: build=%#v run=%#v", staged.Manifest.Build, staged.Manifest.Run)
	}

	stagedBinaryPath := filepath.Join(stagingDir, filepath.FromSlash(artifactPath))
	data, err := os.ReadFile(stagedBinaryPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", stagedBinaryPath, err)
	}
	if !strings.Contains(string(data), "GESTALT_PLUGIN_WRITE_CATALOG") {
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
	artifactPath := ".gestalt/build/provider"
	buildScript := `mkdir -p .gestalt/build
cat > .gestalt/build/provider <<'SH'
#!/bin/sh
if [ -n "$GESTALT_PLUGIN_WRITE_CATALOG" ]; then
  printf 'name: provider\noperations:\n  - id: packaged_catalog\n    method: POST\n' > "$GESTALT_PLUGIN_WRITE_CATALOG"
fi
SH
chmod +x .gestalt/build/provider
`
	mustWriteFile(t, filepath.Join(root, "build.sh"), []byte(buildScript), 0o755)
	mustWriteFile(t, filepath.Join(root, "run.sh"), []byte(`#!/bin/sh
set -eu
if [ -n "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: run_catalog\n    method: POST\n' > "$GESTALT_PLUGIN_WRITE_CATALOG"
fi
`), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/test/apps/provider",
		Version: "0.0.1-alpha.1",
		Spec:    &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Run:        []string{"./run.sh"},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
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
		Kind:       providermanifestv1.KindApp,
		AppName: "prepared-stage-test",
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
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
		Run:     []string{"npm", "run", "dev"},
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
					Run:        []string{fakeUVPath, "run", "--frozen", "./provider.sh", "--dev"},
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "provider.sh", Args: []string{"--serve"}},
					Spec:       &providermanifestv1.Spec{},
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
if [ -n "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: uv_catalog\n    method: POST\n' > "$GESTALT_PLUGIN_WRITE_CATALOG"
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
					Run:        []string{fakeUVPath, "run", "--frozen", "./provider.sh"},
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "provider.sh"},
					Spec:       &providermanifestv1.Spec{},
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
					Run:        []string{"./provider.sh"},
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: ".gestalt/build/provider"},
					Spec:       &providermanifestv1.Spec{},
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
					Run:     []string{"./provider.sh"},
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
					Kind:    providermanifestv1.KindAuthentication,
					Source:  "github.com/test/providers/auth",
					Version: "0.0.1-alpha.1",
					Run:     []string{"./auth.sh", "--serve"},
					Spec:    &providermanifestv1.Spec{},
				}))

				execution, err := SourceManifestExecution(manifestPath, providermanifestv1.KindAuthentication, SourceBuildOptions{})
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
	artifactPath := ".gestalt/build/provider"
	hostPlatform := runtime.GOOS + "/" + runtime.GOARCH
	buildScript := `set -eu
mkdir -p .gestalt/build
if [ "${GOOS:-}/` + "${GOARCH:-}" + `" = "` + hostPlatform + `" ]; then
  cat > .gestalt/build/provider <<'SH'
#!/bin/sh
if [ -n "$GESTALT_PLUGIN_WRITE_CATALOG" ]; then
  printf 'name: provider\noperations:\n  - id: host_catalog\n    method: POST\n' > "$GESTALT_PLUGIN_WRITE_CATALOG"
fi
SH
else
  cat > .gestalt/build/provider <<'SH'
#!/bin/sh
if [ -n "$GESTALT_PLUGIN_WRITE_CATALOG" ]; then
  echo "target artifact should not generate catalogs" >&2
  exit 42
fi
echo target artifact
SH
fi
chmod +x .gestalt/build/provider
`
	mustWriteFile(t, filepath.Join(root, "build.sh"), []byte(buildScript), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/test/apps/provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Example Provider",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	_, err := StageSourcePreparedInstallDir(manifestPath, stagingDir, StageSourcePreparedInstallOptions{
		Kind:       providermanifestv1.KindApp,
		AppName: "prepared-stage-test",
		GOOS:       targetGOOS,
		GOARCH:     targetGOARCH,
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

	stagedBinary, err := os.ReadFile(filepath.Join(stagingDir, filepath.FromSlash(artifactPath)))
	if err != nil {
		t.Fatalf("ReadFile(staged binary): %v", err)
	}
	if !strings.Contains(string(stagedBinary), "target artifact") {
		t.Fatalf("staged binary did not come from target build: %s", stagedBinary)
	}
}

func TestStageSourcePreparedInstallDir_RunOnlyFailsReleasePackaging(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "provider.sh"), []byte(`#!/bin/sh
set -eu
if [ -n "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: local_only\n    method: POST\n' > "$GESTALT_PLUGIN_WRITE_CATALOG"
fi
`), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/test/apps/run-only",
		Version: "0.0.1-alpha.1",
		Run:     []string{"./provider.sh"},
		Spec:    &providermanifestv1.Spec{},
	}))

	_, err := StageSourcePreparedInstallDir(manifestPath, filepath.Join(t.TempDir(), "prepared"), StageSourcePreparedInstallOptions{
		Kind:       providermanifestv1.KindApp,
		AppName: "run-only",
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
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

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", logPath, err)
	}
	return string(logData)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func TestStagePreparedInstallDir_WithEntrypointCopiesGeneratedCatalog(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "provider"), []byte(`#!/bin/sh
if [ -n "$GESTALT_PLUGIN_WRITE_CATALOG" ]; then
  printf 'name: provider\noperations:\n  - id: echo\n    method: POST\n' > "$GESTALT_PLUGIN_WRITE_CATALOG"
fi
`), 0o755)

	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/test/apps/provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Example Provider",
		Spec:        &providermanifestv1.Spec{},
		Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: "provider"},
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	_, err := StagePreparedInstallDir(manifestPath, stagingDir, StagePreparedInstallOptions{
		AppName: "prepared-stage-test",
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
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

package providerpkg

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	prefixScriptPath := filepath.Join(root, "prefix.sh")
	mustWriteFile(t, prefixScriptPath, []byte("#!/bin/sh\nexec \"$@\"\n"), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", mustManifestYAML(t, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Example Provider",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Run:        &providermanifestv1.SourceRun{CommandPrefix: []string{prefixScriptPath}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	staged, err := StageSourcePreparedInstallDir(manifestPath, stagingDir, StageSourcePreparedInstallOptions{
		Kind:       providermanifestv1.KindPlugin,
		PluginName: "prepared-stage-test",
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
	if len(catalogData) == 0 {
		t.Fatal("staged catalog.yaml is empty")
	}
}

func TestSourceManifestExecution_PrepareOnlyBuildAndRunPrefix(t *testing.T) { //nolint:paralleltest // Uses t.Setenv through installFakeUV.
	if runtime.GOOS == windowsOS {
		t.Skip("fake uv fixture uses POSIX shell")
	}

	root := t.TempDir()
	logPath := installFakeUV(t, root)
	mustWriteFile(t, filepath.Join(root, "provider.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", []byte(`
kind: plugin
source: github.com/test/plugins/uv-provider
version: 0.0.1-alpha.1
build: [uv, sync, --frozen, --no-install-project]
run:
  commandPrefix: [uv, run, --frozen]
entrypoint:
  artifactPath: provider.sh
  args: [--serve]
spec: {}
`))

	execution, err := SourceManifestExecution(manifestPath, providermanifestv1.KindPlugin, SourceBuildOptions{})
	if err != nil {
		t.Fatalf("SourceManifestExecution: %v", err)
	}
	if execution.Command != "uv" {
		t.Fatalf("execution.Command = %q, want uv", execution.Command)
	}
	wantArgs := []string{"run", "--frozen", filepath.Join(root, "provider.sh"), "--serve"}
	if !reflect.DeepEqual(execution.Args, wantArgs) {
		t.Fatalf("execution.Args = %#v, want %#v", execution.Args, wantArgs)
	}
	if execution.Workdir != root {
		t.Fatalf("execution.Workdir = %q, want %q", execution.Workdir, root)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", logPath, err)
	}
	if got, want := string(logData), root+"|sync --frozen --no-install-project"; !strings.Contains(got, want) {
		t.Fatalf("uv log = %q, want to contain %q", got, want)
	}
}

func TestPrepareSourceManifest_PrepareOnlyBuildRunsBeforeStaticCatalogWithRunPrefix(t *testing.T) { //nolint:paralleltest // Uses t.Setenv through installFakeUV.
	if runtime.GOOS == windowsOS {
		t.Skip("fake uv fixture uses POSIX shell")
	}

	root := t.TempDir()
	logPath := installFakeUV(t, root)
	mustWriteFile(t, filepath.Join(root, "provider.sh"), []byte(`#!/bin/sh
set -eu
if [ -n "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
  printf 'name: provider\noperations:\n  - id: uv_catalog\n    method: POST\n' > "$GESTALT_PLUGIN_WRITE_CATALOG"
fi
`), 0o755)
	manifestPath := mustWriteManifestData(t, root, "manifest.yaml", []byte(`
kind: plugin
source: github.com/test/plugins/uv-provider
version: 0.0.1-alpha.1
build: [uv, sync, --frozen, --no-install-project]
run:
  commandPrefix: [uv, run, --frozen]
entrypoint:
  artifactPath: provider.sh
spec: {}
`))

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

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", logPath, err)
	}
	logText := string(logData)
	syncLine := root + "|sync --frozen --no-install-project"
	runLine := root + "|run --frozen " + filepath.Join(root, "provider.sh")
	syncIdx := strings.Index(logText, syncLine)
	runIdx := strings.Index(logText, runLine)
	if syncIdx < 0 || runIdx < 0 || syncIdx > runIdx {
		t.Fatalf("uv log = %q, want sync before catalog run", logText)
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
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/provider",
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
		Kind:       providermanifestv1.KindPlugin,
		PluginName: "prepared-stage-test",
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

func installFakeUV(t *testing.T, root string) string {
	t.Helper()

	binDir := filepath.Join(root, "bin")
	logPath := filepath.Join(root, "uv.log")
	mustWriteFile(t, filepath.Join(binDir, "uv"), []byte(`#!/bin/sh
set -eu
printf '%s|%s\n' "$PWD" "$*" >> "$UV_LOG"
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
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("UV_LOG", logPath)
	return logPath
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
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/provider",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Example Provider",
		Spec:        &providermanifestv1.Spec{},
		Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: "provider"},
	}))

	stagingDir := filepath.Join(t.TempDir(), "prepared")
	_, err := StagePreparedInstallDir(manifestPath, stagingDir, StagePreparedInstallOptions{
		PluginName: "prepared-stage-test",
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

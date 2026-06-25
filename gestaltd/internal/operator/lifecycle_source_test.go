package operator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers"
	"gopkg.in/yaml.v3"
)

const (
	testOwner   = "testowner"
	testRepo    = "testrepo"
	testApp     = "testapp"
	testVersion = "1.0.0"
	testSource  = "github.com/" + testOwner + "/" + testRepo + "/apps/" + testApp
	testBinary  = "fake-binary-content"
)

func TestLifecycleGitSourceBuildLockSyncContract(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repoDir := filepath.Join(dir, "providers")
	const packageSource = "github.com/acme/providers/apps/alpha"
	writeSourceProviderTree(t, filepath.Join(repoDir, "apps", "alpha"), packageSource, "1.2.3", "alpha-binary")
	runGitTestCommand(t, repoDir, "init")
	runGitTestCommand(t, repoDir, "config", "user.email", "test@example.com")
	runGitTestCommand(t, repoDir, "config", "user.name", "Test")
	runGitTestCommand(t, repoDir, "add", ".")
	runGitTestCommand(t, repoDir, "commit", "-m", "provider")
	ref := strings.TrimSpace(runGitTestOutput(t, repoDir, "rev-parse", "HEAD"))

	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: gestaltd.config/v8
%s
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
apps:
  alpha:
    source:
      git:
        repo: file://%s
        ref: %s
        path: apps/alpha/manifest.yaml
        materialization: source
`, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), filepath.ToSlash(artifactsDir), filepath.ToSlash(repoDir), ref)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	entry := lock.Providers.App["alpha"]
	if entry.SourceRef == nil {
		t.Fatal("sourceRef missing")
	}
	if entry.SourceRef.Materialization != gitMaterializationSource {
		t.Fatalf("sourceRef materialization = %q", entry.SourceRef.Materialization)
	}
	if entry.SourceRef.Ref != ref {
		t.Fatalf("sourceRef.ref = %q, want %q", entry.SourceRef.Ref, ref)
	}
	if entry.Source != "git+file://"+repoDir+"@"+ref+"#apps/alpha/manifest.yaml" {
		t.Fatalf("source = %q", entry.Source)
	}
	if entry.Version != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", entry.Version)
	}
	if len(entry.Archives) != 0 {
		t.Fatalf("source-built git lock archives = %+v, want none", entry.Archives)
	}

	if err := os.RemoveAll(filepath.Join(artifactsDir, "providers", "alpha")); err != nil {
		t.Fatalf("remove prepared provider: %v", err)
	}
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths after removing prepared provider: %v", err)
	}
	preparedManifest := filepath.Join(artifactsDir, "providers", "alpha", "manifest.yaml")
	if _, err := os.Stat(preparedManifest); err != nil {
		t.Fatalf("prepared manifest not restored: %v", err)
	}
}

func TestLifecycleGitSourceUIBuildLockSyncContract(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	dir := t.TempDir()
	repoDir := filepath.Join(dir, "providers")
	const packageSource = "github.com/acme/providers/ui/roadmap"
	writeSourceUITree(t, filepath.Join(repoDir, "ui", "roadmap"), packageSource, "1.2.3")
	runGitTestCommand(t, repoDir, "init")
	runGitTestCommand(t, repoDir, "config", "user.email", "test@example.com")
	runGitTestCommand(t, repoDir, "config", "user.name", "Test")
	runGitTestCommand(t, repoDir, "add", ".")
	runGitTestCommand(t, repoDir, "commit", "-m", "ui")
	ref := strings.TrimSpace(runGitTestOutput(t, repoDir, "rev-parse", "HEAD"))

	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: gestaltd.config/v8
%s
  ui:
    roadmap:
      source:
        git:
          repo: file://%s
          ref: %s
          path: ui/roadmap/manifest.yaml
          materialization: source
      path: /roadmap
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), filepath.ToSlash(repoDir), ref, filepath.ToSlash(artifactsDir))
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	entry := lock.Providers.UI["roadmap"]
	if entry.SourceRef == nil {
		t.Fatal("sourceRef missing")
	}
	if entry.SourceRef.Materialization != gitMaterializationSource {
		t.Fatalf("sourceRef materialization = %q", entry.SourceRef.Materialization)
	}
	preparedIndex := filepath.Join(artifactsDir, "ui", "roadmap", "dist", "index.html")
	if err := os.RemoveAll(filepath.Join(artifactsDir, "ui", "roadmap")); err != nil {
		t.Fatalf("remove prepared ui: %v", err)
	}
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths after removing prepared ui: %v", err)
	}
	if _, err := os.Stat(preparedIndex); err != nil {
		t.Fatalf("prepared ui index not restored: %v", err)
	}
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	ui := cfg.Providers.UI["roadmap"]
	if ui == nil || ui.ResolvedManifest == nil {
		t.Fatalf("ui resolved manifest = %+v", ui)
	}
	if ui.ResolvedManifest.Build != nil {
		t.Fatalf("prepared ui manifest retained build metadata: %+v", ui.ResolvedManifest.Build)
	}
	if got, want := filepath.ToSlash(ui.ResolvedAssetRoot), filepath.ToSlash(filepath.Join(artifactsDir, "ui", "roadmap", "dist")); got != want {
		t.Fatalf("ResolvedAssetRoot = %q, want %q", got, want)
	}
}

func TestLifecycleSyncLockedPreparesIndependentLocalUIsInParallel(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	for _, tc := range []struct {
		name      string
		bound     bool
		configure func(t *testing.T, dir string, apps, uis map[string]string) (string, *Lockfile)
	}{
		{
			name: "standalone",
			configure: func(t *testing.T, dir string, _ map[string]string, uis map[string]string) (string, *Lockfile) {
				t.Helper()
				configPath := writeLocalUITestConfig(t, dir, uis)
				lock, err := ReadLockfile(filepath.Join(dir, LockfileName))
				if err != nil {
					t.Fatalf("read lockfile: %v", err)
				}
				return configPath, lock
			},
		},
		{
			name:  "app-bound",
			bound: true,
			configure: func(t *testing.T, dir string, apps, uis map[string]string) (string, *Lockfile) {
				t.Helper()
				return writeAppBoundLocalUITestConfig(t, dir, apps, uis)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			syncDir := filepath.Join(dir, "sync")
			if err := os.MkdirAll(syncDir, 0o755); err != nil {
				t.Fatalf("mkdir sync dir: %v", err)
			}
			alphaApp := filepath.Join(dir, "apps", "alpha")
			betaApp := filepath.Join(dir, "apps", "beta")
			alphaDir := filepath.Join(dir, "ui", "alpha")
			betaDir := filepath.Join(dir, "ui", "beta")
			writeSourceProviderTree(t, alphaApp, "github.com/acme/apps/alpha", "1.2.3", "alpha-binary")
			writeSourceProviderTree(t, betaApp, "github.com/acme/apps/beta", "1.2.3", "beta-binary")
			writeBlockingSourceUITree(t, alphaDir, "github.com/acme/ui/alpha", "1.2.3", fmt.Sprintf(`#!/bin/sh
set -eu
touch %s/alpha.started
i=0
while [ ! -f %s/beta.started ]; do
  i=$((i + 1))
  if [ "$i" -gt 100 ]; then
    echo "timed out waiting for beta" >&2
    exit 42
  fi
  sleep 0.05
done
mkdir -p dist
printf '<html>alpha</html>\n' > dist/index.html
`, syncDir, syncDir))
			writeBlockingSourceUITree(t, betaDir, "github.com/acme/ui/beta", "1.2.3", fmt.Sprintf(`#!/bin/sh
set -eu
touch %s/beta.started
i=0
while [ ! -f %s/alpha.started ]; do
  i=$((i + 1))
  if [ "$i" -gt 100 ]; then
    echo "timed out waiting for alpha" >&2
    exit 42
  fi
  sleep 0.05
done
mkdir -p dist
printf '<html>beta</html>\n' > dist/index.html
`, syncDir, syncDir))

			configPath, lock := tc.configure(t, dir, map[string]string{
				"alpha": filepath.Join(alphaApp, "manifest.yaml"),
				"beta":  filepath.Join(betaApp, "manifest.yaml"),
			}, map[string]string{
				"alpha": filepath.Join(alphaDir, "manifest.yaml"),
				"beta":  filepath.Join(betaDir, "manifest.yaml"),
			})
			for _, name := range []string{"alpha", "beta"} {
				if _, ok := lock.Providers.UI[name]; ok {
					t.Fatalf("local ui %q should not be serialized in committed lockfile: %#v", name, lock.Providers.UI)
				}
			}
			loaded, err := config.Load(configPath)
			if err != nil {
				t.Fatalf("Load config: %v", err)
			}
			for _, name := range []string{"alpha", "beta"} {
				entry := loaded.Providers.UI[name]
				if entry == nil {
					t.Fatalf("loaded ui %q = nil", name)
					return
				}
				if got := entry.OwnerApp; tc.bound {
					if got != name {
						t.Fatalf("ui %q OwnerApp = %q, want %q", name, got, name)
					}
				} else if got != "" {
					t.Fatalf("ui %q OwnerApp = %q, want empty", name, got)
				}
			}
			lockPath := filepath.Join(dir, LockfileName)
			before, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("read lockfile before sync: %v", err)
			}
			if err := NewLifecycle().SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{Parallelism: 2}); err != nil {
				t.Fatalf("SyncAtPathsWithStatePathsOptions: %v", err)
			}
			after, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("read lockfile after sync: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("sync --locked rewrote committed lockfile")
			}
			for _, name := range []string{"alpha", "beta"} {
				if _, err := os.Stat(filepath.Join(dir, "artifacts", "ui", name, "dist", "index.html")); err != nil {
					t.Fatalf("prepared ui %s not found: %v", name, err)
				}
			}
		})
	}
}

func TestLifecycleSyncLockedParallelismOnePreparesLocalUIsSequentially(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	dir := t.TempDir()
	logPath := filepath.Join(dir, "build.log")
	alphaApp := filepath.Join(dir, "apps", "alpha")
	betaApp := filepath.Join(dir, "apps", "beta")
	alphaDir := filepath.Join(dir, "ui", "alpha")
	betaDir := filepath.Join(dir, "ui", "beta")
	writeSourceProviderTree(t, alphaApp, "github.com/acme/apps/alpha", "1.2.3", "alpha-binary")
	writeSourceProviderTree(t, betaApp, "github.com/acme/apps/beta", "1.2.3", "beta-binary")
	writeBlockingSourceUITree(t, alphaDir, "github.com/acme/ui/alpha", "1.2.3", fmt.Sprintf(`#!/bin/sh
set -eu
printf 'alpha start\n' >> %s
mkdir -p dist
printf '<html>alpha</html>\n' > dist/index.html
printf 'alpha end\n' >> %s
`, logPath, logPath))
	writeBlockingSourceUITree(t, betaDir, "github.com/acme/ui/beta", "1.2.3", fmt.Sprintf(`#!/bin/sh
set -eu
printf 'beta start\n' >> %s
mkdir -p dist
printf '<html>beta</html>\n' > dist/index.html
printf 'beta end\n' >> %s
`, logPath, logPath))

	configPath, _ := writeAppBoundLocalUITestConfig(t, dir, map[string]string{
		"alpha": filepath.Join(alphaApp, "manifest.yaml"),
		"beta":  filepath.Join(betaApp, "manifest.yaml"),
	}, map[string]string{
		"alpha": filepath.Join(alphaDir, "manifest.yaml"),
		"beta":  filepath.Join(betaDir, "manifest.yaml"),
	})
	if err := NewLifecycle().SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{Parallelism: 1}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePathsOptions: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read build log: %v", err)
	}
	want := "alpha start\nalpha end\nbeta start\nbeta end\n"
	if string(data) != want {
		t.Fatalf("build log = %q, want %q", data, want)
	}
}

func TestLifecycleSyncLockedParallelizesAppOwnedUIs(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	dir := t.TempDir()
	syncDir := filepath.Join(dir, "sync")
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}
	alphaApp := filepath.Join(dir, "apps", "alpha")
	betaApp := filepath.Join(dir, "apps", "beta")
	alphaUI := filepath.Join(dir, "ui", "alpha")
	betaUI := filepath.Join(dir, "ui", "beta")
	alphaManifest := writeOwnedUISourceAppTree(t, alphaApp, alphaUI, "alpha", fmt.Sprintf(`#!/bin/sh
set -eu
touch %s/alpha.started
i=0
while [ ! -f %s/beta.started ]; do
  i=$((i + 1))
  if [ "$i" -gt 100 ]; then
    echo "timed out waiting for beta" >&2
    exit 42
  fi
  sleep 0.05
done
mkdir -p dist
printf '<html>alpha</html>\n' > dist/index.html
`, syncDir, syncDir))
	betaManifest := writeOwnedUISourceAppTree(t, betaApp, betaUI, "beta", fmt.Sprintf(`#!/bin/sh
set -eu
touch %s/beta.started
i=0
while [ ! -f %s/alpha.started ]; do
  i=$((i + 1))
  if [ "$i" -gt 100 ]; then
    echo "timed out waiting for alpha" >&2
    exit 42
  fi
  sleep 0.05
done
mkdir -p dist
printf '<html>beta</html>\n' > dist/index.html
`, syncDir, syncDir))

	configPath := writeOwnedUIAppTestConfig(t, dir, map[string]string{
		"alpha": alphaManifest,
		"beta":  betaManifest,
	})
	if err := NewLifecycle().SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{Parallelism: 2}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePathsOptions: %v", err)
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(dir, "artifacts", "providers", name, "_owned_ui", name, "dist", "index.html")); err != nil {
			t.Fatalf("prepared owned ui %s not found: %v", name, err)
		}
	}
}

func TestLifecycleSyncLockedParallelismOnePreparesAppOwnedUIsSequentially(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	dir := t.TempDir()
	logPath := filepath.Join(dir, "build.log")
	alphaManifest := writeOwnedUISourceAppTree(t, filepath.Join(dir, "apps", "alpha"), filepath.Join(dir, "ui", "alpha"), "alpha", fmt.Sprintf(`#!/bin/sh
set -eu
printf 'alpha start\n' >> %s
mkdir -p dist
printf '<html>alpha</html>\n' > dist/index.html
printf 'alpha end\n' >> %s
`, logPath, logPath))
	betaManifest := writeOwnedUISourceAppTree(t, filepath.Join(dir, "apps", "beta"), filepath.Join(dir, "ui", "beta"), "beta", fmt.Sprintf(`#!/bin/sh
set -eu
printf 'beta start\n' >> %s
mkdir -p dist
printf '<html>beta</html>\n' > dist/index.html
printf 'beta end\n' >> %s
`, logPath, logPath))

	configPath := writeOwnedUIAppTestConfig(t, dir, map[string]string{
		"alpha": alphaManifest,
		"beta":  betaManifest,
	})
	if err := NewLifecycle().SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{Parallelism: 1}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePathsOptions: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read build log: %v", err)
	}
	want := "alpha start\nalpha end\nbeta start\nbeta end\n"
	if string(data) != want {
		t.Fatalf("build log = %q, want %q", data, want)
	}
}

func TestLifecycleSyncLockedSerializesAppOwnedUIsWithSharedSource(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	dir := t.TempDir()
	logPath := filepath.Join(dir, "build.log")
	lockDir := filepath.Join(dir, "owned-ui.lock")
	sharedUI := filepath.Join(dir, "ui", "shared")
	writeBlockingSourceUITree(t, sharedUI, "github.com/acme/ui/shared", "1.2.3", fmt.Sprintf(`#!/bin/sh
set -eu
if ! mkdir %s; then
  printf 'overlap\n' >> %s
  exit 43
fi
trap 'rmdir %s' EXIT
printf 'start\n' >> %s
sleep 0.1
mkdir -p dist
printf '<html>shared</html>\n' > dist/index.html
printf 'end\n' >> %s
`, lockDir, logPath, lockDir, logPath, logPath))
	alphaManifest := writeOwnedUISourceAppForUI(t, filepath.Join(dir, "apps", "alpha"), filepath.Join(sharedUI, "manifest.yaml"), "alpha")
	betaManifest := writeOwnedUISourceAppForUI(t, filepath.Join(dir, "apps", "beta"), filepath.Join(sharedUI, "manifest.yaml"), "beta")

	configPath := writeOwnedUIAppTestConfig(t, dir, map[string]string{
		"alpha": alphaManifest,
		"beta":  betaManifest,
	})
	if err := NewLifecycle().SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{Parallelism: 2}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePathsOptions: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read build log: %v", err)
	}
	want := "start\nend\nstart\nend\n"
	if string(data) != want {
		t.Fatalf("build log = %q, want %q", data, want)
	}
}

func TestLifecycleSyncLockedSerializesLocalUIsWithSharedOutput(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui", "shared")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("mkdir ui dir: %v", err)
	}
	writeNamedBlockingSourceUIManifest(t, uiDir, "manifest-alpha.yaml", "github.com/acme/ui/alpha", "1.2.3", "build-alpha.sh")
	writeNamedBlockingSourceUIManifest(t, uiDir, "manifest-beta.yaml", "github.com/acme/ui/beta", "1.2.3", "build-beta.sh")
	script := `#!/bin/sh
set -eu
if [ -f running ]; then
  echo overlap > overlap
  exit 43
fi
touch running
sleep 0.2
rm running
mkdir -p dist
printf '<html>ok</html>\n' > dist/index.html
`
	if err := os.WriteFile(filepath.Join(uiDir, "build-alpha.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write alpha build: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uiDir, "build-beta.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write beta build: %v", err)
	}

	configPath := writeLocalUITestConfig(t, dir, map[string]string{
		"alpha": filepath.Join(uiDir, "manifest-alpha.yaml"),
		"beta":  filepath.Join(uiDir, "manifest-beta.yaml"),
	})
	if err := NewLifecycle().SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{Parallelism: 2}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePathsOptions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(uiDir, "overlap")); !os.IsNotExist(err) {
		t.Fatalf("shared-output builds overlapped, stat overlap err=%v", err)
	}
}

func TestLifecycleCheckSyncWithParallelismDoesNotMaterializeLocalUI(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	dir := t.TempDir()
	startedPath := filepath.Join(dir, "started")
	uiDir := filepath.Join(dir, "ui", "alpha")
	writeBlockingSourceUITree(t, uiDir, "github.com/acme/ui/alpha", "1.2.3", fmt.Sprintf(`#!/bin/sh
set -eu
touch %s
mkdir -p dist
printf '<html>alpha</html>\n' > dist/index.html
`, startedPath))
	configPath := writeLocalUITestConfig(t, dir, map[string]string{
		"alpha": filepath.Join(uiDir, "manifest.yaml"),
	})
	err := NewLifecycle().CheckSyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{Parallelism: 2})
	if err == nil || !strings.Contains(err.Error(), `prepared artifact`) {
		t.Fatalf("CheckSyncAtPathsWithStatePathsOptions error = %v, want stale prepared artifact", err)
	}
	if _, err := os.Stat(startedPath); !os.IsNotExist(err) {
		t.Fatalf("sync --check materialized local ui, stat started err=%v", err)
	}
}

func TestLifecycleLocalSourceLockedExecutionUsesPreparedArtifactsWithoutSourceTree(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	dir := t.TempDir()
	const (
		appSource = "github.com/acme/tools/apps/alpha"
		uiSource  = "github.com/acme/tools/ui/roadmap"
		version   = "1.2.3"
	)
	appDir := filepath.Join(dir, "apps", "alpha")
	uiDir := filepath.Join(dir, "ui", "roadmap")
	writeSourceProviderTree(t, appDir, appSource, version, "alpha-binary")
	writeSourceUITree(t, uiDir, uiSource, version)

	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: gestaltd.config/v8
%s  ui:
    roadmap:
      source:
        path: ui/roadmap/manifest.yaml
      path: /roadmap
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
apps:
  alpha:
    source:
      path: apps/alpha/manifest.yaml
`, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), filepath.ToSlash(artifactsDir))
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	if _, ok := lock.Providers.App["alpha"]; ok {
		t.Fatalf("local source provider lock entry should be omitted: %#v", lock.Providers)
	}
	if _, ok := lock.Providers.UI["roadmap"]; ok {
		t.Fatalf("local source ui lock entry should be omitted: %#v", lock.Providers.UI)
	}
	if _, err := os.Stat(filepath.Join(artifactsDir, ".gestaltd")); !os.IsNotExist(err) {
		t.Fatalf("lock should not create prepared artifact dirs for local source entries, stat err=%v", err)
	}
	lock, err = lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("restore canonical LockAtPathsWithStatePaths: %v", err)
	}
	if _, ok := lock.Providers.App["alpha"]; ok {
		t.Fatalf("restored local source provider lock entry should be omitted: %#v", lock.Providers)
	}
	if _, ok := lock.Providers.UI["roadmap"]; ok {
		t.Fatalf("restored local source ui lock entry should be omitted: %#v", lock.Providers.UI)
	}
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}

	preparedProviderDir := filepath.Join(artifactsDir, "providers", "alpha")
	preparedProvider := filepath.Join(preparedProviderDir, "bin", "alpha")
	if _, err := os.Stat(preparedProvider); err != nil {
		t.Fatalf("prepared provider binary missing before source removal: %v", err)
	}
	preparedProviderMetadata := filepath.Join(preparedProviderDir, preparedLockMetadataFile)
	if _, err := os.Stat(preparedProviderMetadata); err != nil {
		t.Fatalf("prepared provider lock metadata missing before source removal: %v", err)
	}
	providerMetadata, err := os.ReadFile(preparedProviderMetadata)
	if err != nil {
		t.Fatalf("read prepared provider lock metadata before source removal: %v", err)
	}
	preparedUI := filepath.Join(artifactsDir, "ui", "roadmap", "dist")
	if _, err := os.Stat(filepath.Join(preparedUI, "index.html")); err != nil {
		t.Fatalf("prepared ui asset missing before source removal: %v", err)
	}
	preparedUIMetadata := filepath.Join(filepath.Dir(preparedUI), preparedLockMetadataFile)
	if _, err := os.Stat(preparedUIMetadata); err != nil {
		t.Fatalf("prepared ui lock metadata missing before source removal: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "apps")); err != nil {
		t.Fatalf("remove app source tree: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "ui")); err != nil {
		t.Fatalf("remove ui source tree: %v", err)
	}

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true) without local source tree: %v", err)
	}
	app := cfg.Apps["alpha"]
	if app == nil || app.ResolvedManifest == nil {
		t.Fatalf("resolved app = %+v", app)
	}
	if got, want := filepath.ToSlash(app.Command), filepath.ToSlash(preparedProvider); got != want {
		t.Fatalf("app command = %q, want %q", got, want)
	}
	ui := cfg.Providers.UI["roadmap"]
	if ui == nil || ui.ResolvedManifest == nil {
		t.Fatalf("resolved ui = %+v", ui)
	}
	if got, want := filepath.ToSlash(ui.ResolvedAssetRoot), filepath.ToSlash(preparedUI); got != want {
		t.Fatalf("ResolvedAssetRoot = %q, want %q", got, want)
	}
	if err := lc.CheckSyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err == nil || !strings.Contains(err.Error(), `prepared artifact for provider "alpha" is missing or stale`) {
		t.Fatalf("CheckSyncAtPathsWithStatePaths without local source tree error = %v, want stale provider artifact", err)
	}
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err == nil || !strings.Contains(err.Error(), `manifest for app "alpha" not found`) {
		t.Fatalf("SyncAtPathsWithStatePaths without local source tree error = %v, want missing source manifest", err)
	}

	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("restore config after source tree removal: %v", err)
	}
	writeSourceProviderTree(t, appDir, appSource, version, "alpha-binary")
	writeSourceUITree(t, uiDir, uiSource, version)
	if err := os.Remove(preparedProviderMetadata); err != nil {
		t.Fatalf("remove prepared provider lock metadata: %v", err)
	}
	if err := os.Remove(preparedUIMetadata); err != nil {
		t.Fatalf("remove prepared ui lock metadata: %v", err)
	}
	if err := lc.CheckSyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err == nil || !strings.Contains(err.Error(), `prepared artifact for provider "alpha" is missing or stale`) {
		t.Fatalf("CheckSyncAtPathsWithStatePaths without prepared lock metadata error = %v, want stale provider artifact", err)
	}
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths backfilling prepared lock metadata: %v", err)
	}
	if _, err := os.Stat(preparedProviderMetadata); err != nil {
		t.Fatalf("prepared provider lock metadata not backfilled: %v", err)
	}
	if _, err := os.Stat(preparedUIMetadata); err != nil {
		t.Fatalf("prepared ui lock metadata not backfilled: %v", err)
	}
	if err := os.WriteFile(preparedProvider, []byte("stale-binary"), 0o755); err != nil {
		t.Fatalf("write stale prepared provider binary: %v", err)
	}
	if err := os.WriteFile(preparedProviderMetadata, []byte(`{"inputDigest":"stale","kind":"app","name":"alpha"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write stale prepared provider lock metadata before sync: %v", err)
	}
	if err := os.WriteFile(filepath.Join(preparedUI, "index.html"), []byte("<html>stale</html>\n"), 0o644); err != nil {
		t.Fatalf("write stale prepared ui asset: %v", err)
	}
	if err := os.WriteFile(preparedUIMetadata, []byte(`{"inputDigest":"stale","kind":"ui","name":"roadmap"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write stale prepared ui lock metadata before sync: %v", err)
	}
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths re-materializing stale prepared lock metadata: %v", err)
	}
	providerData, err := os.ReadFile(preparedProvider)
	if err != nil {
		t.Fatalf("read re-materialized prepared provider binary: %v", err)
	}
	if string(providerData) != "alpha-binary" {
		t.Fatalf("prepared provider binary after stale metadata sync = %q, want re-materialized source artifact", string(providerData))
	}
	uiData, err := os.ReadFile(filepath.Join(preparedUI, "index.html"))
	if err != nil {
		t.Fatalf("read re-materialized prepared ui asset: %v", err)
	}
	if string(uiData) != "<html>roadmap</html>\n" {
		t.Fatalf("prepared ui asset after stale metadata sync = %q, want re-materialized source asset", string(uiData))
	}
	if err := os.RemoveAll(filepath.Join(dir, "apps")); err != nil {
		t.Fatalf("remove app source tree after backfill: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "ui")); err != nil {
		t.Fatalf("remove ui source tree after backfill: %v", err)
	}
	if _, _, err := lc.LoadForExecutionAtPath(configPath, true); err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true) after metadata backfill: %v", err)
	}

	if err := os.WriteFile(preparedProviderMetadata, []byte(`{"inputDigest":"stale","kind":"app","name":"alpha"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write stale prepared provider lock metadata: %v", err)
	}
	if _, _, err := lc.LoadForExecutionAtPath(configPath, true); err == nil || !strings.Contains(err.Error(), `prepared artifact for provider "alpha" is missing or stale`) {
		t.Fatalf("LoadForExecutionAtPath with stale prepared provider metadata error = %v, want stale provider artifact", err)
	}
	if err := os.WriteFile(preparedProviderMetadata, providerMetadata, 0o644); err != nil {
		t.Fatalf("restore prepared provider lock metadata: %v", err)
	}
	if err := os.WriteFile(preparedUIMetadata, []byte(`{"inputDigest":"stale","kind":"ui","name":"roadmap"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write stale prepared ui lock metadata: %v", err)
	}
	if _, _, err := lc.LoadForExecutionAtPath(configPath, true); err == nil || !strings.Contains(err.Error(), `prepared artifact for ui "roadmap" is missing or stale`) {
		t.Fatalf("LoadForExecutionAtPath with stale prepared ui metadata error = %v, want stale ui artifact", err)
	}
}

func TestLockAtPathsSkipsMissingConfiguredLocalSources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: gestaltd.config/v8
%s  ui:
    missing:
      source:
        path: ui/missing/manifest.yaml
      path: /missing
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
apps:
  missing:
    source:
      path: apps/missing/manifest.yaml
`, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), filepath.ToSlash(artifactsDir))
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lock, err := NewLifecycle().LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	if _, ok := lock.Providers.App["missing"]; ok {
		t.Fatalf("local source provider lock entry should be omitted: %#v", lock.Providers)
	}
	if _, ok := lock.Providers.UI["missing"]; ok {
		t.Fatalf("local source ui lock entry should be omitted: %#v", lock.Providers.UI)
	}
	if _, err := os.Stat(filepath.Join(artifactsDir, ".gestaltd")); !os.IsNotExist(err) {
		t.Fatalf("lock should not create prepared artifact dirs for missing local sources, stat err=%v", err)
	}
}

func TestLockAtPathsRejectsCommittedProviderInvokesField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeLocalRelease := func(name, source, version string) string {
		t.Helper()

		archivePath := buildExecutableArchiveWithConfigSchema(t, dir, name+"-src", source, version, providermanifestv1.KindApp, name, name+"-binary")
		archiveData, err := os.ReadFile(archivePath)
		if err != nil {
			t.Fatalf("read %s archive: %v", name, err)
		}
		archiveSum := sha256.Sum256(archiveData)
		metadataRelPath := filepath.ToSlash(filepath.Join("providers", name, "provider-release.yaml"))
		metadataPath := filepath.Join(dir, filepath.FromSlash(metadataRelPath))
		archiveName := name + ".tar.gz"
		if err := os.MkdirAll(filepath.Dir(metadataPath), 0o755); err != nil {
			t.Fatalf("create %s metadata dir: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(metadataPath), archiveName), archiveData, 0o644); err != nil {
			t.Fatalf("write %s archive: %v", name, err)
		}
		writeProviderReleaseMetadataFileWithStaticValidation(t, metadataPath, providerReleaseMetadataFixture{
			Package: source,
			Kind:    providermanifestv1.KindApp,
			Version: version, Artifacts: map[string]providerrelease.Artifact{
				providerpkg.CurrentPlatformString(): {
					Path:   archiveName,
					SHA256: hex.EncodeToString(archiveSum[:]),
				},
			},
		})
		return "./" + metadataRelPath
	}

	callerSource := writeLocalRelease("caller", "github.com/acme/tools/caller", "1.0.0")
	targetSource := writeLocalRelease("target", "github.com/acme/tools/target", "1.0.0")
	configPath := filepath.Join(dir, "gestaltd.yaml")
	writeConfig := func(callerInvokes string) {
		t.Helper()

		configYAML := fmt.Sprintf(`
apiVersion: gestaltd.config/v8
%sapps:
  target:
    source: %s
  caller:
    source: %s%s
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), targetSource, callerSource, callerInvokes, filepath.ToSlash(filepath.Join(dir, "artifacts")))
		if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	writeConfig("")

	lock, err := NewLifecycle().LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	callerStatic := lock.Providers.App["caller"].ValidationManifest
	if callerStatic == nil || callerStatic.Entrypoint != nil {
		t.Fatalf("caller static manifest entrypoint = %+v, want nil", callerStatic)
	}

	writeConfig(`
    invokes:
      - app: target
        operation: missing`)
	if _, err := NewLifecycle().LoadForStaticValidationAtPathsWithStatePaths([]string{configPath}, StatePaths{}, StaticValidationOptions{}); err == nil || !strings.Contains(err.Error(), "field invokes not found") {
		t.Fatalf("LoadForStaticValidationAtPathsWithStatePaths error = %v, want field invokes not found", err)
	}
	if _, err := NewLifecycle().LockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err == nil || !strings.Contains(err.Error(), "field invokes not found") {
		t.Fatalf("LockAtPathsWithStatePaths error = %v, want field invokes not found", err)
	}
}

func TestCheckLockAtPathsReportsMissingProviderDrift(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const (
		source  = "github.com/acme/tools/target"
		version = "1.0.0"
	)
	archivePath := buildExecutableArchive(t, dir, "target-src", source, version, providermanifestv1.KindApp, "target", "target-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSum := sha256.Sum256(archiveData)
	metadataRelPath := filepath.ToSlash(filepath.Join("providers", "target", "provider-release.yaml"))
	metadataPath := filepath.Join(dir, filepath.FromSlash(metadataRelPath))
	if err := os.MkdirAll(filepath.Dir(metadataPath), 0o755); err != nil {
		t.Fatalf("create metadata dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(metadataPath), "target.tar.gz"), archiveData, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	writeProviderReleaseMetadataFileWithStaticValidation(t, metadataPath, providerReleaseMetadataFixture{
		Package: source,
		Kind:    providermanifestv1.KindApp,
		Version: version, Artifacts: map[string]providerrelease.Artifact{
			providerpkg.CurrentPlatformString(): {
				Path:   "target.tar.gz",
				SHA256: hex.EncodeToString(archiveSum[:]),
			},
		},
	})

	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: gestaltd.config/v8
%sapps:
  target:
    source: ./%s
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), metadataRelPath, filepath.ToSlash(filepath.Join(dir, "artifacts")))
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	delete(lock.Providers.App, "target")
	if err := WriteLockfile(filepath.Join(dir, LockfileName), lock); err != nil {
		t.Fatalf("write stale lockfile: %v", err)
	}

	err = lc.CheckLockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err == nil {
		t.Fatal("CheckLockAtPathsWithStatePaths unexpectedly succeeded")
	}
	if got := err.Error(); !strings.Contains(got, "lockfile is out of date; run `gestaltd lock") || !strings.Contains(got, "missing providers.app.target") {
		t.Fatalf("CheckLockAtPathsWithStatePaths error = %v, want lock command and missing provider drift", err)
	}
}

func TestLifecycleGitSourceSnapshotRequireContract(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const (
		ref           = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		gestaltRef    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		packageSource = "github.com/acme/providers/apps/alpha"
		version       = "0.0.0-snapshot.gaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	archivePath := buildV2Archive(t, dir, packageSource, version, "snapshot-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSHA, err := providerpkg.ArchiveDigest(archivePath)
	if err != nil {
		t.Fatalf("archive digest: %v", err)
	}
	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	metadataPath := "/snapshots/github.com/acme/providers/" + ref + "/apps/alpha/provider-release.yaml"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadata := providerReleaseMetadataFixture{
				Package:     packageSource,
				Kind:        providermanifestv1.KindApp,
				Version:     version,
				ArchivePath: archivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   "alpha.tar.gz",
						SHA256: archiveSHA,
					},
				},
			}
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if !serveProviderReleaseFixture(t, w, r.URL.Path, metadataPath, metadata) {
				http.NotFound(w, r)
			}
		case "/snapshots/github.com/acme/providers/" + ref + "/apps/alpha/alpha.tar.gz":
			archiveCount.Add(1)
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: gestaltd.config/v8
providerSnapshotRepositories:
  valon:
    url: %s/snapshots
    gestaltRef: %s
%s
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
apps:
  alpha:
    source:
      git:
        repo: https://github.com/acme/providers.git
        ref: %s
        path: apps/alpha/manifest.yaml
        artifactRepository: valon
        materialization: snapshot
`, srv.URL, gestaltRef, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "snapshot.db")), filepath.ToSlash(artifactsDir), ref)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lock, err := NewLifecycle().WithHTTPClient(srv.Client()).LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	entry := lock.Providers.App["alpha"]
	if entry.SourceRef == nil {
		t.Fatal("sourceRef missing")
	}
	if entry.SourceRef.Materialization != gitMaterializationSnapshot {
		t.Fatalf("sourceRef materialization = %q", entry.SourceRef.Materialization)
	}
	if entry.SourceRef.ResolvedGestaltRef != gestaltRef {
		t.Fatalf("resolvedGestaltRef = %q, want %q", entry.SourceRef.ResolvedGestaltRef, gestaltRef)
	}
	if entry.Source != srv.URL+"/snapshots/github.com/acme/providers/"+ref+"/apps/alpha/provider-release.yaml?sourceRef="+ref {
		t.Fatalf("source = %q", entry.Source)
	}
	if entry.Version != version {
		t.Fatalf("version = %q, want %q", entry.Version, version)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 0 {
		t.Fatalf("archive count = %d, want 0", got)
	}
	archiveBeforeCheck := archiveCount.Load()
	if err := NewLifecycle().WithHTTPClient(srv.Client()).CheckLockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("CheckLockAtPathsWithStatePaths: %v", err)
	}
	if got := archiveCount.Load(); got != archiveBeforeCheck {
		t.Fatalf("archive count after check = %d, want %d", got, archiveBeforeCheck)
	}
}

func TestLifecycleGitSourceSnapshotRequirePrimesSecretsProvider(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const (
		ref           = "cccccccccccccccccccccccccccccccccccccccc"
		gestaltRef    = "dddddddddddddddddddddddddddddddddddddddd"
		packageSource = "github.com/acme/providers/secrets/google"
		version       = "0.0.0-snapshot.gcccccccccccccccccccccccccccccccccccccccc"
	)
	archivePath := buildV2ArchiveForKind(t, dir, providermanifestv1.KindSecrets, packageSource, version, artifactRelPath("secrets-provider"), "secrets-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSHA, err := providerpkg.ArchiveDigest(archivePath)
	if err != nil {
		t.Fatalf("archive digest: %v", err)
	}
	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	metadataPath := "/snapshots/github.com/acme/providers/" + ref + "/secrets/google/provider-release.yaml"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadata := providerReleaseMetadataFixture{
				Package:     packageSource,
				Kind:        providermanifestv1.KindSecrets,
				Version:     version,
				ArchivePath: archivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   "secrets.tar.gz",
						SHA256: archiveSHA,
					},
				},
			}
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if !serveProviderReleaseFixture(t, w, r.URL.Path, metadataPath, metadata) {
				http.NotFound(w, r)
			}
		case "/snapshots/github.com/acme/providers/" + ref + "/secrets/google/secrets.tar.gz":
			archiveCount.Add(1)
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "artifacts")
	indexedDBManifestPath := writeStubIndexedDBManifest(t, dir)
	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: gestaltd.config/v8
providerSnapshotRepositories:
  valon:
    url: %s/snapshots
    gestaltRef: %s
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey:
    secret:
      provider: secrets
      name: encryption-key
providers:
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        path: %q
  secrets:
    secrets:
      source:
        git:
          repo: https://github.com/acme/providers.git
          ref: %s
          path: secrets/google/manifest.yaml
          artifactRepository: valon
          materialization: snapshot
`, srv.URL, gestaltRef, filepath.ToSlash(artifactsDir), filepath.ToSlash(indexedDBManifestPath), filepath.Join(dir, "secrets.db"), ref)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lock, err := NewLifecycle().WithHTTPClient(srv.Client()).LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	entry := lock.Providers.Secrets["secrets"]
	if entry.SourceRef == nil {
		t.Fatal("secrets sourceRef missing")
	}
	if entry.SourceRef.Materialization != gitMaterializationSnapshot {
		t.Fatalf("secrets materialization = %q", entry.SourceRef.Materialization)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 0 {
		t.Fatalf("archive count = %d, want 0", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func sha256hex(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func writeSourceProviderTree(t *testing.T, dir, source, version, binaryContent string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create source provider dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provider"), []byte(binaryContent), 0o755); err != nil {
		t.Fatalf("write provider binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte("name: alpha\noperations:\n  - id: ping\n    method: GET\n"), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	srcParts := strings.Split(source, "/")
	appName := srcParts[len(srcParts)-1]
	buildOutput := ".gestaltd/bin/" + appName
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\ncp provider %s\nchmod +x %s\n", buildOutput, buildOutput)
	if err := os.WriteFile(filepath.Join(dir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("write build script: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:      source,
		Version:     version,
		Kind:        providermanifestv1.KindApp,
		DisplayName: "Alpha",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh", "provider"},
		},
	}
	data, err := encodeSourceManifestForTest(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeOwnedUISourceAppTree(t *testing.T, appDir, uiDir, name, uiBuildScript string) string {
	t.Helper()
	writeBlockingSourceUITree(t, uiDir, "github.com/acme/ui/"+name, "1.2.3", uiBuildScript)
	return writeOwnedUISourceAppForUI(t, appDir, filepath.Join(uiDir, "manifest.yaml"), name)
}

func writeOwnedUISourceAppForUI(t *testing.T, appDir, uiManifestPath, name string) string {
	t.Helper()
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("create source app dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "provider"), []byte(name+"-provider"), 0o755); err != nil {
		t.Fatalf("write provider binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "catalog.yaml"), []byte("name: "+name+"\noperations:\n  - id: ping\n    method: GET\n"), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	ownedUIPath, err := filepath.Rel(appDir, uiManifestPath)
	if err != nil {
		t.Fatalf("relative owned ui path: %v", err)
	}
	buildOutput := ".gestaltd/bin/" + name
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\ncp provider %s\nchmod +x %s\n", buildOutput, buildOutput)
	if err := os.WriteFile(filepath.Join(appDir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("write build script: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:      "github.com/acme/apps/" + name,
		Version:     "1.2.3",
		Kind:        providermanifestv1.KindApp,
		DisplayName: name,
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{
			UI: &providermanifestv1.OwnedUI{Path: filepath.ToSlash(ownedUIPath)},
		}),
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh", "provider"},
		},
	}
	data, err := encodeSourceManifestForTest(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode app manifest: %v", err)
	}
	manifestPath := filepath.Join(appDir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatalf("write app manifest: %v", err)
	}
	return manifestPath
}

func writeSourceUITree(t *testing.T, dir, source, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir source ui dir: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindUI,
		Source:  source,
		Version: version,
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: "dist"},
	}
	data, err := encodeSourceManifestForTest(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode ui manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatalf("write ui manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build.sh"), []byte("mkdir -p dist\nprintf '<html>roadmap</html>\\n' > dist/index.html\n"), 0o755); err != nil {
		t.Fatalf("write ui build script: %v", err)
	}
}

func writeBlockingSourceUITree(t *testing.T, dir, source, version, buildScript string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir source ui dir: %v", err)
	}
	writeNamedBlockingSourceUIManifest(t, dir, "manifest.yaml", source, version, "build.sh")
	if err := os.WriteFile(filepath.Join(dir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("write ui build script: %v", err)
	}
}

func writeNamedBlockingSourceUIManifest(t *testing.T, dir, manifestName, source, version, buildScript string) {
	t.Helper()
	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindUI,
		Source:  source,
		Version: version,
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./" + buildScript},
			Inputs:  []string{buildScript},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: "dist"},
	}
	data, err := encodeSourceManifestForTest(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode ui manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), data, 0o644); err != nil {
		t.Fatalf("write ui manifest: %v", err)
	}
}

func writeLocalUITestConfig(t *testing.T, dir string, uiManifests map[string]string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: gestaltd.config/v8\n")
	b.WriteString(requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")))
	b.WriteString("  ui:\n")
	for _, name := range slices.Sorted(maps.Keys(uiManifests)) {
		b.WriteString("    " + name + ":\n")
		b.WriteString("      source:\n")
		b.WriteString("        path: " + filepath.ToSlash(uiManifests[name]) + "\n")
		b.WriteString("      path: /" + name + "\n")
	}
	b.WriteString("server:\n")
	b.WriteString("  providers:\n")
	b.WriteString("    indexeddb: sqlite\n")
	b.WriteString("  artifactsDir: " + filepath.ToSlash(filepath.Join(dir, "artifacts")) + "\n")
	b.WriteString("  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")

	configPath := filepath.Join(dir, "gestaltd.yaml")
	if err := os.WriteFile(configPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := NewLifecycle().LockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}
	return configPath
}

func writeOwnedUIAppTestConfig(t *testing.T, dir string, apps map[string]string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: gestaltd.config/v8\n")
	b.WriteString(requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")))
	b.WriteString("apps:\n")
	for _, name := range slices.Sorted(maps.Keys(apps)) {
		b.WriteString("  " + name + ":\n")
		b.WriteString("    source:\n")
		b.WriteString("      path: " + filepath.ToSlash(apps[name]) + "\n")
		b.WriteString("    ui:\n")
		b.WriteString("      path: /" + name + "\n")
	}
	b.WriteString("server:\n")
	b.WriteString("  providers:\n")
	b.WriteString("    indexeddb: sqlite\n")
	b.WriteString("  artifactsDir: " + filepath.ToSlash(filepath.Join(dir, "artifacts")) + "\n")
	b.WriteString("  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")

	configPath := filepath.Join(dir, "gestaltd.yaml")
	if err := os.WriteFile(configPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := NewLifecycle().LockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}
	return configPath
}

func writeAppBoundLocalUITestConfig(t *testing.T, dir string, apps, uiManifests map[string]string) (string, *Lockfile) {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: gestaltd.config/v8\n")
	b.WriteString(requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")))
	b.WriteString("  ui:\n")
	for _, name := range slices.Sorted(maps.Keys(uiManifests)) {
		b.WriteString("    " + name + ":\n")
		b.WriteString("      source:\n")
		b.WriteString("        path: " + filepath.ToSlash(uiManifests[name]) + "\n")
	}
	b.WriteString("apps:\n")
	for _, name := range slices.Sorted(maps.Keys(apps)) {
		b.WriteString("  " + name + ":\n")
		b.WriteString("    source:\n")
		b.WriteString("      path: " + filepath.ToSlash(apps[name]) + "\n")
		b.WriteString("    ui:\n")
		b.WriteString("      bundle: " + name + "\n")
		b.WriteString("      path: /" + name + "\n")
	}
	b.WriteString("server:\n")
	b.WriteString("  providers:\n")
	b.WriteString("    indexeddb: sqlite\n")
	b.WriteString("  artifactsDir: " + filepath.ToSlash(filepath.Join(dir, "artifacts")) + "\n")
	b.WriteString("  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")

	configPath := filepath.Join(dir, "gestaltd.yaml")
	if err := os.WriteFile(configPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	lock, err := NewLifecycle().LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("write lockfile: %v", err)
	}
	return configPath, lock
}

func runGitTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

func runGitTestOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func artifactRelPath(binary string) string {
	return providerpkg.PackageExecutablePath(binary, runtime.GOOS)
}

func buildV2Archive(t *testing.T, dir, source, version, binaryContent string) string {
	t.Helper()

	artPath := artifactRelPath("alpha")
	return buildV2ArchiveForArtifact(t, dir, source, version, artPath, "", binaryContent)
}

func buildV2ArchiveForKind(t *testing.T, dir, kind, source, version, artifactPath, binaryContent string) string {
	t.Helper()

	safeName := strings.NewReplacer("/", "-", ".", "_").Replace(kind + "-" + artifactPath + "-" + binaryContent)
	srcDir := filepath.Join(dir, safeName+"-src")
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artifactPath))), 0o755); err != nil {
		t.Fatalf("create provider src dir: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kind:    kind,
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   artifactPath,
			SHA256: sha256hex(binaryContent),
		}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}
	manifestBytes, err := providerpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), manifestBytes, 0o644); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artifactPath)), []byte(binaryContent), 0o755); err != nil {
		t.Fatalf("write provider artifact: %v", err)
	}

	archivePath := filepath.Join(dir, safeName+".tar.gz")
	if err := providerpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir %s: %v", kind, err)
	}
	return archivePath
}

func buildV2ArchiveForArtifact(t *testing.T, dir, source, version, artifactPath, libc, binaryContent string) string {
	t.Helper()

	safeName := strings.NewReplacer("/", "-", ".", "_").Replace(artifactPath + "-" + libc + "-" + binaryContent)
	srcDir := filepath.Join(dir, safeName+"-src")
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artifactPath))), 0755); err != nil {
		t.Fatalf("create provider src dir: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kind:    providermanifestv1.KindApp, Spec: &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				LibC:   libc,
				Path:   artifactPath,
				SHA256: sha256hex(binaryContent),
			},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}

	manifestBytes, err := providerpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644); err != nil {
		t.Fatalf("write provider catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artifactPath)), []byte(binaryContent), 0755); err != nil {
		t.Fatalf("write provider artifact: %v", err)
	}

	archivePath := filepath.Join(dir, safeName+".tar.gz")
	if err := providerpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir app: %v", err)
	}

	return archivePath
}

func TestFingerprintLocalReleaseMetadataIgnoresAdjacentSourceTree(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "provider")
	writeSourceProviderTree(t, sourceDir, "github.com/acme/providers/alpha", "1.0.0", "alpha-binary")
	metadataPath := filepath.Join(sourceDir, "dist", "provider-release.yaml")
	writeProviderReleaseMetadataFileWithStaticValidation(t, metadataPath, providerReleaseMetadataFixture{
		Package: "github.com/acme/providers/alpha",
		Kind:    providermanifestv1.KindApp, Version: "1.0.0",
		Artifacts: map[string]providerrelease.Artifact{
			providerpkg.CurrentPlatformString(): {Path: "alpha.tar.gz", SHA256: "abc123"},
		},
	})

	first, err := fingerprintLocalReleaseMetadataDigest(metadataPath)
	if err != nil {
		t.Fatalf("fingerprintLocalReleaseMetadataDigest first: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "provider.go"), []byte("source changed\n"), 0o644); err != nil {
		t.Fatalf("mutate adjacent source tree: %v", err)
	}
	second, err := fingerprintLocalReleaseMetadataDigest(metadataPath)
	if err != nil {
		t.Fatalf("fingerprintLocalReleaseMetadataDigest second: %v", err)
	}
	if second != first {
		t.Fatalf("local release metadata digest changed after adjacent source edit: %q != %q", second, first)
	}

	writeProviderReleaseMetadataFileWithStaticValidation(t, metadataPath, providerReleaseMetadataFixture{
		Package: "github.com/acme/providers/alpha",
		Kind:    providermanifestv1.KindApp, Version: "1.0.1",
		Artifacts: map[string]providerrelease.Artifact{
			providerpkg.CurrentPlatformString(): {Path: "alpha.tar.gz", SHA256: "abc123"},
		},
	})
	third, err := fingerprintLocalReleaseMetadataDigest(metadataPath)
	if err != nil {
		t.Fatalf("fingerprintLocalReleaseMetadataDigest third: %v", err)
	}
	if third == first {
		t.Fatalf("local release metadata digest did not change after metadata edit: %q", third)
	}
}

func TestLockAtPathsUsesProviderReleaseStaticValidationWithoutUnpackingArchive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/static-lock"
	version := "1.2.3"
	metadataPath := filepath.Join(dir, "provider-release.yaml")
	writeProviderReleaseMetadataFileWithStaticValidation(t, metadataPath, providerReleaseMetadataFixture{
		Package: packageSource,
		Kind:    providermanifestv1.KindApp,
		Version: version,
		Artifacts: map[string]providerrelease.Artifact{
			providerpkg.CurrentPlatformString(): {
				Path:   "missing-archive.tar.gz",
				SHA256: strings.Repeat("a", 64),
			},
		},
		Manifest: &providermanifestv1.Manifest{
			Kind:    providermanifestv1.KindApp,
			Source:  packageSource,
			Version: version,
			Spec:    &providermanifestv1.Spec{},
		},
		Catalog: &catalog.Catalog{
			Name: "static-lock",
			Operations: []catalog.CatalogOperation{
				{ID: "echo", Method: "POST"},
			},
		},
	})

	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: %s
%sapps:
  static-lock:
    source: %s
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.ConfigAPIVersion, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), filepath.ToSlash(metadataPath), filepath.ToSlash(filepath.Join(dir, "artifacts")))
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lock, err := NewLifecycle().LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	entry := lock.Providers.App["static-lock"]
	if entry.ValidationManifest == nil {
		t.Fatal("lock validation manifest is nil")
	}
	if entry.ArtifactManifest != "" || entry.Executable != "" {
		t.Fatalf("runtime paths = (%q, %q), want omitted for static validation fast path", entry.ArtifactManifest, entry.Executable)
	}
}

func TestSyncAtPathsUsesLockedStaticValidationCatalogForOpenAPIProvider(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, "artifacts")
	packageSource := "github.com/acme/tools/static-openapi"
	version := "1.2.3"
	archivePath := buildExecutableArchiveDataWithSpec(t, dir, "static-openapi-src", packageSource, version, providermanifestv1.KindApp, "static-openapi", []byte("static-openapi-binary"), withNoAuthDefaultConnection(&providermanifestv1.Spec{
		Surfaces: &providermanifestv1.ProviderSurfaces{
			OpenAPI: &providermanifestv1.OpenAPISurface{Document: "http://127.0.0.1:1/openapi.yaml"},
		},
	}))
	archiveSHA, err := providerpkg.ArchiveDigest(archivePath)
	if err != nil {
		t.Fatalf("ArchiveDigest: %v", err)
	}

	metadataPath := filepath.Join(dir, "provider-release.yaml")
	writeProviderReleaseMetadataFileWithStaticValidation(t, metadataPath, providerReleaseMetadataFixture{
		Package:     packageSource,
		Kind:        providermanifestv1.KindApp,
		Version:     version,
		ArchivePath: archivePath,
		Artifacts: map[string]providerrelease.Artifact{
			providerpkg.CurrentPlatformString(): {
				Path:   filepath.Base(archivePath),
				SHA256: archiveSHA,
			},
		},
	})

	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: %s
%sapps:
  static-openapi:
    source: %s
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.ConfigAPIVersion, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), filepath.ToSlash(metadataPath), filepath.ToSlash(artifactsDir))
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	if err := lc.SyncAtPathsWithStatePathsOptions([]string{configPath}, StatePaths{}, SyncOptions{Parallelism: 1}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePathsOptions: %v", err)
	}
}

func TestLockAtPathsRejectsProviderReleaseWhenStaticValidationCatalogMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/static-lock"
	version := "1.2.3"
	metadataPath := filepath.Join(dir, "provider-release.yaml")
	writeProviderReleaseMetadataFileWithStaticValidation(t, metadataPath, providerReleaseMetadataFixture{
		Package: packageSource,
		Kind:    providermanifestv1.KindApp,
		Version: version,
		Artifacts: map[string]providerrelease.Artifact{
			providerpkg.CurrentPlatformString(): {
				Path:   "missing-archive.tar.gz",
				SHA256: strings.Repeat("a", 64),
			},
		},
		Manifest: &providermanifestv1.Manifest{
			Kind:    providermanifestv1.KindApp,
			Source:  packageSource,
			Version: version,
			Spec:    &providermanifestv1.Spec{},
		},
		NoCatalog:    true,
		AllowInvalid: true,
	})

	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: %s
%sapps:
  static-lock:
    source: %s
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.ConfigAPIVersion, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), filepath.ToSlash(metadataPath), filepath.ToSlash(filepath.Join(dir, "artifacts")))
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewLifecycle().LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err == nil {
		t.Fatal("LockAtPathsWithStatePaths unexpectedly succeeded")
	}
	if got := err.Error(); !strings.Contains(got, "must include catalog metadata unless the validation manifest is MCP-only") ||
		!strings.Contains(got, packageSource) ||
		!strings.Contains(got, metadataPath) {
		t.Fatalf("LockAtPathsWithStatePaths error = %v, want catalog validation failure", err)
	}
}

func TestLockAtPathsRejectsPackageLocalStaticValidationSurfaceAsSelfContained(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/static-openapi"
	version := "1.2.3"
	metadataPath := filepath.Join(dir, "provider-release.yaml")
	writeProviderReleaseMetadataFileWithStaticValidation(t, metadataPath, providerReleaseMetadataFixture{
		Package: packageSource,
		Kind:    providermanifestv1.KindApp,
		Version: version,
		Artifacts: map[string]providerrelease.Artifact{
			providerpkg.CurrentPlatformString(): {
				Path:   "missing-archive.tar.gz",
				SHA256: strings.Repeat("a", 64),
			},
		},
		Manifest: &providermanifestv1.Manifest{
			Kind:    providermanifestv1.KindApp,
			Source:  packageSource,
			Version: version,
			Spec: &providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					OpenAPI: &providermanifestv1.OpenAPISurface{Document: "openapi.yaml"},
				},
			},
		},
		AllowInvalid: true,
	})

	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := fmt.Sprintf(`
apiVersion: %s
%sapps:
  static-openapi:
    source: %s
server:
  providers:
    indexeddb: sqlite
  artifactsDir: %s
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.ConfigAPIVersion, requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), filepath.ToSlash(metadataPath), filepath.ToSlash(filepath.Join(dir, "artifacts")))
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewLifecycle().LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err == nil {
		t.Fatal("LockAtPathsWithStatePaths unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "validation manifest must be self-contained") {
		t.Fatalf("LockAtPathsWithStatePaths error = %v, want self-contained validation manifest error", err)
	}
}

func buildExecutableArchive(t *testing.T, dir, srcDirName, source, version, kind, binaryName, binaryContent string) string {
	t.Helper()

	return buildExecutableArchiveData(t, dir, srcDirName, source, version, kind, binaryName, []byte(binaryContent))
}

func buildExecutableArchiveWithConfigSchema(t *testing.T, dir, srcDirName, source, version, kind, binaryName, binaryContent string) string {
	t.Helper()

	artPath := artifactRelPath(binaryName)
	srcDir := filepath.Join(dir, srcDirName)
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artPath))), 0o755); err != nil {
		t.Fatalf("create app src dir: %v", err)
	}
	binaryData := []byte(binaryContent)
	manifest := &providermanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kind:    kind,
		Spec:    &providermanifestv1.Spec{ConfigSchemaPath: "config.schema.json"},
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
				Path: artPath,
				SHA256: func() string {
					sum := sha256.Sum256(binaryData)
					return hex.EncodeToString(sum[:])
				}(),
			},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artPath},
	}

	manifestBytes, err := providerpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), manifestBytes, 0o644); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "config.schema.json"), []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatalf("write config schema: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0o644); err != nil {
		t.Fatalf("write provider catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artPath)), binaryData, 0o755); err != nil {
		t.Fatalf("write provider artifact: %v", err)
	}

	archivePath := filepath.Join(dir, srcDirName+".tar.gz")
	if err := providerpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir app: %v", err)
	}
	return archivePath
}

func buildExecutableArchiveFromBinaryPath(t *testing.T, dir, srcDirName, source, version, kind, binaryName, binaryPath string) string {
	t.Helper()

	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read binary %s: %v", binaryPath, err)
	}
	return buildExecutableArchiveData(t, dir, srcDirName, source, version, kind, binaryName, data)
}

func buildExecutableArchiveData(t *testing.T, dir, srcDirName, source, version, kind, binaryName string, binaryData []byte) string {
	t.Helper()

	return buildExecutableArchiveDataWithSpec(t, dir, srcDirName, source, version, kind, binaryName, binaryData, &providermanifestv1.Spec{})
}

func buildExecutableArchiveDataWithSpec(t *testing.T, dir, srcDirName, source, version, kind, binaryName string, binaryData []byte, spec *providermanifestv1.Spec) string {
	t.Helper()

	artPath := artifactRelPath(binaryName)
	srcDir := filepath.Join(dir, srcDirName)
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artPath))), 0755); err != nil {
		t.Fatalf("create app src dir: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:  source,
		Version: version,
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
				Path: artPath,
				SHA256: func() string {
					sum := sha256.Sum256(binaryData)
					return hex.EncodeToString(sum[:])
				}(),
			},
		},
	}
	manifest.Kind = kind
	manifest.Spec = spec
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: artPath}

	manifestBytes, err := providerpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644); err != nil {
		t.Fatalf("write provider catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artPath)), binaryData, 0755); err != nil {
		t.Fatalf("write provider artifact: %v", err)
	}

	archivePath := filepath.Join(dir, srcDirName+".tar.gz")
	if err := providerpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir app: %v", err)
	}

	return archivePath
}

type localExecutableManifestArtifact struct {
	goos       string
	goarch     string
	libc       string
	binaryName string
	data       []byte
}

func writeExecutableSourceManifest(t *testing.T, dir, srcDirName, source, version, kind string, artifacts []localExecutableManifestArtifact) string {
	t.Helper()

	srcDir := filepath.Join(dir, srcDirName)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kind:    kind,
		Spec:    &providermanifestv1.Spec{},
	}
	var primary *localExecutableManifestArtifact
	for i := range artifacts {
		artifact := artifacts[i]
		if artifact.goos == runtime.GOOS && artifact.goarch == runtime.GOARCH && artifact.libc == "" {
			primary = &artifacts[i]
			break
		}
	}
	if primary == nil && len(artifacts) > 0 {
		primary = &artifacts[0]
	}
	if primary == nil {
		t.Fatal("writeExecutableSourceManifest: no entrypoint artifact selected")
	}
	buildOutput, err := providerpkg.SourceBuildOutputPath(manifest, runtime.GOOS)
	if err != nil {
		t.Fatalf("SourceBuildOutputPath: %v", err)
	}
	staging := "staging-binary"
	if err := os.WriteFile(filepath.Join(srcDir, staging), primary.data, 0o755); err != nil {
		t.Fatalf("write staging binary: %v", err)
	}
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\ncp %s %s\nchmod +x %s\n", staging, buildOutput, buildOutput)
	if err := os.WriteFile(filepath.Join(srcDir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("write build script: %v", err)
	}
	manifest.Build = &providermanifestv1.SourceBuild{
		Command: []string{"sh", "./build.sh"},
		Inputs:  []string{"build.sh", staging},
	}
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: buildOutput}
	manifestBytes, err := encodeSourceManifestForTest(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode source manifest: %v", err)
	}
	manifestPath := filepath.Join(srcDir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatalf("write source manifest: %v", err)
	}
	if kind == providermanifestv1.KindApp {
		if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0o644); err != nil {
			t.Fatalf("write provider catalog: %v", err)
		}
	}
	return manifestPath
}

var (
	goSourceSecretsBinaryOnce sync.Once
	goSourceSecretsBinaryPath string
	goSourceSecretsBinaryRoot string
	goSourceSecretsBinaryErr  error
)

// buildGoSourceSecretsBinary returns the path to the Go secrets provider binary
// shared by the source-secrets tests. The binary is byte-identical for every
// caller, so it is compiled once per test-binary run and reused rather than
// rebuilt on each call; the shared output directory is removed in TestMain.
// Compiling this binary under -race dominated these tests, so deduplicating the
// redundant `go build` invocations is a meaningful speedup.
func buildGoSourceSecretsBinary(t *testing.T) string {
	t.Helper()

	goSourceSecretsBinaryOnce.Do(func() {
		goSourceSecretsBinaryRoot, goSourceSecretsBinaryPath, goSourceSecretsBinaryErr = compileGoSourceSecretsBinary(t)
	})
	if goSourceSecretsBinaryErr != nil {
		t.Fatalf("build go source secrets binary: %v", goSourceSecretsBinaryErr)
	}
	return goSourceSecretsBinaryPath
}

func compileGoSourceSecretsBinary(t *testing.T) (root, outputPath string, err error) {
	t.Helper()

	root, err = os.MkdirTemp("", "go-source-secrets-*")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir: %w", err)
	}
	providerDir := filepath.Join(root, "go-secrets")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		return "", "", fmt.Errorf("MkdirAll(providerDir): %w", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "go.mod"), []byte(testutil.GeneratedProviderModuleSource(t, "example.com/test-go-secrets")), 0o644); err != nil {
		return "", "", fmt.Errorf("write go.mod: %w", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "go.sum"), testutil.GeneratedProviderModuleSum(t), 0o644); err != nil {
		return "", "", fmt.Errorf("write go.sum: %w", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "secrets.go"), []byte(testutil.GeneratedSecretsPackageSource()), 0o644); err != nil {
		return "", "", fmt.Errorf("write secrets.go: %w", err)
	}
	mainDir := filepath.Join(providerDir, "cmd", "provider")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		return "", "", fmt.Errorf("MkdirAll cmd/provider: %w", err)
	}
	mainSource := `package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	providerpkg "example.com/test-go-secrets"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeSecretsProvider(ctx, providerpkg.New()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
`
	if err := os.WriteFile(filepath.Join(mainDir, "main.go"), []byte(mainSource), 0o644); err != nil {
		return "", "", fmt.Errorf("write main.go: %w", err)
	}
	outputPath = filepath.Join(root, "secrets-provider")
	cmd := exec.Command("go", "build", "-o", outputPath, "./cmd/provider")
	cmd.Dir = providerDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("go build secrets provider: %w", err)
	}
	return root, outputPath, nil
}

func writeBootstrapSecretsManifest(t *testing.T, dir, source, version string) string {
	t.Helper()

	srcParts := strings.Split(source, "/")
	appName := srcParts[len(srcParts)-1]
	buildOutput := ".gestaltd/bin/" + appName
	manifestPath := filepath.Join(dir, "bootstrap-secrets-manifest.yaml")
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\ncp bootstrap-secrets-binary %s\nchmod +x %s\n", buildOutput, buildOutput)
	if err := os.WriteFile(filepath.Join(dir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("write bootstrap build script: %v", err)
	}
	manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindSecrets,
		Source:  source,
		Version: version,
		Spec:    &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh", "bootstrap-secrets-binary"},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat bootstrap: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("write bootstrap manifest: %v", err)
	}
	bootstrapBinaryData, err := os.ReadFile(buildGoSourceSecretsBinary(t))
	if err != nil {
		t.Fatalf("read bootstrap binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bootstrap-secrets-binary"), bootstrapBinaryData, 0o755); err != nil {
		t.Fatalf("write bootstrap binary input: %v", err)
	}
	return manifestPath
}

func TestSourceAppMetadataURLPrepareAndLockedLoad(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name               string
		apiVersion         string
		localSource        bool
		remoteArchives     bool
		tamperLocalArchive bool
	}{
		{name: "remote metadata url", apiVersion: config.ConfigAPIVersion},
		{name: "local metadata file", apiVersion: config.ConfigAPIVersion, localSource: true},
		{name: "local metadata file with remote archives", apiVersion: config.ConfigAPIVersion, localSource: true, remoteArchives: true},
		{name: "local metadata file rejects tampered archive", apiVersion: config.ConfigAPIVersion, localSource: true, tamperLocalArchive: true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			packageSource := "github.com/acme/tools/alpha"
			version := "1.2.3"
			currentArchivePath := buildV2Archive(t, dir, packageSource, version, "metadata-url-app-binary")
			currentArchiveData, err := os.ReadFile(currentArchivePath)
			if err != nil {
				t.Fatalf("read current archive: %v", err)
			}
			currentArchiveSHA := sha256.Sum256(currentArchiveData)

			extraPlatform := struct {
				goos   string
				goarch string
			}{
				goos:   "linux",
				goarch: "amd64",
			}
			for _, candidate := range []struct {
				goos   string
				goarch string
			}{
				{goos: "linux", goarch: "amd64"},
				{goos: "linux", goarch: "arm64"},
				{goos: "darwin", goarch: "amd64"},
				{goos: "darwin", goarch: "arm64"},
			} {
				if candidate.goos != runtime.GOOS || candidate.goarch != runtime.GOARCH {
					extraPlatform = candidate
					break
				}
			}
			extraPlatformKey := providerpkg.PlatformString(extraPlatform.goos, extraPlatform.goarch)
			extraArchiveData := []byte("metadata-extra-platform-archive")
			extraArchiveSHA := sha256.Sum256(extraArchiveData)

			var metadataCount atomic.Int64
			var currentArchiveCount atomic.Int64
			var extraArchiveCount atomic.Int64
			handlerErrs := make(chan error, 4)
			nextHandlerErr := func() error {
				t.Helper()
				select {
				case err := <-handlerErrs:
					return err
				default:
					return nil
				}
			}

			metadataPath := "/providers/alpha/provider-release.yaml"
			currentArchivePathURL := "/providers/alpha/alpha-current.tar.gz"
			extraArchivePathURL := "/providers/alpha/alpha-extra.tar.gz"
			sourceValue := ""
			wantSource := ""
			wantCurrentArchiveURL := ""
			wantExtraArchiveURL := ""
			localCurrentArchivePath := ""
			localMetadataPath := ""
			var srv *httptest.Server

			if tc.localSource {
				metadataRelPath := filepath.ToSlash(filepath.Join("providers", "alpha", "provider-release.yaml"))
				metadataAbsPath := filepath.Join(dir, filepath.FromSlash(metadataRelPath))
				localMetadataPath = metadataAbsPath
				metadataDir := filepath.Dir(metadataAbsPath)
				if err := os.MkdirAll(metadataDir, 0o755); err != nil {
					t.Fatalf("create metadata dir: %v", err)
				}
				currentArchiveName := "alpha-current.tar.gz"
				extraArchiveName := "alpha-extra.tar.gz"
				currentArtifactPath := currentArchiveName
				extraArtifactPath := extraArchiveName
				localCurrentArchivePath = filepath.Join(metadataDir, currentArchiveName)
				if tc.remoteArchives {
					srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						switch r.URL.Path {
						case currentArchivePathURL:
							currentArchiveCount.Add(1)
							if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
								handlerErrs <- fmt.Errorf("current archive authorization = %q, want %q", got, "Bearer test-token")
								http.Error(w, "bad archive authorization", http.StatusBadRequest)
								return
							}
							w.Header().Set("Content-Type", "application/octet-stream")
							_, _ = w.Write(currentArchiveData)
						case extraArchivePathURL:
							extraArchiveCount.Add(1)
							if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
								handlerErrs <- fmt.Errorf("extra archive authorization = %q, want %q", got, "Bearer test-token")
								http.Error(w, "bad archive authorization", http.StatusBadRequest)
								return
							}
							w.Header().Set("Content-Type", "application/octet-stream")
							_, _ = w.Write(extraArchiveData)
						default:
							http.NotFound(w, r)
						}
					}))
					defer srv.Close()
					currentArtifactPath = srv.URL + currentArchivePathURL
					extraArtifactPath = srv.URL + extraArchivePathURL
				} else {
					if err := os.WriteFile(filepath.Join(metadataDir, currentArchiveName), currentArchiveData, 0o644); err != nil {
						t.Fatalf("write current archive: %v", err)
					}
					if err := os.WriteFile(filepath.Join(metadataDir, extraArchiveName), extraArchiveData, 0o644); err != nil {
						t.Fatalf("write extra archive: %v", err)
					}
				}
				writeProviderReleaseMetadataFileWithStaticValidation(t, metadataAbsPath, providerReleaseMetadataFixture{
					Package: packageSource,
					Kind:    providermanifestv1.KindApp,
					Version: version, Artifacts: map[string]providerrelease.Artifact{
						providerpkg.CurrentPlatformString(): {
							Path:   currentArtifactPath,
							SHA256: hex.EncodeToString(currentArchiveSHA[:]),
						},
						extraPlatformKey: {
							Path:   extraArtifactPath,
							SHA256: hex.EncodeToString(extraArchiveSHA[:]),
						},
					},
				})
				sourceValue = "./" + metadataRelPath
				wantSource = metadataRelPath
				if tc.remoteArchives {
					wantCurrentArchiveURL = srv.URL + currentArchivePathURL
					wantExtraArchiveURL = srv.URL + extraArchivePathURL
				} else {
					wantCurrentArchiveURL = currentArchiveName
					wantExtraArchiveURL = extraArchiveName
				}
			} else {
				srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case metadataPath:
						if r.URL.Path == metadataPath {
							metadataCount.Add(1)
						}
						if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
							handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
							http.Error(w, "bad metadata authorization", http.StatusBadRequest)
							return
						}
						metadata := providerReleaseMetadataFixture{
							Package:     packageSource,
							Kind:        providermanifestv1.KindApp,
							Version:     version,
							ArchivePath: currentArchivePath,
							Artifacts: map[string]providerrelease.Artifact{
								providerpkg.CurrentPlatformString(): {
									Path:   filepath.Base(currentArchivePathURL),
									SHA256: hex.EncodeToString(currentArchiveSHA[:]),
								},
								extraPlatformKey: {
									Path:   filepath.Base(extraArchivePathURL),
									SHA256: hex.EncodeToString(extraArchiveSHA[:]),
								},
							},
						}
						if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
							http.NotFound(w, r)
						}
					case currentArchivePathURL:
						currentArchiveCount.Add(1)
						if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
							handlerErrs <- fmt.Errorf("current archive authorization = %q, want %q", got, "Bearer test-token")
							http.Error(w, "bad archive authorization", http.StatusBadRequest)
							return
						}
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(currentArchiveData)
					case extraArchivePathURL:
						extraArchiveCount.Add(1)
						if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
							handlerErrs <- fmt.Errorf("extra archive authorization = %q, want %q", got, "Bearer test-token")
							http.Error(w, "bad archive authorization", http.StatusBadRequest)
							return
						}
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(extraArchiveData)
					default:
						http.NotFound(w, r)
					}
				}))
				defer srv.Close()
				sourceValue = srv.URL + metadataPath + "?download=1"
				wantSource = sourceValue
				wantCurrentArchiveURL = srv.URL + currentArchivePathURL
				wantExtraArchiveURL = srv.URL + extraArchivePathURL
			}

			artifactsDir := filepath.Join(dir, "prepared-artifacts")
			configPath := filepath.Join(dir, "gestalt.yaml")
			configLines := []string{
				"apiVersion: " + tc.apiVersion,
			}
			if tc.localSource {
				indexedDBSource := "github.com/acme/tools/indexeddb-sqlite"
				indexedDBVersion := "0.0.1"
				indexedDBArchivePath := buildExecutableArchive(t, dir, "indexeddb-src", indexedDBSource, indexedDBVersion, providermanifestv1.KindIndexedDB, "indexeddb", "indexeddb-release-binary")
				indexedDBArchiveData, err := os.ReadFile(indexedDBArchivePath)
				if err != nil {
					t.Fatalf("read indexeddb archive: %v", err)
				}
				indexedDBArchiveSum := sha256.Sum256(indexedDBArchiveData)
				indexedDBRelPath := filepath.ToSlash(filepath.Join("providers", "indexeddb", "provider-release.yaml"))
				indexedDBAbsPath := filepath.Join(dir, filepath.FromSlash(indexedDBRelPath))
				indexedDBDir := filepath.Dir(indexedDBAbsPath)
				if err := os.MkdirAll(indexedDBDir, 0o755); err != nil {
					t.Fatalf("create indexeddb metadata dir: %v", err)
				}
				indexedDBArchiveName := "indexeddb-current.tar.gz"
				if err := os.WriteFile(filepath.Join(indexedDBDir, indexedDBArchiveName), indexedDBArchiveData, 0o644); err != nil {
					t.Fatalf("write indexeddb archive: %v", err)
				}
				writeProviderReleaseMetadataFileWithStaticValidation(t, indexedDBAbsPath, providerReleaseMetadataFixture{
					Package: indexedDBSource,
					Kind:    providermanifestv1.KindIndexedDB,
					Version: indexedDBVersion, Artifacts: map[string]providerrelease.Artifact{
						providerpkg.CurrentPlatformString(): {
							Path:   indexedDBArchiveName,
							SHA256: hex.EncodeToString(indexedDBArchiveSum[:]),
						},
					},
				})
				configLines = append(configLines,
					"providers:",
					"  indexeddb:",
					"    sqlite:",
					"      source: ./"+indexedDBRelPath,
					"      config:",
					"        path: "+filepath.Join(dir, "data.db"),
				)
			} else {
				configLines = append(configLines, strings.Split(strings.TrimSuffix(requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), "\n"), "\n")...)
			}
			configLines = append(configLines,
				"apps:",
				"  alpha:",
			)
			if !tc.localSource || tc.remoteArchives {
				configLines = append(configLines,
					"    source:",
				)
				if tc.localSource {
					configLines = append(configLines, "      path: "+sourceValue)
				} else {
					configLines = append(configLines, "      url: "+sourceValue)
				}
				configLines = append(configLines,
					"      auth:",
					"        token: test-token",
				)
			} else {
				configLines = append(configLines, "    source: "+sourceValue)
			}
			configLines = append(configLines,
				"server:",
				"  providers:",
				"    indexeddb: sqlite",
				"  artifactsDir: "+artifactsDir,
				"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			)
			configYAML := strings.Join(configLines, "\n") + "\n"
			if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			lc := NewLifecycle()
			lock, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
			if err == nil {
				if handlerErr := nextHandlerErr(); handlerErr != nil {
					t.Fatal(handlerErr)
				}
			}
			if err != nil {
				t.Fatalf("LockAtPathsWithStatePaths: %v", err)
			}
			if handlerErr := nextHandlerErr(); handlerErr != nil {
				t.Fatal(handlerErr)
			}

			entry, ok := lock.Providers.App["alpha"]
			if !ok {
				t.Fatal(`lock.Providers.App["alpha"] not found`)
			}
			if entry.Source != wantSource {
				t.Fatalf("entry.Source = %q, want %q", entry.Source, wantSource)
			}
			if entry.Package != packageSource {
				t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
			}
			if entry.Kind != providermanifestv1.KindApp {
				t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindApp)
			}
			if entry.Runtime != providerLockRuntimeExecutable {
				t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerLockRuntimeExecutable)
			}
			if entry.Version != version {
				t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
			}
			if got := entry.Archives[providerpkg.CurrentPlatformString()].URL; got != wantCurrentArchiveURL {
				t.Fatalf("current archive URL = %q, want %q", got, wantCurrentArchiveURL)
			}
			wantCurrentSHA := hex.EncodeToString(currentArchiveSHA[:])
			wantExtraSHA := hex.EncodeToString(extraArchiveSHA[:])
			if got := entry.Archives[providerpkg.CurrentPlatformString()].SHA256; got != wantCurrentSHA {
				t.Fatalf("current platform SHA256 = %q, want %q", got, wantCurrentSHA)
			}
			if got := entry.Archives[extraPlatformKey].SHA256; got != wantExtraSHA {
				t.Fatalf("extra platform SHA256 = %q, want %q", got, wantExtraSHA)
			}
			if !tc.localSource {
				if got := metadataCount.Load(); got != 1 {
					t.Fatalf("metadata request count = %d, want 1", got)
				}
				if got := currentArchiveCount.Load(); got != 0 {
					t.Fatalf("current archive request count = %d, want 0", got)
				}
				if got := extraArchiveCount.Load(); got != 0 {
					t.Fatalf("extra archive request count = %d, want 0", got)
				}
			}

			lockData, err := os.ReadFile(filepath.Join(dir, LockfileName))
			if err != nil {
				t.Fatalf("ReadFile lockfile: %v", err)
			}
			var diskLock Lockfile
			if err := json.Unmarshal(lockData, &diskLock); err != nil {
				t.Fatalf("Unmarshal lockfile: %v", err)
			}
			diskEntry, ok := diskLock.Providers.App["alpha"]
			if !ok {
				t.Fatal(`disk lock providers.app["alpha"] not found`)
			}
			if diskEntry.Package != packageSource {
				t.Fatalf("disk lock package = %q, want %q", diskEntry.Package, packageSource)
			}
			if diskEntry.Source != wantSource {
				t.Fatalf("disk lock source = %q, want %q", diskEntry.Source, wantSource)
			}
			if diskEntry.Runtime != providerLockRuntimeExecutable {
				t.Fatalf("disk lock runtime = %q, want %q", diskEntry.Runtime, providerLockRuntimeExecutable)
			}
			if diskEntry.Kind != providermanifestv1.KindApp {
				t.Fatalf("disk lock kind = %q, want %q", diskEntry.Kind, providermanifestv1.KindApp)
			}
			if got := diskEntry.Archives[providerpkg.CurrentPlatformString()].SHA256; got != wantCurrentSHA {
				t.Fatalf("disk lock current SHA256 = %q, want %q", got, wantCurrentSHA)
			}
			if got := diskEntry.Archives[extraPlatformKey].SHA256; got != wantExtraSHA {
				t.Fatalf("disk lock extra SHA256 = %q, want %q", got, wantExtraSHA)
			}
			if got := diskEntry.Archives[extraPlatformKey].URL; got != wantExtraArchiveURL {
				t.Fatalf("disk lock extra archive URL = %q, want %q", got, wantExtraArchiveURL)
			}

			appRoot := filepath.Join(artifactsDir, "providers", "alpha")
			if err := os.RemoveAll(appRoot); err != nil {
				t.Fatalf("RemoveAll app root: %v", err)
			}
			if tc.tamperLocalArchive {
				if err := os.WriteFile(localCurrentArchivePath, []byte("tampered-local-archive"), 0o644); err != nil {
					t.Fatalf("write tampered archive: %v", err)
				}
			}

			metadataBefore := metadataCount.Load()
			currentBefore := currentArchiveCount.Load()
			err = lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{})
			if tc.tamperLocalArchive {
				if handlerErr := nextHandlerErr(); handlerErr != nil {
					t.Fatal(handlerErr)
				}
				if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
					t.Fatalf("SyncAtPathsWithStatePaths error = %v, want digest mismatch", err)
				}
				return
			}
			if err != nil {
				if handlerErr := nextHandlerErr(); handlerErr != nil {
					t.Fatal(handlerErr)
				}
				t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
			}
			if handlerErr := nextHandlerErr(); handlerErr != nil {
				t.Fatal(handlerErr)
			}
			if !tc.localSource || tc.remoteArchives {
				if got := metadataCount.Load(); got != metadataBefore {
					t.Fatalf("metadata request count during sync = %d, want %d", got, metadataBefore)
				}
				if got := currentArchiveCount.Load() - currentBefore; got != 1 {
					t.Fatalf("current archive request count during sync = %d, want 1", got)
				}
				if got := extraArchiveCount.Load(); got != 0 {
					t.Fatalf("extra archive request count after sync = %d, want 0", got)
				}
			}
			if tc.localSource && tc.remoteArchives {
				cacheDir := filepath.Join(dir, "materialized-cache-local-metadata")
				assertRemoteMaterializedCacheRoundTrip(t, lc, configPath, cacheDir, wantCurrentSHA, &currentArchiveCount, func() error {
					return os.RemoveAll(appRoot)
				}, func() {
					if handlerErr := nextHandlerErr(); handlerErr != nil {
						t.Fatal(handlerErr)
					}
				})
			}
			cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
			if err != nil {
				t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
			}
			if cfg.Apps["alpha"] == nil {
				t.Fatal(`cfg.Apps["alpha"] = nil`)
				return
			}
			if cfg.Apps["alpha"].ResolvedManifest == nil {
				t.Fatal(`cfg.Apps["alpha"].ResolvedManifest = nil`)
				return
			}
			if cfg.Apps["alpha"].ResolvedManifest.Source != packageSource {
				t.Fatalf("ResolvedManifest.Source = %q, want %q", cfg.Apps["alpha"].ResolvedManifest.Source, packageSource)
			}
			executablePath := filepath.Join(artifactsDir, PreparedProvidersDir, "alpha", filepath.FromSlash(artifactRelPath("alpha")))
			if cfg.Apps["alpha"].Command != executablePath {
				t.Fatalf("app command = %q, want %q", cfg.Apps["alpha"].Command, executablePath)
			}
			if tc.localSource {
				writeProviderReleaseMetadataFileWithStaticValidation(t, localMetadataPath, providerReleaseMetadataFixture{
					Package:      packageSource,
					Kind:         providermanifestv1.KindApp,
					Version:      "1.2.4",
					AllowInvalid: true,
					Artifacts: map[string]providerrelease.Artifact{
						providerpkg.CurrentPlatformString(): {
							Path:   wantCurrentArchiveURL,
							SHA256: wantCurrentSHA,
						},
						extraPlatformKey: {
							Path:   wantExtraArchiveURL,
							SHA256: wantExtraSHA,
						},
					},
				})
				if _, _, err := lc.LoadForExecutionAtPath(configPath, true); err == nil || !strings.Contains(err.Error(), `lock entry for provider "alpha" is stale`) {
					t.Fatalf("LoadForExecutionAtPath after stale local metadata error = %v, want stale lock entry", err)
				}
			}
		})
	}
}

func TestSourceProviderPackagePrepareAndLockedSync(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/alpha"
	version := "1.2.3"
	currentArchivePath := buildV2Archive(t, dir, packageSource, version, "provider-package-alpha")
	currentArchiveData, err := os.ReadFile(currentArchivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	currentArchiveSHA := sha256.Sum256(currentArchiveData)
	currentArchiveSHAHex := hex.EncodeToString(currentArchiveSHA[:])

	var indexCount atomic.Int64
	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	var failIndexAndMetadata atomic.Bool
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}
	requireAuth := func(w http.ResponseWriter, r *http.Request, subject string) bool {
		t.Helper()
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			handlerErrs <- fmt.Errorf("%s authorization = %q, want %q", subject, got, "Bearer test-token")
			http.Error(w, "bad authorization", http.StatusBadRequest)
			return false
		}
		return true
	}

	indexPath := "/provider-index.yaml"
	metadataPath := "/providers/alpha/v1.2.3/provider-release.yaml"
	archivePath := "/providers/alpha/v1.2.3/alpha.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case indexPath:
			indexCount.Add(1)
			if failIndexAndMetadata.Load() {
				http.Error(w, "index should not be fetched after lock", http.StatusInternalServerError)
				return
			}
			if !requireAuth(w, r, "index") {
				return
			}
			index := fmt.Sprintf(`
schema: gestaltd-provider-index
schemaVersion: 1
packages:
  github.com/acme/tools/alpha:
    displayName: Alpha
    versions:
      %s:
        metadata: providers/alpha/v1.2.3/provider-release.yaml
        kind: app
        runtime: executable
        platforms:
          - %s
`, version, providerpkg.CurrentPlatformString())
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(index))
		case metadataPath:
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if failIndexAndMetadata.Load() {
				http.Error(w, "metadata should not be fetched after lock", http.StatusInternalServerError)
				return
			}
			if !requireAuth(w, r, "metadata") {
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     packageSource,
				Kind:        providermanifestv1.KindApp,
				Version:     version,
				ArchivePath: currentArchivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePath),
						SHA256: currentArchiveSHAHex,
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePath:
			archiveCount.Add(1)
			if !requireAuth(w, r, "archive") {
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"providerRepositories:",
		"  local:",
		"    url: " + srv.URL + indexPath,
		strings.TrimSuffix(requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")), "\n"),
		"apps:",
		"  alpha:",
		"    source:",
		"      repo: local",
		"      package: " + packageSource,
		"      version: \">= 1.0.0, < 2.0.0\"",
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.PrepareAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	entry, ok := lock.Providers.App["alpha"]
	if !ok {
		t.Fatal(`lock.Providers.App["alpha"] not found`)
	}
	if entry.Source != srv.URL+metadataPath {
		t.Fatalf("lock source = %q, want %q", entry.Source, srv.URL+metadataPath)
	}
	if entry.Package != packageSource {
		t.Fatalf("lock package = %q, want %q", entry.Package, packageSource)
	}
	if entry.Version != version {
		t.Fatalf("lock version = %q, want %q", entry.Version, version)
	}
	if got := entry.Archives[providerpkg.CurrentPlatformString()].SHA256; got != currentArchiveSHAHex {
		t.Fatalf("archive SHA256 = %q, want %q", got, currentArchiveSHAHex)
	}
	if got := indexCount.Load(); got != 1 {
		t.Fatalf("index request count = %d, want 1", got)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	entry.Source = srv.URL + metadataPath + "?mirror=1"
	lock.Providers.App["alpha"] = entry
	if err := WriteLockfile(filepath.Join(dir, LockfileName), lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	appRoot := filepath.Join(artifactsDir, "providers", "alpha")
	if err := os.RemoveAll(appRoot); err != nil {
		t.Fatalf("RemoveAll app root: %v", err)
	}
	failIndexAndMetadata.Store(true)
	indexBefore := indexCount.Load()
	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := indexCount.Load(); got != indexBefore {
		t.Fatalf("index request count after sync = %d, want %d", got, indexBefore)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count after sync = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load(); got != archiveBefore+1 {
		t.Fatalf("archive request count after sync = %d, want %d", got, archiveBefore+1)
	}

	cacheDir := filepath.Join(dir, "materialized-cache")
	resetAppRoot := func() error { return os.RemoveAll(appRoot) }
	drainHandlerErr := func() {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	assertCheckSyncDoesNotPopulateMaterializedCache(t, lc, configPath, cacheDir, resetAppRoot)
	assertRemoteMaterializedCacheRoundTrip(t, lc, configPath, cacheDir, currentArchiveSHAHex, &archiveCount, resetAppRoot, drainHandlerErr)
	assertRemoteMaterializedCacheRepair(t, lc, configPath, cacheDir, currentArchiveSHAHex, &archiveCount, resetAppRoot, drainHandlerErr)

	archiveBeforeLoad := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if got := indexCount.Load(); got != indexBefore {
		t.Fatalf("index request count after locked load = %d, want %d", got, indexBefore)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count after locked load = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load(); got != archiveBeforeLoad {
		t.Fatalf("archive request count after locked load = %d, want %d", got, archiveBeforeLoad)
	}
	if cfg.Apps["alpha"] == nil || cfg.Apps["alpha"].ResolvedManifest == nil {
		t.Fatal(`cfg.Apps["alpha"].ResolvedManifest missing`)
	}
	if got := cfg.Apps["alpha"].ResolvedManifest.Source; got != packageSource {
		t.Fatalf("ResolvedManifest.Source = %q, want %q", got, packageSource)
	}
}

func TestSourceProviderPackagesResolveIndexedDBAndS3(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	version := "1.2.3"
	type release struct {
		source       string
		kind         string
		metadataPath string
		archiveFile  string
		archivePath  string
		archiveData  []byte
		archiveSHA   string
	}
	buildRelease := func(name, source, kind string) release {
		t.Helper()
		archiveFile := buildExecutableArchive(t, dir, name+"-src", source, version, kind, name, name+"-binary")
		data, err := os.ReadFile(archiveFile)
		if err != nil {
			t.Fatalf("read %s archive: %v", name, err)
		}
		sum := sha256.Sum256(data)
		return release{
			source:       source,
			kind:         kind,
			metadataPath: "/providers/" + name + "/v1.2.3/provider-release.yaml",
			archiveFile:  archiveFile,
			archivePath:  "/providers/" + name + "/v1.2.3/" + name + ".tar.gz",
			archiveData:  data,
			archiveSHA:   hex.EncodeToString(sum[:]),
		}
	}

	indexedDBRelease := buildRelease("indexeddb", "github.com/acme/tools/indexeddb", providermanifestv1.KindIndexedDB)
	s3Release := buildRelease("s3", "github.com/acme/tools/s3", providermanifestv1.KindS3)
	releases := []release{indexedDBRelease, s3Release}

	var indexCount atomic.Int64
	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	indexPath := "/provider-index.yaml"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == indexPath {
			indexCount.Add(1)
			var b strings.Builder
			b.WriteString("schema: gestaltd-provider-index\nschemaVersion: 1\npackages:\n")
			for _, rel := range releases {
				b.WriteString("  " + rel.source + ":\n")
				b.WriteString("    versions:\n")
				b.WriteString("      " + version + ":\n")
				b.WriteString("        metadata: " + strings.TrimPrefix(rel.metadataPath, "/") + "\n")
				b.WriteString("        kind: " + rel.kind + "\n")
				b.WriteString("        runtime: executable\n")
				b.WriteString("        platforms:\n")
				b.WriteString("          - " + providerpkg.CurrentPlatformString() + "\n")
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(b.String()))
			return
		}
		for _, rel := range releases {
			switch r.URL.Path {
			case rel.metadataPath:
				if r.URL.Path == rel.metadataPath {
					metadataCount.Add(1)
				}
				metadata := providerReleaseMetadataFixture{
					Package:     rel.source,
					Kind:        rel.kind,
					Version:     version,
					ArchivePath: rel.archiveFile,
					Artifacts: map[string]providerrelease.Artifact{
						providerpkg.CurrentPlatformString(): {
							Path:   filepath.Base(rel.archivePath),
							SHA256: rel.archiveSHA,
						},
					},
				}
				if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
					http.NotFound(w, r)
				}
				return
			case rel.archivePath:
				archiveCount.Add(1)
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(rel.archiveData)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"providerRepositories:",
		"  local:",
		"    url: " + srv.URL + indexPath,
		"providers:",
		"  indexeddb:",
		"    main:",
		"      source:",
		"        repo: local",
		"        package: " + indexedDBRelease.source,
		"        version: " + version,
		"      config:",
		"        path: " + filepath.Join(dir, "data.db"),
		"  s3:",
		"    objects:",
		"      source:",
		"        repo: local",
		"        package: " + s3Release.source,
		"        version: " + version,
		"server:",
		"  providers:",
		"    indexeddb: main",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lock, err := NewLifecycle().PrepareAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	indexedDBEntry, ok := lock.Providers.IndexedDB["main"]
	if !ok {
		t.Fatal(`lock.Providers.IndexedDB["main"] not found`)
	}
	if indexedDBEntry.Source != srv.URL+indexedDBRelease.metadataPath {
		t.Fatalf("indexeddb source = %q, want %q", indexedDBEntry.Source, srv.URL+indexedDBRelease.metadataPath)
	}
	if indexedDBEntry.Package != indexedDBRelease.source {
		t.Fatalf("indexeddb package = %q, want %q", indexedDBEntry.Package, indexedDBRelease.source)
	}
	s3Entry, ok := lock.Providers.S3["objects"]
	if !ok {
		t.Fatal(`lock.Providers.S3["objects"] not found`)
	}
	if s3Entry.Source != srv.URL+s3Release.metadataPath {
		t.Fatalf("s3 source = %q, want %q", s3Entry.Source, srv.URL+s3Release.metadataPath)
	}
	if s3Entry.Package != s3Release.source {
		t.Fatalf("s3 package = %q, want %q", s3Entry.Package, s3Release.source)
	}
	if got := indexCount.Load(); got != int64(len(releases)) {
		t.Fatalf("index request count = %d, want %d", got, len(releases))
	}
	if got := metadataCount.Load(); got != int64(len(releases)) {
		t.Fatalf("metadata request count = %d, want %d", got, len(releases))
	}
	if got := archiveCount.Load(); got != int64(len(releases)) {
		t.Fatalf("archive request count = %d, want %d", got, len(releases))
	}
}

func TestSourceWorkflowMetadataURLPrepareAndLockedLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/workflow-runner"
	version := "2.3.4"
	archivePath := buildExecutableArchive(t, dir, "workflow-metadata-src", packageSource, version, providermanifestv1.KindWorkflow, "workflow-runner", "metadata-workflow-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read workflow archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)
	archiveSHAHex := hex.EncodeToString(archiveSHA[:])

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/workflow/provider-release.yaml"
	archivePathURL := "/providers/workflow/workflow-runner.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     packageSource,
				Kind:        providermanifestv1.KindWorkflow,
				Version:     version,
				ArchivePath: archivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: archiveSHAHex,
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := "apiVersion: " + config.ConfigAPIVersion + "\n" + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"  workflow:",
		"    runner:",
		"      source:",
		"        url: " + srv.URL + metadataPath,
		"        auth:",
		"          token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.PrepareAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.Providers.Workflow["runner"]
	if !ok {
		t.Fatal(`lock.Providers.Workflow["runner"] not found`)
	}
	if entry.Package != packageSource {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
	}
	if entry.Kind != providermanifestv1.KindWorkflow {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindWorkflow)
	}
	if entry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerLockRuntimeExecutable)
	}
	if entry.Version != version {
		t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	workflowRoot := filepath.Join(artifactsDir, filepath.FromSlash(PreparedWorkflowDir), "runner")
	if err := os.RemoveAll(workflowRoot); err != nil {
		t.Fatalf("RemoveAll workflow root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	err = lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during sync = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during sync = %d, want 1", got)
	}
	cacheDir := filepath.Join(dir, "materialized-cache")
	assertRemoteMaterializedCacheRoundTrip(t, lc, configPath, cacheDir, archiveSHAHex, &archiveCount, func() error {
		return os.RemoveAll(workflowRoot)
	}, func() {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	})
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	workflow := cfg.Providers.Workflow["runner"]
	if workflow == nil || workflow.ResolvedManifest == nil {
		t.Fatalf("workflow resolved manifest = %+v", workflow)
		return
	}
	if got := workflow.ResolvedManifest.Kind; got != providermanifestv1.KindWorkflow {
		t.Fatalf("workflow manifest kind = %q, want %q", got, providermanifestv1.KindWorkflow)
	}
	if got := workflow.Command; got != resolveLockPath(artifactsDir, entry.Executable) {
		t.Fatalf("workflow command = %q, want %q", got, resolveLockPath(artifactsDir, entry.Executable))
	}
}

func TestSourceExternalCredentialsMetadataURLPrepareAndLockedLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/external-credentials-runner"
	version := "1.4.2"
	archivePath := buildExecutableArchive(t, dir, "external-credentials-src", packageSource, version, providermanifestv1.KindExternalCredentials, "external-credentials-runner", "metadata-external-credentials-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read external credentials archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/external-credentials/provider-release.yaml"
	archivePathURL := "/providers/external-credentials/external-credentials-runner.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     packageSource,
				Kind:        providermanifestv1.KindExternalCredentials,
				Version:     version,
				ArchivePath: archivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := "apiVersion: " + config.ConfigAPIVersion + "\n" + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"  externalCredentials:",
		"    runner:",
		"      source:",
		"        url: " + srv.URL + metadataPath,
		"        auth:",
		"          token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    externalCredentials: runner",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.PrepareAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.Providers.ExternalCredentials["runner"]
	if !ok {
		t.Fatal(`lock.Providers.ExternalCredentials["runner"] not found`)
	}
	if entry.Package != packageSource {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
	}
	if entry.Kind != providermanifestv1.KindExternalCredentials {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindExternalCredentials)
	}
	if entry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerLockRuntimeExecutable)
	}
	if entry.Version != version {
		t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	externalCredentialsRoot := filepath.Join(artifactsDir, filepath.FromSlash(PreparedExternalCredentialsDir), "runner")
	if err := os.RemoveAll(externalCredentialsRoot); err != nil {
		t.Fatalf("RemoveAll external credentials root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	err = lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during sync = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during sync = %d, want 1", got)
	}
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	externalCredentials := cfg.Providers.ExternalCredentials["runner"]
	if externalCredentials == nil || externalCredentials.ResolvedManifest == nil {
		t.Fatalf("external credentials resolved manifest = %+v", externalCredentials)
		return
	}
	if got := externalCredentials.ResolvedManifest.Kind; got != providermanifestv1.KindExternalCredentials {
		t.Fatalf("external credentials manifest kind = %q, want %q", got, providermanifestv1.KindExternalCredentials)
	}
	if got := externalCredentials.Command; got != resolveLockPath(artifactsDir, entry.Executable) {
		t.Fatalf("external credentials command = %q, want %q", got, resolveLockPath(artifactsDir, entry.Executable))
	}
}

func TestSourceUIMetadataURLPrepareAndLockedLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/roadmap-ui"
	version := "0.9.1"
	archivePath := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      packageSource,
		Version:     version,
		DisplayName: "Roadmap UI",
		Spec: &providermanifestv1.Spec{
			AssetRoot: "dist",
		},
	}, map[string]string{
		"dist/index.html": "<html>roadmap</html>",
	}, false)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read ui archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)
	archiveSHAHex := hex.EncodeToString(archiveSHA[:])

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/roadmap/provider-release.yaml"
	archivePathURL := "/providers/roadmap/roadmap-ui.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     packageSource,
				Kind:        providermanifestv1.KindUI,
				Version:     version,
				ArchivePath: archivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: archiveSHAHex,
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := "apiVersion: " + config.ConfigAPIVersion + "\n" + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"  ui:",
		"    roadmap:",
		"      source:",
		"        url: " + srv.URL + metadataPath + "?download=1",
		"        auth:",
		"          token: test-token",
		"      path: /roadmap",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.PrepareAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.Providers.UI["roadmap"]
	if !ok {
		t.Fatal(`lock.Providers.UI["roadmap"] not found`)
	}
	if entry.Source != srv.URL+metadataPath+"?download=1" {
		t.Fatalf("entry.Source = %q, want %q", entry.Source, srv.URL+metadataPath+"?download=1")
	}
	if entry.Package != packageSource {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
	}
	if entry.Kind != providermanifestv1.KindUI {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindUI)
	}
	if entry.Runtime != providerLockRuntimeUI {
		t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerLockRuntimeUI)
	}
	if entry.Version != version {
		t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	uiRoot := filepath.Join(artifactsDir, filepath.FromSlash(PreparedUIDir), "roadmap")
	if err := os.RemoveAll(uiRoot); err != nil {
		t.Fatalf("RemoveAll ui root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	err = lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during sync = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during sync = %d, want 1", got)
	}
	cacheDir := filepath.Join(dir, "materialized-cache")
	assertRemoteMaterializedCacheRoundTrip(t, lc, configPath, cacheDir, archiveSHAHex, &archiveCount, func() error {
		return os.RemoveAll(uiRoot)
	}, func() {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	})
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	ui := cfg.Providers.UI["roadmap"]
	if ui == nil || ui.ResolvedManifest == nil {
		t.Fatalf("ui resolved manifest = %+v", ui)
		return
	}
	if got := ui.ResolvedManifest.Kind; got != providermanifestv1.KindUI {
		t.Fatalf("ui manifest kind = %q, want %q", got, providermanifestv1.KindUI)
	}
	if got := ui.ResolvedManifest.Source; got != packageSource {
		t.Fatalf("ui manifest source = %q, want %q", got, packageSource)
	}
	if got := ui.ResolvedAssetRoot; got != resolveLockPath(artifactsDir, entry.AssetRoot) {
		t.Fatalf("ui asset root = %q, want %q", got, resolveLockPath(artifactsDir, entry.AssetRoot))
	}
}

func TestSourceAppPrepareRejectsMetadataSourceManifestMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/gadget"
	version := "2.0.0"

	archivePath := buildV2Archive(t, dir, "github.com/acme/tools/other-gadget", version, "fake-gadget-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	metadataPath := "/providers/gadget/provider-release.yaml"
	archivePathURL := "/providers/gadget/gadget-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadata := providerReleaseMetadataFixture{
				Package:      packageSource,
				Kind:         providermanifestv1.KindApp,
				Version:      version,
				ArchivePath:  archivePath,
				AllowInvalid: true,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePathURL:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"apps:",
		"  gadget:",
		"    source: " + srv.URL + metadataPath,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	_, err = lc.PrepareAtPath(configPath)
	if err == nil {
		t.Fatal("PrepareAtPath unexpectedly succeeded")
		return
	}
	if !strings.Contains(err.Error(), `provider "gadget" manifest source "github.com/acme/tools/other-gadget" does not match metadata package "github.com/acme/tools/gadget"`) {
		t.Fatalf("PrepareAtPath error = %v, want installed manifest package mismatch", err)
	}
}

func TestSourceAppMetadataURLUsesGenericAuthenticatedFetch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	const packageSource = testSource
	const version = testVersion

	currentArchivePath := buildV2Archive(t, dir, packageSource, version, "metadata-github-asset-app-binary")
	currentArchiveData, err := os.ReadFile(currentArchivePath)
	if err != nil {
		t.Fatalf("read current archive: %v", err)
	}
	currentArchiveSHA := sha256.Sum256(currentArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/releases/assets/999"
	archivePath := "/releases/assets/123"
	metadataURL := ""
	archiveURL := ""
	var currentMu sync.RWMutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("metadata accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad metadata accept", http.StatusBadRequest)
				return
			}
			currentMu.RLock()
			currentArchiveURL := archiveURL
			currentMu.RUnlock()
			metadata := providerReleaseMetadataFixture{
				Package:     packageSource,
				Kind:        providermanifestv1.KindApp,
				Version:     version,
				ArchivePath: currentArchivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   currentArchiveURL,
						SHA256: hex.EncodeToString(currentArchiveSHA[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePath:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("archive accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad archive accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	currentMu.Lock()
	metadataURL = srv.URL + metadataPath
	archiveURL = srv.URL + archivePath
	currentMu.Unlock()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"apps:",
		"  alpha:",
		"    source:",
		"      url: " + metadataURL,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.PrepareAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.Providers.App["alpha"]
	if !ok {
		t.Fatal(`lock.Providers.App["alpha"] not found`)
	}
	if got := entry.Archives[providerpkg.CurrentPlatformString()].URL; got != archiveURL {
		t.Fatalf("current archive URL = %q, want %q", got, archiveURL)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	appRoot := filepath.Join(artifactsDir, "providers", "alpha")
	if err := os.RemoveAll(appRoot); err != nil {
		t.Fatalf("RemoveAll app root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	err = lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during sync = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during sync = %d, want 1", got)
	}
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if cfg.Apps["alpha"] == nil || cfg.Apps["alpha"].ResolvedManifest == nil {
		t.Fatal("resolved metadata app manifest missing after locked load")
	}
}

func TestSourceAppGitHubReleaseSourceUsesResolvedAssetURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := buildV2Archive(t, dir, testSource, testVersion, testBinary)
	currentArchiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	currentArchiveSHA := sha256.Sum256(currentArchiveData)

	const (
		repo         = "valon-technologies/toolshed"
		tag          = "apps/workplace-hub/v0.0.1-alpha.1"
		metadataID   = int64(101)
		archiveID    = int64(202)
		metadataName = "provider-release.yaml"
		archiveName  = "alpha-current.tar.gz"
	)

	archiveAssetURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/assets/%d", repo, archiveID)
	logicalSource := "github-release://github.com/valon-technologies/toolshed?asset=provider-release.yaml&tag=apps%2Fworkplace-hub%2Fv0.0.1-alpha.1"

	var releaseCount atomic.Int64
	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 8)
	releaseFixture := func() providerReleaseFixtureFiles {
		return newProviderReleaseFixtureFiles(t, providerReleaseMetadataFixture{
			Package:     testSource,
			Kind:        providermanifestv1.KindApp,
			Version:     testVersion,
			ArchivePath: archivePath,
			Artifacts: map[string]providerrelease.Artifact{
				providerpkg.CurrentPlatformString(): {
					Path:   "./" + archiveName,
					SHA256: hex.EncodeToString(currentArchiveSHA[:]),
				},
			},
		})
	}
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPath := r.URL.EscapedPath()
		switch {
		case escapedPath == "/repos/valon-technologies/toolshed/releases/tags/"+url.PathEscape(tag) || r.URL.Path == "/repos/valon-technologies/toolshed/releases/tags/"+tag:
			releaseCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("release authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad release authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
				handlerErrs <- fmt.Errorf("release accept = %q, want %q", got, "application/vnd.github+json")
				http.Error(w, "bad release accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"assets":[{"id":%d,"name":"%s"},{"id":%d,"name":"%s"}]}`,
				metadataID, metadataName,
				archiveID, archiveName,
			)
		case escapedPath == fmt.Sprintf("/repos/%s/releases/assets/%d", repo, metadataID):
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("metadata accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad metadata accept", http.StatusBadRequest)
				return
			}
			setYAMLContentType(w)
			_, _ = w.Write(releaseFixture().Metadata)
		case escapedPath == fmt.Sprintf("/repos/%s/releases/assets/%d", repo, archiveID):
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("archive accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad archive accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	baseURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	serverClient := srv.Client()
	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			clone := req.Clone(req.Context())
			clone.URL.Scheme = baseURL.Scheme
			clone.URL.Host = baseURL.Host
			clone.Host = baseURL.Host
			return serverClient.Transport.RoundTrip(clone)
		}),
	}

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"apps:",
		"  alpha:",
		"    source:",
		"      githubRelease:",
		"        repo: " + repo,
		"        tag: " + tag,
		"        asset: " + metadataName,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle().WithHTTPClient(client)
	lock, err := lc.PrepareAtPath(configPath)
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	entry, ok := lock.Providers.App["alpha"]
	if !ok {
		t.Fatal(`lock.Providers.App["alpha"] not found`)
	}
	if entry.Source != logicalSource {
		t.Fatalf("lock source = %q, want %q", entry.Source, logicalSource)
	}
	if got := entry.Archives[providerpkg.CurrentPlatformString()].URL; got != archiveAssetURL {
		t.Fatalf("current archive URL = %q, want %q", got, archiveAssetURL)
	}
	if got := releaseCount.Load(); got != 1 {
		t.Fatalf("release request count = %d, want 1", got)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}
	if err := nextHandlerErr(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceAppMetadataURLRetriesTransientRemoteMetadataFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := buildV2Archive(t, dir, testSource, testVersion, testBinary)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	const metadataPath = "/providers/alpha/provider-release.yaml"
	const archivePathURL = "/providers/alpha/alpha-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			count := metadataCount.Load()
			if r.URL.Path == metadataPath {
				count = metadataCount.Add(1)
			}
			if count <= 2 {
				http.Error(w, "bad gateway", http.StatusBadGateway)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("metadata accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad metadata accept", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     testSource,
				Kind:        providermanifestv1.KindApp,
				Version:     testVersion,
				ArchivePath: archivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   "./alpha-current.tar.gz",
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePathURL:
			archiveCount.Add(1)
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"apps:",
		"  alpha:",
		"    source:",
		"      url: " + srv.URL + metadataPath,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.PrepareAtPath(configPath); err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != 3 {
		t.Fatalf("metadata request count = %d, want 3", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}
}

func TestSourceAppMetadataURLRejectsOversizedRemoteMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var metadataCount atomic.Int64
	handlerErrs := make(chan error, 2)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/releases/assets/999"
	oversizedBody := bytes.Repeat([]byte("x"), providerrelease.MaxBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != metadataPath {
			http.NotFound(w, r)
			return
		}
		metadataCount.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
			http.Error(w, "bad metadata authorization", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/octet-stream" {
			handlerErrs <- fmt.Errorf("metadata accept = %q, want %q", got, "application/octet-stream")
			http.Error(w, "bad metadata accept", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(oversizedBody)
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"apps:",
		"  alpha:",
		"    source:",
		"      url: " + srv.URL + metadataPath,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	_, err := lc.PrepareAtPath(configPath)
	if err == nil {
		t.Fatal("PrepareAtPath unexpectedly succeeded")
		return
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("provider release metadata exceeds %d byte limit", providerrelease.MaxBytes)) {
		t.Fatalf("PrepareAtPath error = %v, want metadata size limit", err)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
}

func TestSourceAppMetadataURLUnlockedLoadRefreshesMutableMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	const packageSource = testSource
	const initialVersion = "1.0.0"
	const updatedVersion = "1.0.1"

	initialArchivePath := buildV2Archive(t, dir, packageSource, initialVersion, "metadata-mutable-app-v1")
	initialArchiveData, err := os.ReadFile(initialArchivePath)
	if err != nil {
		t.Fatalf("read initial archive: %v", err)
	}
	initialArchiveSHA := sha256.Sum256(initialArchiveData)

	updatedArchivePath := buildV2Archive(t, dir, packageSource, updatedVersion, "metadata-mutable-app-v2")
	updatedArchiveData, err := os.ReadFile(updatedArchivePath)
	if err != nil {
		t.Fatalf("read updated archive: %v", err)
	}
	updatedArchiveSHA := sha256.Sum256(updatedArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	var currentMu sync.RWMutex
	currentVersion := initialVersion
	currentArchivePath := initialArchivePath
	currentArchiveData := initialArchiveData
	currentArchiveSHA := initialArchiveSHA

	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/alpha/provider-release.yaml"
	currentArchivePathURL := "/providers/alpha/alpha-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			currentMu.RLock()
			version := currentVersion
			archiveSHA := currentArchiveSHA
			currentMu.RUnlock()
			metadata := providerReleaseMetadataFixture{
				Package:     packageSource,
				Kind:        providermanifestv1.KindApp,
				Version:     version,
				ArchivePath: currentArchivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(currentArchivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case currentArchivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			currentMu.RLock()
			archiveData := currentArchiveData
			currentMu.RUnlock()
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"apps:",
		"  alpha:",
		"    source:",
		"      url: " + srv.URL + metadataPath,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.PrepareAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := lock.Providers.App["alpha"].Version; got != initialVersion {
		t.Fatalf("initial lock version = %q, want %q", got, initialVersion)
	}

	currentMu.Lock()
	currentVersion = updatedVersion
	currentArchivePath = updatedArchivePath
	currentArchiveData = updatedArchiveData
	currentArchiveSHA = updatedArchiveSHA
	currentMu.Unlock()

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, false)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=false): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count after unlocked load = %d, want %d (fresh lockfile should not re-resolve metadata)", got, metadataBefore)
	}
	if got := archiveCount.Load(); got != archiveBefore {
		t.Fatalf("archive request count after unlocked load = %d, want %d (fresh lockfile should not re-materialize artifacts)", got, archiveBefore)
	}
	if cfg.Apps["alpha"] == nil || cfg.Apps["alpha"].ResolvedManifest == nil {
		t.Fatal("resolved metadata app manifest missing after unlocked refresh")
		return
	}
	if got := cfg.Apps["alpha"].ResolvedManifest.Version; got != initialVersion {
		t.Fatalf("resolved manifest version after unlocked load = %q, want %q", got, initialVersion)
	}

	updatedLock, err := ReadLockfile(filepath.Join(dir, LockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := updatedLock.Providers.App["alpha"].Version; got != initialVersion {
		t.Fatalf("lock version after unlocked load = %q, want %q", got, initialVersion)
	}

	lock, err = lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("LockAtPathsWithStatePaths after remote metadata drift: %v", err)
	}
	if got := lock.Providers.App["alpha"].Version; got != updatedVersion {
		t.Fatalf("lock version after explicit re-lock = %q, want %q", got, updatedVersion)
	}
}

func TestMaterializeLockedComponent_AllowsGenericDeclarativeTelemetryAndAuditPackages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const source = "github.com/acme/providers/declarative"
	const version = "1.0.0"

	pkgPath := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  source,
		Version: version,
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://api.example.com",
					Operations: []providermanifestv1.ProviderOperation{
						{Name: "ping", Method: "GET", Path: "/ping"},
					},
				},
			},
		},
	}, nil, false)
	pkgData, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	pkgSum := sha256.Sum256(pkgData)

	lc := NewLifecycle()

	for _, kind := range []string{providerLockKindTelemetry, providerLockKindAudit} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(pkgData)
			}))
			defer srv.Close()

			entry := LockEntry{
				Source:  source,
				Version: version,
				Archives: map[string]LockArchive{
					platformKeyGeneric: {
						URL:    srv.URL,
						SHA256: hex.EncodeToString(pkgSum[:]),
					},
				},
			}
			providerEntry := &config.ProviderEntry{
				Source: config.NewMetadataSource("https://example.invalid/github-com-acme-providers-declarative/v1.0.0/provider-release.yaml"),
			}
			destDir := filepath.Join(dir, kind)
			if err := lc.materializeLockedComponent(context.Background(), lifecyclePaths{}, kind, "default", providerEntry, entry, destDir, syncArtifactReasonPreparedMissing); err != nil {
				t.Fatalf("materializeLockedComponent: %v", err)
			}
			install, err := inspectPreparedInstall(destDir)
			if err != nil {
				t.Fatalf("inspectPreparedInstall: %v", err)
			}
			if install.manifest == nil || !install.manifest.IsDeclarativeOnlyProvider() {
				t.Fatalf("prepared manifest = %#v, want declarative manifest", install.manifest)
			}
		})
	}
}

func TestSourceAppLoadForExecution_RehydratesWhenCachedManifestVersionMismatchesLock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "github.com/acme/tools/gadget"
	version := "2.0.0"
	binaryContent := "fake-gadget-binary"

	archivePath := buildV2Archive(t, dir, source, version, binaryContent)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	var downloadCount atomic.Int64
	metadataPath := "/providers/gadget/provider-release.yaml"
	archivePathURL := "/providers/gadget/gadget-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadata := providerReleaseMetadataFixture{
				Package:     source,
				Kind:        providermanifestv1.KindApp,
				Version:     version,
				ArchivePath: archivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePathURL:
			downloadCount.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"apps:",
		"  gadget:",
		"    source: " + srv.URL + metadataPath,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.PrepareAtPath(configPath); err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}

	lock, err := ReadLockfile(filepath.Join(filepath.Dir(configPath), LockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if _, ok := lock.Providers.App["gadget"]; !ok {
		t.Fatal(`lock.Providers.App["gadget"] not found`)
	}
	install, err := inspectPreparedInstall(filepath.Join(artifactsDir, "providers", "gadget"))
	if err != nil {
		t.Fatalf("inspectPreparedInstall: %v", err)
	}
	manifestPath := install.manifestPath

	_, staleManifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile(%s): %v", manifestPath, err)
	}
	staleManifest.Version = "1.9.9"
	staleBytes, err := providerpkg.EncodeManifest(staleManifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, staleBytes, 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", manifestPath, err)
	}

	downloadsBefore := downloadCount.Load()
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if got := downloadCount.Load() - downloadsBefore; got != 1 {
		t.Fatalf("download count during sync = %d, want 1", got)
	}

	gotManifest := cfg.Apps["gadget"].ResolvedManifest
	if gotManifest == nil {
		t.Fatal("ResolvedManifest is nil")
		return
	}
	if gotManifest.Version != version {
		t.Fatalf("ResolvedManifest.Version = %q, want %q", gotManifest.Version, version)
	}

	_, readBack, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile(%s) after rehydrate: %v", manifestPath, err)
	}
	if readBack.Version != version {
		t.Fatalf("cached manifest version = %q, want %q", readBack.Version, version)
	}
}

func TestSourceAuthAppLoadForExecution(t *testing.T) {
	dir := t.TempDir()
	source := "github.com/acme/tools/auth-widget"
	version := "2.0.0"
	binaryContent := "fake-auth-binary"
	bootstrapManifestPath := writeBootstrapSecretsManifest(t, dir, "github.com/acme/tools/bootstrap-secrets", "0.1.0")

	archivePath := buildExecutableArchive(t, dir, "auth-src", source, version, providermanifestv1.KindIdentity, "auth-app", binaryContent)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSum := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(archiveSum[:])

	var metadataCount atomic.Int64
	var downloadCount atomic.Int64
	metadataPath := "/providers/auth/provider-release.yaml"
	archivePathURL := "/providers/auth/auth-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     source,
				Kind:        providermanifestv1.KindIdentity,
				Version:     version,
				ArchivePath: archivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: archiveSHA,
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePathURL:
			downloadCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    secrets:",
		"      source:",
		"        path: " + bootstrapManifestPath,
		"  identity:",
		"    auth:",
		"      source:",
		"        url: " + srv.URL + metadataPath,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: secrets",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"    identity: auth",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("source-token", "ghp_inline_auth_source_token")

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = providerdrivers.SecretsProviderFactory
	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lc = lc.WithSourceAuthSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveSourceAuthSecrets(ctx, cfg, factories)
	})
	lc = lc.WithHTTPClient(srv.Client())
	lock, err := lc.PrepareAtPath(configPath)
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	authLockEntry := mustLockEntryByName(t, lock.Providers.Identity, "auth")
	if authLockEntry.Source != srv.URL+metadataPath {
		t.Fatalf("lock.Providers.Identity[auth].Source = %q, want %q", authLockEntry.Source, srv.URL+metadataPath)
	}
	if authLockEntry.Executable == "" {
		t.Fatal("lock.Providers.Identity.Executable is empty")
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}

	metadataBefore := metadataCount.Load()
	_, _, err = lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
	}

	authRoot := filepath.Join(artifactsDir, "auth")
	if err := os.RemoveAll(authRoot); err != nil {
		t.Fatalf("RemoveAll auth root: %v", err)
	}

	downloadsBefore := downloadCount.Load()
	err = lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths after cache removal: %v", err)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during sync = %d, want %d", got, metadataBefore)
	}
	if got := downloadCount.Load() - downloadsBefore; got != 1 {
		t.Fatalf("download count during sync = %d, want 1", got)
	}
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath after sync: %v", err)
	}

	authProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindIdentity)
	if authProvider == nil {
		t.Fatal("auth provider is nil after load")
		return
	}
	executablePath := resolveLockPath(artifactsDir, authLockEntry.Executable)
	if authProvider.Command != executablePath {
		t.Fatalf("auth provider command = %q, want %q", authProvider.Command, executablePath)
	}
	authCfg, err := config.NodeToMap(authProvider.Config)
	if err != nil {
		t.Fatalf("NodeToMap(auth config): %v", err)
	}
	if authCfg["command"] != executablePath {
		t.Fatalf("auth config command = %v, want %q", authCfg["command"], executablePath)
	}
	sourceCfg, ok := authCfg["source"].(map[string]any)
	if !ok {
		t.Fatalf("auth source config = %#v", authCfg["source"])
	}
	if want := srv.URL + metadataPath; sourceCfg["url"] != want {
		t.Fatalf("auth source url = %v, want %q", sourceCfg["url"], want)
	}
	nested, ok := authCfg["config"].(map[string]any)
	if !ok || nested["clientId"] != "managed-auth-client" {
		t.Fatalf("auth nested config = %#v", authCfg["config"])
	}
}

func TestSourceAuthAppPrepareAllowsMissingEnvPlaceholderInNonStringField(t *testing.T) {
	dir := t.TempDir()
	source := "github.com/acme/tools/auth-widget"
	version := "2.0.0"
	portEnv := "GESTALT_TEST_PORT_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	bootstrapManifestPath := writeBootstrapSecretsManifest(t, dir, "github.com/acme/tools/bootstrap-secrets", "0.1.0")

	archivePath := buildExecutableArchive(t, dir, "auth-src", source, version, providermanifestv1.KindIdentity, "auth-app", "fake-auth-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSum := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(archiveSum[:])

	var metadataCount atomic.Int64
	metadataPath := "/providers/auth/provider-release.yaml"
	archivePathURL := "/providers/auth/auth-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     source,
				Kind:        providermanifestv1.KindIdentity,
				Version:     version,
				ArchivePath: archivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: archiveSHA,
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePathURL:
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    secrets:",
		"      source:",
		"        path: " + bootstrapManifestPath,
		"  identity:",
		"    auth:",
		"      source:",
		"        url: " + srv.URL + metadataPath,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: secrets",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"    identity: auth",
		"  public:",
		"    port: ${" + portEnv + "}",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("source-token", "ghp_inline_auth_source_token")

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = providerdrivers.SecretsProviderFactory
	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lc = lc.WithSourceAuthSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveSourceAuthSecrets(ctx, cfg, factories)
	})
	lc = lc.WithHTTPClient(srv.Client())
	lock, err := lc.PrepareAtPath(configPath)
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}

	authLockEntry := mustLockEntryByName(t, lock.Providers.Identity, "auth")
	if authLockEntry.Source != srv.URL+metadataPath {
		t.Fatalf("lock.Providers.Identity[auth].Source = %q, want %q", authLockEntry.Source, srv.URL+metadataPath)
	}
	if authLockEntry.Executable == "" {
		t.Fatal("lock.Providers.Identity.Executable is empty")
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
}

func TestLockAndSyncSkipRuntimeOnlySecretRefs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSource := "github.com/acme/tools/runtime-secret-free-lock"
	appManifestPath := writeExecutableSourceManifest(t, dir, "app-src", appSource, "1.0.0", providermanifestv1.KindApp, []localExecutableManifestArtifact{{
		goos:       runtime.GOOS,
		goarch:     runtime.GOARCH,
		binaryName: "app",
		data:       []byte("app-binary"),
	}})
	runtimeSecretsManifestPath := writeBootstrapSecretsManifest(t, dir, "github.com/acme/tools/runtime-secrets", "0.1.0")

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    runtime_only:",
		"      source:",
		"        path: " + runtimeSecretsManifestPath,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey:",
		"    secret:",
		"      provider: runtime_only",
		"      name: encryption-key",
		"apps:",
		"  alpha:",
		"    source:",
		"      path: " + appManifestPath,
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fullResolverCalled := atomic.Bool{}
	lc := NewLifecycle().
		WithConfigSecretResolver(func(context.Context, *config.Config) error {
			fullResolverCalled.Store(true)
			return fmt.Errorf("full config secret resolver should not run during lock/sync")
		}).
		WithSourceAuthSecretResolver(func(context.Context, *config.Config) error {
			return nil
		})

	if _, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	if err := lc.CheckLockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("CheckLockAtPathsWithStatePaths: %v", err)
	}
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}
	if err := lc.CheckSyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("CheckSyncAtPathsWithStatePaths: %v", err)
	}
	if fullResolverCalled.Load() {
		t.Fatal("full config secret resolver ran during lock/sync")
	}

	if _, _, err := lc.LoadForExecutionAtPath(configPath, true); err == nil {
		t.Fatal("LoadForExecutionAtPath unexpectedly succeeded without runtime secret resolution")
	}
}

func TestLockRejectsLocalSourceAuthSecretsProviderBeforeFetch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name            string
		secretsYAML     []string
		secretRef       string
		selectedSecrets string
		wantError       string
	}{
		{
			name: "direct local source auth secrets provider",
			secretsYAML: []string{
				"    bootstrap:",
				"      source:",
				"        path: %s",
			},
			secretRef:       "bootstrap",
			selectedSecrets: "bootstrap",
			wantError:       "source.path",
		},
		{
			name: "source auth secrets provider depends on local source secrets provider",
			secretsYAML: []string{
				"    zzlocal:",
				"      source:",
				"        path: %s",
				"    package:",
				"      source:",
				"        url: https://example.invalid/secrets/provider-release.yaml",
				"        auth:",
				"          token:",
				"            secret:",
				"              provider: zzlocal",
				"              name: source-token",
			},
			secretRef:       "package",
			selectedSecrets: "zzlocal",
			wantError:       "package -> zzlocal",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			bootstrapManifestPath := writeBootstrapSecretsManifest(t, dir, "github.com/acme/tools/bootstrap-secrets", "0.1.0")
			secretsYAML := make([]string, 0, len(tc.secretsYAML))
			for _, line := range tc.secretsYAML {
				secretsYAML = append(secretsYAML, strings.ReplaceAll(line, "%s", bootstrapManifestPath))
			}
			configYAML := strings.Join(append([]string{
				"apiVersion: " + config.ConfigAPIVersion,
				requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
				"  secrets:",
			}, append(secretsYAML, []string{
				"server:",
				"  providers:",
				"    indexeddb: sqlite",
				"    secrets: " + tc.selectedSecrets,
				"  artifactsDir: " + filepath.Join(dir, "prepared-artifacts"),
				"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"apps:",
				"  private:",
				"    source:",
				"      url: https://example.invalid/private/provider-release.yaml",
				"      auth:",
				"        token:",
				"          secret:",
				"            provider: " + tc.secretRef,
				"            name: source-token",
			}...)...), "\n") + "\n"

			configPath := filepath.Join(dir, "gestalt.yaml")
			if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			lc := NewLifecycle().
				WithConfigSecretResolver(func(context.Context, *config.Config) error {
					return fmt.Errorf("full config secret resolver should not run during lock")
				}).
				WithSourceAuthSecretResolver(func(context.Context, *config.Config) error {
					return fmt.Errorf("source auth resolver should not run before local source secrets rejection")
				})

			if _, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("LockAtPathsWithStatePaths error = %v, want local source secrets provider rejection", err)
			}
		})
	}
}

func TestStaticValidationNeedsSourceAuthSecretsSkipsNilRuntimeProviders(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Runtime: config.RuntimeConfig{
			Providers: map[string]*config.RuntimeProviderEntry{
				"nil-runtime": nil,
			},
		},
	}

	needs, err := staticValidationNeedsSourceAuthSecrets(lifecyclePaths{}, cfg, newLockfile(), "linux/amd64")
	if err != nil {
		t.Fatalf("staticValidationNeedsSourceAuthSecrets: %v", err)
	}
	if needs {
		t.Fatal("staticValidationNeedsSourceAuthSecrets returned true for nil runtime provider")
	}
}

func TestLockAndSyncResolveSourceAuthSecretRefsFromPackageSecretsProvider(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const (
		secretsSource  = "github.com/acme/tools/package-secrets"
		secretsVersion = "0.1.0"
		appSource      = "github.com/acme/tools/private-app"
		appVersion     = "1.0.0"
		authToken      = "ghp_inline_auth_source_token"
	)
	secretsArchivePath := buildExecutableArchive(
		t,
		dir,
		"package-secrets-src",
		secretsSource,
		secretsVersion,
		providermanifestv1.KindSecrets,
		"secrets-app",
		"fake-secrets-binary",
	)
	secretsArchiveData, err := os.ReadFile(secretsArchivePath)
	if err != nil {
		t.Fatalf("read secrets archive: %v", err)
	}
	secretsArchiveSum := sha256.Sum256(secretsArchiveData)

	appArchivePath := buildExecutableArchive(t, dir, "private-package-auth-src", appSource, appVersion, providermanifestv1.KindApp, "private-app", "private-app-binary")
	appArchiveData, err := os.ReadFile(appArchivePath)
	if err != nil {
		t.Fatalf("read app archive: %v", err)
	}
	appArchiveSum := sha256.Sum256(appArchiveData)

	var indexCount atomic.Int64
	var secretsMetadataCount atomic.Int64
	var secretsArchiveCount atomic.Int64
	var appMetadataCount atomic.Int64
	var appArchiveCount atomic.Int64
	handlerErrs := make(chan error, 8)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}
	requireAppAuth := func(w http.ResponseWriter, r *http.Request) bool {
		t.Helper()
		if got := r.Header.Get("Authorization"); got != "Bearer "+authToken {
			handlerErrs <- fmt.Errorf("%s authorization = %q, want %q", r.URL.Path, got, "Bearer "+authToken)
			http.Error(w, "bad authorization", http.StatusUnauthorized)
			return false
		}
		return true
	}

	indexPath := "/provider-index.yaml"
	secretsMetadataPath := "/providers/secrets/v0.1.0/provider-release.yaml"
	secretsArchivePathURL := "/providers/secrets/v0.1.0/secrets.tar.gz"
	appMetadataPath := "/providers/private/v1.0.0/provider-release.yaml"
	appArchivePathURL := "/providers/private/v1.0.0/private.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case indexPath:
			indexCount.Add(1)
			index := fmt.Sprintf(`
schema: gestaltd-provider-index
schemaVersion: 1
packages:
  %s:
    displayName: Package Secrets
    versions:
      %s:
        metadata: %s
        kind: secrets
        runtime: executable
        platforms:
          - %s
`, secretsSource, secretsVersion, strings.TrimPrefix(secretsMetadataPath, "/"), providerpkg.CurrentPlatformString())
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(index))
		case secretsMetadataPath:
			if r.URL.Path == secretsMetadataPath {
				secretsMetadataCount.Add(1)
			}
			metadata := providerReleaseMetadataFixture{
				Package:     secretsSource,
				Kind:        providermanifestv1.KindSecrets,
				Version:     secretsVersion,
				ArchivePath: secretsArchivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(secretsArchivePathURL),
						SHA256: hex.EncodeToString(secretsArchiveSum[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case secretsArchivePathURL:
			secretsArchiveCount.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(secretsArchiveData)
		case appMetadataPath:
			if r.URL.Path == appMetadataPath {
				appMetadataCount.Add(1)
			}
			if !requireAppAuth(w, r) {
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     appSource,
				Kind:        providermanifestv1.KindApp,
				Version:     appVersion,
				ArchivePath: appArchivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(appArchivePathURL),
						SHA256: hex.EncodeToString(appArchiveSum[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case appArchivePathURL:
			appArchiveCount.Add(1)
			if !requireAppAuth(w, r) {
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(appArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"providerRepositories:",
		"  local:",
		"    url: " + srv.URL + indexPath,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    bootstrap:",
		"      source:",
		"        repo: local",
		"        package: " + secretsSource,
		"        version: " + secretsVersion,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: bootstrap",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"apps:",
		"  private:",
		"    source:",
		"      url: " + srv.URL + appMetadataPath,
		"      auth:",
		"        token:",
		"          secret:",
		"            provider: bootstrap",
		"            name: source-token",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{Secrets: map[string]string{"source-token": authToken}}, nil
	}
	lc := NewLifecycle().
		WithConfigSecretResolver(func(context.Context, *config.Config) error {
			return fmt.Errorf("full config secret resolver should not run during lock/sync")
		}).
		WithSourceAuthSecretResolver(func(ctx context.Context, cfg *config.Config) error {
			return bootstrap.ResolveSourceAuthSecrets(ctx, cfg, factories)
		}).
		WithHTTPClient(srv.Client())

	if _, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := indexCount.Load(); got != 1 {
		t.Fatalf("index request count = %d, want 1", got)
	}
	if got := secretsMetadataCount.Load(); got != 1 {
		t.Fatalf("secrets metadata request count = %d, want 1", got)
	}
	if got := secretsArchiveCount.Load(); got != 1 {
		t.Fatalf("secrets archive request count = %d, want 1", got)
	}
	if got := appMetadataCount.Load(); got != 1 {
		t.Fatalf("app metadata request count = %d, want 1", got)
	}
	if got := appArchiveCount.Load(); got != 0 {
		t.Fatalf("app archive request count = %d, want 0", got)
	}

	if err := lc.CheckLockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("CheckLockAtPathsWithStatePaths: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	lockData, err := os.ReadFile(filepath.Join(dir, LockfileName))
	if err != nil {
		t.Fatalf("ReadFile lockfile: %v", err)
	}
	if strings.Contains(string(lockData), authToken) {
		t.Fatal("lockfile contains resolved source auth token")
	}
	if _, err := lc.LoadForStaticValidationAtPathsWithStatePaths([]string{configPath}, StatePaths{}, StaticValidationOptions{}); err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("LoadForStaticValidationAtPathsWithStatePaths: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	if err := os.RemoveAll(filepath.Join(artifactsDir, "secrets", "bootstrap")); err != nil {
		t.Fatalf("RemoveAll secrets root: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(artifactsDir, "providers", "private")); err != nil {
		t.Fatalf("RemoveAll app root: %v", err)
	}
	indexBefore := indexCount.Load()
	secretsMetadataBefore := secretsMetadataCount.Load()
	appMetadataBefore := appMetadataCount.Load()
	secretsArchivesBefore := secretsArchiveCount.Load()
	appArchivesBefore := appArchiveCount.Load()
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := indexCount.Load(); got != indexBefore {
		t.Fatalf("index request count after sync = %d, want %d", got, indexBefore)
	}
	if got := secretsMetadataCount.Load(); got != secretsMetadataBefore {
		t.Fatalf("secrets metadata request count after sync = %d, want %d", got, secretsMetadataBefore)
	}
	if got := appMetadataCount.Load(); got != appMetadataBefore {
		t.Fatalf("app metadata request count after sync = %d, want %d", got, appMetadataBefore)
	}
	if got := secretsArchiveCount.Load() - secretsArchivesBefore; got != 1 {
		t.Fatalf("secrets archive requests during sync = %d, want 1", got)
	}
	if got := appArchiveCount.Load() - appArchivesBefore; got != 1 {
		t.Fatalf("app archive requests during sync = %d, want 1", got)
	}

	validationLC := NewLifecycle().
		WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
			return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
		}).
		WithSourceAuthSecretResolver(func(ctx context.Context, cfg *config.Config) error {
			return bootstrap.ResolveSourceAuthSecrets(ctx, cfg, factories)
		}).
		WithHTTPClient(srv.Client())
	appMetadataBeforeValidation := appMetadataCount.Load()
	secretsArchivesBeforeValidation := secretsArchiveCount.Load()
	if _, err := validationLC.LoadForValidationAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("LoadForValidationAtPathsWithStatePaths: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := appMetadataCount.Load(); got == appMetadataBeforeValidation {
		t.Fatal("runtime validation did not fetch app metadata")
	}
	if got := secretsArchiveCount.Load() - secretsArchivesBeforeValidation; got != 1 {
		t.Fatalf("secrets archive requests during runtime validation = %d, want 1", got)
	}
}

func TestLockSourceAuthSecretRefsRequireSourceAuthResolver(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bootstrapManifestPath := writeBootstrapSecretsManifest(t, dir, "github.com/acme/tools/bootstrap-secrets", "0.1.0")

	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    bootstrap:",
		"      source:",
		"        path: " + bootstrapManifestPath,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: bootstrap",
		"  artifactsDir: " + filepath.Join(dir, "prepared-artifacts"),
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"apps:",
		"  private:",
		"    source:",
		"      url: https://example.invalid/provider-release.yaml",
		"      auth:",
		"        token:",
		"          secret:",
		"            provider: bootstrap",
		"            name: source-token",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = providerdrivers.SecretsProviderFactory
	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	if _, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err == nil || !strings.Contains(err.Error(), "source auth secret resolver is required") {
		t.Fatalf("LockAtPathsWithStatePaths error = %v, want missing source auth resolver", err)
	}
}

func TestManagedIndexedDBSourcesLoadForExecutionWithMultipleBindings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mainSource := "github.com/acme/providers/indexeddb-main"
	archiveSource := "github.com/acme/providers/indexeddb-archive"
	version := "1.0.0"

	mainManifestPath := writeExecutableSourceManifest(t, dir, "indexeddb-main-src", mainSource, version, providermanifestv1.KindIndexedDB, []localExecutableManifestArtifact{{
		goos: runtime.GOOS, goarch: runtime.GOARCH, binaryName: "indexeddb-main", data: []byte("main-indexeddb-binary"),
	}})
	archiveManifestPath := writeExecutableSourceManifest(t, dir, "indexeddb-archive-src", archiveSource, version, providermanifestv1.KindIndexedDB, []localExecutableManifestArtifact{{
		goos: runtime.GOOS, goarch: runtime.GOARCH, binaryName: "indexeddb-archive", data: []byte("archive-indexeddb-binary"),
	}})

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"providers:",
		"  indexeddb:",
		"    main:",
		"      source:",
		"        path: " + mainManifestPath,
		"      config:",
		`        dsn: "sqlite://main.db"`,
		"    archive:",
		"      source:",
		"        path: " + archiveManifestPath,
		"      config:",
		`        dsn: "sqlite://archive.db"`,
		"server:",
		"  providers:",
		"    indexeddb: main",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle()
	lock, err := lc.PrepareAtPath(configPath)
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if len(lock.Providers.IndexedDB) != 0 {
		t.Fatalf("lock.Providers.IndexedDB = %#v, want no local source entries", lock.Providers.IndexedDB)
	}

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}

	for _, name := range []string{"main", "archive"} {
		entry := cfg.Providers.IndexedDB[name]
		if entry == nil {
			t.Fatalf("cfg.Providers.IndexedDB[%q] = nil", name)
			return
		}
		if entry.ResolvedManifest == nil {
			t.Fatalf("cfg.Providers.IndexedDB[%q].ResolvedManifest = nil", name)
			return
		}
		wantCommand := filepath.Join(artifactsDir, "indexeddb", name, "bin", name)
		if entry.Command != wantCommand {
			t.Fatalf("cfg.Providers.IndexedDB[%q].Command = %q, want %q", name, entry.Command, wantCommand)
		}
	}
}

func TestManagedCacheSourcesLoadForExecutionWithMultipleBindings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sessionSource := "github.com/acme/providers/cache-session"
	rateLimitSource := "github.com/acme/providers/cache-rate-limit"
	version := "1.0.0"
	bootstrapManifestPath := writeBootstrapSecretsManifest(t, dir, "github.com/acme/tools/bootstrap-secrets", "0.1.0")

	sessionManifestPath := writeExecutableSourceManifest(t, dir, "cache-session-src", sessionSource, version, providermanifestv1.KindCache, []localExecutableManifestArtifact{{
		goos: runtime.GOOS, goarch: runtime.GOARCH, binaryName: "cache-session", data: []byte("session-cache-binary"),
	}})
	rateLimitManifestPath := writeExecutableSourceManifest(t, dir, "cache-rate-limit-src", rateLimitSource, version, providermanifestv1.KindCache, []localExecutableManifestArtifact{{
		goos: runtime.GOOS, goarch: runtime.GOARCH, binaryName: "cache-rate-limit", data: []byte("rate-limit-cache-binary"),
	}})

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	indexedDBManifestPath := writeStubIndexedDBManifest(t, dir)
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		"providers:",
		"  secrets:",
		"    session:",
		"      source:",
		"        path: " + bootstrapManifestPath,
		"    rate_limit:",
		"      source:",
		"        path: " + bootstrapManifestPath,
		"  indexeddb:",
		"    main:",
		"      source:",
		"        path: " + indexedDBManifestPath,
		"      config:",
		`        path: "` + filepath.Join(dir, "gestalt.db") + `"`,
		"  cache:",
		"    session:",
		"      source:",
		"        path: " + sessionManifestPath,
		"      config:",
		"        password:",
		"          secret:",
		"            provider: session",
		"            name: generated-secret",
		"    rate_limit:",
		"      source:",
		"        path: " + rateLimitManifestPath,
		"      config:",
		"        password:",
		"          secret:",
		"            provider: rate_limit",
		"            name: source-token",
		"server:",
		"  providers:",
		"    indexeddb: main",
		"    secrets: session",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = providerdrivers.SecretsProviderFactory
	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lock, err := lc.PrepareAtPath(configPath)
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	if len(lock.Providers.Cache) != 0 {
		t.Fatalf("lock.Providers.Cache = %#v, want no local source entries", lock.Providers.Cache)
	}
	lockPath := filepath.Join(dir, LockfileName)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile lockfile: %v", err)
	}
	var diskLock Lockfile
	if err := json.Unmarshal(lockData, &diskLock); err != nil {
		t.Fatalf("Unmarshal lockfile: %v", err)
	}
	if len(diskLock.Providers.Cache) != 0 {
		t.Fatalf("disk lock cache entries = %#v, want no local source entries", diskLock.Providers.Cache)
	}

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}

	wantPasswords := map[string]string{
		"session":    "generated-secret-value",
		"rate_limit": "ghp_inline_auth_source_token",
	}
	for _, name := range []string{"session", "rate_limit"} {
		entry := cfg.Providers.Cache[name]
		if entry == nil {
			t.Fatalf("cfg.Providers.Cache[%q] = nil", name)
			return
		}
		if entry.ResolvedManifest == nil {
			t.Fatalf("cfg.Providers.Cache[%q].ResolvedManifest = nil", name)
			return
		}
		wantCommand := filepath.Join(artifactsDir, "cache", name, "bin", name)
		if entry.Command != wantCommand {
			t.Fatalf("cfg.Providers.Cache[%q].Command = %q, want %q", name, entry.Command, wantCommand)
		}
		runtimeCfg, err := config.NodeToMap(entry.Config)
		if err != nil {
			t.Fatalf("NodeToMap(cache %q config): %v", name, err)
		}
		configMap, ok := runtimeCfg["config"].(map[string]any)
		if !ok {
			t.Fatalf("cache %q runtime config = %#v", name, runtimeCfg["config"])
		}
		if got := configMap["password"]; got != wantPasswords[name] {
			t.Fatalf("cache %q password = %#v, want %q", name, got, wantPasswords[name])
		}
	}
}

func TestManagedCacheSourcesLockRecordsReleaseMetadataArchives(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		apiVersion  string
		localSource bool
	}{
		{name: "remote metadata url", apiVersion: config.ConfigAPIVersion},
		{name: "local metadata file", apiVersion: config.ConfigAPIVersion, localSource: true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cacheSource := "github.com/acme/tools/cache-session"
			version := "1.0.0"

			extraPlatform := struct{ GOOS, GOARCH string }{GOOS: "linux", GOARCH: "amd64"}
			if runtime.GOOS == extraPlatform.GOOS && runtime.GOARCH == extraPlatform.GOARCH {
				extraPlatform = struct{ GOOS, GOARCH string }{GOOS: "darwin", GOARCH: "arm64"}
			}
			extraPlatformKey := providerpkg.PlatformString(extraPlatform.GOOS, extraPlatform.GOARCH)
			currentArchivePath := buildExecutableArchive(t, dir, "cache-src", cacheSource, version, providermanifestv1.KindCache, "cache-app", "fake-cache-binary")
			currentArchiveData, err := os.ReadFile(currentArchivePath)
			if err != nil {
				t.Fatalf("read current archive: %v", err)
			}
			currentArchiveSum := sha256.Sum256(currentArchiveData)
			extraArchivePathURL := "/providers/cache/cache-extra.tar.gz"
			currentArchivePathURL := "/providers/cache/cache-current.tar.gz"
			metadataPath := "/providers/cache/provider-release.yaml"
			extraArchivePath := buildExecutableArchive(t, dir, "cache-extra-src", cacheSource, version, providermanifestv1.KindCache, "cache-extra", "fake-cache-extra-platform-archive")
			extraArchiveData, err := os.ReadFile(extraArchivePath)
			if err != nil {
				t.Fatalf("read extra archive: %v", err)
			}
			extraArchiveSum := sha256.Sum256(extraArchiveData)

			sourceValue := ""
			wantSource := ""
			wantExtraArchiveURL := ""
			var client *http.Client
			var srv *httptest.Server

			if tc.localSource {
				metadataRelPath := filepath.ToSlash(filepath.Join("providers", "cache", "provider-release.yaml"))
				metadataAbsPath := filepath.Join(dir, filepath.FromSlash(metadataRelPath))
				metadataDir := filepath.Dir(metadataAbsPath)
				if err := os.MkdirAll(metadataDir, 0o755); err != nil {
					t.Fatalf("create metadata dir: %v", err)
				}
				currentArchiveName := "cache-current.tar.gz"
				extraArchiveName := "cache-extra.tar.gz"
				if err := os.WriteFile(filepath.Join(metadataDir, currentArchiveName), currentArchiveData, 0o644); err != nil {
					t.Fatalf("write current archive: %v", err)
				}
				if err := os.WriteFile(filepath.Join(metadataDir, extraArchiveName), extraArchiveData, 0o644); err != nil {
					t.Fatalf("write extra archive: %v", err)
				}
				writeProviderReleaseMetadataFileWithStaticValidation(t, metadataAbsPath, providerReleaseMetadataFixture{
					Package: cacheSource,
					Kind:    providermanifestv1.KindCache,
					Version: version, Artifacts: map[string]providerrelease.Artifact{
						providerpkg.CurrentPlatformString(): {
							Path:   currentArchiveName,
							SHA256: hex.EncodeToString(currentArchiveSum[:]),
						},
						extraPlatformKey: {
							Path:   extraArchiveName,
							SHA256: hex.EncodeToString(extraArchiveSum[:]),
						},
					},
				})
				sourceValue = "./" + metadataRelPath
				wantSource = metadataRelPath
				wantExtraArchiveURL = extraArchiveName
			} else {
				srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case metadataPath:
						metadata := providerReleaseMetadataFixture{
							Package:     cacheSource,
							Kind:        providermanifestv1.KindCache,
							Version:     version,
							ArchivePath: currentArchivePath,
							Artifacts: map[string]providerrelease.Artifact{
								providerpkg.CurrentPlatformString(): {
									Path:   filepath.Base(currentArchivePathURL),
									SHA256: hex.EncodeToString(currentArchiveSum[:]),
								},
								extraPlatformKey: {
									Path:   filepath.Base(extraArchivePathURL),
									SHA256: hex.EncodeToString(extraArchiveSum[:]),
								},
							},
						}
						if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
							http.NotFound(w, r)
						}
					case currentArchivePathURL:
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(currentArchiveData)
					case extraArchivePathURL:
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(extraArchiveData)
					default:
						http.NotFound(w, r)
					}
				}))
				defer srv.Close()
				client = srv.Client()
				sourceValue = srv.URL + metadataPath
				wantSource = sourceValue
				wantExtraArchiveURL = srv.URL + extraArchivePathURL
			}

			artifactsDir := filepath.Join(dir, "prepared-artifacts")
			configLines := []string{
				"apiVersion: " + tc.apiVersion,
				"providers:",
				"  cache:",
				"    session:",
				"      source: " + sourceValue,
				"server:",
				"  providers:",
				"    indexeddb: sqlite",
				"  artifactsDir: " + artifactsDir,
				"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}
			configYAML := strings.Join(configLines, "\n") + "\n"

			configPath := filepath.Join(dir, "gestalt.yaml")
			if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			lc := NewLifecycle()
			if client != nil {
				lc = lc.WithHTTPClient(client)
			}
			lock, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{})
			if err != nil {
				t.Fatalf("LockAtPathsWithStatePaths: %v", err)
			}

			entry, ok := lock.Providers.Cache["session"]
			if !ok {
				t.Fatal(`lock.Providers.Cache["session"] not found`)
			}
			if entry.Source != wantSource {
				t.Fatalf("lock source = %q, want %q", entry.Source, wantSource)
			}
			if entry.ValidationManifest == nil {
				t.Fatal("lock static manifest is nil")
			}
			if len(entry.ValidationManifest.Artifacts) != 0 {
				t.Fatalf("lock static manifest artifacts = %+v, want nil", entry.ValidationManifest.Artifacts)
			}
			if entry.ValidationManifest.Entrypoint != nil {
				t.Fatalf("lock static manifest entrypoint = %+v, want nil", entry.ValidationManifest.Entrypoint)
			}
			wantCurrentSHA := hex.EncodeToString(currentArchiveSum[:])
			wantExtraSHA := hex.EncodeToString(extraArchiveSum[:])
			if got := entry.Archives[providerpkg.CurrentPlatformString()].SHA256; got != wantCurrentSHA {
				t.Fatalf("lock current-platform SHA256 = %q, want %q", got, wantCurrentSHA)
			}
			if got := entry.Archives[extraPlatformKey].SHA256; got != wantExtraSHA {
				t.Fatalf("lock extra-platform SHA256 = %q, want %q", got, wantExtraSHA)
			}
			if got := entry.Archives[extraPlatformKey].URL; got != wantExtraArchiveURL {
				t.Fatalf("lock extra-platform URL = %q, want %q", got, wantExtraArchiveURL)
			}

			readBack, err := ReadLockfile(filepath.Join(dir, LockfileName))
			if err != nil {
				t.Fatalf("ReadLockfile: %v", err)
			}
			if got := readBack.Providers.Cache["session"].Source; got != wantSource {
				t.Fatalf("readBack source = %q, want %q", got, wantSource)
			}
			readBackManifest := readBack.Providers.Cache["session"].ValidationManifest
			if readBackManifest == nil {
				t.Fatal("readBack static manifest is nil")
				return
			}
			if len(readBackManifest.Artifacts) != 0 {
				t.Fatalf("readBack static manifest artifacts = %+v, want nil", readBackManifest.Artifacts)
			}
			if readBackManifest.Entrypoint != nil {
				t.Fatalf("readBack static manifest entrypoint = %+v, want nil", readBackManifest.Entrypoint)
			}
			if got := readBack.Providers.Cache["session"].Archives[providerpkg.CurrentPlatformString()].SHA256; got != wantCurrentSHA {
				t.Fatalf("readBack current-platform SHA256 = %q, want %q", got, wantCurrentSHA)
			}
			if got := readBack.Providers.Cache["session"].Archives[extraPlatformKey].SHA256; got != wantExtraSHA {
				t.Fatalf("readBack extra-platform SHA256 = %q, want %q", got, wantExtraSHA)
			}
			if got := readBack.Providers.Cache["session"].Archives[extraPlatformKey].URL; got != wantExtraArchiveURL {
				t.Fatalf("readBack extra-platform URL = %q, want %q", got, wantExtraArchiveURL)
			}
		})
	}
}

func TestSourceSecretsAppBootstrapsManagedAuthSourceToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secretsSourceToken := "ghp_inline_auth_source_token"
	bootstrapSource := "github.com/acme/tools/bootstrap-secrets"
	bootstrapVersion := "0.1.0"
	secretsSource := "github.com/acme/tools/secrets-widget"
	secretsVersion := "1.0.0"
	authSource := "github.com/acme/tools/auth-widget"
	authVersion := "2.0.0"
	_ = writeBootstrapSecretsManifest(t, dir, bootstrapSource, bootstrapVersion)

	secretsArchivePath := buildExecutableArchiveFromBinaryPath(
		t,
		dir,
		"secrets-src",
		secretsSource,
		secretsVersion,
		providermanifestv1.KindSecrets,
		"secrets-app",
		buildGoSourceSecretsBinary(t),
	)
	authArchivePath := buildExecutableArchive(
		t,
		dir,
		"auth-src",
		authSource,
		authVersion,
		providermanifestv1.KindIdentity,
		"auth-app",
		"fake-auth-binary",
	)

	secretsArchiveData, err := os.ReadFile(secretsArchivePath)
	if err != nil {
		t.Fatalf("read secrets archive: %v", err)
	}
	secretsArchiveSum := sha256.Sum256(secretsArchiveData)
	authArchiveData, err := os.ReadFile(authArchivePath)
	if err != nil {
		t.Fatalf("read auth archive: %v", err)
	}
	authArchiveSum := sha256.Sum256(authArchiveData)

	var secretsMetadataCount atomic.Int64
	var authMetadataCount atomic.Int64
	var secretsDownloads atomic.Int64
	var authDownloads atomic.Int64
	secretsMetadataPath := "/providers/secrets/provider-release.yaml"
	secretsArchivePathURL := "/providers/secrets/secrets.tar.gz"
	authMetadataPath := "/providers/auth/provider-release.yaml"
	authArchivePathURL := "/providers/auth/auth.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case secretsMetadataPath:
			if r.URL.Path == secretsMetadataPath {
				secretsMetadataCount.Add(1)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer "+secretsSourceToken {
				http.Error(w, "bad auth header for secrets metadata", http.StatusUnauthorized)
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     secretsSource,
				Kind:        providermanifestv1.KindSecrets,
				Version:     secretsVersion,
				ArchivePath: secretsArchivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(secretsArchivePathURL),
						SHA256: hex.EncodeToString(secretsArchiveSum[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case secretsArchivePathURL:
			secretsDownloads.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer "+secretsSourceToken {
				http.Error(w, "bad auth header for secrets download", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(secretsArchiveData)
		case authMetadataPath:
			if r.URL.Path == authMetadataPath {
				authMetadataCount.Add(1)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad auth header for auth metadata", http.StatusUnauthorized)
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     authSource,
				Kind:        providermanifestv1.KindIdentity,
				Version:     authVersion,
				ArchivePath: authArchivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(authArchivePathURL),
						SHA256: hex.EncodeToString(authArchiveSum[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case authArchivePathURL:
			authDownloads.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				http.Error(w, "bad auth header for auth download", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(authArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    bootstrap:",
		"      source:",
		"        path: ./bootstrap-secrets-manifest.yaml",
		"    secrets:",
		"      source:",
		"        url: " + srv.URL + secretsMetadataPath,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: bootstrap",
		"              name: source-token",
		"  identity:",
		"    auth:",
		"      source:",
		"        url: " + srv.URL + authMetadataPath,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: secrets",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"    identity: auth",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = providerdrivers.SecretsProviderFactory

	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lc = lc.WithSourceAuthSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveSourceAuthSecrets(ctx, cfg, factories)
	})
	lc = lc.WithHTTPClient(srv.Client())

	lock, err := lc.PrepareAtPath(configPath)
	if err != nil {
		t.Fatalf("PrepareAtPath: %v", err)
	}
	secretsLockEntry := mustLockEntryByName(t, lock.Providers.Secrets, "secrets")
	authLockEntry := mustLockEntryByName(t, lock.Providers.Identity, "auth")
	if got := secretsMetadataCount.Load(); got != 1 {
		t.Fatalf("secrets metadata request count = %d, want 1", got)
	}
	if got := authMetadataCount.Load(); got != 1 {
		t.Fatalf("auth metadata request count = %d, want 1", got)
	}

	secretsRoot := filepath.Join(artifactsDir, "secrets", "secrets")
	if err := os.RemoveAll(secretsRoot); err != nil {
		t.Fatalf("RemoveAll secrets provider root: %v", err)
	}
	authRoot := filepath.Join(artifactsDir, "auth")
	if err := os.RemoveAll(authRoot); err != nil {
		t.Fatalf("RemoveAll auth root: %v", err)
	}

	secretsMetadataBefore := secretsMetadataCount.Load()
	authMetadataBefore := authMetadataCount.Load()
	secretsDownloadsBefore := secretsDownloads.Load()
	authDownloadsBefore := authDownloads.Load()
	err = lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{})
	if err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}
	if got := secretsMetadataCount.Load(); got != secretsMetadataBefore {
		t.Fatalf("secrets metadata requests during sync = %d, want %d", got, secretsMetadataBefore)
	}
	if got := authMetadataCount.Load(); got != authMetadataBefore {
		t.Fatalf("auth metadata requests during sync = %d, want %d", got, authMetadataBefore)
	}
	if got := secretsDownloads.Load() - secretsDownloadsBefore; got != 1 {
		t.Fatalf("secrets download count during sync = %d, want 1", got)
	}
	if got := authDownloads.Load() - authDownloadsBefore; got != 1 {
		t.Fatalf("auth download count during sync = %d, want 1", got)
	}
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	authProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindIdentity)
	if authProvider == nil || authProvider.Source.Auth == nil {
		t.Fatalf("auth provider source auth = %#v", authProvider)
		return
	}
	if authProvider.Source.Auth.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved auth source token = %q, want %q", authProvider.Source.Auth.Token, "ghp_inline_auth_source_token")
	}

	secretsExecutablePath := resolveLockPath(artifactsDir, secretsLockEntry.Executable)
	secretsProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindSecrets)
	if secretsProvider == nil {
		t.Fatal("secrets provider is nil after load")
		return
	}
	if secretsProvider.Source.Auth == nil {
		t.Fatalf("secrets provider source auth = %#v", secretsProvider)
		return
	}
	if secretsProvider.Source.Auth.Token != secretsSourceToken {
		t.Fatalf("resolved secrets source token = %q, want %q", secretsProvider.Source.Auth.Token, secretsSourceToken)
	}
	if secretsProvider.Command != secretsExecutablePath {
		t.Fatalf("secrets provider command = %q, want %q", secretsProvider.Command, secretsExecutablePath)
	}
	authExecutablePath := resolveLockPath(artifactsDir, authLockEntry.Executable)
	if authProvider.Command != authExecutablePath {
		t.Fatalf("auth provider command = %q, want %q", authProvider.Command, authExecutablePath)
	}
}

func TestLoadForExecutionAtPath_UnlockedBootstrapMetadataPreparesOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bootstrapSource := "github.com/acme/tools/bootstrap-secrets"
	bootstrapVersion := "0.1.0"
	authSource := "github.com/acme/tools/auth-widget"
	authVersion := "2.0.0"
	_ = writeBootstrapSecretsManifest(t, dir, bootstrapSource, bootstrapVersion)
	authArchivePath := buildExecutableArchive(
		t,
		dir,
		"auth-metadata-src",
		authSource,
		authVersion,
		providermanifestv1.KindIdentity,
		"auth-app",
		"fake-auth-binary",
	)
	authArchiveData, err := os.ReadFile(authArchivePath)
	if err != nil {
		t.Fatalf("read auth archive: %v", err)
	}
	authArchiveSum := sha256.Sum256(authArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/auth/provider-release.yaml"
	archivePathURL := "/providers/auth/auth-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer ghp_inline_auth_source_token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadataFixture{
				Package:     authSource,
				Kind:        providermanifestv1.KindIdentity,
				Version:     authVersion,
				ArchivePath: authArchivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(authArchiveSum[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer ghp_inline_auth_source_token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(authArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    bootstrap:",
		"      source:",
		"        path: ./bootstrap-secrets-manifest.yaml",
		"  identity:",
		"    auth:",
		"      source:",
		"        url: " + srv.URL + metadataPath + "?download=1",
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: bootstrap",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: bootstrap",
		"    identity: auth",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = providerdrivers.SecretsProviderFactory

	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lc = lc.WithSourceAuthSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveSourceAuthSecrets(ctx, cfg, factories)
	})

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, false)
	if err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("LoadForExecutionAtPath(locked=false): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	authProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindIdentity)
	if authProvider == nil || authProvider.Source.Auth == nil {
		t.Fatalf("auth provider source auth = %#v", authProvider)
		return
	}
	if authProvider.Source.Auth.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved auth source token = %q, want %q", authProvider.Source.Auth.Token, "ghp_inline_auth_source_token")
	}
}

func TestLoadForExecutionAtPath_UnlockedMetadataSecretsProviderResolvesConfigSecrets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secretsSource := "github.com/acme/tools/secrets-widget"
	secretsVersion := "1.0.0"

	secretsArchivePath := buildExecutableArchiveFromBinaryPath(
		t,
		dir,
		"secrets-metadata-src",
		secretsSource,
		secretsVersion,
		providermanifestv1.KindSecrets,
		"secrets-app",
		buildGoSourceSecretsBinary(t),
	)
	secretsArchiveData, err := os.ReadFile(secretsArchivePath)
	if err != nil {
		t.Fatalf("read secrets archive: %v", err)
	}
	secretsArchiveSum := sha256.Sum256(secretsArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	metadataPath := "/providers/secrets/provider-release.yaml"
	archivePathURL := "/providers/secrets/secrets-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			if r.URL.Path == metadataPath {
				metadataCount.Add(1)
			}
			metadata := providerReleaseMetadataFixture{
				Package:     secretsSource,
				Kind:        providermanifestv1.KindSecrets,
				Version:     secretsVersion,
				ArchivePath: secretsArchivePath,
				Artifacts: map[string]providerrelease.Artifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(secretsArchiveSum[:]),
					},
				},
			}
			if !serveProviderReleaseFixtureForRequest(t, w, r.URL.Path, metadata) {
				http.NotFound(w, r)
			}
		case archivePathURL:
			archiveCount.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(secretsArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.ConfigAPIVersion,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    secrets:",
		"      source: " + srv.URL + metadataPath + "?download=1",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey:",
		"    secret:",
		"      provider: secrets",
		"      name: source-token",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = providerdrivers.SecretsProviderFactory

	lc := NewLifecycle().WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=false): %v", err)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}
	if got := cfg.Server.EncryptionKey; got != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved encryption key = %q, want %q", got, "ghp_inline_auth_source_token")
	}

	secretsProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindSecrets)
	if secretsProvider == nil {
		t.Fatal("secrets provider is nil after load")
		return
	}
	if secretsProvider.Command == "" {
		t.Fatal("secrets provider command is empty after load")
	}
}

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeProviderLocalFrontend struct {
	ready  chan struct{}
	exited chan struct{}
	url    string
}

func (f fakeProviderLocalFrontend) FrontendReady() <-chan struct{} { return f.ready }
func (f fakeProviderLocalFrontend) AllExited() <-chan struct{}     { return f.exited }
func (f fakeProviderLocalFrontend) FrontendURL() string            { return f.url }

func TestWaitProviderLocalFrontend(t *testing.T) {
	t.Parallel()

	t.Run("returns direct URL after provider and frontend readiness", func(t *testing.T) {
		t.Parallel()
		initialized := make(chan struct{})
		frontend := fakeProviderLocalFrontend{
			ready:  make(chan struct{}),
			exited: make(chan struct{}),
			url:    "http://127.0.0.1:5173/data-platform-dashboard/",
		}
		close(initialized)
		close(frontend.ready)
		got, ok := waitProviderLocalFrontend(context.Background(), initialized, func(app string) (providerLocalFrontend, bool) {
			if app != "data-platform-dashboard" {
				t.Fatalf("lookup app = %q", app)
			}
			return frontend, true
		}, "data-platform-dashboard")
		if !ok || got != frontend.url {
			t.Fatalf("waitProviderLocalFrontend() = %q, %v; want %q, true", got, ok, frontend.url)
		}
	})

	t.Run("stops when frontend exits", func(t *testing.T) {
		t.Parallel()
		initialized := make(chan struct{})
		frontend := fakeProviderLocalFrontend{ready: make(chan struct{}), exited: make(chan struct{})}
		close(initialized)
		close(frontend.exited)
		if got, ok := waitProviderLocalFrontend(context.Background(), initialized, func(string) (providerLocalFrontend, bool) {
			return frontend, true
		}, "dashboard"); ok || got != "" {
			t.Fatalf("waitProviderLocalFrontend() = %q, %v; want empty, false", got, ok)
		}
	})

	t.Run("stops when startup is canceled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if got, ok := waitProviderLocalFrontend(ctx, make(chan struct{}), func(string) (providerLocalFrontend, bool) {
			t.Fatal("lookup should not run")
			return nil, false
		}, "dashboard"); ok || got != "" {
			t.Fatalf("waitProviderLocalFrontend() = %q, %v; want empty, false", got, ok)
		}
	})
}

func TestProviderLocalReadyURL(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		session *providerLocalSession
		want    string
	}{
		{
			name:    "nil session",
			session: nil,
			want:    "",
		},
		{
			name: "provider without mounted UI",
			session: &providerLocalSession{
				PublicURL: "http://localhost:8080/",
			},
			want: "http://localhost:8080/",
		},
		{
			name: "provider with root-mounted UI",
			session: &providerLocalSession{
				PublicURL:         "http://localhost:8080/",
				AutoMountedUIPath: "/",
			},
			want: "http://localhost:8080/",
		},
		{
			name: "provider with mounted UI",
			session: &providerLocalSession{
				PublicURL:         "http://localhost:8080/",
				AutoMountedUIPath: "/data-platform-dashboard",
			},
			want: "http://localhost:8080/data-platform-dashboard/",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := providerLocalReadyURL(test.session); got != test.want {
				t.Fatalf("providerLocalReadyURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMaybeRunServeProviderLocalRejectsLockedWithoutConfig(t *testing.T) {
	t.Parallel()

	_, err := maybeRunServeProviderLocal(serveProviderLocalOptions{
		Paths:         []string{"./ui"},
		Locked:        true,
		LockedAllowed: true,
	})
	if err == nil || !strings.Contains(err.Error(), "--locked requires --config") {
		t.Fatalf("error = %v, want --locked requires --config", err)
	}
}

func TestMaybeRunServeProviderLocalRejectsLockfileAndArtifactsDirWithoutConfig(t *testing.T) {
	t.Parallel()

	_, err := maybeRunServeProviderLocal(serveProviderLocalOptions{
		Paths:        []string{"./ui"},
		ArtifactsDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "--lockfile and --artifacts-dir require --config") {
		t.Fatalf("error = %v, want --lockfile and --artifacts-dir require --config", err)
	}
}

func TestPrepareProviderLocalOverlaySessionCollectsDevAppKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	setAppManifestSource(t, appDir, "github.com/acme/apps/demo")

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
apps:
  demo:
    source: https://example.invalid/apps/demo
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	appManifest := componentProviderManifestPath(t, appDir)
	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:        []string{appManifest, appManifest},
		ConfigPaths:  []string{baseCfg},
		Locked:       true,
		FleetOverlay: true,
	})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	if len(session.DevAppKeys) != 2 {
		t.Fatalf("DevAppKeys = %#v, want two entries", session.DevAppKeys)
	}
	if session.DevAppKeys[0] != "demo" || session.DevAppKeys[1] != "demo" {
		t.Fatalf("DevAppKeys = %#v, want demo twice", session.DevAppKeys)
	}
	if len(session.ConfigPaths) != 3 {
		t.Fatalf("ConfigPaths len = %d, want base + 2 overlays", len(session.ConfigPaths))
	}
}

func TestPrepareProviderLocalOverlaySessionForwardsNoSync(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	setAppManifestSource(t, appDir, "github.com/acme/apps/demo")

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
apps:
  demo:
    source: https://example.invalid/apps/demo
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	appManifest := componentProviderManifestPath(t, appDir)
	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:        []string{appManifest},
		ConfigPaths:  []string{baseCfg},
		Locked:       true,
		NoSync:       true,
		FleetOverlay: true,
	})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	if !session.Locked {
		t.Fatalf("session.Locked = false, want true under fleet overlay")
	}
	if !session.NoSync {
		t.Fatalf("session.NoSync = false, want true so --locked --no-sync binds pinned artifacts as-is")
	}
}

func TestPrepareProviderLocalOverlaySessionRejectsUIKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui", "demo")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: ui
source: github.com/acme/apps/demo-ui
version: "1.0.0"
run:
  command: [sh, -c, echo]
build:
  command: [sh, -c, "mkdir -p out && echo ok > out/index.html"]
spec:
  assetRoot: out
`
	if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	_, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths: []string{uiDir},
	})
	if err == nil || (!strings.Contains(err.Error(), "apps.demo-ui.static") && !strings.Contains(err.Error(), `manifest kind "ui" is not valid`)) {
		t.Fatalf("error = %v, want ui kind rejection", err)
	}
}

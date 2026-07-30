package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type startupRecordingRestarter struct {
	startedApp     string
	startedVersion string
	stoppedApp     string
}

func (*startupRecordingRestarter) Restartable(string) (bool, error) { return true, nil }
func (r *startupRecordingRestarter) StopApp(_ context.Context, app string) error {
	r.stoppedApp = app
	return nil
}
func (r *startupRecordingRestarter) StartApp(_ context.Context, app, version string) error {
	r.startedApp = app
	r.startedVersion = version
	return nil
}
func (*startupRecordingRestarter) AbortRestarts() {}

type serverRuntimeSnapshotter struct{}

func (serverRuntimeSnapshotter) SnapshotRegistryApps() map[string]core.RegistryAppRuntimeObservation {
	return map[string]core.RegistryAppRuntimeObservation{
		"g-issues": {State: core.GestaltdInstanceAppStateNotRunning},
	}
}

func TestRegistryAppStartupMaterializesAndStartsKnownVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fixture := registrytest.NewInstallFixture(t)
	services := testutil.NewStubServices(t)
	installedAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	if _, err := services.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "registry:first-install",
		ToVersion:   fixture.Version,
		Timestamp:   installedAt,
		Metadata: coredata.ChangeRequestMetadata(&core.AppInstallation{
			AppName:     "g-issues",
			Version:     fixture.Version,
			Registry:    "toolshed",
			InstalledAt: installedAt,
			UpdatedAt:   installedAt,
		}),
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}
	restarter := &startupRecordingRestarter{}
	artifactsDir := t.TempDir()
	oldPath := appregistry.MaterializedPath(artifactsDir, "g-issues", "old")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("MkdirAll old version: %v", err)
	}
	cfg := &config.Config{
		AppRegistries: map[string]config.AppRegistryConfig{"toolshed": fixture.Registry},
		Apps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
		Server: config.ServerConfig{ArtifactsDir: artifactsDir},
	}
	result := &bootstrap.Result{Services: services, AppRestarter: restarter}

	registryAppStartup(cfg, result, fixture.Reader)(ctx)

	if restarter.startedApp != "g-issues" || restarter.startedVersion != fixture.Version {
		t.Fatalf("started = %s@%s", restarter.startedApp, restarter.startedVersion)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old version stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(appregistry.MaterializedPath(artifactsDir, "g-issues", fixture.Version), "manifest.yaml")); err != nil {
		t.Fatalf("desired manifest: %v", err)
	}
}

func TestRegistryAppStartupStopsEmptyProjection(t *testing.T) {
	t.Parallel()
	restarter := &startupRecordingRestarter{}
	artifactsDir := t.TempDir()
	oldPath := appregistry.MaterializedPath(artifactsDir, "g-issues", "old")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("MkdirAll old version: %v", err)
	}
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
		Server: config.ServerConfig{ArtifactsDir: artifactsDir},
	}
	result := &bootstrap.Result{Services: testutil.NewStubServices(t), AppRestarter: restarter}

	registryAppStartup(cfg, result, nil)(context.Background())

	if restarter.stoppedApp != "g-issues" {
		t.Fatalf("stopped app = %q", restarter.stoppedApp)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old version stat error = %v, want not exist", err)
	}
}

func TestStartAppRegistryHeartbeatWriterUsesBootstrapReadiness(t *testing.T) {
	t.Setenv("SOURCE_VERSION", "source-v3")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	services := testutil.NewStubServices(t)
	ready := make(chan struct{})
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
	}
	result := &bootstrap.Result{
		Services:                services,
		AppProvidersInitialized: ready,
		AppRuntimeSnapshotter:   serverRuntimeSnapshotter{},
	}
	writer, err := startAppRegistryHeartbeatWriter(ctx, cfg, result)
	if err != nil {
		t.Fatalf("startAppRegistryHeartbeatWriter: %v", err)
	}
	if writer == nil {
		t.Fatal("writer = nil")
	}
	t.Cleanup(writer.Stop)
	time.Sleep(20 * time.Millisecond)
	if heartbeats, err := services.GestaltdInstanceHeartbeats.List(ctx); err != nil || len(heartbeats) != 0 {
		t.Fatalf("heartbeats before ready = %#v, err %v", heartbeats, err)
	}
	close(ready)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		heartbeats, listErr := services.GestaltdInstanceHeartbeats.List(ctx)
		if listErr == nil && len(heartbeats) == 1 {
			if heartbeats[0].SourceVersion != "source-v3" {
				t.Fatalf("source version = %q", heartbeats[0].SourceVersion)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("heartbeat was not written after bootstrap readiness")
}

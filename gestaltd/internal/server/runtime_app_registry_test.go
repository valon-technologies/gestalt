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

func TestServerInstallerWiringPreservesConfiguredRolloutMode(t *testing.T) {
	t.Parallel()
	for _, mode := range []config.AppRegistryRolloutMode{
		config.AppRegistryRolloutModeEnrollment,
		config.AppRegistryRolloutModeHeartbeat,
	} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			services := testutil.NewStubServices(t)
			manual := newAppRegistryInstaller(Config{
				Services:               services,
				AppRegistryRolloutMode: mode,
			})
			if manual == nil || manual.RolloutMode != core.AppRolloutMode(mode) {
				t.Fatalf("manual installer mode = %#v", manual)
			}

			fixture := registrytest.NewInstallFixture(t)
			cfg := &config.Config{
				AppRegistries: map[string]config.AppRegistryConfig{"toolshed": fixture.Registry},
				Apps: map[string]*config.ProviderEntry{
					"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
				},
			}
			cfg.Server.AppRegistry.RolloutMode = mode
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			controller, err := startAppRegistryAutoDeployController(ctx, cfg, &bootstrap.Result{Services: services}, "0.1.0", fixture.Reader)
			if err != nil {
				t.Fatalf("startAppRegistryAutoDeployController: %v", err)
			}
			if controller == nil {
				t.Fatal("auto-deploy controller is nil")
			}
			defer controller.Stop()
			autoInstaller, ok := controller.Installer.(*appregistry.Installer)
			if !ok || autoInstaller.RolloutMode != core.AppRolloutMode(mode) {
				t.Fatalf("auto-deploy installer = %#v", controller.Installer)
			}
		})
	}
}

func TestCatalogPollerWiringPreservesIndependentCadences(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Server.AppRegistry.CatalogPollInterval = "5s"
	cfg.Server.AppRegistry.HeartbeatInterval = "7s"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	poller := startAppRegistryCatalogPoller(
		ctx,
		cfg,
		&bootstrap.Result{Services: testutil.NewStubServices(t)},
		0,
		false,
		nil,
		nil,
	)
	if poller == nil {
		t.Fatal("catalog poller is nil")
	}
	poller.Stop()
	if poller.Interval != 5*time.Second {
		t.Fatalf("catalog interval = %v, want 5s", poller.Interval)
	}
	if poller.HeartbeatEvaluationInterval != 7*time.Second {
		t.Fatalf("heartbeat evaluation interval = %v, want 7s", poller.HeartbeatEvaluationInterval)
	}
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

func TestRegistryAppStartupSkipsRemotePlacement(t *testing.T) {
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
	artifactsDir := t.TempDir()
	restarter := &startupRecordingRestarter{}
	cfg := &config.Config{
		AppRegistries: map[string]config.AppRegistryConfig{"toolshed": fixture.Registry},
		Apps: map[string]*config.ProviderEntry{
			"g-issues": {
				Source: config.ProviderSource{Registry: "toolshed"},
				Remote: config.DefaultRemoteName,
			},
		},
		Server: config.ServerConfig{ArtifactsDir: artifactsDir},
	}
	result := &bootstrap.Result{Services: services, AppRestarter: restarter}

	registryAppStartup(cfg, result, fixture.Reader)(ctx)

	if restarter.startedApp != "" || restarter.stoppedApp != "" {
		t.Fatalf("remote registry lifecycle called restarter: started=%q stopped=%q", restarter.startedApp, restarter.stoppedApp)
	}
	if _, err := os.Stat(appregistry.MaterializedPath(artifactsDir, "g-issues", fixture.Version)); !os.IsNotExist(err) {
		t.Fatalf("remote registry materialized package stat error = %v, want not exist", err)
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

package server

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
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
	cfg := &config.Config{
		AppRegistries: map[string]config.AppRegistryConfig{"toolshed": fixture.Registry},
		Apps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
		Server: config.ServerConfig{ArtifactsDir: t.TempDir()},
	}
	result := &bootstrap.Result{Services: services, AppRestarter: restarter}

	registryAppStartup(cfg, result, fixture.Reader)(ctx)

	if restarter.startedApp != "g-issues" || restarter.startedVersion != fixture.Version {
		t.Fatalf("started = %s@%s", restarter.startedApp, restarter.startedVersion)
	}
}

func TestRegistryAppStartupStopsEmptyProjection(t *testing.T) {
	t.Parallel()
	restarter := &startupRecordingRestarter{}
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
	}
	result := &bootstrap.Result{Services: testutil.NewStubServices(t), AppRestarter: restarter}

	registryAppStartup(cfg, result, nil)(context.Background())

	if restarter.stoppedApp != "g-issues" {
		t.Fatalf("stopped app = %q", restarter.stoppedApp)
	}
}

package appregistry_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestInstaller_does_not_record_change_request_on_failure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(registrySrv.Close)

	registry, err := config.NewGCSAppRegistry(registrytest.Bucket)
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": registry,
		},
		Reader:         registrytest.NewReaderForServer(t, registrySrv.URL),
		ChangeRequests: svc.AppVersionChangeRequests,
		Locks:          svc.AppVersionInstallLocks,
		Rollouts:       svc.AppRollouts,
	}

	_, err = installer.Install(ctx, appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  "missing-version",
		Actor:    "user:alice",
	})
	if err == nil {
		t.Fatal("expected install error")
	}

	requests, err := svc.AppVersionChangeRequests.ListRequestsByApp(ctx, "g-issues")
	if err != nil {
		t.Fatalf("ListRequestsByApp: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestInstaller_does_not_materialize_locally(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		ConfigApps: map[string]*config.ProviderEntry{
			"g-issues": configEntryWithResolvedVersion("0.0.0-config"),
		},
		Reader:         fixture.Reader,
		ChangeRequests: svc.AppVersionChangeRequests,
		Locks:          svc.AppVersionInstallLocks,
		Rollouts:       svc.AppRollouts,
	}

	_, err := installer.Install(ctx, appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
		Actor:    "user:alice",
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	materialized := filepath.Join(artifactsDir, appregistry.RegistryInstallSubdir, "g-issues", fixture.Version)
	if _, err := os.Stat(materialized); err == nil {
		t.Fatalf("expected no local materialization at %s", materialized)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat materialized path: %v", err)
	}
}

func TestInstaller_rejects_already_installed_version(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		ConfigApps: map[string]*config.ProviderEntry{
			"g-issues": configEntryWithResolvedVersion("0.0.0-config"),
		},
		Reader:         fixture.Reader,
		ChangeRequests: svc.AppVersionChangeRequests,
		Locks:          svc.AppVersionInstallLocks,
		Rollouts:       svc.AppRollouts,
	}

	input := appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
		Actor:    "user:alice",
	}
	if _, err := installer.Install(ctx, input); err != nil {
		t.Fatalf("first install: %v", err)
	}

	_, err := installer.Install(ctx, input)
	if err == nil {
		t.Fatal("expected error for already installed version")
	}
	if !errors.Is(err, appregistry.ErrAppVersionAlreadyInstalled) {
		t.Fatalf("install error = %v, want %v", err, appregistry.ErrAppVersionAlreadyInstalled)
	}
}

func TestInstaller_creates_one_active_rollout_per_app(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		ConfigApps: map[string]*config.ProviderEntry{
			"g-issues": configEntryWithResolvedVersion("0.0.0-config"),
		},
		Reader:         fixture.Reader,
		ChangeRequests: svc.AppVersionChangeRequests,
		Locks:          svc.AppVersionInstallLocks,
		Rollouts:       svc.AppRollouts,
		Now:            func() time.Time { return now },
	}

	if _, err := installer.Install(ctx, appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
	}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	rollout, err := svc.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if rollout.Version != fixture.Version || rollout.State != core.AppRolloutStateEnrolling {
		t.Fatalf("rollout = %#v", rollout)
	}
	if rollout.EnrollmentEndsAt != now.Add(appregistry.DefaultRolloutEnrollmentWindow) {
		t.Fatalf("EnrollmentEndsAt = %v", rollout.EnrollmentEndsAt)
	}
	if rollout.Deadline != now.Add(appregistry.DefaultRolloutTimeout) {
		t.Fatalf("Deadline = %v", rollout.Deadline)
	}

	_, err = installer.Install(ctx, appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  "another-version",
	})
	if !errors.Is(err, appregistry.ErrAppRolloutActive) {
		t.Fatalf("second install error = %v, want %v", err, appregistry.ErrAppRolloutActive)
	}
}

func configEntryWithResolvedVersion(version string) *config.ProviderEntry {
	entry := &config.ProviderEntry{}
	entry.Source.SetResolvedPackage("", version)
	return entry
}

func TestInstaller_records_from_version_on_first_install_and_upgrade(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		ConfigApps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
		Reader:         fixture.Reader,
		ChangeRequests: svc.AppVersionChangeRequests,
		Locks:          svc.AppVersionInstallLocks,
		Rollouts:       svc.AppRollouts,
	}

	if _, err := installer.Add(ctx, appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
		Actor:    "user:alice",
	}); err != nil {
		t.Fatalf("first install: %v", err)
	}

	requests, err := svc.AppVersionChangeRequests.ListRequestsByApp(ctx, "g-issues")
	if err != nil {
		t.Fatalf("ListRequestsByApp: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %#v", requests)
	}
	if requests[0].FromVersion != appregistry.RegistryFirstInstallVersion {
		t.Fatalf("first from_version = %q, want %q", requests[0].FromVersion, appregistry.RegistryFirstInstallVersion)
	}
	if requests[0].ToVersion != fixture.Version {
		t.Fatalf("first to_version = %q, want %s", requests[0].ToVersion, fixture.Version)
	}

	if _, err := svc.AppRollouts.MarkComplete(ctx, "g-issues", fixture.Version, time.Now()); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}
	nextVersion := "0.0.0-snapshot.gdef456"
	installer.Reader = fixture.NewDigestMismatchReader(t, nextVersion)
	if _, err := installer.Upgrade(ctx, appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  nextVersion,
		Actor:    "user:alice",
	}); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	requests, err = svc.AppVersionChangeRequests.ListRequestsByApp(ctx, "g-issues")
	if err != nil {
		t.Fatalf("ListRequestsByApp after upgrade: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests after upgrade = %#v", requests)
	}
	var upgradeRequest *core.AppVersionChangeRequest
	for _, request := range requests {
		if request.ToVersion == nextVersion {
			upgradeRequest = request
			break
		}
	}
	if upgradeRequest == nil || upgradeRequest.FromVersion != fixture.Version {
		t.Fatalf("upgrade request = %#v, want from_version %q", upgradeRequest, fixture.Version)
	}
}

func TestInstaller_add_and_upgrade_enforce_catalog_state(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)
	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{"toolshed": fixture.Registry},
		ConfigApps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
		Reader:         fixture.Reader,
		ChangeRequests: svc.AppVersionChangeRequests,
		Locks:          svc.AppVersionInstallLocks,
		Rollouts:       svc.AppRollouts,
	}
	input := appregistry.InstallInput{Registry: "toolshed", App: "g-issues", Version: fixture.Version}

	if _, err := installer.Upgrade(ctx, input); !errors.Is(err, appregistry.ErrAppCatalogEmpty) {
		t.Fatalf("upgrade empty catalog error = %v, want %v", err, appregistry.ErrAppCatalogEmpty)
	}
	if _, err := installer.Add(ctx, input); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := installer.Add(ctx, input); !errors.Is(err, appregistry.ErrAppCatalogNotEmpty) {
		t.Fatalf("add non-empty catalog error = %v, want %v", err, appregistry.ErrAppCatalogNotEmpty)
	}
}

func TestInstaller_add_requires_deploy_registry_binding(t *testing.T) {
	t.Parallel()

	installer := &appregistry.Installer{}
	_, err := installer.Add(t.Context(), appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  "1.0.0",
	})
	if !errors.Is(err, appregistry.ErrAppRegistryBinding) {
		t.Fatalf("Add error = %v, want %v", err, appregistry.ErrAppRegistryBinding)
	}
}

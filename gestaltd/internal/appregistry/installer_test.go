package appregistry_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
		Reader:         fixture.Reader,
		ChangeRequests: svc.AppVersionChangeRequests,
		Locks:          svc.AppVersionInstallLocks,
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
		Reader:         fixture.Reader,
		ChangeRequests: svc.AppVersionChangeRequests,
		Locks:          svc.AppVersionInstallLocks,
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

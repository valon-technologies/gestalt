package appregistry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestInstaller_records_install_failed_for_missing_version(t *testing.T) {
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
		Reader:       registrytest.NewReaderForServer(t, registrySrv.URL),
		Catalog:      svc.AppVersionCatalog,
		Locks:        svc.AppVersionInstallLocks,
		ArtifactsDir: t.TempDir(),
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

	records, err := svc.AppVersionCatalog.ListRecordsByApp(ctx, "g-issues")
	if err != nil {
		t.Fatalf("ListRecordsByApp: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v", records)
	}
	if records[0].Type != core.AppVersionCatalogRecordTypeInstallFailed {
		t.Fatalf("record type = %q", records[0].Type)
	}
	if records[0].Version != "missing-version" {
		t.Fatalf("record version = %q", records[0].Version)
	}
}

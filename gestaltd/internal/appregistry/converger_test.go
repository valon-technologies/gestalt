package appregistry_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestConverger_materializes_catalog_known_version_locally(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)
	platform := providerpkg.CurrentPlatformString()
	publicRoot, err := fixture.Registry.PublicURL()
	if err != nil {
		t.Fatalf("PublicURL: %v", err)
	}

	artifactsDir := t.TempDir()
	_, err = svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
		App:       "g-issues",
		Version:   fixture.Version,
		Type:      core.AppVersionCatalogRecordTypeVersionAdded,
		Actor:     "user:alice",
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Metadata: coredata.VersionAddedMetadata(&core.AppInstallation{
			AppName:            "g-issues",
			Version:            fixture.Version,
			Registry:           "toolshed",
			SourceRef:          "abc123def456abc123def456abc123def456abcd",
			ProviderReleaseURL: appregistry.PublicURL(publicRoot, appregistry.AppVersionEntryPath("g-issues", fixture.Version)),
			ArtifactChecksums:  map[string]string{platform: fixture.SHA256},
		}, appregistry.LocalMaterializedPath(artifactsDir, "g-issues", fixture.Version)),
	})
	if err != nil {
		t.Fatalf("AppendRecord: %v", err)
	}

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:       fixture.Reader,
		Catalog:      svc.AppVersionCatalog,
		Locks:        svc.AppVersionInstallLocks,
		ArtifactsDir: artifactsDir,
	}
	converger := appregistry.NewConverger(installer, svc.AppVersionCatalog)
	if converger == nil {
		t.Fatal("expected converger")
	}

	path := appregistry.LocalMaterializedPath(artifactsDir, "g-issues", fixture.Version)
	if appregistry.IsLocallyMaterialized(path) {
		t.Fatal("expected version to be missing before convergence")
	}
	if err := converger.ConvergeOnce(ctx); err != nil {
		t.Fatalf("ConvergeOnce: %v", err)
	}
	if !appregistry.IsLocallyMaterialized(path) {
		t.Fatalf("expected version materialized at %s", path)
	}
}

func TestConverger_skips_already_materialized_version(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:       fixture.Reader,
		Catalog:      svc.AppVersionCatalog,
		Locks:        svc.AppVersionInstallLocks,
		ArtifactsDir: artifactsDir,
	}
	if _, err := installer.Install(ctx, appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
		Actor:    "user:alice",
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	converger := appregistry.NewConverger(installer, svc.AppVersionCatalog)
	if err := converger.ConvergeOnce(ctx); err != nil {
		t.Fatalf("ConvergeOnce: %v", err)
	}
}

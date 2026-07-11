package appregistry_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestInstallerInstallsRegistryAppAndWritesPromotedState(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	services := testutil.NewStubServices(t)
	artifactsDir := t.TempDir()
	promotedAt := time.Date(2026, 7, 10, 15, 30, 0, 0, time.UTC)

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:        fixture.Reader,
		Installations: services.AppInstallations,
		Events:        services.AppInstallationEvents,
		ArtifactsDir:  artifactsDir,
		Now: func() time.Time {
			return promotedAt
		},
	}

	result, err := installer.Install(t.Context(), appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
		Actor:    "user:test",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if result.Installation.RolloutStatus != core.AppInstallationRolloutStatusPromoted {
		t.Fatalf("rollout_status = %q, want promoted", result.Installation.RolloutStatus)
	}
	if result.Installation.ResolvedVersion != fixture.Version {
		t.Fatalf("resolved_version = %q", result.Installation.ResolvedVersion)
	}
	if result.Installation.ActiveSince == nil {
		t.Fatal("active_since is nil")
	}
	if !result.Installation.ActiveSince.Equal(promotedAt) {
		t.Fatalf("active_since = %v, want %v", result.Installation.ActiveSince, promotedAt)
	}
	if result.MaterializedPath == "" {
		t.Fatal("materialized path is empty")
	}
	if _, err := os.Stat(filepath.Join(result.MaterializedPath, "manifest.yaml")); err != nil {
		t.Fatalf("stat materialized manifest: %v", err)
	}

	events, err := services.AppInstallationEvents.ListEventsByApp(t.Context(), "g-issues")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].Type != core.AppInstallationEventTypeInstallRequested {
		t.Fatalf("events[0].type = %q", events[0].Type)
	}
	if events[1].Type != core.AppInstallationEventTypePromoted {
		t.Fatalf("events[1].type = %q", events[1].Type)
	}
}

func TestInstallerFailedUpgradePreservesPromotedMetadata(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	badVersion := "0.0.0-snapshot.bad"
	services := testutil.NewStubServices(t)
	artifactsDir := t.TempDir()
	goldVersion := "0.0.0-snapshot.gold"
	sourceRef := "abc123def456abc123def456abc123def456abcd"
	activeSince := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	platform := providerpkg.CurrentPlatformString()

	if _, err := services.AppInstallations.PutInstallation(t.Context(), &core.AppInstallation{
		AppName:            "g-issues",
		VersionConstraint:  goldVersion,
		ResolvedVersion:    goldVersion,
		SourceRef:          sourceRef,
		Registry:           "toolshed",
		ProviderReleaseURL: "https://example.com/gold.json",
		ArtifactChecksums:  map[string]string{platform: "gold-checksum"},
		RolloutStatus:      core.AppInstallationRolloutStatusPromoted,
		ActiveSince:        &activeSince,
	}); err != nil {
		t.Fatalf("PutInstallation: %v", err)
	}

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:        fixture.NewDigestMismatchReader(t, badVersion),
		Installations: services.AppInstallations,
		Events:        services.AppInstallationEvents,
		ArtifactsDir:  artifactsDir,
	}

	_, err := installer.Install(t.Context(), appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  badVersion,
	})
	if err == nil {
		t.Fatal("expected install failure")
	}

	stored, err := services.AppInstallations.GetInstallation(t.Context(), "g-issues")
	if err != nil {
		t.Fatalf("GetInstallation: %v", err)
	}
	if stored.RolloutStatus != core.AppInstallationRolloutStatusFailed {
		t.Fatalf("rollout_status = %q, want failed", stored.RolloutStatus)
	}
	if stored.ResolvedVersion != goldVersion {
		t.Fatalf("resolved_version = %q, want gold version preserved", stored.ResolvedVersion)
	}
	if stored.SourceRef != sourceRef {
		t.Fatalf("source_ref = %q, want gold metadata preserved", stored.SourceRef)
	}
	if stored.ActiveSince == nil || !stored.ActiveSince.Equal(activeSince) {
		t.Fatalf("active_since = %v, want gold metadata preserved", stored.ActiveSince)
	}
}

func TestInstallerRetryAfterFailedUpgradePreservesPreviousVersion(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	services := testutil.NewStubServices(t)
	artifactsDir := t.TempDir()
	goldVersion := "0.0.0-snapshot.gold"
	sourceRef := "abc123def456abc123def456abc123def456abcd"
	activeSince := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	if _, err := services.AppInstallations.PutInstallation(t.Context(), &core.AppInstallation{
		AppName:           "g-issues",
		VersionConstraint: goldVersion,
		ResolvedVersion:   goldVersion,
		SourceRef:         sourceRef,
		Registry:          "toolshed",
		RolloutStatus:     core.AppInstallationRolloutStatusFailed,
		ActiveSince:       &activeSince,
	}); err != nil {
		t.Fatalf("PutInstallation: %v", err)
	}

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:        fixture.Reader,
		Installations: services.AppInstallations,
		Events:        services.AppInstallationEvents,
		ArtifactsDir:  artifactsDir,
	}

	result, err := installer.Install(t.Context(), appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if result.Installation.PreviousResolvedVersion != goldVersion {
		t.Fatalf("previous_resolved_version = %q, want %q", result.Installation.PreviousResolvedVersion, goldVersion)
	}

	events, err := services.AppInstallationEvents.ListEventsByApp(t.Context(), "g-issues")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if len(events) == 0 || events[0].FromVersion != goldVersion {
		t.Fatalf("install_requested from_version = %q, want %q", events[0].FromVersion, goldVersion)
	}
}

func TestInstallerSetsPreviousResolvedVersionOnUpgrade(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	services := testutil.NewStubServices(t)
	artifactsDir := t.TempDir()
	goldVersion := "0.0.0-snapshot.gold"
	activeSince := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	installedAt := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)

	if _, err := services.AppInstallations.PutInstallation(t.Context(), &core.AppInstallation{
		AppName:           "g-issues",
		VersionConstraint: goldVersion,
		ResolvedVersion:   goldVersion,
		Registry:          "toolshed",
		RolloutStatus:     core.AppInstallationRolloutStatusPromoted,
		ActiveSince:       &activeSince,
		InstalledAt:       installedAt,
	}); err != nil {
		t.Fatalf("PutInstallation: %v", err)
	}

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:        fixture.Reader,
		Installations: services.AppInstallations,
		Events:        services.AppInstallationEvents,
		ArtifactsDir:  artifactsDir,
	}

	result, err := installer.Install(t.Context(), appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if result.Installation.PreviousResolvedVersion != goldVersion {
		t.Fatalf("previous_resolved_version = %q, want %q", result.Installation.PreviousResolvedVersion, goldVersion)
	}
	if !result.Installation.InstalledAt.Equal(installedAt) {
		t.Fatalf("installed_at = %v, want %v", result.Installation.InstalledAt, installedAt)
	}
}

func TestInstallerRetryFromPendingPreservesPreviousVersion(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	services := testutil.NewStubServices(t)
	artifactsDir := t.TempDir()
	goldVersion := "0.0.0-snapshot.gold"
	inFlightVersion := "0.0.0-snapshot.inflight"
	activeSince := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	sourceRef := "abc123def456abc123def456abc123def456abcd"
	platform := providerpkg.CurrentPlatformString()

	if _, err := services.AppInstallations.PutInstallation(t.Context(), &core.AppInstallation{
		AppName:                 "g-issues",
		VersionConstraint:       inFlightVersion,
		ResolvedVersion:         inFlightVersion,
		PreviousResolvedVersion: goldVersion,
		SourceRef:               sourceRef,
		Registry:                "toolshed",
		ProviderReleaseURL:      "https://example.com/gold.json",
		ArtifactChecksums:       map[string]string{platform: "gold-checksum"},
		RolloutStatus:           core.AppInstallationRolloutStatusPending,
		ActiveSince:             &activeSince,
	}); err != nil {
		t.Fatalf("PutInstallation: %v", err)
	}

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:        fixture.Reader,
		Installations: services.AppInstallations,
		Events:        services.AppInstallationEvents,
		ArtifactsDir:  artifactsDir,
	}

	result, err := installer.Install(t.Context(), appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if result.Installation.PreviousResolvedVersion != goldVersion {
		t.Fatalf("previous_resolved_version = %q, want %q", result.Installation.PreviousResolvedVersion, goldVersion)
	}

	events, err := services.AppInstallationEvents.ListEventsByApp(t.Context(), "g-issues")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if len(events) == 0 || events[0].FromVersion != goldVersion {
		t.Fatalf("install_requested from_version = %q, want %q", events[0].FromVersion, goldVersion)
	}
}

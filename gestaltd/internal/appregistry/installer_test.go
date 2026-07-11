package appregistry_test

import (
	"errors"
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

func TestRegistryReader_FetchPublishedVersionNotFound(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	publicURL, err := fixture.Registry.PublicURL()
	if err != nil {
		t.Fatalf("PublicURL: %v", err)
	}
	_, err = fixture.Reader.FetchPublishedVersion(t.Context(), publicURL, "g-issues", "missing-version")
	if !errors.Is(err, appregistry.ErrRegistryDocumentNotFound) {
		t.Fatalf("FetchPublishedVersion error = %v, want ErrRegistryDocumentNotFound", err)
	}
}

func TestInstallerInstallFailureHandling(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	activeSince := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	goldVersion := "0.0.0-snapshot.gold"
	sourceRef := "abc123def456abc123def456abc123def456abcd"
	platform := providerpkg.CurrentPlatformString()
	checksums := map[string]string{platform: fixture.SHA256}

	tests := []struct {
		name string
		seed func(t *testing.T, services *testutil.Services)
		run  func(t *testing.T, installer *appregistry.Installer)
		want func(t *testing.T, stored *core.AppInstallation)
	}{
		{
			name: "fresh_install",
			seed: func(t *testing.T, services *testutil.Services) {},
			run: func(t *testing.T, installer *appregistry.Installer) {
				t.Helper()
				_, err := installer.Install(t.Context(), appregistry.InstallInput{
					Registry: "toolshed",
					App:      "g-issues",
					Version:  fixture.Version,
				})
				if err == nil {
					t.Fatal("expected install failure")
				}
			},
			want: func(t *testing.T, stored *core.AppInstallation) {
				t.Helper()
				if stored.RolloutStatus != core.AppInstallationRolloutStatusFailed {
					t.Fatalf("rollout_status = %q, want failed", stored.RolloutStatus)
				}
			},
		},
		{
			name: "failed_upgrade_preserves_promoted",
			seed: func(t *testing.T, services *testutil.Services) {
				t.Helper()
				if _, err := services.AppInstallations.PutInstallation(t.Context(), &core.AppInstallation{
					AppName:           "g-issues",
					VersionConstraint: goldVersion,
					ResolvedVersion:   goldVersion,
					Registry:          "toolshed",
					RolloutStatus:     core.AppInstallationRolloutStatusPromoted,
					ActiveSince:       &activeSince,
				}); err != nil {
					t.Fatalf("PutInstallation: %v", err)
				}
			},
			run: func(t *testing.T, installer *appregistry.Installer) {
				t.Helper()
				_, err := installer.Install(t.Context(), appregistry.InstallInput{
					Registry: "toolshed",
					App:      "g-issues",
					Version:  fixture.Version,
				})
				if err == nil {
					t.Fatal("expected install failure")
				}
			},
			want: func(t *testing.T, stored *core.AppInstallation) {
				t.Helper()
				if stored.RolloutStatus != core.AppInstallationRolloutStatusFailed {
					t.Fatalf("rollout_status = %q, want failed", stored.RolloutStatus)
				}
				if stored.ResolvedVersion != goldVersion {
					t.Fatalf("resolved_version = %q, want %q", stored.ResolvedVersion, goldVersion)
				}
				if stored.ActiveSince == nil || !stored.ActiveSince.Equal(activeSince) {
					t.Fatalf("active_since = %v, want %v", stored.ActiveSince, activeSince)
				}
			},
		},
		{
			name: "same_version_reinstall_preserves_metadata",
			seed: func(t *testing.T, services *testutil.Services) {
				t.Helper()
				if _, err := services.AppInstallations.PutInstallation(t.Context(), &core.AppInstallation{
					AppName:           "g-issues",
					VersionConstraint: fixture.Version,
					ResolvedVersion:   fixture.Version,
					SourceRef:         sourceRef,
					Registry:          "toolshed",
					ArtifactChecksums: checksums,
					RolloutStatus:     core.AppInstallationRolloutStatusPromoted,
					ActiveSince:       &activeSince,
				}); err != nil {
					t.Fatalf("PutInstallation: %v", err)
				}
			},
			run: func(t *testing.T, installer *appregistry.Installer) {
				t.Helper()
				_, err := installer.Install(t.Context(), appregistry.InstallInput{
					Registry: "toolshed",
					App:      "g-issues",
					Version:  fixture.Version,
				})
				if err == nil {
					t.Fatal("expected install failure")
				}
			},
			want: func(t *testing.T, stored *core.AppInstallation) {
				t.Helper()
				if stored.RolloutStatus != core.AppInstallationRolloutStatusFailed {
					t.Fatalf("rollout_status = %q, want failed", stored.RolloutStatus)
				}
				if stored.ResolvedVersion != fixture.Version {
					t.Fatalf("resolved_version = %q", stored.ResolvedVersion)
				}
				if stored.SourceRef != sourceRef {
					t.Fatalf("source_ref = %q, want preserved", stored.SourceRef)
				}
				if stored.ArtifactChecksums[platform] != checksums[platform] {
					t.Fatalf("artifact_checksums = %#v, want preserved", stored.ArtifactChecksums)
				}
				if stored.ActiveSince == nil || !stored.ActiveSince.Equal(activeSince) {
					t.Fatalf("active_since = %v, want %v", stored.ActiveSince, activeSince)
				}
			},
		},
		{
			name: "second_failed_retry_preserves_gold",
			seed: func(t *testing.T, services *testutil.Services) {
				t.Helper()
				if _, err := services.AppInstallations.PutInstallation(t.Context(), &core.AppInstallation{
					AppName:           "g-issues",
					VersionConstraint: goldVersion,
					ResolvedVersion:   goldVersion,
					Registry:          "toolshed",
					RolloutStatus:     core.AppInstallationRolloutStatusPromoted,
					ActiveSince:       &activeSince,
				}); err != nil {
					t.Fatalf("PutInstallation: %v", err)
				}
			},
			run: func(t *testing.T, installer *appregistry.Installer) {
				t.Helper()
				otherVersion := "0.0.0-snapshot.other"
				for _, version := range []string{fixture.Version, otherVersion} {
					reader := fixture.NewDigestMismatchReader(t, version)
					retryInstaller := &appregistry.Installer{
						Registries: map[string]config.AppRegistryConfig{
							"toolshed": fixture.Registry,
						},
						Reader:        reader,
						Installations: installer.Installations,
						Events:        installer.Events,
						ArtifactsDir:  installer.ArtifactsDir,
					}
					_, err := retryInstaller.Install(t.Context(), appregistry.InstallInput{
						Registry: "toolshed",
						App:      "g-issues",
						Version:  version,
					})
					if err == nil {
						t.Fatalf("expected install failure for version %q", version)
					}
				}
			},
			want: func(t *testing.T, stored *core.AppInstallation) {
				t.Helper()
				if stored.RolloutStatus != core.AppInstallationRolloutStatusFailed {
					t.Fatalf("rollout_status = %q, want failed", stored.RolloutStatus)
				}
				if stored.ResolvedVersion != goldVersion {
					t.Fatalf("resolved_version = %q, want %q", stored.ResolvedVersion, goldVersion)
				}
			},
		},
		{
			name: "pending_retry_preserves_gold",
			seed: func(t *testing.T, services *testutil.Services) {
				t.Helper()
				if _, err := services.AppInstallations.PutInstallation(t.Context(), &core.AppInstallation{
					AppName:                 "g-issues",
					VersionConstraint:       fixture.Version,
					ResolvedVersion:         fixture.Version,
					SourceRef:               sourceRef,
					Registry:                "toolshed",
					ArtifactChecksums:       checksums,
					RolloutStatus:           core.AppInstallationRolloutStatusPending,
					PreviousResolvedVersion: goldVersion,
					ActiveSince:             &activeSince,
				}); err != nil {
					t.Fatalf("PutInstallation: %v", err)
				}
			},
			run: func(t *testing.T, installer *appregistry.Installer) {
				t.Helper()
				_, err := installer.Install(t.Context(), appregistry.InstallInput{
					Registry: "toolshed",
					App:      "g-issues",
					Version:  fixture.Version,
				})
				if err == nil {
					t.Fatal("expected install failure")
				}
			},
			want: func(t *testing.T, stored *core.AppInstallation) {
				t.Helper()
				if stored.RolloutStatus != core.AppInstallationRolloutStatusFailed {
					t.Fatalf("rollout_status = %q, want failed", stored.RolloutStatus)
				}
				if stored.ResolvedVersion != goldVersion {
					t.Fatalf("resolved_version = %q, want %q", stored.ResolvedVersion, goldVersion)
				}
				if stored.PreviousResolvedVersion != goldVersion {
					t.Fatalf("previous_resolved_version = %q, want %q", stored.PreviousResolvedVersion, goldVersion)
				}
				if stored.SourceRef != sourceRef {
					t.Fatalf("source_ref = %q, want preserved gold metadata", stored.SourceRef)
				}
				if stored.ActiveSince == nil || !stored.ActiveSince.Equal(activeSince) {
					t.Fatalf("active_since = %v, want preserved gold metadata", stored.ActiveSince)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			services := testutil.NewStubServices(t)
			tc.seed(t, services)

			reader := fixture.NewDigestMismatchReader(t, fixture.Version)
			installer := &appregistry.Installer{
				Registries: map[string]config.AppRegistryConfig{
					"toolshed": fixture.Registry,
				},
				Reader:        reader,
				Installations: services.AppInstallations,
				Events:        services.AppInstallationEvents,
				ArtifactsDir:  t.TempDir(),
			}

			tc.run(t, installer)

			stored, err := services.AppInstallations.GetInstallation(t.Context(), "g-issues")
			if err != nil {
				t.Fatalf("GetInstallation: %v", err)
			}
			tc.want(t, stored)
		})
	}
}

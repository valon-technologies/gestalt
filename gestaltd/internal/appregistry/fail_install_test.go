package appregistry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestRestoreMaterializationBackup(t *testing.T) {
	t.Parallel()

	t.Run("restores_when_backup_exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		materialized := filepath.Join(dir, "g-issues", "0.0.0-snapshot.v1")
		backup := materialized + ".backup"
		if err := os.MkdirAll(materialized, 0o755); err != nil {
			t.Fatalf("MkdirAll materialized: %v", err)
		}
		if err := os.WriteFile(filepath.Join(materialized, "content.txt"), []byte("old"), 0o644); err != nil {
			t.Fatalf("WriteFile old: %v", err)
		}
		if err := os.Rename(materialized, backup); err != nil {
			t.Fatalf("Rename to backup: %v", err)
		}
		if err := os.MkdirAll(materialized, 0o755); err != nil {
			t.Fatalf("MkdirAll staged materialized: %v", err)
		}
		if err := os.WriteFile(filepath.Join(materialized, "content.txt"), []byte("new"), 0o644); err != nil {
			t.Fatalf("WriteFile staged: %v", err)
		}

		restoreMaterializationBackup(materialized, backup)

		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Fatalf("backup should be removed, stat err = %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(materialized, "content.txt")); err != nil {
			t.Fatalf("ReadFile restored materialized: %v", err)
		} else if string(data) != "old" {
			t.Fatalf("restored content = %q, want old", data)
		}
	})

	t.Run("removes_materialized_when_no_backup", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		materialized := filepath.Join(dir, "g-issues", "0.0.0-snapshot.v1")
		backup := materialized + ".backup"
		if err := os.MkdirAll(materialized, 0o755); err != nil {
			t.Fatalf("MkdirAll materialized: %v", err)
		}
		if err := os.WriteFile(filepath.Join(materialized, "orphan.txt"), []byte("orphan"), 0o644); err != nil {
			t.Fatalf("WriteFile orphan: %v", err)
		}

		restoreMaterializationBackup(materialized, backup)

		if _, err := os.Stat(materialized); !os.IsNotExist(err) {
			t.Fatalf("materialized should be removed on first-install rollback, stat err = %v", err)
		}
	})
}

func TestInstallerFleetRaceGuards(t *testing.T) {
	t.Parallel()

	t.Run("fail_install_skips_restore_when_fleet_advanced", func(t *testing.T) {
		t.Parallel()

		services := testutil.NewStubServices(t)
		activeSince := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
		if _, err := services.AppInstallations.PutInstallation(context.Background(), &core.AppInstallation{
			AppName:           "g-issues",
			VersionConstraint: "0.0.0-snapshot.v3",
			ResolvedVersion:   "0.0.0-snapshot.v3",
			Registry:          "toolshed",
			SourceRef:         "newer-source-ref",
			RolloutStatus:     core.AppInstallationRolloutStatusPromoted,
			ActiveSince:       &activeSince,
		}); err != nil {
			t.Fatalf("PutInstallation: %v", err)
		}

		priorV1 := &core.AppInstallation{
			AppName:           "g-issues",
			VersionConstraint: "0.0.0-snapshot.v1",
			ResolvedVersion:   "0.0.0-snapshot.v1",
			Registry:          "toolshed",
			SourceRef:         "stale-source-ref",
			RolloutStatus:     core.AppInstallationRolloutStatusPromoted,
		}
		installer := &Installer{
			Installations: services.AppInstallations,
			Events:        services.AppInstallationEvents,
		}

		_, err := installer.failInstall(context.Background(), "g-issues", priorV1, &core.AppInstallation{
			AppName:           "g-issues",
			VersionConstraint: "0.0.0-snapshot.v2",
			ResolvedVersion:   "0.0.0-snapshot.v2",
			RolloutStatus:     core.AppInstallationRolloutStatusPending,
		}, "0.0.0-snapshot.v1", "0.0.0-snapshot.v2", "user:test", "toolshed", fmt.Errorf("download failed"))
		if err == nil {
			t.Fatal("expected failure")
		}

		stored, err := services.AppInstallations.GetInstallation(context.Background(), "g-issues")
		if err != nil {
			t.Fatalf("GetInstallation: %v", err)
		}
		if stored.RolloutStatus != core.AppInstallationRolloutStatusPromoted {
			t.Fatalf("rollout_status = %q, want promoted", stored.RolloutStatus)
		}
		if stored.ResolvedVersion != "0.0.0-snapshot.v3" {
			t.Fatalf("resolved_version = %q, want newer promoted version preserved", stored.ResolvedVersion)
		}
		if stored.SourceRef != "newer-source-ref" {
			t.Fatalf("source_ref = %q, want newer promoted metadata preserved", stored.SourceRef)
		}
	})

	t.Run("write_pending_preserves_promoted_metadata", func(t *testing.T) {
		t.Parallel()

		services := testutil.NewStubServices(t)
		activeSince := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
		if _, err := services.AppInstallations.PutInstallation(context.Background(), &core.AppInstallation{
			AppName:           "g-issues",
			VersionConstraint: "0.0.0-snapshot.v1",
			ResolvedVersion:   "0.0.0-snapshot.v1",
			Registry:          "toolshed",
			SourceRef:         "gold-source-ref",
			RolloutStatus:     core.AppInstallationRolloutStatusPromoted,
			ActiveSince:       &activeSince,
		}); err != nil {
			t.Fatalf("PutInstallation: %v", err)
		}

		baseline, err := services.AppInstallations.GetInstallation(context.Background(), "g-issues")
		if err != nil {
			t.Fatalf("GetInstallation: %v", err)
		}

		installer := &Installer{
			Installations: services.AppInstallations,
			Events:        services.AppInstallationEvents,
		}
		if _, err := installer.writePendingInstall(context.Background(), "g-issues", baseline, &core.AppInstallation{
			AppName:           "g-issues",
			VersionConstraint: "0.0.0-snapshot.v2",
			ResolvedVersion:   "0.0.0-snapshot.v2",
			Registry:          "toolshed",
			SourceRef:         "pending-source-ref",
			RolloutStatus:     core.AppInstallationRolloutStatusPending,
		}); err != nil {
			t.Fatalf("writePendingInstall: %v", err)
		}

		stored, err := services.AppInstallations.GetInstallation(context.Background(), "g-issues")
		if err != nil {
			t.Fatalf("GetInstallation: %v", err)
		}
		if stored.RolloutStatus != core.AppInstallationRolloutStatusPending {
			t.Fatalf("rollout_status = %q, want pending", stored.RolloutStatus)
		}
		if stored.ResolvedVersion != "0.0.0-snapshot.v2" {
			t.Fatalf("resolved_version = %q, want pending target", stored.ResolvedVersion)
		}
		if stored.SourceRef != "gold-source-ref" {
			t.Fatalf("source_ref = %q, want promoted metadata preserved", stored.SourceRef)
		}
		if stored.ActiveSince == nil || !stored.ActiveSince.Equal(activeSince) {
			t.Fatalf("active_since = %v, want promoted metadata preserved", stored.ActiveSince)
		}
	})

	t.Run("write_pending_rejects_advanced_fleet_state", func(t *testing.T) {
		t.Parallel()

		services := testutil.NewStubServices(t)
		activeSince := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
		updatedAt := time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC)
		if _, err := services.AppInstallations.PutInstallation(context.Background(), &core.AppInstallation{
			AppName:           "g-issues",
			VersionConstraint: "0.0.0-snapshot.v3",
			ResolvedVersion:   "0.0.0-snapshot.v3",
			Registry:          "toolshed",
			SourceRef:         "newer-source-ref",
			RolloutStatus:     core.AppInstallationRolloutStatusPromoted,
			ActiveSince:       &activeSince,
			UpdatedAt:         updatedAt,
		}); err != nil {
			t.Fatalf("PutInstallation: %v", err)
		}

		staleBaseline := &core.AppInstallation{
			AppName:           "g-issues",
			VersionConstraint: "0.0.0-snapshot.v1",
			ResolvedVersion:   "0.0.0-snapshot.v1",
			Registry:          "toolshed",
			SourceRef:         "stale-source-ref",
			RolloutStatus:     core.AppInstallationRolloutStatusPromoted,
			UpdatedAt:         time.Date(2026, 7, 10, 11, 0, 0, 0, time.UTC),
		}
		installer := &Installer{
			Installations: services.AppInstallations,
			Events:        services.AppInstallationEvents,
		}

		_, err := installer.writePendingInstall(context.Background(), "g-issues", staleBaseline, &core.AppInstallation{
			AppName:           "g-issues",
			VersionConstraint: "0.0.0-snapshot.v2",
			ResolvedVersion:   "0.0.0-snapshot.v2",
			Registry:          "toolshed",
			RolloutStatus:     core.AppInstallationRolloutStatusPending,
		})
		if !errors.Is(err, ErrInstallFleetStateAdvanced) {
			t.Fatalf("writePendingInstall error = %v, want ErrInstallFleetStateAdvanced", err)
		}

		stored, err := services.AppInstallations.GetInstallation(context.Background(), "g-issues")
		if err != nil {
			t.Fatalf("GetInstallation: %v", err)
		}
		if stored.RolloutStatus != core.AppInstallationRolloutStatusPromoted {
			t.Fatalf("rollout_status = %q, want promoted preserved", stored.RolloutStatus)
		}
		if stored.ResolvedVersion != "0.0.0-snapshot.v3" {
			t.Fatalf("resolved_version = %q, want newer promoted version preserved", stored.ResolvedVersion)
		}
	})
}

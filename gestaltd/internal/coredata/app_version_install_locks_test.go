package coredata_test

import (
	"context"
	"testing"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppVersionInstallLockService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("acquire_and_release", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		if err := svc.AppVersionInstallLocks.Acquire(ctx, "g-issues", "0.0.1", "holder-a", time.Minute); err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		if err := svc.AppVersionInstallLocks.Release(ctx, "g-issues", "0.0.1", "holder-a"); err != nil {
			t.Fatalf("Release: %v", err)
		}
		if err := svc.AppVersionInstallLocks.Acquire(ctx, "g-issues", "0.0.1", "holder-b", time.Minute); err != nil {
			t.Fatalf("Acquire after release: %v", err)
		}
	})

	t.Run("second_holder_blocked_while_lock_active", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		if err := svc.AppVersionInstallLocks.Acquire(ctx, "g-issues", "0.0.1", "holder-a", time.Minute); err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		err := svc.AppVersionInstallLocks.Acquire(ctx, "g-issues", "0.0.1", "holder-b", time.Minute)
		if err == nil {
			t.Fatal("expected lock held error")
		}
		if err != coredata.ErrAppVersionInstallLockHeld {
			t.Fatalf("Acquire error = %v, want %v", err, coredata.ErrAppVersionInstallLockHeld)
		}
	})

	t.Run("stale_lock_can_be_taken_over", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		if err := svc.AppVersionInstallLocks.Acquire(ctx, "g-issues", "0.0.1", "holder-a", time.Millisecond); err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
		if err := svc.AppVersionInstallLocks.Acquire(ctx, "g-issues", "0.0.1", "holder-b", time.Minute); err != nil {
			t.Fatalf("Acquire stale takeover: %v", err)
		}
	})

	t.Run("different_versions_of_one_app_are_serialized", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		if err := svc.AppVersionInstallLocks.Acquire(ctx, "g-issues", "0.0.1", "holder-a", time.Minute); err != nil {
			t.Fatalf("Acquire v1: %v", err)
		}
		if err := svc.AppVersionInstallLocks.Acquire(ctx, "g-issues", "0.0.2", "holder-b", time.Minute); err != coredata.ErrAppVersionInstallLockHeld {
			t.Fatalf("Acquire v2 error = %v, want %v", err, coredata.ErrAppVersionInstallLockHeld)
		}
	})

	t.Run("different_apps_install_in_parallel", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		if err := svc.AppVersionInstallLocks.Acquire(ctx, "g-issues", "0.0.1", "holder-a", time.Minute); err != nil {
			t.Fatalf("Acquire g-issues: %v", err)
		}
		if err := svc.AppVersionInstallLocks.Acquire(ctx, "g-slack", "0.0.1", "holder-b", time.Minute); err != nil {
			t.Fatalf("Acquire g-slack: %v", err)
		}
	})

	t.Run("active_legacy_version_scoped_lock_blocks_app", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		now := time.Now().UTC()
		if err := svc.DB.ObjectStore(coredata.StoreAppVersionInstallLocks).Add(ctx, idb.Record{
			"id":          "g-issues\x000.0.1",
			"app":         "g-issues",
			"version":     "0.0.1",
			"holder":      "legacy-holder",
			"acquired_at": now,
			"expires_at":  now.Add(time.Minute),
		}); err != nil {
			t.Fatalf("Add legacy lock: %v", err)
		}
		if err := svc.AppVersionInstallLocks.Acquire(ctx, "g-issues", "0.0.2", "holder-b", time.Minute); err != coredata.ErrAppVersionInstallLockHeld {
			t.Fatalf("Acquire error = %v, want %v", err, coredata.ErrAppVersionInstallLockHeld)
		}
	})
}

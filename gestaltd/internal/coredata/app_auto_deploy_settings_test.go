package coredata_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAutoDeploySettingsService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("missing settings", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t).AutoDeploySettings
		if _, err := svc.Get(ctx, "g-issues"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("Get missing error = %v, want %v", err, core.ErrNotFound)
		}
	})

	t.Run("ensure store", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t).AutoDeploySettings
		if err := svc.EnsureStore(ctx); err != nil {
			t.Fatalf("EnsureStore: %v", err)
		}
	})

	t.Run("update initializes and round trips", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t).AutoDeploySettings
		failedAt := time.Date(2026, 7, 28, 12, 0, 0, 123456789, time.UTC)
		got, err := svc.Update(ctx, " g-issues ", func(settings *core.AppAutoDeploySettings) error {
			settings.Enabled = true
			settings.Paused = true
			settings.PauseReason = "rollout_failed"
			settings.PendingVersion = " v2 "
			settings.LastSeenVersion = " v2 "
			settings.LastError = " validation failed "
			settings.LastFailedRolloutAt = failedAt
			return nil
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.App != "g-issues" || !got.Enabled || !got.Paused || got.PauseReason != "rollout_failed" || got.PendingVersion != "v2" ||
			got.LastSeenVersion != "v2" || got.LastError != "validation failed" ||
			!got.LastFailedRolloutAt.Equal(failedAt.Truncate(time.Millisecond)) {
			t.Fatalf("updated settings = %#v", got)
		}
		stored, err := svc.Get(ctx, "g-issues")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if *stored != *got {
			t.Fatalf("stored settings = %#v, want %#v", stored, got)
		}
	})

	t.Run("update clears fields and preserves app", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t).AutoDeploySettings
		if _, err := svc.Update(ctx, "g-issues", func(settings *core.AppAutoDeploySettings) error {
			settings.Enabled = true
			settings.PendingVersion = "v2"
			settings.LastError = "failed"
			return nil
		}); err != nil {
			t.Fatalf("seed settings: %v", err)
		}
		got, err := svc.Update(ctx, "g-issues", func(settings *core.AppAutoDeploySettings) error {
			settings.App = "different-app"
			settings.Enabled = false
			settings.PendingVersion = ""
			settings.LastError = ""
			return nil
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.App != "g-issues" || got.Enabled || got.PendingVersion != "" || got.LastError != "" {
			t.Fatalf("updated settings = %#v", got)
		}
	})

	t.Run("validation and failed update", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t).AutoDeploySettings
		if _, err := svc.Get(ctx, " "); err == nil {
			t.Fatal("Get with empty app succeeded")
		}
		if _, err := svc.Update(ctx, "", func(*core.AppAutoDeploySettings) error { return nil }); err == nil {
			t.Fatal("Update with empty app succeeded")
		}
		if _, err := svc.Update(ctx, "g-issues", nil); err == nil {
			t.Fatal("Update with nil function succeeded")
		}
		wantErr := errors.New("stop")
		if _, err := svc.Update(ctx, "g-issues", func(settings *core.AppAutoDeploySettings) error {
			settings.Enabled = true
			return fmt.Errorf("wrapped: %w", wantErr)
		}); !errors.Is(err, wantErr) {
			t.Fatalf("failed update error = %v, want %v", err, wantErr)
		}
		if _, err := svc.Get(ctx, "g-issues"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("Get after failed update error = %v, want %v", err, core.ErrNotFound)
		}
	})

	t.Run("update for rollout ignores stale rollout", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t)
		createdAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
		failed, err := svc.AppRollouts.Create(ctx, &core.AppRollout{
			App: "g-issues", Version: "v1", State: core.AppRolloutStateEnrolling,
			CreatedAt: createdAt, EnrollmentEndsAt: createdAt.Add(time.Minute), Deadline: createdAt.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("create failed rollout: %v", err)
		}
		if _, err := svc.AppRollouts.MarkFailedForRollout(ctx, failed, createdAt.Add(time.Minute)); err != nil {
			t.Fatalf("mark failed rollout: %v", err)
		}
		failed, err = svc.AppRollouts.Get(ctx, "g-issues")
		if err != nil {
			t.Fatalf("get failed rollout: %v", err)
		}
		if _, applied, err := svc.AutoDeploySettings.UpdateForRollout(ctx, failed, func(settings *core.AppAutoDeploySettings) error {
			settings.Enabled = true
			settings.Paused = true
			return nil
		}); err != nil || !applied {
			t.Fatalf("initial update: applied=%v err=%v", applied, err)
		}
		replacement, err := svc.AppRollouts.Create(ctx, &core.AppRollout{
			App: "g-issues", Version: "v1", State: core.AppRolloutStateEnrolling,
			CreatedAt: createdAt.Add(2 * time.Minute), EnrollmentEndsAt: createdAt.Add(3 * time.Minute), Deadline: createdAt.Add(4 * time.Minute),
		})
		if err != nil {
			t.Fatalf("create replacement rollout: %v", err)
		}
		if _, err := svc.AppRollouts.MarkFailedForRollout(ctx, replacement, createdAt.Add(3*time.Minute)); err != nil {
			t.Fatalf("mark replacement failed rollout: %v", err)
		}
		if _, applied, err := svc.AutoDeploySettings.UpdateForRollout(ctx, failed, func(settings *core.AppAutoDeploySettings) error {
			settings.Paused = false
			return nil
		}); err != nil || applied {
			t.Fatalf("stale update: applied=%v err=%v", applied, err)
		}
		if replacement.CreatedAt.Equal(failed.CreatedAt) {
			t.Fatal("replacement rollout did not get a new identity")
		}
		settings, err := svc.AutoDeploySettings.Get(ctx, "g-issues")
		if err != nil {
			t.Fatalf("get settings: %v", err)
		}
		if !settings.Paused {
			t.Fatalf("stale update changed settings: %#v", settings)
		}
	})
}

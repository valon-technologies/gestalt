package coredata_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestGestaltdSourceVersionActivationRetargetsActiveRollouts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC)

	if _, err := services.GestaltdSourceVersionState.CurrentForAdmission(ctx); !errors.Is(err, coredata.ErrGestaltdSourceVersionUnavailable) {
		t.Fatalf("CurrentForAdmission before activation error = %v, want unavailable", err)
	}
	if _, err := services.GestaltdSourceVersionState.Activate(ctx, "source-old", start, false, 2*time.Minute, 15*time.Minute); err != nil {
		t.Fatalf("Activate old: %v", err)
	}
	original, err := services.GestaltdSourceVersionState.CreateAppRollout(ctx, &core.AppRollout{
		App:              "g-issues",
		Version:          "v2",
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        start.Add(time.Minute),
		EnrollmentEndsAt: start.Add(3 * time.Minute),
		Deadline:         start.Add(16 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	if original.TargetSourceVersion != "source-old" {
		t.Fatalf("original target = %q, want source-old", original.TargetSourceVersion)
	}

	activatedAt := start.Add(5 * time.Minute)
	if _, err := services.GestaltdSourceVersionState.Activate(ctx, "source-new", activatedAt, false, 2*time.Minute, 15*time.Minute); err != nil {
		t.Fatalf("Activate new: %v", err)
	}
	if _, err := services.AppRollouts.MarkRestartingForRollout(ctx, original); !errors.Is(err, coredata.ErrAppRolloutEpochMismatch) {
		t.Fatalf("stale MarkRestartingForRollout error = %v, want epoch mismatch", err)
	}
	if _, err := services.AppRollouts.MarkCompleteForRollout(ctx, original, activatedAt); !errors.Is(err, coredata.ErrAppRolloutEpochMismatch) {
		t.Fatalf("stale MarkCompleteForRollout error = %v, want epoch mismatch", err)
	}
	if _, err := services.AppRollouts.MarkFailedForRollout(ctx, original, activatedAt); !errors.Is(err, coredata.ErrAppRolloutEpochMismatch) {
		t.Fatalf("stale MarkFailedForRollout error = %v, want epoch mismatch", err)
	}

	current, err := services.GestaltdSourceVersionState.CurrentForAdmission(ctx)
	if err != nil {
		t.Fatalf("CurrentForAdmission after activation: %v", err)
	}
	if current != "source-new" {
		t.Fatalf("current source version = %q, want source-new", current)
	}
	rollout, err := services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if rollout.TargetSourceVersion != "source-new" || rollout.State != core.AppRolloutStateEnrolling {
		t.Fatalf("retargeted rollout = %#v", rollout)
	}
	if !rollout.CreatedAt.Equal(activatedAt) ||
		!rollout.EnrollmentEndsAt.Equal(activatedAt.Add(2*time.Minute)) ||
		!rollout.Deadline.Equal(activatedAt.Add(15*time.Minute)) {
		t.Fatalf("retargeted rollout timestamps = %#v", rollout)
	}
}

func TestGestaltdSourceVersionActivationIsIdempotentWithoutRetry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC)
	if _, err := services.GestaltdSourceVersionState.Activate(ctx, "source-a", start, false, 2*time.Minute, 15*time.Minute); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	rollout, err := services.GestaltdSourceVersionState.CreateAppRollout(ctx, &core.AppRollout{
		App:              "g-issues",
		Version:          "v2",
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        start.Add(time.Minute),
		EnrollmentEndsAt: start.Add(3 * time.Minute),
		Deadline:         start.Add(16 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create rollout: %v", err)
	}

	state, err := services.GestaltdSourceVersionState.Activate(ctx, "source-a", start.Add(5*time.Minute), false, 2*time.Minute, 15*time.Minute)
	if err != nil {
		t.Fatalf("repeat Activate: %v", err)
	}
	if !state.UpdatedAt.Equal(start) {
		t.Fatalf("activation timestamp = %v, want %v", state.UpdatedAt, start)
	}
	current, err := services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if !current.CreatedAt.Equal(rollout.CreatedAt) {
		t.Fatalf("rollout epoch changed from %v to %v", rollout.CreatedAt, current.CreatedAt)
	}
}

func TestGestaltdSourceVersionRetryReopensFailuresSinceActivation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 27, 18, 0, 0, 0, time.UTC)
	if _, err := services.GestaltdSourceVersionState.Activate(ctx, "source-a", start, false, 2*time.Minute, 15*time.Minute); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	for _, app := range []string{"recent-failure", "older-failure"} {
		if _, err := services.GestaltdSourceVersionState.CreateAppRollout(ctx, &core.AppRollout{
			App:              app,
			Version:          "v2",
			State:            core.AppRolloutStateEnrolling,
			CreatedAt:        start.Add(time.Minute),
			EnrollmentEndsAt: start.Add(3 * time.Minute),
			Deadline:         start.Add(16 * time.Minute),
		}); err != nil {
			t.Fatalf("Create rollout %s: %v", app, err)
		}
	}
	if _, err := services.AppRollouts.MarkFailed(ctx, "recent-failure", "v2", start.Add(time.Minute)); err != nil {
		t.Fatalf("MarkFailed recent: %v", err)
	}
	if _, err := services.AppRollouts.MarkFailed(ctx, "older-failure", "v2", start.Add(-time.Minute)); err != nil {
		t.Fatalf("MarkFailed older: %v", err)
	}

	retryAt := start.Add(5 * time.Minute)
	if _, err := services.GestaltdSourceVersionState.Activate(ctx, "source-a", retryAt, true, 2*time.Minute, 15*time.Minute); err != nil {
		t.Fatalf("retry Activate: %v", err)
	}
	recent, err := services.AppRollouts.Get(ctx, "recent-failure")
	if err != nil {
		t.Fatalf("Get recent failure: %v", err)
	}
	if recent.State != core.AppRolloutStateEnrolling || !recent.FailedAt.IsZero() || !recent.CreatedAt.Equal(retryAt) {
		t.Fatalf("reopened recent rollout = %#v", recent)
	}
	older, err := services.AppRollouts.Get(ctx, "older-failure")
	if err != nil {
		t.Fatalf("Get older failure: %v", err)
	}
	if older.State != core.AppRolloutStateFailed {
		t.Fatalf("older rollout state = %q, want failed", older.State)
	}
}

package coredata_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppRolloutService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	start := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	newRollout := func(app, version string) *core.AppRollout {
		return &core.AppRollout{
			App:              app,
			Version:          version,
			State:            core.AppRolloutStateEnrolling,
			CreatedAt:        start,
			EnrollmentEndsAt: start.Add(2 * time.Minute),
			Deadline:         start.Add(15 * time.Minute),
		}
	}

	t.Run("one_active_rollout_per_app", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t)
		if _, err := svc.AppRollouts.Create(ctx, newRollout("g-issues", "v1")); err != nil {
			t.Fatalf("Create v1: %v", err)
		}
		if _, err := svc.AppRollouts.Create(ctx, newRollout("g-issues", "v2")); !errors.Is(err, coredata.ErrAppRolloutActive) {
			t.Fatalf("Create v2 error = %v, want %v", err, coredata.ErrAppRolloutActive)
		}
		if _, err := svc.AppRollouts.Create(ctx, newRollout("g-slack", "v1")); err != nil {
			t.Fatalf("Create different app: %v", err)
		}
	})

	t.Run("concurrent_creates_admit_one_version", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t)
		startCreate := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, version := range []string{"v1", "v2"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-startCreate
				_, err := svc.AppRollouts.Create(ctx, newRollout("g-issues", version))
				errs <- err
			}()
		}
		close(startCreate)
		wg.Wait()
		close(errs)
		succeeded := 0
		blocked := 0
		for err := range errs {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, coredata.ErrAppRolloutActive):
				blocked++
			default:
				t.Fatalf("Create error = %v", err)
			}
		}
		if succeeded != 1 || blocked != 1 {
			t.Fatalf("succeeded = %d, blocked = %d; want 1 each", succeeded, blocked)
		}
	})

	t.Run("terminal_rollout_can_be_replaced", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t)
		if _, err := svc.AppRollouts.Create(ctx, newRollout("g-issues", "v1")); err != nil {
			t.Fatalf("Create v1: %v", err)
		}
		if _, err := svc.AppRollouts.MarkRestarting(ctx, "g-issues", "v1"); err != nil {
			t.Fatalf("MarkRestarting: %v", err)
		}
		if _, err := svc.AppRollouts.MarkComplete(ctx, "g-issues", "v1", start.Add(time.Minute)); err != nil {
			t.Fatalf("MarkComplete: %v", err)
		}
		if _, err := svc.AppRollouts.Create(ctx, newRollout("g-issues", "v2")); err != nil {
			t.Fatalf("Create v2: %v", err)
		}
		got, err := svc.AppRollouts.Get(ctx, "g-issues")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Version != "v2" || got.State != core.AppRolloutStateEnrolling {
			t.Fatalf("rollout = %#v, want v2 enrolling", got)
		}
	})

	t.Run("list_active_excludes_terminal", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t)
		if _, err := svc.AppRollouts.Create(ctx, newRollout("g-issues", "v1")); err != nil {
			t.Fatalf("Create g-issues: %v", err)
		}
		if _, err := svc.AppRollouts.Create(ctx, newRollout("g-slack", "v1")); err != nil {
			t.Fatalf("Create g-slack: %v", err)
		}
		if _, err := svc.AppRollouts.MarkFailed(ctx, "g-slack", "v1", start.Add(15*time.Minute)); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
		active, err := svc.AppRollouts.ListActive(ctx)
		if err != nil {
			t.Fatalf("ListActive: %v", err)
		}
		if len(active) != 1 || active[0].App != "g-issues" {
			t.Fatalf("active = %#v, want g-issues only", active)
		}
		combined, err := svc.AppRollouts.ListActiveAndRecentTerminal(ctx, start)
		if err != nil {
			t.Fatalf("ListActiveAndRecentTerminal: %v", err)
		}
		if len(combined) != 2 {
			t.Fatalf("active and recent terminal = %#v, want both apps from one snapshot", combined)
		}
		combined, err = svc.AppRollouts.ListActiveAndRecentTerminal(ctx, start.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("ListActiveAndRecentTerminal after cutoff: %v", err)
		}
		if len(combined) != 1 || combined[0].App != "g-issues" {
			t.Fatalf("active and recent terminal = %#v, want active app only after cutoff", combined)
		}
	})
}

func TestAppInstanceMaterializationServiceListByAppVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	for _, instanceID := range []string{"replica-a", "replica-b"} {
		if _, err := svc.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
			InstanceID: instanceID,
			App:        "g-issues",
			Version:    "v1",
		}); err != nil {
			t.Fatalf("Acknowledge(%s): %v", instanceID, err)
		}
	}
	if _, err := svc.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID: "replica-c",
		App:        "g-issues",
		Version:    "v2",
	}); err != nil {
		t.Fatalf("Acknowledge other version: %v", err)
	}
	got, err := svc.AppInstanceMaterializations.ListByAppVersion(ctx, "g-issues", "v1")
	if err != nil {
		t.Fatalf("ListByAppVersion: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("materializations = %#v, want 2", got)
	}
}

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

func TestAppVersionRecoveryObservationServiceRecordsFirstObservation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppVersionRecoveryObservations
	recoveredAt := time.Date(2026, 7, 30, 13, 52, 15, 123456789, time.UTC)
	first := &core.AppVersionRecoveryObservation{
		ID:                      " request-1 ",
		App:                     " g-issues ",
		Version:                 " v2 ",
		RecoveredAt:             recoveredAt,
		SourceVersion:           " source-a ",
		LiveInstances:           5,
		MinimumHealthyInstances: 5,
	}
	recorded, err := svc.Record(ctx, first)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if recorded.ID != "request-1" || recorded.RecoveredAt.Nanosecond() != 123000000 {
		t.Fatalf("recorded observation = %#v", recorded)
	}

	duplicate := *first
	duplicate.RecoveredAt = recoveredAt.Add(time.Hour)
	duplicate.LiveInstances = 8
	recorded, err = svc.Record(ctx, &duplicate)
	if err != nil {
		t.Fatalf("duplicate Record: %v", err)
	}
	if !recorded.RecoveredAt.Equal(recoveredAt.Truncate(time.Millisecond)) || recorded.LiveInstances != 5 {
		t.Fatalf("duplicate rewrote observation: %#v", recorded)
	}
}

func TestAppVersionRecoveryObservationServiceGetMany(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppVersionRecoveryObservations
	if _, err := svc.Record(ctx, &core.AppVersionRecoveryObservation{
		ID:                      "request-1",
		App:                     "g-issues",
		Version:                 "v2",
		RecoveredAt:             time.Date(2026, 7, 30, 13, 52, 15, 0, time.UTC),
		SourceVersion:           "source-a",
		LiveInstances:           5,
		MinimumHealthyInstances: 5,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := svc.GetMany(ctx, []string{"request-1", "missing"})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(got) != 1 || got["request-1"].Version != "v2" {
		t.Fatalf("observations = %#v", got)
	}
}

func TestAppVersionRecoveryObservationServiceRecordIfCurrentFailedFencesState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*testing.T, *coredata.Services, time.Time)
	}{
		{
			name: "newer desired version",
			mutate: func(t *testing.T, services *coredata.Services, now time.Time) {
				t.Helper()
				if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
					ID:          "request-v3",
					App:         "g-issues",
					FromVersion: "v2",
					ToVersion:   "v3",
					Timestamp:   now.Add(time.Minute),
				}); err != nil {
					t.Fatalf("Append newer request: %v", err)
				}
			},
		},
		{
			name: "newer source version",
			mutate: func(t *testing.T, services *coredata.Services, now time.Time) {
				t.Helper()
				if _, err := services.GestaltdSourceVersionState.Activate(
					context.Background(), "source-b", now.Add(time.Minute), false, time.Minute, 15*time.Minute, 5,
				); err != nil {
					t.Fatalf("Activate newer source: %v", err)
				}
			},
		},
		{
			name: "changed minimum",
			mutate: func(t *testing.T, services *coredata.Services, now time.Time) {
				t.Helper()
				if _, err := services.GestaltdSourceVersionState.Activate(
					context.Background(), "source-a", now.Add(time.Minute), false, time.Minute, 15*time.Minute, 6,
				); err != nil {
					t.Fatalf("Activate changed minimum: %v", err)
				}
			},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			services, now := recoveryServiceFixture(t)
			tc.mutate(t, services, now)
			recorded, ok, err := services.AppVersionRecoveryObservations.RecordIfCurrentFailed(
				context.Background(),
				recoveryObservation(now),
			)
			if err != nil {
				t.Fatalf("RecordIfCurrentFailed: %v", err)
			}
			if ok || recorded != nil {
				t.Fatalf("stale observation recorded: %#v, ok=%v", recorded, ok)
			}
			if _, err := services.AppVersionRecoveryObservations.Get(context.Background(), "request-v2"); !errors.Is(err, core.ErrNotFound) {
				t.Fatalf("Get stale recovery error = %v, want not found", err)
			}
		})
	}
}

func TestAppVersionRecoveryObservationServiceRecordIfCurrentFailedIsConcurrentSafe(t *testing.T) {
	t.Parallel()

	services, now := recoveryServiceFixture(t)
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, recorded, err := services.AppVersionRecoveryObservations.RecordIfCurrentFailed(
				context.Background(),
				recoveryObservation(now),
			)
			if err == nil && !recorded {
				err = errors.New("current failed request was unexpectedly fenced")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent RecordIfCurrentFailed: %v", err)
		}
	}
	count, err := services.DB.ObjectStore(coredata.StoreAppVersionRecoveryObservations).Count(context.Background(), nil)
	if err != nil {
		t.Fatalf("Count recoveries: %v", err)
	}
	if count != 1 {
		t.Fatalf("recovery count = %d, want 1", count)
	}
}

func TestAppVersionRecoveryObservationFenceMatchesDesiredVersionTimestampTie(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services, now := recoveryFenceServices(t)
	requests := []*core.AppVersionChangeRequest{
		{
			ID:          "z-id-low-version",
			App:         "g-issues",
			FromVersion: "v0",
			ToVersion:   "v1",
			Timestamp:   now,
		},
		{
			ID:          "a-id-high-version",
			App:         "g-issues",
			FromVersion: "v1",
			ToVersion:   "v2",
			Timestamp:   now,
		},
	}
	for _, request := range requests {
		if _, err := services.AppVersionChangeRequests.AppendRequest(ctx, request); err != nil {
			t.Fatalf("AppendRequest(%s): %v", request.ID, err)
		}
	}
	known, err := services.AppVersionChangeRequests.ListKnownVersionsByApp(ctx, "g-issues")
	if err != nil {
		t.Fatalf("ListKnownVersionsByApp: %v", err)
	}
	if desired := coredata.LatestKnownVersion(known); desired != "v2" {
		t.Fatalf("LatestKnownVersion = %q, want v2", desired)
	}
	if err := services.AppVersionRolloutOutcomes.RecordFailed(
		ctx, "a-id-high-version", "g-issues", "v2", now.Add(time.Minute),
	); err != nil {
		t.Fatalf("RecordFailed: %v", err)
	}

	recorded, ok, err := services.AppVersionRecoveryObservations.RecordIfCurrentFailed(
		ctx,
		recoveryObservationFor("a-id-high-version", "v2", now.Add(2*time.Minute)),
	)
	if err != nil {
		t.Fatalf("RecordIfCurrentFailed: %v", err)
	}
	if !ok || recorded == nil || recorded.ID != "a-id-high-version" {
		t.Fatalf("recorded = %#v, ok=%v", recorded, ok)
	}
}

func TestAppVersionRecoveryObservationFenceBreaksSameVersionTieByRevisionID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services, now := recoveryFenceServices(t)
	for _, id := range []string{"a-revision", "z-revision"} {
		if _, err := services.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
			ID:          id,
			App:         "g-issues",
			FromVersion: "v1",
			ToVersion:   "v2",
			Timestamp:   now,
		}); err != nil {
			t.Fatalf("AppendRequest(%s): %v", id, err)
		}
		if err := services.AppVersionRolloutOutcomes.RecordFailed(
			ctx, id, "g-issues", "v2", now.Add(time.Minute),
		); err != nil {
			t.Fatalf("RecordFailed(%s): %v", id, err)
		}
	}

	recorded, ok, err := services.AppVersionRecoveryObservations.RecordIfCurrentFailed(
		ctx,
		recoveryObservationFor("a-revision", "v2", now.Add(2*time.Minute)),
	)
	if err != nil {
		t.Fatalf("RecordIfCurrentFailed(stale revision): %v", err)
	}
	if ok || recorded != nil {
		t.Fatalf("stale same-version revision recorded: %#v, ok=%v", recorded, ok)
	}

	recorded, ok, err = services.AppVersionRecoveryObservations.RecordIfCurrentFailed(
		ctx,
		recoveryObservationFor("z-revision", "v2", now.Add(2*time.Minute)),
	)
	if err != nil {
		t.Fatalf("RecordIfCurrentFailed(latest revision): %v", err)
	}
	if !ok || recorded == nil || recorded.ID != "z-revision" {
		t.Fatalf("latest same-version revision = %#v, ok=%v", recorded, ok)
	}
}

func recoveryServiceFixture(t *testing.T) (*coredata.Services, time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 13, 52, 15, 0, time.UTC)
	services := testutil.NewStubServices(t)
	if _, err := services.GestaltdSourceVersionState.Activate(
		ctx, "source-a", now.Add(-time.Hour), false, time.Minute, 15*time.Minute, 5,
	); err != nil {
		t.Fatalf("Activate source: %v", err)
	}
	if _, err := services.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
		ID:          "request-v2",
		App:         "g-issues",
		FromVersion: "v1",
		ToVersion:   "v2",
		Timestamp:   now.Add(-20 * time.Minute),
	}); err != nil {
		t.Fatalf("Append request: %v", err)
	}
	if err := services.AppVersionRolloutOutcomes.RecordFailed(
		ctx, "request-v2", "g-issues", "v2", now.Add(-5*time.Minute),
	); err != nil {
		t.Fatalf("Record failed outcome: %v", err)
	}
	return services, now
}

func recoveryFenceServices(t *testing.T) (*coredata.Services, time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 7, 30, 13, 52, 15, 0, time.UTC)
	services := testutil.NewStubServices(t)
	if _, err := services.GestaltdSourceVersionState.Activate(
		ctx, "source-a", now.Add(-time.Hour), false, time.Minute, 15*time.Minute, 5,
	); err != nil {
		t.Fatalf("Activate source: %v", err)
	}
	return services, now
}

func recoveryObservation(now time.Time) *core.AppVersionRecoveryObservation {
	return recoveryObservationFor("request-v2", "v2", now)
}

func recoveryObservationFor(id, version string, now time.Time) *core.AppVersionRecoveryObservation {
	return &core.AppVersionRecoveryObservation{
		ID:                      id,
		App:                     "g-issues",
		Version:                 version,
		RecoveredAt:             now,
		SourceVersion:           "source-a",
		LiveInstances:           5,
		MinimumHealthyInstances: 5,
	}
}

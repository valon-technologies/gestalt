package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
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

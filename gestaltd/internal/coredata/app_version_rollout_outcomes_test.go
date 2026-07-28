package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppVersionRolloutOutcomeServiceRecordCompleteIsIdempotent(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t).AppVersionRolloutOutcomes
	ctx := context.Background()
	completedAt := time.Date(2026, 7, 24, 20, 45, 8, 0, time.UTC)

	if err := svc.RecordComplete(ctx, "req-1", "g-issues", "v2", completedAt); err != nil {
		t.Fatalf("RecordComplete: %v", err)
	}
	if err := svc.RecordComplete(ctx, "req-1", "g-issues", "v2", completedAt.Add(time.Minute)); err != nil {
		t.Fatalf("duplicate RecordComplete: %v", err)
	}

	outcome, err := svc.Get(ctx, "req-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if outcome.Version != "v2" || !outcome.CompletedAt.Equal(completedAt) || !outcome.FailedAt.IsZero() {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestAppVersionRolloutOutcomeServiceRecordFailed(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t).AppVersionRolloutOutcomes
	ctx := context.Background()
	failedAt := time.Date(2026, 7, 24, 20, 54, 41, 0, time.UTC)

	if err := svc.RecordFailed(ctx, "req-2", "g-issues", "v3", failedAt); err != nil {
		t.Fatalf("RecordFailed: %v", err)
	}

	outcomes, err := svc.GetMany(ctx, []string{"req-2", "missing"})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %#v", outcomes)
	}
	outcome := outcomes["req-2"]
	if outcome.FailedAt != failedAt || !outcome.CompletedAt.IsZero() {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestAppVersionRolloutOutcomeServiceGetMissing(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t).AppVersionRolloutOutcomes
	_, err := svc.Get(context.Background(), "missing")
	if err == nil || err != core.ErrNotFound {
		t.Fatalf("Get error = %v, want ErrNotFound", err)
	}
}

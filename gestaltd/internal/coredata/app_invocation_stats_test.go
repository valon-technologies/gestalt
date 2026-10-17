package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/observability"
)

func TestAppInvocationStatsSumsEveryWriter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppInvocationStats
	start := time.Now().UTC().Truncate(observability.AppInvocationBucketWidth)
	a := observability.AppInvocationWriter{InstanceID: "replica-a", BootID: "boot-1"}
	b := observability.AppInvocationWriter{InstanceID: "replica-b", BootID: "boot-1"}

	if err := svc.WriteBuckets(ctx, a, []observability.AppInvocationBucket{
		{Provider: "billing", Operation: "invoices.list", Start: start, Requests: 3, Errors: 1, DurationSum: 300 * time.Millisecond},
		{Provider: "billing", Operation: "invoices.get", Start: start, Requests: 2, DurationSum: 100 * time.Millisecond},
		{Provider: "other-app", Operation: "x", Start: start, Requests: 99},
	}); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := svc.WriteBuckets(ctx, b, []observability.AppInvocationBucket{
		{Provider: "billing", Operation: "invoices.list", Start: start, Requests: 4, Errors: 2, DurationSum: 400 * time.Millisecond},
	}); err != nil {
		t.Fatalf("write b: %v", err)
	}

	got, err := svc.AppInvocationStats(ctx, "billing", start.Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got.Requests != 9 || got.Errors != 3 || got.DurationSum != 800*time.Millisecond {
		t.Fatalf("totals = %+v, want requests 9 errors 3 duration 800ms", got)
	}
	if got.Instances != 2 {
		t.Fatalf("instances = %d, want 2", got.Instances)
	}
	if len(got.Operations) != 2 || got.Operations[0].Operation != "invoices.get" ||
		got.Operations[1].Operation != "invoices.list" || got.Operations[1].Requests != 7 {
		t.Fatalf("operations = %+v", got.Operations)
	}
}

func TestAppInvocationStatsRewriteIsCumulativeNotAdditive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppInvocationStats
	start := time.Now().UTC().Truncate(observability.AppInvocationBucketWidth)
	writer := observability.AppInvocationWriter{InstanceID: "replica-a", BootID: "boot-1"}

	for _, requests := range []int64{2, 5} {
		if err := svc.WriteBuckets(ctx, writer, []observability.AppInvocationBucket{
			{Provider: "billing", Operation: "invoices.list", Start: start, Requests: requests},
		}); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	got, err := svc.AppInvocationStats(ctx, "billing", start.Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got.Requests != 5 {
		t.Fatalf("requests = %d, want 5 (a rewrite replaces the writer's row)", got.Requests)
	}
}

func TestAppInvocationStatsRestartKeepsPreviousProcessCounts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppInvocationStats
	start := time.Now().UTC().Truncate(observability.AppInvocationBucketWidth)
	before := observability.AppInvocationWriter{InstanceID: "replica-a", BootID: "boot-1"}
	after := observability.AppInvocationWriter{InstanceID: "replica-a", BootID: "boot-2"}

	if err := svc.WriteBuckets(ctx, before, []observability.AppInvocationBucket{
		{Provider: "billing", Operation: "invoices.list", Start: start, Requests: 6},
	}); err != nil {
		t.Fatalf("write before restart: %v", err)
	}
	if err := svc.WriteBuckets(ctx, after, []observability.AppInvocationBucket{
		{Provider: "billing", Operation: "invoices.list", Start: start, Requests: 1},
	}); err != nil {
		t.Fatalf("write after restart: %v", err)
	}
	got, err := svc.AppInvocationStats(ctx, "billing", start.Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got.Requests != 7 {
		t.Fatalf("requests = %d, want 7 across both boots", got.Requests)
	}
	if got.Instances != 1 {
		t.Fatalf("instances = %d, want 1 (same instance, two boots)", got.Instances)
	}
}

func TestAppInvocationStatsWindowExcludesOlderBuckets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppInvocationStats
	now := time.Now().UTC().Truncate(observability.AppInvocationBucketWidth)
	writer := observability.AppInvocationWriter{InstanceID: "replica-a", BootID: "boot-1"}
	if err := svc.WriteBuckets(ctx, writer, []observability.AppInvocationBucket{
		{Provider: "billing", Operation: "old", Start: now.Add(-48 * time.Hour), Requests: 10},
		{Provider: "billing", Operation: "new", Start: now, Requests: 1},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := svc.AppInvocationStats(ctx, "billing", now.Add(-observability.DefaultAppInvocationWindow), 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got.Requests != 1 || len(got.Operations) != 1 || got.Operations[0].Operation != "new" {
		t.Fatalf("stats = %+v, want only the in-window bucket", got)
	}
}

func TestAppInvocationStatsMergesRecentAcrossWritersNewestFirst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppInvocationStats
	base := time.Now().UTC().Truncate(time.Second)
	a := observability.AppInvocationWriter{InstanceID: "replica-a", BootID: "boot-1"}
	b := observability.AppInvocationWriter{InstanceID: "replica-b", BootID: "boot-1"}

	if err := svc.WriteRecent(ctx, a, "billing", []observability.InvocationRecord{
		{ID: 1, Operation: "a-old", Outcome: observability.InvocationPassed, Status: 200, Timestamp: base.Add(-3 * time.Second)},
		{ID: 2, Operation: "a-new", Outcome: observability.InvocationFailed, Status: 500, Duration: time.Second, Timestamp: base.Add(-1 * time.Second)},
	}); err != nil {
		t.Fatalf("write recent a: %v", err)
	}
	if err := svc.WriteRecent(ctx, b, "billing", []observability.InvocationRecord{
		{ID: 1, Operation: "b-mid", Outcome: observability.InvocationPassed, Status: 200, Timestamp: base.Add(-2 * time.Second)},
	}); err != nil {
		t.Fatalf("write recent b: %v", err)
	}

	got, err := svc.AppInvocationStats(ctx, "billing", base.Add(-time.Hour), 2)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(got.Recent) != 2 || got.Recent[0].Operation != "a-new" || got.Recent[1].Operation != "b-mid" {
		t.Fatalf("recent = %+v, want a-new then b-mid", got.Recent)
	}
	if got.Recent[0].ID == got.Recent[1].ID {
		t.Fatalf("recent ids collide: %+v", got.Recent)
	}
	if got.Recent[0].Outcome != observability.InvocationFailed || got.Recent[0].Status != 500 || got.Recent[0].Duration != time.Second {
		t.Fatalf("recent[0] = %+v", got.Recent[0])
	}
}

func TestAppInvocationStatsPruneDropsExpiredRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppInvocationStats
	now := time.Now().UTC().Truncate(observability.AppInvocationBucketWidth)
	writer := observability.AppInvocationWriter{InstanceID: "replica-a", BootID: "boot-1"}
	if err := svc.WriteBuckets(ctx, writer, []observability.AppInvocationBucket{
		{Provider: "billing", Operation: "old", Start: now.Add(-96 * time.Hour), Requests: 10},
		{Provider: "billing", Operation: "new", Start: now, Requests: 1},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := svc.PruneBefore(ctx, now.Add(-observability.DefaultAppInvocationRetention)); err != nil {
		t.Fatalf("prune: %v", err)
	}
	got, err := svc.AppInvocationStats(ctx, "billing", now.Add(-200*time.Hour), 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if got.Requests != 1 {
		t.Fatalf("requests = %d, want 1 after pruning the expired bucket", got.Requests)
	}
}

func TestAppInvocationStatsRequiresWriterIdentity(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t).AppInvocationStats
	err := svc.WriteBuckets(context.Background(), observability.AppInvocationWriter{InstanceID: "replica-a"}, []observability.AppInvocationBucket{
		{Provider: "billing", Operation: "x", Start: time.Now(), Requests: 1},
	})
	if err == nil {
		t.Fatal("expected an error for a writer without a boot id")
	}
}

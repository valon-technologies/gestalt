package appregistry

import (
	"testing"
	"time"
)

func TestPendingVersionsForAdmin_ExcludesPublishedAndSortsNewestFirst(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 7, 24, 18, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 24, 19, 0, 0, 0, time.UTC)
	pending := &PendingIndex{
		Pending: map[string]PendingVersion{
			"0.0.1": {Version: "0.0.1", StartedAt: older, UpdatedAt: older, Phase: PendingPhasePublishing},
			"0.0.2": {Version: "0.0.2", StartedAt: newer, UpdatedAt: newer, Phase: PendingPhasePublishing},
			"0.0.3": {Version: "0.0.3", StartedAt: newer, UpdatedAt: newer, Phase: PendingPhasePublishing},
		},
	}
	published := map[string]struct{}{"0.0.3": {}}

	got := PendingVersionsForAdmin(pending, published)
	if len(got) != 2 || got[0].Version != "0.0.2" || got[1].Version != "0.0.1" {
		t.Fatalf("pending versions = %#v", got)
	}
}

func TestFailedVersionsForAdmin_ExcludesPublishedAndPending(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 7, 24, 18, 0, 0, 0, time.UTC)
	failedAt := startedAt.Add(35 * time.Minute)
	failed := &FailedIndex{
		Failed: map[string]FailedVersion{
			"0.0.1": {Version: "0.0.1", StartedAt: startedAt, FailedAt: failedAt, Reason: FailedReasonStale},
			"0.0.2": {Version: "0.0.2", StartedAt: startedAt, FailedAt: failedAt, Reason: FailedReasonWorkflowFailed},
			"0.0.3": {Version: "0.0.3", StartedAt: startedAt, FailedAt: failedAt, Reason: FailedReasonWorkflowFailed},
		},
	}
	published := map[string]struct{}{"0.0.2": {}}
	pending := map[string]struct{}{"0.0.3": {}}

	got := FailedVersionsForAdmin(failed, published, pending)
	if len(got) != 1 || got[0].Version != "0.0.1" {
		t.Fatalf("failed versions = %#v", got)
	}
}

func TestDurationSecondsBetween(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 24, 19, 0, 0, 0, time.UTC)
	end := start.Add(4*time.Minute + 32*time.Second)
	seconds, ok := DurationSecondsBetween(start, end)
	if !ok || seconds != 272 {
		t.Fatalf("DurationSecondsBetween() = (%d, %v), want (272, true)", seconds, ok)
	}
	if _, ok := DurationSecondsBetween(time.Time{}, end); ok {
		t.Fatal("expected zero start to return false")
	}
}

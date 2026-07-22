package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppInstanceMaterializationAcknowledgeForRolloutResetsStaleProgress(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	svc := services.AppInstanceMaterializations
	oldAck := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	if _, err := svc.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID:     "replica-a",
		App:            "g-issues",
		Version:        "1.0.0",
		AcknowledgedAt: oldAck,
	}); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if _, err := svc.MarkMaterialized(ctx, "replica-a", "g-issues", "1.0.0", oldAck.Add(time.Second)); err != nil {
		t.Fatalf("MarkMaterialized: %v", err)
	}
	if _, err := svc.MarkStopped(ctx, "replica-a", "g-issues", "1.0.0", oldAck.Add(2*time.Second)); err != nil {
		t.Fatalf("MarkStopped: %v", err)
	}
	if _, err := svc.MarkRestarted(ctx, "replica-a", "g-issues", "1.0.0", oldAck.Add(3*time.Second)); err != nil {
		t.Fatalf("MarkRestarted: %v", err)
	}
	if _, err := svc.RecordFailure(ctx, "replica-a", "g-issues", "1.0.0", oldAck.Add(4*time.Second), "old failure"); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}

	rolloutCreatedAt := oldAck.Add(24 * time.Hour)
	newAck := rolloutCreatedAt.Add(time.Second)
	got, err := svc.AcknowledgeForRollout(ctx, &core.AppInstanceMaterialization{
		InstanceID:     "replica-a",
		App:            "g-issues",
		Version:        "1.0.0",
		AcknowledgedAt: newAck,
	}, rolloutCreatedAt)
	if err != nil {
		t.Fatalf("AcknowledgeForRollout: %v", err)
	}
	if !got.AcknowledgedAt.Equal(newAck) {
		t.Fatalf("AcknowledgedAt = %v, want %v", got.AcknowledgedAt, newAck)
	}
	if !got.MaterializedAt.IsZero() || !got.StoppedAt.IsZero() || !got.RestartedAt.IsZero() {
		t.Fatalf("stale progress was not reset: %#v", got)
	}
	if got.AttemptCount != 0 || !got.LastErrorAt.IsZero() || got.LastErrorMessage != "" {
		t.Fatalf("stale failure was not reset: %#v", got)
	}
}

package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppInstanceMaterializationRecordFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	if _, err := svc.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID: "replica-a", App: "g-issues", Version: "1.0.0",
	}); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	failedAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 2; i++ {
		got, err := svc.AppInstanceMaterializations.RecordFailure(
			ctx, "replica-a", "g-issues", "1.0.0", failedAt, "start failed",
		)
		if err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
		if got.AttemptCount != i {
			t.Fatalf("AttemptCount = %d, want %d", got.AttemptCount, i)
		}
		if got.LastErrorAt != failedAt || got.LastErrorMessage != "start failed" {
			t.Fatalf("failure fields = %#v", got)
		}
	}
}

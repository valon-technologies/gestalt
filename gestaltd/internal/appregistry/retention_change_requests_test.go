package appregistry_test

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestDeployableUntilDeadlinesFromChangeRequests_usesLatestDeactivation(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	second := first.Add(24 * time.Hour)
	deadlines := appregistry.DeployableUntilDeadlinesFromChangeRequests([]*core.AppVersionChangeRequest{
		{
			FromVersion:                "v1",
			ToVersion:                  "v2",
			Timestamp:                  first,
			FromVersionDeployableUntil: ptrTime(first.Add(720 * time.Hour)),
		},
		{
			FromVersion:                "v1",
			ToVersion:                  "v3",
			Timestamp:                  second,
			FromVersionDeployableUntil: ptrTime(second.Add(720 * time.Hour)),
		},
	})
	if got := deadlines["v1"]; !got.Equal(second.Add(720*time.Hour)) {
		t.Fatalf("deadline = %s, want %s", got, second.Add(720*time.Hour))
	}
}

package coredata

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestLatestKnownVersionBreaksTimestampTiesByVersion(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	installations := []*core.AppInstallation{
		{Version: "1.0.0", UpdatedAt: updatedAt},
		{Version: "2.0.0", UpdatedAt: updatedAt},
	}
	if got := LatestKnownVersion(installations); got != "2.0.0" {
		t.Fatalf("LatestKnownVersion = %q, want 2.0.0", got)
	}
}

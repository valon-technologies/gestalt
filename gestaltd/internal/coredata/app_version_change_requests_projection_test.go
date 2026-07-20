package coredata

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestKnownVersionsAreOrderedByInstallTime(t *testing.T) {
	t.Parallel()

	earlier := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Minute)
	installations := knownVersionsFromRequests([]*core.AppVersionChangeRequest{
		{App: "g-issues", ToVersion: "v10", Timestamp: later},
		{App: "g-issues", ToVersion: "v9", Timestamp: earlier},
	})

	if len(installations) != 2 {
		t.Fatalf("installations = %#v", installations)
	}
	if installations[0].Version != "v9" || installations[1].Version != "v10" {
		t.Fatalf("versions = [%s, %s], want [v9, v10]", installations[0].Version, installations[1].Version)
	}
	if latest := LatestKnownVersion(installations); latest != "v10" {
		t.Fatalf("LatestKnownVersion = %q, want v10", latest)
	}
}

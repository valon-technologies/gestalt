package coredata_test

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func TestLatestKnownVersionBreaksTimestampTiesLexicographically(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	lower := &core.AppInstallation{Version: "1.0.0", UpdatedAt: at}
	higher := &core.AppInstallation{Version: "2.0.0", UpdatedAt: at}
	for _, installations := range [][]*core.AppInstallation{
		{lower, higher},
		{higher, lower},
	} {
		if got := coredata.LatestKnownVersion(installations); got != "2.0.0" {
			t.Fatalf("LatestKnownVersion = %q, want 2.0.0", got)
		}
	}
}

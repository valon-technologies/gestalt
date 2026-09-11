package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
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

func TestInstallationFromChangeRequestPreservesSourceIdentity(t *testing.T) {
	t.Parallel()

	request := &core.AppVersionChangeRequest{
		App:       "g-issues",
		ToVersion: "1.2.3",
		Metadata: coredata.ChangeRequestMetadata(&core.AppInstallation{
			AppName:          "g-issues",
			Version:          "1.2.3",
			SourceRepository: " github.com/valon-technologies/valon-tools ",
			SourceRef:        " abc123 ",
		}),
	}

	installation := coredata.InstallationFromChangeRequest(request)
	if installation.SourceRepository != "github.com/valon-technologies/valon-tools" {
		t.Fatalf("SourceRepository = %q", installation.SourceRepository)
	}
	if installation.SourceRef != "abc123" {
		t.Fatalf("SourceRef = %q", installation.SourceRef)
	}
}

func TestAppVersionChangeRequestServiceListDesiredRevisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := testutil.NewStubServices(t).AppVersionChangeRequests
	start := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	requests := []*core.AppVersionChangeRequest{
		{ID: "issues-v1", App: "g-issues", FromVersion: "v0", ToVersion: "v1", Timestamp: start},
		{ID: "issues-v2-first", App: "g-issues", FromVersion: "v1", ToVersion: "v2", Timestamp: start.Add(time.Minute)},
		{ID: "issues-v2-latest", App: "g-issues", FromVersion: "v1", ToVersion: "v2", Timestamp: start.Add(2 * time.Minute)},
		{ID: "slack-v1", App: "g-slack", FromVersion: "v0", ToVersion: "v1", Timestamp: start},
	}
	for _, request := range requests {
		if _, err := service.AppendRequest(ctx, request); err != nil {
			t.Fatalf("AppendRequest(%s): %v", request.ID, err)
		}
	}

	got, err := service.ListDesiredRevisions(ctx)
	if err != nil {
		t.Fatalf("ListDesiredRevisions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("desired revisions = %#v, want 2", got)
	}
	if got[0].App != "g-issues" || got[0].Version != "v2" || got[0].ChangeRequestID != "issues-v2-latest" {
		t.Fatalf("g-issues desired revision = %#v", got[0])
	}
	if got[1].App != "g-slack" || got[1].Version != "v1" || got[1].ChangeRequestID != "slack-v1" {
		t.Fatalf("g-slack desired revision = %#v", got[1])
	}
}

package coredata_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppRegistryPublishSessionDedupeAndUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	svc := services.AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.1.0",
		DedupeKey: "g-issues\x000.1.0\x00abc", DeclarationDigest: "abc",
		DeclarationJSON:    []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		PublisherSubjectID: "user:alice", StagingPrefix: "apps/g-issues/publish-staging/pub_test",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.1.0",
		DedupeKey: "g-issues\x000.1.0\x00abc", DeclarationDigest: "abc",
		DeclarationJSON:    []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		PublisherSubjectID: "user:alice", StagingPrefix: "apps/g-issues/publish-staging/pub_test2",
	}); !errors.Is(err, coredata.ErrPublishSessionConflict) {
		t.Fatalf("duplicate dedupe Create error = %v", err)
	}
	got, err := svc.GetByDedupeKey(ctx, session.DedupeKey)
	if err != nil {
		t.Fatalf("GetByDedupeKey: %v", err)
	}
	if got.ID != session.ID {
		t.Fatalf("GetByDedupeKey = %q, want %q", got.ID, session.ID)
	}
}

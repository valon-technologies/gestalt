package coredata_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppRegistryPublishSessionConcurrentCreateClaimsVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	svc := services.AppRegistryPublishSessions

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(digest string) {
			defer wg.Done()
			<-start
			_, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
				App: "g-issues", Registry: "toolshed", Version: "0.2.0",
				DedupeKey: "g-issues\x000.2.0\x00" + digest, DeclarationDigest: digest,
				DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
				StagingPrefix:   "apps/g-issues/publish-staging/pub_" + digest,
			})
			errs <- err
		}(map[int]string{0: "aaa", 1: "bbb"}[i])
	}
	close(start)
	wg.Wait()
	close(errs)

	var okCount, conflictCount int
	for err := range errs {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, coredata.ErrPublishSessionVersionLocked):
			conflictCount++
		default:
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("ok=%d conflict=%d", okCount, conflictCount)
	}
}

func TestAppRegistryPublishSessionClaimFinalizeIsExclusive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.2.1",
		DedupeKey: "dedupe-finalize", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_finalize",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.ClaimFinalize(ctx, session.ID, 15*time.Minute); err != nil {
		t.Fatalf("first ClaimFinalize: %v", err)
	}
	if _, err := svc.ClaimFinalize(ctx, session.ID, 15*time.Minute); !errors.Is(err, coredata.ErrPublishSessionFinalizeConflict) {
		t.Fatalf("second ClaimFinalize error = %v", err)
	}
}

func TestAppRegistryPublishSessionNeverMarksFailedPublished(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.2.2",
		DedupeKey: "dedupe-failed", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_failed",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	failed, err := svc.MarkFailed(ctx, session.ID, "", session.UpdatedAt, "boom")
	if err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if failed.State != core.AppRegistryPublishSessionFailed {
		t.Fatalf("state = %q", failed.State)
	}
	if _, err := svc.MarkPublished(ctx, session.ID, failed.FinalizeClaimToken, failed.UpdatedAt, failed.UpdatedAt); !errors.Is(err, coredata.ErrPublishSessionStateConflict) {
		t.Fatalf("MarkPublished after failed error = %v", err)
	}
}

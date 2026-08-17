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

func TestAppRegistryPublishSessionSameClockMultipleTransitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.2.7",
		DedupeKey: "dedupe-same-clock", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_same_clock",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if session.Revision != 1 {
		t.Fatalf("create revision = %d, want 1", session.Revision)
	}

	claimed, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}
	if claimed.Revision != 2 {
		t.Fatalf("claim revision = %d, want 2", claimed.Revision)
	}

	renewed, err := svc.RenewFinalizeClaim(ctx, session.ID, claimed.FinalizeClaimToken, claimed.Revision, 2*time.Minute)
	if err != nil {
		t.Fatalf("RenewFinalizeClaim: %v", err)
	}
	if renewed.Revision != 3 {
		t.Fatalf("renew revision = %d, want 3", renewed.Revision)
	}
	if renewed.UpdatedAt.Equal(claimed.UpdatedAt) {
		// Same millisecond truncation is expected; revision must still advance.
		if renewed.Revision == claimed.Revision {
			t.Fatalf("revision did not advance under same UpdatedAt")
		}
	}
	if _, err := svc.RenewFinalizeClaim(ctx, session.ID, claimed.FinalizeClaimToken, claimed.Revision, time.Minute); !errors.Is(err, coredata.ErrPublishSessionStateConflict) {
		t.Fatalf("stale revision RenewFinalizeClaim = %v", err)
	}
}

func TestAppRegistryPublishSessionLegacyRecordWithoutRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	svc := services.AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.2.8",
		DedupeKey: "dedupe-legacy", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_legacy",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	store := services.DB.ObjectStore(coredata.StoreAppRegistryPublishSessions)
	rec, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get legacy record: %v", err)
	}
	delete(rec, "revision")
	if err := store.Put(ctx, rec); err != nil {
		t.Fatalf("Put legacy record: %v", err)
	}

	legacy, err := svc.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if legacy.Revision != 0 {
		t.Fatalf("legacy revision = %d, want 0", legacy.Revision)
	}

	claimed, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize legacy: %v", err)
	}
	if claimed.Revision != 1 {
		t.Fatalf("post-claim revision = %d, want 1", claimed.Revision)
	}
	if _, err := svc.ClaimFinalize(ctx, session.ID, time.Minute); !errors.Is(err, coredata.ErrPublishSessionFinalizeConflict) {
		t.Fatalf("active legacy claim must reject takeover: %v", err)
	}
}

func TestAppRegistryPublishSessionConcurrentRenewFinalizeClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.2.9",
		DedupeKey: "dedupe-concurrent-renew-rev", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_concurrent_renew_rev",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	claimed, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.RenewFinalizeClaim(ctx, session.ID, claimed.FinalizeClaimToken, claimed.Revision, 2*time.Minute)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var okCount, conflictCount int
	for err := range results {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, coredata.ErrPublishSessionStateConflict):
			conflictCount++
		default:
			t.Fatalf("unexpected renew error: %v", err)
		}
	}
	if okCount != 1 || conflictCount != workers-1 {
		t.Fatalf("ok=%d conflict=%d", okCount, conflictCount)
	}
	final, err := svc.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Revision != claimed.Revision+1 {
		t.Fatalf("final revision = %d, want %d", final.Revision, claimed.Revision+1)
	}
	if final.State != core.AppRegistryPublishSessionFinalizing {
		t.Fatalf("state = %q, want finalizing", final.State)
	}
}

func TestAppRegistryPublishSessionConcurrentClaimFinalizeTakeover(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.2.10",
		DedupeKey: "dedupe-concurrent-takeover-rev", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_concurrent_takeover_rev",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	first, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}
	if _, err := svc.Update(ctx, session.ID, func(current *core.AppRegistryPublishSession) error {
		current.FinalizeClaimExpiresAt = time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
		return nil
	}); err != nil {
		t.Fatalf("expire claim: %v", err)
	}

	const workers = 4
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]*core.AppRegistryPublishSession, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = svc.ClaimFinalize(ctx, session.ID, time.Minute)
		}(i)
	}
	close(start)
	wg.Wait()

	var okCount, conflictCount int
	var winner *core.AppRegistryPublishSession
	for i, err := range errs {
		switch {
		case err == nil:
			okCount++
			winner = results[i]
		case errors.Is(err, coredata.ErrPublishSessionFinalizeConflict):
			conflictCount++
		case errors.Is(err, coredata.ErrPublishSessionStateConflict):
			conflictCount++
		default:
			t.Fatalf("unexpected takeover error: %v", err)
		}
	}
	if okCount != 1 || conflictCount != workers-1 {
		t.Fatalf("ok=%d conflict=%d", okCount, conflictCount)
	}
	if winner == nil || winner.FinalizeClaimToken == first.FinalizeClaimToken {
		t.Fatal("expected new claim token after expired concurrent takeover")
	}
}

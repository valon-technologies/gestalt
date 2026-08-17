package coredata_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func mustClaimFinalizeAcquired(t *testing.T, svc *coredata.AppRegistryPublishSessionService, ctx context.Context, id string, leaseTTL time.Duration) *core.AppRegistryPublishSession {
	t.Helper()
	result, err := svc.ClaimFinalize(ctx, id, leaseTTL)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}
	if result.Outcome != coredata.FinalizeClaimOutcomeAcquired {
		t.Fatalf("ClaimFinalize outcome = %q, want acquired", result.Outcome)
	}
	return result.Session
}

func TestAppRegistryPublishSessionClaimFinalizeAlreadyPublished(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.3.0-dev.claim-published",
		DedupeKey: "dedupe-claim-published", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_claim_published",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	claimed := mustClaimFinalizeAcquired(t, svc, ctx, session.ID, time.Minute)
	published, err := svc.MarkPublished(ctx, claimed.ID, claimed.FinalizeClaimToken, claimed.Revision, claimed.FinalizePublishedAt)
	if err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}

	result, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize published: %v", err)
	}
	if result.Outcome != coredata.FinalizeClaimOutcomeAlreadyPublished {
		t.Fatalf("outcome = %q, want already-published", result.Outcome)
	}
	if result.Session.State != core.AppRegistryPublishSessionPublished {
		t.Fatalf("state = %q", result.Session.State)
	}
	if !result.Session.PublishedAt.Equal(published.PublishedAt) {
		t.Fatalf("PublishedAt = %v, want %v", result.Session.PublishedAt, published.PublishedAt)
	}
}

func TestAppRegistryPublishSessionClaimFinalizeInProgress(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.3.0-dev.claim-in-progress",
		DedupeKey: "dedupe-claim-in-progress", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_claim_in_progress",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	first := mustClaimFinalizeAcquired(t, svc, ctx, session.ID, time.Minute)

	result, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize in-progress: %v", err)
	}
	if result.Outcome != coredata.FinalizeClaimOutcomeInProgress {
		t.Fatalf("outcome = %q, want in-progress", result.Outcome)
	}
	if result.Session.FinalizeClaimToken != first.FinalizeClaimToken {
		t.Fatalf("in-progress must not rotate claim token")
	}
}

func TestAppRegistryPublishSessionClaimFinalizeFailedTerminal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.3.0-dev.claim-failed",
		DedupeKey: "dedupe-claim-failed", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_claim_failed",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.MarkFailed(ctx, session.ID, "", session.Revision, "boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	_, err = svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if !errors.Is(err, coredata.ErrPublishSessionTerminal) {
		t.Fatalf("ClaimFinalize failed session = %v", err)
	}
}

func TestAppRegistryPublishSessionClaimFinalizeExpiredTakeoverAcquired(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.3.0-dev.claim-takeover",
		DedupeKey: "dedupe-claim-takeover", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_claim_takeover",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	first := mustClaimFinalizeAcquired(t, svc, ctx, session.ID, time.Minute)
	firstPublishedAt := first.FinalizePublishedAt
	if _, err := svc.MutatePublishSessionForTest(ctx, session.ID, func(current *core.AppRegistryPublishSession) error {
		current.FinalizeClaimExpiresAt = time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
		return nil
	}); err != nil {
		t.Fatalf("expire claim: %v", err)
	}

	result, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("takeover ClaimFinalize: %v", err)
	}
	if result.Outcome != coredata.FinalizeClaimOutcomeAcquired {
		t.Fatalf("outcome = %q, want acquired", result.Outcome)
	}
	if result.Session.FinalizeClaimToken == first.FinalizeClaimToken {
		t.Fatal("expected new claim token after expired takeover")
	}
	if !result.Session.FinalizePublishedAt.Equal(firstPublishedAt) {
		t.Fatalf("FinalizePublishedAt = %v, want %v", result.Session.FinalizePublishedAt, firstPublishedAt)
	}
}

func TestAppRegistryPublishSessionClaimFinalizeRaceWinnerPublishedBeforeLoserClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.3.0-dev.claim-race-published",
		DedupeKey: "dedupe-claim-race-published", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_claim_race_published",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	winner := mustClaimFinalizeAcquired(t, svc, ctx, session.ID, time.Minute)
	if _, err := svc.MarkPublished(ctx, winner.ID, winner.FinalizeClaimToken, winner.Revision, winner.FinalizePublishedAt); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}

	result, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("loser ClaimFinalize: %v", err)
	}
	if result.Outcome != coredata.FinalizeClaimOutcomeAlreadyPublished {
		t.Fatalf("outcome = %q, want already-published", result.Outcome)
	}
}

func TestAppRegistryPublishSessionClaimFinalizeConcurrentFromUploading(t *testing.T) {
	t.Parallel()

	const iterations = 100
	for i := range iterations {
		ctx := context.Background()
		svc := testutil.NewStubServices(t).AppRegistryPublishSessions
		version := fmt.Sprintf("0.3.0-dev.claim-upload-race-%d", i)
		session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
			App: "g-issues", Registry: "toolshed", Version: version,
			DedupeKey: "dedupe-claim-upload-race-" + version, DeclarationDigest: "digest",
			DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
			StagingPrefix:   "apps/g-issues/publish-staging/pub_claim_upload_race_" + version,
		})
		if err != nil {
			t.Fatalf("iteration %d Create: %v", i, err)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		outcomes := make([]coredata.FinalizeClaimOutcome, 2)
		errs := make([]error, 2)
		for idx := range outcomes {
			go func(slot int) {
				defer wg.Done()
				result, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
				errs[slot] = err
				if result != nil {
					outcomes[slot] = result.Outcome
				}
			}(idx)
		}
		wg.Wait()

		acquired := 0
		for slot, outcome := range outcomes {
			if errs[slot] != nil {
				t.Fatalf("iteration %d slot %d ClaimFinalize: %v", i, slot, errs[slot])
			}
			switch outcome {
			case coredata.FinalizeClaimOutcomeAcquired:
				acquired++
			case coredata.FinalizeClaimOutcomeInProgress:
			default:
				t.Fatalf("iteration %d slot %d unexpected outcome %q", i, slot, outcome)
			}
		}
		if acquired != 1 {
			t.Fatalf("iteration %d acquired=%d, want exactly one owner", i, acquired)
		}
		final, err := svc.Get(ctx, session.ID)
		if err != nil {
			t.Fatalf("iteration %d Get: %v", i, err)
		}
		if final.State != core.AppRegistryPublishSessionFinalizing {
			t.Fatalf("iteration %d final state = %q, want finalizing", i, final.State)
		}
	}
}

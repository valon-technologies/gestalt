package coredata_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppRegistryPublishSessionRenewFinalizeClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.2.6",
		DedupeKey: "dedupe-renew", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_renew",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	claimed, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}
	renewed, err := svc.RenewFinalizeClaim(ctx, session.ID, claimed.FinalizeClaimToken, claimed.UpdatedAt, 2*time.Minute)
	if err != nil {
		t.Fatalf("RenewFinalizeClaim: %v", err)
	}
	if !renewed.FinalizeClaimExpiresAt.After(claimed.FinalizeClaimExpiresAt) {
		t.Fatalf("expiresAt = %v, want after %v", renewed.FinalizeClaimExpiresAt, claimed.FinalizeClaimExpiresAt)
	}
	if _, err := svc.RenewFinalizeClaim(ctx, session.ID, claimed.FinalizeClaimToken, claimed.UpdatedAt, time.Minute); !errors.Is(err, coredata.ErrPublishSessionStateConflict) {
		t.Fatalf("stale revision RenewFinalizeClaim = %v", err)
	}
	if _, err := svc.ClaimFinalize(ctx, session.ID, time.Minute); !errors.Is(err, coredata.ErrPublishSessionFinalizeConflict) {
		t.Fatalf("active claim must reject ClaimFinalize: %v", err)
	}
}

func TestAppRegistryPublishSessionClaimFinalizeTakeoverAfterExpiry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.2.3",
		DedupeKey: "dedupe-expired", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_expired",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	first, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}
	firstPublishedAt := first.FinalizePublishedAt
	firstToken := first.FinalizeClaimToken

	expired, err := svc.Update(ctx, session.ID, func(current *core.AppRegistryPublishSession) error {
		current.FinalizeClaimExpiresAt = time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("expire claim: %v", err)
	}
	second, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("takeover ClaimFinalize: %v", err)
	}
	if second.FinalizeClaimToken == firstToken {
		t.Fatal("expected new claim token after takeover")
	}
	if !second.FinalizePublishedAt.Equal(firstPublishedAt) {
		t.Fatalf("publishedAt = %v, want %v", second.FinalizePublishedAt, firstPublishedAt)
	}
	if second.FinalizeClaimExpiresAt.Before(expired.FinalizeClaimExpiresAt) {
		t.Fatalf("claim expiry not renewed: %v", second.FinalizeClaimExpiresAt)
	}
}

func TestAppRegistryPublishSessionMarkPublishedRequiresClaimToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.2.4",
		DedupeKey: "dedupe-token", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_token",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	claimed, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}
	if _, err := svc.MarkPublished(ctx, claimed.ID, "stale-token", claimed.UpdatedAt, claimed.FinalizePublishedAt); !errors.Is(err, coredata.ErrPublishSessionClaimMismatch) {
		t.Fatalf("MarkPublished stale token error = %v", err)
	}
	published, err := svc.MarkPublished(ctx, claimed.ID, claimed.FinalizeClaimToken, claimed.UpdatedAt, claimed.FinalizePublishedAt)
	if err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}
	if !published.PublishedAt.Equal(claimed.FinalizePublishedAt) {
		t.Fatalf("PublishedAt = %v, want %v", published.PublishedAt, claimed.FinalizePublishedAt)
	}
}

func TestAppRegistryPublishSessionMarkFailedRejectsStaleClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.2.5",
		DedupeKey: "dedupe-fail-token", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_fail_token",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	claimed, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}
	if _, err := svc.MarkFailed(ctx, claimed.ID, "stale-token", claimed.UpdatedAt, "boom"); !errors.Is(err, coredata.ErrPublishSessionClaimMismatch) {
		t.Fatalf("MarkFailed stale token error = %v", err)
	}
}

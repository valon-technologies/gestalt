package appregistry_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func TestPublishSessionConcurrentFinalizeExactlyOneOwner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := newPublishSessionServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	service := newPublishSessionService(t, services.AppRegistryPublishSessions, store, signer)
	limits := service.Limits
	limits.FinalizeClaimLeaseTTL = 30 * time.Minute
	service.Limits = limits

	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.concurrent-one")
	created, err := service.Create(ctx, appregistry.CreatePublishSessionInput{
		App: "g-issues", Registry: "toolshed", StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:         "https://storage.googleapis.com/gestalt-app-registry",
		PublisherSubjectID: "user:alice", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, lease := range created.Session.UploadLeases {
		if err := appregistry.ApplyMemoryUpload(mem, lease.UploadURL, artifactBytes, declaration.Artifacts[0].SHA256); err != nil {
			t.Fatalf("upload: %v", err)
		}
	}

	input := appregistry.FinalizePublishSessionInput{
		App: "g-issues", PublishID: created.Session.ID, StorageRoot: "gs://gestalt-app-registry",
		PublicRoot: "https://storage.googleapis.com/gestalt-app-registry",
	}
	var sideEffects int32
	countingStore := &promoteCountingStore{
		WritableRegistryStore: store,
		onPromote: func() {
			atomic.AddInt32(&sideEffects, 1)
		},
	}
	service.Store = countingStore
	service.Writer = &appregistry.Writer{Store: countingStore}

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.Finalize(ctx, input)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var successCount, inProgressCount int
	for err := range results {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, appregistry.ErrPublishFinalizeInProgress):
			inProgressCount++
		default:
			t.Fatalf("unexpected finalize error: %v", err)
		}
	}
	if successCount != 1 || inProgressCount != workers-1 {
		t.Fatalf("success=%d inProgress=%d", successCount, inProgressCount)
	}
	if got := atomic.LoadInt32(&sideEffects); got != 1 {
		t.Fatalf("promotion side effects = %d, want 1", got)
	}
	session, err := services.AppRegistryPublishSessions.Get(ctx, created.Session.ID)
	if err != nil {
		t.Fatalf("Get session: %v", err)
	}
	if session.State != core.AppRegistryPublishSessionPublished {
		t.Fatalf("state = %q, want published", session.State)
	}
}

func TestPublishSessionConcurrentFinalizeCompetitorNeverMarksFailed(t *testing.T) {
	t.Parallel()

	h := newFinalizeCrashHarness(t, "0.3.0-dev.concurrent-fail-safe")
	claimed, err := h.sessions.ClaimFinalize(h.ctx, h.created.Session.ID, 30*time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := h.service.Finalize(h.ctx, h.finalizeInput())
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := h.service.Finalize(h.ctx, h.finalizeInput())
		errs <- err
	}()
	wg.Wait()
	close(errs)

	var inProgressCount int
	for err := range errs {
		if errors.Is(err, appregistry.ErrPublishFinalizeInProgress) {
			inProgressCount++
		}
	}
	if inProgressCount != 2 {
		t.Fatalf("inProgress=%d, want 2 competing callers blocked", inProgressCount)
	}
	session, err := h.sessions.Get(h.ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get session: %v", err)
	}
	if session.State != core.AppRegistryPublishSessionFinalizing {
		t.Fatalf("state = %q, want finalizing", session.State)
	}
	if session.FailureReason != "" {
		t.Fatalf("competing finalize must not mark failed: %q", session.FailureReason)
	}
}

func TestPublishSessionConcurrentFinalizeExpiredTakeoverGetsNewToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := newPublishSessionServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.3.0-dev.concurrent-token",
		DedupeKey: "dedupe-concurrent-token", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_concurrent_token",
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
	second, err := svc.ClaimFinalize(ctx, session.ID, time.Minute)
	if err != nil {
		t.Fatalf("takeover ClaimFinalize: %v", err)
	}
	if second.FinalizeClaimToken == first.FinalizeClaimToken {
		t.Fatal("expected new claim token after expired takeover")
	}
	if _, err := svc.ClaimFinalize(ctx, session.ID, time.Minute); !errors.Is(err, coredata.ErrPublishSessionFinalizeConflict) {
		t.Fatalf("active claim must reject second ClaimFinalize: %v", err)
	}
}

func TestPublishSessionRenewFinalizeClaimPreventsPrematureTakeover(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := newPublishSessionServices(t).AppRegistryPublishSessions
	session, err := svc.Create(ctx, coredata.CreateAppRegistryPublishSessionInput{
		App: "g-issues", Registry: "toolshed", Version: "0.3.0-dev.concurrent-renew",
		DedupeKey: "dedupe-concurrent-renew", DeclarationDigest: "digest",
		DeclarationJSON: []byte(`{"schema":"gestaltd.app.publish.declaration.v1"}`),
		StagingPrefix:   "apps/g-issues/publish-staging/pub_concurrent_renew",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	claimed, err := svc.ClaimFinalize(ctx, session.ID, 2*time.Second)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	renewed, err := svc.RenewFinalizeClaim(ctx, session.ID, claimed.FinalizeClaimToken, claimed.Revision, 2*time.Second)
	if err != nil {
		t.Fatalf("RenewFinalizeClaim: %v", err)
	}
	if !renewed.FinalizeClaimExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("claim not renewed: %v", renewed.FinalizeClaimExpiresAt)
	}
	if _, err := svc.ClaimFinalize(ctx, session.ID, time.Second); !errors.Is(err, coredata.ErrPublishSessionFinalizeConflict) {
		t.Fatalf("renewed claim must block takeover: %v", err)
	}
	if _, err := svc.RenewFinalizeClaim(ctx, session.ID, claimed.FinalizeClaimToken, renewed.Revision, time.Second); err != nil {
		t.Fatalf("RenewFinalizeClaim with current revision: %v", err)
	}
}

func TestPublishSessionFinalizeSlowWriterRenewsClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := newPublishSessionServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	service := newPublishSessionService(t, services.AppRegistryPublishSessions, store, signer)
	limits := service.Limits
	limits.FinalizeClaimLeaseTTL = 5 * time.Second
	service.Limits = limits
	slowStore := &slowPromoteStore{
		WritableRegistryStore: store,
		delay:                 3 * time.Second,
	}
	service.Store = slowStore
	service.Writer = &appregistry.Writer{Store: slowStore}

	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.concurrent-slow")
	created, err := service.Create(ctx, appregistry.CreatePublishSessionInput{
		App: "g-issues", Registry: "toolshed", StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:         "https://storage.googleapis.com/gestalt-app-registry",
		PublisherSubjectID: "user:alice", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, lease := range created.Session.UploadLeases {
		if err := appregistry.ApplyMemoryUpload(mem, lease.UploadURL, artifactBytes, declaration.Artifacts[0].SHA256); err != nil {
			t.Fatalf("upload: %v", err)
		}
	}
	result, err := service.Finalize(ctx, appregistry.FinalizePublishSessionInput{
		App: "g-issues", PublishID: created.Session.ID, StorageRoot: "gs://gestalt-app-registry",
		PublicRoot: "https://storage.googleapis.com/gestalt-app-registry",
	})
	if err != nil {
		t.Fatalf("Finalize with slow writer: %v", err)
	}
	if result.Session.State != core.AppRegistryPublishSessionPublished {
		t.Fatalf("state = %q", result.Session.State)
	}
}

func TestPromoteObjectMatchingDestinationRaceSucceeds(t *testing.T) {
	t.Parallel()

	store, _ := appregistry.NewMemoryPublishStores()
	sourceURL := appregistry.StorageURL("gs://gestalt-app-registry", "staging/obj.tgz")
	destURL := appregistry.StorageURL("gs://gestalt-app-registry", "apps/g-issues/0.3.0/linux-amd64.tar.gz")
	digest := declarationDigestForTest(t, "race-match")
	if err := seedMemoryObject(store, sourceURL, []byte("artifact"), digest); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	described, err := store.DescribeObject(sourceURL)
	if err != nil {
		t.Fatalf("DescribeObject source: %v", err)
	}
	input := appregistry.PromoteObjectInput{
		SourceURL: sourceURL, SourceGeneration: described.Generation, DestURL: destURL, ExpectedSHA256: digest,
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.PromoteObject(input)
		}()
	}
	wg.Wait()
	close(errs)
	var successCount int
	for err := range errs {
		if err == nil {
			successCount++
			continue
		}
		t.Fatalf("promote race error = %v", err)
	}
	if successCount != 2 {
		t.Fatalf("success=%d, want both promoters to succeed idempotently", successCount)
	}
	destDescribed, err := store.DescribeObject(destURL)
	if err != nil || destDescribed.Generation == 0 || destDescribed.SHA256 != digest {
		t.Fatalf("destination = %#v, err=%v", destDescribed, err)
	}
}

func TestPromoteObjectMismatchingDestinationRemainsTerminal(t *testing.T) {
	t.Parallel()

	store, _ := appregistry.NewMemoryPublishStores()
	sourceURL := appregistry.StorageURL("gs://gestalt-app-registry", "staging/obj-mismatch.tgz")
	destURL := appregistry.StorageURL("gs://gestalt-app-registry", "apps/g-issues/0.3.0/linux-amd64.tar.gz")
	wantDigest := declarationDigestForTest(t, "race-mismatch-want")
	wrongDigest := declarationDigestForTest(t, "race-mismatch-wrong")
	if err := seedMemoryObject(store, sourceURL, []byte("artifact"), wantDigest); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if err := seedMemoryObject(store, destURL, []byte("other"), wrongDigest); err != nil {
		t.Fatalf("seed destination: %v", err)
	}
	sourceDescribed, err := store.DescribeObject(sourceURL)
	if err != nil {
		t.Fatalf("DescribeObject source: %v", err)
	}
	err = store.PromoteObject(appregistry.PromoteObjectInput{
		SourceURL: sourceURL, SourceGeneration: sourceDescribed.Generation, DestURL: destURL, ExpectedSHA256: wantDigest,
	})
	if !errors.Is(err, appregistry.ErrObjectPreconditionFailed) {
		t.Fatalf("promote mismatch = %v", err)
	}
	described, describeErr := store.DescribeObject(destURL)
	if describeErr != nil || described.SHA256 != wrongDigest {
		t.Fatalf("destination must remain immutable conflict: %#v err=%v", described, describeErr)
	}
}

type promoteCountingStore struct {
	appregistry.WritableRegistryStore
	onPromote func()
}

func (s *promoteCountingStore) PromoteObject(input appregistry.PromoteObjectInput) error {
	if s.onPromote != nil {
		s.onPromote()
	}
	return s.WritableRegistryStore.PromoteObject(input)
}

type slowPromoteStore struct {
	appregistry.WritableRegistryStore
	delay time.Duration
}

func (s *slowPromoteStore) PromoteObject(input appregistry.PromoteObjectInput) error {
	time.Sleep(s.delay)
	return s.WritableRegistryStore.PromoteObject(input)
}

func declarationDigestForTest(t *testing.T, seed string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func seedMemoryObject(store appregistry.WritableRegistryStore, storageURL string, data []byte, digest string) error {
	path, err := appregistry.WriteTempJSON("gestalt-promote-seed-*", data)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(path) }()
	return store.WriteImmutableObject(appregistry.WriteImmutableObjectInput{
		LocalPath: path, StorageURL: storageURL, SHA256: digest,
	})
}

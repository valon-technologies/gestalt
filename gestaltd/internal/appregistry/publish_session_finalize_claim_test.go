package appregistry_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type finalizeCrashHarness struct {
	t             *testing.T
	ctx           context.Context
	service       *appregistry.PublishSessionService
	sessions      *coredata.AppRegistryPublishSessionService
	mem           *appregistry.MemoryObjectStore
	created       *appregistry.CreatePublishSessionResult
	declaration   *appregistry.PublishDeclaration
	artifactBytes []byte
	fixedNow      time.Time
}

func newFinalizeCrashHarness(t *testing.T, version string) *finalizeCrashHarness {
	t.Helper()
	ctx := context.Background()
	services := newPublishSessionServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	service := newPublishSessionService(t, services.AppRegistryPublishSessions, store, signer)
	fixedNow := time.Now().UTC().Truncate(time.Millisecond)
	service.Now = func() time.Time { return fixedNow }
	limits := service.Limits
	limits.FinalizeClaimLeaseTTL = 30 * time.Minute
	service.Limits = limits

	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", version)
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
	return &finalizeCrashHarness{
		t: t, ctx: ctx, service: service, sessions: services.AppRegistryPublishSessions, mem: mem,
		created: created, declaration: declaration, artifactBytes: artifactBytes, fixedNow: fixedNow,
	}
}

func (h *finalizeCrashHarness) finalizeInput() appregistry.FinalizePublishSessionInput {
	return appregistry.FinalizePublishSessionInput{
		App: "g-issues", PublishID: h.created.Session.ID, StorageRoot: "gs://gestalt-app-registry",
		PublicRoot: "https://storage.googleapis.com/gestalt-app-registry",
	}
}

func (h *finalizeCrashHarness) claimSession() (*core.AppRegistryPublishSession, time.Time) {
	h.t.Helper()
	claimed, err := h.sessions.ClaimFinalize(h.ctx, h.created.Session.ID, 30*time.Minute)
	if err != nil {
		h.t.Fatalf("ClaimFinalize: %v", err)
	}
	return claimed, claimed.FinalizePublishedAt
}

func TestPublishSessionFinalizeCrashBeforePromotion(t *testing.T) {
	t.Parallel()
	h := newFinalizeCrashHarness(t, "0.3.0-dev.crash1")
	_, wantPublishedAt := h.claimSession()

	result, err := h.service.Finalize(h.ctx, h.finalizeInput())
	if err != nil {
		t.Fatalf("Finalize after claimed crash: %v", err)
	}
	if !result.Session.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("PublishedAt = %v, want %v", result.Session.PublishedAt, wantPublishedAt)
	}
}

func TestPublishSessionFinalizeCrashBeforeRetention(t *testing.T) {
	t.Parallel()
	h := newFinalizeCrashHarness(t, "0.3.0-dev.crash2")
	_, wantPublishedAt := h.claimSession()

	store := h.service.Store.(appregistry.WritableRegistryStore)
	h.service.Writer = &appregistry.Writer{Store: &retentionFailStore{
		RegistryObjectStore: store,
		retentionURL:        appregistry.RetentionStorageURL(appregistry.StorageURL("gs://gestalt-app-registry", appregistry.AppIndexPath("g-issues")), "g-issues"),
	}}
	_, err := h.service.Finalize(h.ctx, h.finalizeInput())
	if err == nil {
		t.Fatal("expected retention failure")
	}

	h.service.Writer = &appregistry.Writer{Store: store}
	result, err := h.service.Finalize(h.ctx, h.finalizeInput())
	if err != nil {
		t.Fatalf("retry Finalize: %v", err)
	}
	if !result.Session.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("PublishedAt = %v, want %v", result.Session.PublishedAt, wantPublishedAt)
	}
}

func TestPublishSessionFinalizeCrashBeforeIndex(t *testing.T) {
	t.Parallel()
	h := newFinalizeCrashHarness(t, "0.3.0-dev.crash3")
	_, wantPublishedAt := h.claimSession()

	store := h.service.Store.(appregistry.WritableRegistryStore)
	indexURL := appregistry.StorageURL("gs://gestalt-app-registry", appregistry.AppIndexPath("g-issues"))
	h.service.Writer = &appregistry.Writer{
		Store: &catalogConflictStore{
			RegistryObjectStore: store,
			failURL:             indexURL,
			failRemaining:       1,
		},
		CatalogAttempts: 1,
	}
	_, err := h.service.Finalize(h.ctx, h.finalizeInput())
	if err == nil {
		t.Fatal("expected index failure")
	}

	h.service.Writer = &appregistry.Writer{Store: store}
	result, err := h.service.Finalize(h.ctx, h.finalizeInput())
	if err != nil {
		t.Fatalf("retry Finalize: %v", err)
	}
	if !result.Session.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("PublishedAt = %v, want %v", result.Session.PublishedAt, wantPublishedAt)
	}
}

func TestPublishSessionFinalizeCrashAfterIndex(t *testing.T) {
	t.Parallel()
	h := newFinalizeCrashHarness(t, "0.3.0-dev.crash4")
	result, err := h.service.Finalize(h.ctx, h.finalizeInput())
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	wantPublishedAt := result.Session.PublishedAt

	_, err = h.sessions.Update(h.ctx, result.Session.ID, func(session *core.AppRegistryPublishSession) error {
		session.State = core.AppRegistryPublishSessionFinalizing
		return nil
	})
	if err != nil {
		t.Fatalf("simulate stale finalizing state: %v", err)
	}
	status, err := h.service.Status(h.ctx, "g-issues", h.created.Session.ID, "gs://gestalt-app-registry", "")
	if err != nil {
		t.Fatalf("Status reconcile: %v", err)
	}
	if status.Session.State != core.AppRegistryPublishSessionPublished {
		t.Fatalf("state = %q", status.Session.State)
	}
	if !status.Session.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("PublishedAt = %v, want %v", status.Session.PublishedAt, wantPublishedAt)
	}
}

func TestPublishSessionFinalizeExpiredClaimTakeover(t *testing.T) {
	t.Parallel()
	h := newFinalizeCrashHarness(t, "0.3.0-dev.crash5")
	first, err := h.sessions.ClaimFinalize(h.ctx, h.created.Session.ID, time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}
	wantPublishedAt := first.FinalizePublishedAt

	if _, err := h.sessions.Update(h.ctx, first.ID, func(session *core.AppRegistryPublishSession) error {
		session.FinalizeClaimExpiresAt = time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
		return nil
	}); err != nil {
		t.Fatalf("expire claim: %v", err)
	}
	result, err := h.service.Finalize(h.ctx, h.finalizeInput())
	if err != nil {
		t.Fatalf("Finalize after takeover: %v", err)
	}
	if !result.Session.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("PublishedAt = %v, want %v", result.Session.PublishedAt, wantPublishedAt)
	}
}

func TestPublishSessionFinalizeRejectsStaleClaimToken(t *testing.T) {
	t.Parallel()
	h := newFinalizeCrashHarness(t, "0.3.0-dev.crash6")
	claimed, err := h.sessions.ClaimFinalize(h.ctx, h.created.Session.ID, 30*time.Minute)
	if err != nil {
		t.Fatalf("ClaimFinalize: %v", err)
	}
	_, err = h.sessions.MarkPublished(h.ctx, claimed.ID, "stale-token", claimed.UpdatedAt, claimed.FinalizePublishedAt)
	if !errors.Is(err, coredata.ErrPublishSessionClaimMismatch) {
		t.Fatalf("MarkPublished stale token = %v", err)
	}
}

func TestPublishSessionFinalizeTimestampStabilityAcrossRetries(t *testing.T) {
	t.Parallel()
	h := newFinalizeCrashHarness(t, "0.3.0-dev.crash7")
	claimed, wantPublishedAt := h.claimSession()

	store := h.service.Store.(appregistry.WritableRegistryStore)
	h.service.Writer = &appregistry.Writer{Store: &retentionFailStore{
		RegistryObjectStore: store,
		retentionURL:        appregistry.RetentionStorageURL(appregistry.StorageURL("gs://gestalt-app-registry", appregistry.AppIndexPath("g-issues")), "g-issues"),
	}}
	if _, err := h.service.Finalize(h.ctx, h.finalizeInput()); err == nil {
		t.Fatal("expected first finalize failure")
	}
	if _, err := h.sessions.Update(h.ctx, claimed.ID, func(session *core.AppRegistryPublishSession) error {
		session.FinalizeClaimExpiresAt = time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
		return nil
	}); err != nil {
		t.Fatalf("expire claim: %v", err)
	}
	h.service.Writer = &appregistry.Writer{Store: store}
	result, err := h.service.Finalize(h.ctx, h.finalizeInput())
	if err != nil {
		t.Fatalf("retry Finalize: %v", err)
	}
	if !result.Session.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("PublishedAt = %v, want %v", result.Session.PublishedAt, wantPublishedAt)
	}
	entry, err := appregistry.LoadPublishedEntry(h.mem, "gs://gestalt-app-registry", "g-issues", "0.3.0-dev.crash7")
	if err != nil || entry == nil {
		t.Fatalf("LoadPublishedEntry = %v, %v", entry, err)
	}
	if !entry.PublishedAt.Equal(wantPublishedAt) {
		t.Fatalf("entry PublishedAt = %v, want %v", entry.PublishedAt, wantPublishedAt)
	}
}

func TestPublishSessionFailedSessionsNeverRevive(t *testing.T) {
	t.Parallel()
	h := newFinalizeCrashHarness(t, "0.3.0-dev.crash8")
	failed, err := h.sessions.MarkFailed(h.ctx, h.created.Session.ID, "", h.created.Session.UpdatedAt, "terminal")
	if err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	_, err = h.sessions.MarkPublished(h.ctx, failed.ID, failed.FinalizeClaimToken, failed.UpdatedAt, h.fixedNow)
	if !errors.Is(err, coredata.ErrPublishSessionStateConflict) {
		t.Fatalf("MarkPublished after failed = %v", err)
	}
	_, err = h.service.Finalize(h.ctx, h.finalizeInput())
	if !errors.Is(err, appregistry.ErrPublishSessionFailed) {
		t.Fatalf("Finalize failed session = %v", err)
	}
}

type promoteFailStore struct {
	appregistry.WritableRegistryStore
}

func (s *promoteFailStore) PromoteObject(input appregistry.PromoteObjectInput) error {
	return fmt.Errorf("%w: simulated promotion failure", appregistry.ErrObjectPreconditionFailed)
}

type retentionFailStore struct {
	appregistry.RegistryObjectStore
	retentionURL string
}

func (s *retentionFailStore) WriteCatalogObject(input appregistry.WriteCatalogObjectInput) error {
	if input.StorageURL == s.retentionURL {
		return fmt.Errorf("%w: simulated retention failure", appregistry.ErrObjectPreconditionFailed)
	}
	return s.RegistryObjectStore.WriteCatalogObject(input)
}

type catalogConflictStore struct {
	appregistry.RegistryObjectStore
	failURL       string
	failRemaining int
}

func (s *catalogConflictStore) WriteCatalogObject(input appregistry.WriteCatalogObjectInput) error {
	if s.failRemaining > 0 && input.StorageURL == s.failURL {
		s.failRemaining--
		return fmt.Errorf("%w: simulated index failure", appregistry.ErrObjectPreconditionFailed)
	}
	return s.RegistryObjectStore.WriteCatalogObject(input)
}

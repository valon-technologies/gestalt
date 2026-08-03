package agentroute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/testing"
)

func TestIndexedDBStoreCreateResumeAndList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	store, err := NewIndexedDBStore(ctx, db)
	if err != nil {
		t.Fatalf("NewIndexedDBStore: %v", err)
	}
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	req := CreateRequest{
		Route: Route{
			AgentID:             "agent-1",
			OwnerSubjectID:      "user:owner-1",
			CredentialSubjectID: "user:owner-1",
			ProviderName:        "codex-production",
			ConfigRevision:      "rev-1",
			AuthorityRef:        "authority-1",
			RequestFingerprint:  "fingerprint-1",
		},
		IdempotencyKey: "create-1",
	}
	created, wasCreated, err := store.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !wasCreated || created.AgentID != "agent-1" || created.State != StateActive {
		t.Fatalf("Create = (%#v, %v), want active newly-created route", created, wasCreated)
	}

	replayed, wasCreated, err := store.Create(ctx, req)
	if err != nil {
		t.Fatalf("replayed Create: %v", err)
	}
	if wasCreated || replayed.AgentID != created.AgentID {
		t.Fatalf("replayed Create = (%#v, %v), want existing route", replayed, wasCreated)
	}

	owned, err := store.GetOwned(ctx, "agent-1", "user:owner-1")
	if err != nil {
		t.Fatalf("GetOwned: %v", err)
	}
	if owned.ProviderName != "codex-production" || owned.ConfigRevision != "rev-1" {
		t.Fatalf("GetOwned = %#v", owned)
	}
	if _, err := store.GetOwned(ctx, "agent-1", "user:other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetOwned wrong owner error = %v, want ErrNotFound", err)
	}

	byKey, err := store.FindByIdempotency(ctx, "user:owner-1", "create-1")
	if err != nil {
		t.Fatalf("FindByIdempotency: %v", err)
	}
	if byKey.AgentID != "agent-1" {
		t.Fatalf("FindByIdempotency = %#v", byKey)
	}

	routes, err := store.ListOwned(ctx, "user:owner-1", StateActive)
	if err != nil {
		t.Fatalf("ListOwned: %v", err)
	}
	if len(routes) != 1 || routes[0].AgentID != "agent-1" {
		t.Fatalf("ListOwned = %#v", routes)
	}
}

func TestIndexedDBStoreRejectsConflictingIdempotencyReplay(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewIndexedDBStore(ctx, &coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("NewIndexedDBStore: %v", err)
	}
	req := CreateRequest{
		Route: Route{
			AgentID:            "agent-1",
			OwnerSubjectID:     "user:owner-1",
			ProviderName:       "codex-production",
			ConfigRevision:     "rev-1",
			RequestFingerprint: "fingerprint-1",
		},
		IdempotencyKey: "create-1",
	}
	if _, _, err := store.Create(ctx, req); err != nil {
		t.Fatalf("Create: %v", err)
	}
	req.AgentID = "agent-2"
	req.RequestFingerprint = "fingerprint-2"
	if _, _, err := store.Create(ctx, req); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting Create error = %v, want ErrConflict", err)
	}
}

func TestIndexedDBStoreCompareAndSwapAndArchive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewIndexedDBStore(ctx, &coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("NewIndexedDBStore: %v", err)
	}
	store.now = func() time.Time {
		return time.Date(2026, time.July, 28, 13, 0, 0, 0, time.UTC)
	}
	req := CreateRequest{Route: Route{
		AgentID:        "agent-1",
		OwnerSubjectID: "user:owner-1",
		ProviderName:   "codex-production",
		ConfigRevision: "rev-1",
	}}
	if _, _, err := store.Create(ctx, req); err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := store.CompareAndSwapRevision(ctx, "agent-1", "user:owner-1", "rev-1", "rev-2")
	if err != nil {
		t.Fatalf("CompareAndSwapRevision: %v", err)
	}
	if updated.ConfigRevision != "rev-2" {
		t.Fatalf("CompareAndSwapRevision revision = %q, want rev-2", updated.ConfigRevision)
	}
	if _, err := store.CompareAndSwapRevision(ctx, "agent-1", "user:owner-1", "rev-1", "rev-3"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CompareAndSwapRevision error = %v, want ErrConflict", err)
	}
	if _, err := store.CompareAndSwapRevision(ctx, "agent-1", "user:owner-1", "rev-1", "rev-2"); err != nil {
		t.Fatalf("idempotent CompareAndSwapRevision: %v", err)
	}

	archived, err := store.Archive(ctx, "agent-1", "user:owner-1")
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if archived.State != StateArchived {
		t.Fatalf("Archive state = %q, want archived", archived.State)
	}
	if _, err := store.Archive(ctx, "agent-1", "user:owner-1"); err != nil {
		t.Fatalf("idempotent Archive: %v", err)
	}
	if _, err := store.CompareAndSwapRevision(ctx, "agent-1", "user:owner-1", "rev-2", "rev-3"); !errors.Is(err, ErrConflict) {
		t.Fatalf("archived CompareAndSwapRevision error = %v, want ErrConflict", err)
	}
}

func TestIndexedDBStorePersistsAcrossStoreInstances(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := &coretesting.StubIndexedDB{}
	first, err := NewIndexedDBStore(ctx, db)
	if err != nil {
		t.Fatalf("first NewIndexedDBStore: %v", err)
	}
	if _, _, err := first.Create(ctx, CreateRequest{Route: Route{
		AgentID:        "agent-1",
		OwnerSubjectID: "user:owner-1",
		ProviderName:   "codex-production",
		ConfigRevision: "rev-1",
	}}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	second, err := NewIndexedDBStore(ctx, db)
	if err != nil {
		t.Fatalf("second NewIndexedDBStore: %v", err)
	}
	route, err := second.GetOwned(ctx, "agent-1", "user:owner-1")
	if err != nil {
		t.Fatalf("GetOwned from second store: %v", err)
	}
	if route.ProviderName != "codex-production" {
		t.Fatalf("persisted route = %#v", route)
	}
}

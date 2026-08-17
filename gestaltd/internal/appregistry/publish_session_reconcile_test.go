package appregistry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestPublishSessionFinalizeAlreadyPublishedMissingRegistryProofFailsClosed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := newPublishSessionServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	service := newPublishSessionService(t, services.AppRegistryPublishSessions, store, signer)

	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.reconcile-missing")
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
	claimed := mustClaimFinalizeAcquired(t, services.AppRegistryPublishSessions, ctx, created.Session.ID, time.Minute)
	if _, err := services.AppRegistryPublishSessions.MarkPublished(ctx, claimed.ID, claimed.FinalizeClaimToken, claimed.Revision, claimed.FinalizePublishedAt); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}

	_, err = service.Finalize(ctx, appregistry.FinalizePublishSessionInput{
		App: "g-issues", PublishID: created.Session.ID, StorageRoot: "gs://gestalt-app-registry",
		PublicRoot: "https://storage.googleapis.com/gestalt-app-registry",
	})
	if !errors.Is(err, appregistry.ErrPublishReconcileMismatch) {
		t.Fatalf("Finalize missing proof = %v, want reconcile mismatch", err)
	}
}

func TestPublishSessionFinalizeAlreadyPublishedMismatchedRegistryProofFailsClosed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := newPublishSessionServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	service := newPublishSessionService(t, services.AppRegistryPublishSessions, store, signer)

	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.reconcile-mismatch")
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
	if _, err := service.Finalize(ctx, appregistry.FinalizePublishSessionInput{
		App: "g-issues", PublishID: created.Session.ID, StorageRoot: "gs://gestalt-app-registry",
		PublicRoot: "https://storage.googleapis.com/gestalt-app-registry",
	}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if _, err := services.AppRegistryPublishSessions.MutatePublishSessionForTest(ctx, created.Session.ID, func(session *core.AppRegistryPublishSession) error {
		session.DeclarationDigest = "mismatched-digest"
		return nil
	}); err != nil {
		t.Fatalf("MutatePublishSessionForTest: %v", err)
	}

	_, err = service.Finalize(ctx, appregistry.FinalizePublishSessionInput{
		App: "g-issues", PublishID: created.Session.ID, StorageRoot: "gs://gestalt-app-registry",
		PublicRoot: "https://storage.googleapis.com/gestalt-app-registry",
	})
	if !errors.Is(err, appregistry.ErrPublishReconcileMismatch) {
		t.Fatalf("Finalize mismatched proof = %v, want reconcile mismatch", err)
	}
}

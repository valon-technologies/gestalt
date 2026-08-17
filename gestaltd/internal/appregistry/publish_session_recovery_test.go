package appregistry_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestPublishSessionFinalizeReconcilesAfterIndexCommit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := newPublishSessionServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	service := newPublishSessionService(t, services.AppRegistryPublishSessions, store, signer)
	service.Now = func() time.Time { return time.Now().UTC() }

	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.5")
	created, err := service.Create(ctx, appregistry.CreatePublishSessionInput{
		App: "g-issues", Registry: "toolshed", StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declaration, PublisherSubjectID: "user:alice",
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
		t.Fatalf("Finalize: %v", err)
	}
	_, err = services.AppRegistryPublishSessions.MutatePublishSessionForTest(ctx, result.Session.ID, func(session *core.AppRegistryPublishSession) error {
		session.State = core.AppRegistryPublishSessionFinalizing
		return nil
	})
	if err != nil {
		t.Fatalf("simulate stale session state: %v", err)
	}
	status, err := service.Status(ctx, "g-issues", created.Session.ID, "gs://gestalt-app-registry", "")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Session.State != core.AppRegistryPublishSessionPublished {
		t.Fatalf("reconciled state = %q", status.Session.State)
	}
}

func TestPublishSessionCreateRejectsPublishedVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := newPublishSessionServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	service := newPublishSessionService(t, services.AppRegistryPublishSessions, store, signer)
	service.Now = func() time.Time { return time.Now().UTC() }
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.6")
	created, err := service.Create(ctx, appregistry.CreatePublishSessionInput{
		App: "g-issues", Registry: "toolshed", StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declaration, PublisherSubjectID: "user:alice",
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
	declB, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.6")
	declB.Artifacts[0].SHA256 = strings.Repeat("c", 64)
	_, err = service.Create(ctx, appregistry.CreatePublishSessionInput{
		App: "g-issues", Registry: "toolshed", StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declB, PublisherSubjectID: "user:bob",
	})
	if !errors.Is(err, appregistry.ErrPublishVersionConflict) {
		t.Fatalf("second create error = %v", err)
	}
}

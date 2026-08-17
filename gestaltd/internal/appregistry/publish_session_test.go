package appregistry_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestPublishSessionCreateRenewAndFinalize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := newPublishSessionServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	service := newPublishSessionService(t, services.AppRegistryPublishSessions, store, signer)
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.1")
	digest, err := appregistry.DeclarationDigest(declaration)
	if err != nil {
		t.Fatalf("DeclarationDigest: %v", err)
	}

	first, err := service.Create(ctx, appregistry.CreatePublishSessionInput{
		App:                "g-issues",
		Registry:           "toolshed",
		StorageRoot:        "gs://gestalt-app-registry",
		PublicRoot:         "https://storage.googleapis.com/gestalt-app-registry",
		PublisherSubjectID: "user:alice",
		Declaration:        declaration,
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if first.Session.ID == "" || first.Renewed {
		t.Fatalf("first create = %#v", first)
	}

	for _, lease := range first.Session.UploadLeases {
		if err := appregistry.ApplyMemoryUpload(mem, lease.UploadURL, artifactBytes, declaration.Artifacts[0].SHA256); err != nil {
			t.Fatalf("ApplyMemoryUpload(%s): %v", lease.Platform, err)
		}
	}

	second, err := service.Create(ctx, appregistry.CreatePublishSessionInput{
		App:                "g-issues",
		Registry:           "toolshed",
		StorageRoot:        "gs://gestalt-app-registry",
		PublicRoot:         "https://storage.googleapis.com/gestalt-app-registry",
		PublisherSubjectID: "user:alice",
		Declaration:        declaration,
	})
	if err != nil {
		t.Fatalf("retry Create() = %v", err)
	}
	if second.Session.ID != first.Session.ID {
		t.Fatalf("dedupe session ids = %q vs %q", second.Session.ID, first.Session.ID)
	}
	_ = digest

	result, err := service.Finalize(ctx, appregistry.FinalizePublishSessionInput{
		App:         "g-issues",
		PublishID:   first.Session.ID,
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
	})
	if err != nil {
		t.Fatalf("Finalize() = %v", err)
	}
	if result.Session.State != core.AppRegistryPublishSessionPublished {
		t.Fatalf("session state = %q", result.Session.State)
	}
	indexChecker := appregistry.StoreIndexChecker{Store: store, StorageRoot: "gs://gestalt-app-registry"}
	published, err := indexChecker.VersionPublished(ctx, "gs://gestalt-app-registry", "g-issues", "0.3.0-dev.1")
	if err != nil || !published {
		t.Fatalf("VersionPublished = (%v, %v)", published, err)
	}
}

func TestPublishSessionVersionConflictIsTerminal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := newPublishSessionServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	service := newPublishSessionService(t, services.AppRegistryPublishSessions, store, signer)

	declA, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.2")
	declB, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.2")
	declB.Artifacts[0].SHA256 = strings.Repeat("b", 64)

	if _, err := service.Create(ctx, appregistry.CreatePublishSessionInput{
		App: "g-issues", Registry: "toolshed", StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:         "https://storage.googleapis.com/gestalt-app-registry",
		PublisherSubjectID: "user:alice", Declaration: declA,
	}); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	_, err := service.Create(ctx, appregistry.CreatePublishSessionInput{
		App: "g-issues", Registry: "toolshed", StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:         "https://storage.googleapis.com/gestalt-app-registry",
		PublisherSubjectID: "user:bob", Declaration: declB,
	})
	if !errors.Is(err, appregistry.ErrPublishVersionConflict) {
		t.Fatalf("Create B error = %v, want version conflict", err)
	}
}

func TestPublishSessionRenewExpiredLease(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := newPublishSessionServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	service := newPublishSessionService(t, services.AppRegistryPublishSessions, store, signer)
	service.Now = func() time.Time { return now }

	declaration, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.3")
	created, err := service.Create(ctx, appregistry.CreatePublishSessionInput{
		App: "g-issues", Registry: "toolshed", StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:         "https://storage.googleapis.com/gestalt-app-registry",
		PublisherSubjectID: "user:alice", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	firstURL := created.Session.UploadLeases[0].UploadURL

	service.Now = func() time.Time { return now.Add(2 * time.Hour) }
	renewed, err := service.Create(ctx, appregistry.CreatePublishSessionInput{
		App: "g-issues", Registry: "toolshed", StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:         "https://storage.googleapis.com/gestalt-app-registry",
		PublisherSubjectID: "user:alice", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("renew Create: %v", err)
	}
	if !renewed.Renewed {
		t.Fatal("expected renewed leases")
	}
	if renewed.Session.UploadLeases[0].UploadURL == firstURL {
		t.Fatal("expected new upload URL after lease renewal")
	}
}

func TestPublishSessionPublicViewRedactsPublisher(t *testing.T) {
	t.Parallel()

	view := appregistry.PublishSessionPublicView(&core.AppRegistryPublishSession{
		ID:                 "pub_test",
		App:                "g-issues",
		PublisherSubjectID: "user:secret",
		UploadLeases: []core.AppRegistryUploadLease{{
			Platform:  "linux/amd64",
			UploadURL: "https://upload.example/upload",
			ExpiresAt: time.Now().UTC(),
		}},
	})
	if view["publisher"] != nil {
		t.Fatalf("public view leaked publisher: %#v", view)
	}
	if _, ok := view["uploads"]; !ok {
		t.Fatalf("public view = %#v", view)
	}
}

func newPublishSessionServices(t *testing.T) *coredata.Services {
	t.Helper()
	return testutil.NewStubServices(t)
}

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

func newPublishSessionService(t *testing.T, sessions *coredata.AppRegistryPublishSessionService, store appregistry.WritableRegistryStore, signer appregistry.RegistryUploadSigner) *appregistry.PublishSessionService {
	t.Helper()
	limits := appregistry.DefaultPublishSessionLimits()
	limits.RequiredPlatforms = []string{"linux/amd64"}
	writer := &appregistry.Writer{Store: store}
	return &appregistry.PublishSessionService{
		Sessions: sessions,
		Store:    store,
		Signer:   signer,
		Writer:   writer,
		Index:    appregistry.StoreIndexChecker{Store: store, StorageRoot: "gs://gestalt-app-registry"},
		Limits:   limits,
	}
}

func testPublishDeclaration(t *testing.T, appName, version string) (*appregistry.PublishDeclaration, []byte) {
	t.Helper()
	artifactBytes := []byte("artifact-" + version)
	sum := sha256.Sum256(artifactBytes)
	digest := hex.EncodeToString(sum[:])
	release := &providerrelease.Metadata{
		Schema:        providerrelease.SchemaName,
		SchemaVersion: providerrelease.SchemaVersion,
		Package:       "github.com/valon-technologies/valon-tools/apps/" + appName,
		Kind:          providermanifestv1.KindApp,
		Version:       version,
		Runtime:       providerrelease.RuntimeExecutable,
		Artifacts: providerrelease.Artifacts{
			"linux/amd64": {Path: "linux-amd64.tar.gz", SHA256: digest},
		},
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: &providermanifestv1.Manifest{
				Kind:    providermanifestv1.KindApp,
				Source:  "github.com/valon-technologies/valon-tools/apps/" + appName,
				Version: version,
				Spec:    &providermanifestv1.Spec{},
			},
			Catalog: &catalog.Catalog{
				Name: appName,
				Operations: []catalog.CatalogOperation{{
					ID:     "echo",
					Method: "POST",
				}},
			},
		},
	}
	declaration := &appregistry.PublishDeclaration{
		Schema: appregistry.PublishDeclarationSchemaVersion,
		Manifest: &providermanifestv1.Manifest{
			Kind:    providermanifestv1.KindApp,
			Source:  "github.com/valon-technologies/valon-tools/apps/" + appName,
			Version: version,
			Spec:    &providermanifestv1.Spec{},
		},
		ManifestPath:    "apps/" + appName + "/manifest.yaml",
		ReleaseMetadata: release,
		PublicationKind: appregistry.PublicationKindLocal,
		LocalSource:     &appregistry.LocalSourceState{CommitSHA: "651a5c30feb995c9364c38f63d0d5c3880bc2055"},
		BuilderVersion:  "0.1.0-test",
		Artifacts: []appregistry.PublishDeclarationArtifact{{
			Platform: "linux/amd64",
			Filename: "linux-amd64.tar.gz",
			SHA256:   digest,
			Size:     int64(len(artifactBytes)),
		}},
	}
	return declaration, artifactBytes
}

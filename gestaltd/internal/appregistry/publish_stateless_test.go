package appregistry_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestStatelessPublishBeginResumeAndFinalize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, mem := newStatelessPublishHarness(t)
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.1")

	first, err := service.Begin(ctx, appregistry.BeginPublishInput{
		App: "g-issues", Registry: "toolshed",
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Begin() = %v", err)
	}
	if first.State != appregistry.PublishStateUploading || len(first.Uploads) != 1 {
		t.Fatalf("first begin = %#v", first)
	}
	if err := appregistry.ApplyMemoryUpload(mem, first.Uploads[0].UploadURL, artifactBytes, declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}

	second, err := service.Begin(ctx, appregistry.BeginPublishInput{
		App: "g-issues", Registry: "toolshed",
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("resume Begin() = %v", err)
	}
	if second.PublishID != first.PublishID || second.State != appregistry.PublishStateUploading || len(second.Uploads) != 0 {
		t.Fatalf("resume begin = %#v", second)
	}

	final, err := service.Finalize(ctx, appregistry.FinalizePublishInput{
		App: "g-issues", PublishID: first.PublishID, Registry: "toolshed",
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Finalize() = %v", err)
	}
	if final.State != appregistry.PublishStatePublished {
		t.Fatalf("finalize = %#v", final)
	}

	idempotent, err := service.Finalize(ctx, appregistry.FinalizePublishInput{
		App: "g-issues", PublishID: first.PublishID, Registry: "toolshed",
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declaration,
	})
	if err != nil || idempotent.State != appregistry.PublishStatePublished {
		t.Fatalf("idempotent finalize = %#v, %v", idempotent, err)
	}
}

func TestStatelessPublishVersionConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, mem := newStatelessPublishHarness(t)
	declA, bytesA := testPublishDeclaration(t, "g-issues", "0.3.0-dev.2")
	declB, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.2")
	declB.Artifacts[0].SHA256 = strings.Repeat("b", 64)

	beginA, err := service.Begin(ctx, appregistry.BeginPublishInput{
		App: "g-issues", Registry: "toolshed",
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declA,
	})
	if err != nil {
		t.Fatalf("Begin A: %v", err)
	}
	if err := appregistry.ApplyMemoryUpload(mem, beginA.Uploads[0].UploadURL, bytesA, declA.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload A: %v", err)
	}
	if _, err := service.Finalize(ctx, appregistry.FinalizePublishInput{
		App: "g-issues", PublishID: beginA.PublishID, Registry: "toolshed",
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declA,
	}); err != nil {
		t.Fatalf("Finalize A: %v", err)
	}

	_, err = service.Begin(ctx, appregistry.BeginPublishInput{
		App: "g-issues", Registry: "toolshed",
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declB,
	})
	if !errors.Is(err, appregistry.ErrPublishVersionConflict) {
		t.Fatalf("Begin B error = %v, want version conflict", err)
	}
}

func TestStatelessPublishConcurrentFinalizeMatching(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, mem := newStatelessPublishHarness(t)
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.3")

	begin, err := service.Begin(ctx, appregistry.BeginPublishInput{
		App: "g-issues", Registry: "toolshed",
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := appregistry.ApplyMemoryUpload(mem, begin.Uploads[0].UploadURL, artifactBytes, declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}

	const workers = 6
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Finalize(ctx, appregistry.FinalizePublishInput{
				App: "g-issues", PublishID: begin.PublishID, Registry: "toolshed",
				StorageRoot: "gs://gestalt-app-registry",
				PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
				Declaration: declaration,
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Finalize: %v", err)
		}
	}
}

func TestStatelessPublishMissingFinalize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newStatelessPublishHarness(t)
	declaration, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.4")
	begin, err := service.Begin(ctx, appregistry.BeginPublishInput{
		App: "g-issues", Registry: "toolshed",
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := service.Finalize(ctx, appregistry.FinalizePublishInput{
		App: "g-issues", PublishID: begin.PublishID, Registry: "toolshed",
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		Declaration: declaration,
	}); !errors.Is(err, appregistry.ErrPublishUploadMissing) {
		t.Fatalf("Finalize missing error = %v", err)
	}
}

func newStatelessPublishHarness(t *testing.T) (*appregistry.StatelessPublishService, *appregistry.MemoryObjectStore) {
	t.Helper()
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	limits := appregistry.DefaultPublishLimits()
	limits.RequiredPlatforms = []string{"linux/amd64"}
	return &appregistry.StatelessPublishService{
		Store:  store,
		Signer: signer,
		Writer: &appregistry.Writer{Store: store},
		Index:  appregistry.StoreIndexChecker{Store: store, StorageRoot: "gs://gestalt-app-registry"},
		Limits: limits,
	}, mem
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
				Kind: providermanifestv1.KindApp, Source: "github.com/valon-technologies/valon-tools/apps/" + appName,
				Version: version, Spec: &providermanifestv1.Spec{},
			},
			Catalog: &catalog.Catalog{Name: appName, Operations: []catalog.CatalogOperation{{ID: "echo", Method: "POST"}}},
		},
	}
	return &appregistry.PublishDeclaration{
		Schema: appregistry.PublishDeclarationSchemaVersion,
		Manifest: &providermanifestv1.Manifest{
			Kind: providermanifestv1.KindApp, Source: "github.com/valon-technologies/valon-tools/apps/" + appName,
			Version: version, Spec: &providermanifestv1.Spec{},
		},
		ManifestPath: "apps/" + appName + "/manifest.yaml", ReleaseMetadata: release,
		PublicationKind: appregistry.PublicationKindLocal,
		LocalSource:     &appregistry.LocalSourceState{CommitSHA: "651a5c30feb995c9364c38f63d0d5c3880bc2055"},
		Artifacts: []appregistry.PublishDeclarationArtifact{{
			Platform: "linux/amd64", Filename: "linux-amd64.tar.gz", SHA256: digest, Size: int64(len(artifactBytes)),
		}},
	}, artifactBytes
}

package appregistry_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const testPublishStorageRoot = "gs://gestalt-app-registry"
const testPublishPublicRoot = "https://storage.googleapis.com/gestalt-app-registry"

func TestStatelessPublishBeginResumeAndFinalize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, mem := newStatelessPublishHarness(t)
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.1")

	first, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
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

	second, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("resume Begin() = %v", err)
	}
	if second.PublishID != first.PublishID || second.State != appregistry.PublishStateUploading || len(second.Uploads) != 0 {
		t.Fatalf("resume begin = %#v", second)
	}

	final, err := service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", PublishID: first.PublishID, Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Finalize() = %v", err)
	}
	if final.State != appregistry.PublishStatePublished {
		t.Fatalf("finalize = %#v", final)
	}

	idempotent, err := service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", PublishID: first.PublishID, Declaration: declaration,
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
	declB.ReleaseMetadata.Artifacts["linux/amd64"] = providerrelease.Artifact{
		Path: "linux-amd64.tar.gz", SHA256: strings.Repeat("b", 64),
	}

	beginA, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declA,
	})
	if err != nil {
		t.Fatalf("Begin A: %v", err)
	}
	if err := appregistry.ApplyMemoryUpload(mem, beginA.Uploads[0].UploadURL, bytesA, declA.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload A: %v", err)
	}
	if _, err := service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", PublishID: beginA.PublishID, Declaration: declA,
	}); err != nil {
		t.Fatalf("Finalize A: %v", err)
	}

	_, err = service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declB,
	})
	if !errors.Is(err, appregistry.ErrPublishVersionConflict) {
		t.Fatalf("Begin B error = %v, want version conflict", err)
	}
}

func TestStatelessPublishConcurrentFinalizeMatching(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var tick atomic.Uint64
	service, mem := newStatelessPublishHarnessWithNow(t, func() time.Time {
		return time.Now().UTC().Add(time.Duration(tick.Add(1)) * time.Second)
	})
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.3")

	begin, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
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
			_, err := service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
				App: "g-issues", PublishID: begin.PublishID, Declaration: declaration,
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
	loaded, err := appregistry.LoadPublishedState(service.Store, service.StorageRoot, "g-issues", "0.3.0-dev.3")
	if err != nil {
		t.Fatalf("LoadPublishedState: %v", err)
	}
	if loaded.State != appregistry.PublishedLoadVerified {
		t.Fatalf("published state = %v, want verified", loaded.State)
	}
	if !loaded.Entry.PublishedAt.Before(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("publishedAt = %v, want recent winning timestamp", loaded.Entry.PublishedAt)
	}
}

func TestStatelessPublishMissingFinalize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newStatelessPublishHarness(t)
	declaration, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.4")
	begin, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", PublishID: begin.PublishID, Declaration: declaration,
	}); !errors.Is(err, appregistry.ErrPublishUploadMissing) {
		t.Fatalf("Finalize missing error = %v", err)
	}
}

func TestStatelessPublishRejectsWrongRegistry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newStatelessPublishHarness(t)
	declaration, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.5")
	_, err := service.Begin(ctx, "other-registry", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
	})
	if !errors.Is(err, appregistry.ErrPublishRegistryNotEnrolled) {
		t.Fatalf("Begin error = %v, want registry not enrolled", err)
	}
}

func TestStatelessPublishCorruptIndexFailsClosed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, mem := newStatelessPublishHarness(t)
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.6")
	begin, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := appregistry.ApplyMemoryUpload(mem, begin.Uploads[0].UploadURL, artifactBytes, declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}

	indexPath := appregistry.StorageURL(service.StorageRoot, appregistry.AppIndexPath("g-issues"))
	indexData := []byte(`{"schemaVersion":1,"apps":{"g-issues":{"versions":{"0.3.0-dev.6":{"metadata":"apps/g-issues/versions/0.3.0-dev.6.json","publishedAt":"2026-08-17T12:00:00Z","publishId":"pub_corrupt","declarationDigest":"deadbeef"}}}}}`)
	tmpPath, err := appregistry.WriteTempJSON("gestalt-corrupt-index-*", indexData)
	if err != nil {
		t.Fatalf("WriteTempJSON: %v", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()
	if err := mem.WriteCatalogObject(appregistry.WriteCatalogObjectInput{
		LocalPath: tmpPath, StorageURL: indexPath, SourceRef: "local", Generation: 0,
	}); err != nil {
		t.Fatalf("seed corrupt index: %v", err)
	}

	_, err = service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", PublishID: begin.PublishID, Declaration: declaration,
	})
	if !errors.Is(err, appregistry.ErrPublishReconcileMismatch) {
		t.Fatalf("Finalize corrupt index error = %v", err)
	}
}

func newStatelessPublishHarness(t *testing.T) (*appregistry.StatelessPublishService, *appregistry.MemoryObjectStore) {
	t.Helper()
	return newStatelessPublishHarnessWithNow(t, func() time.Time { return time.Now().UTC() })
}

func newStatelessPublishHarnessWithNow(t *testing.T, now func() time.Time) (*appregistry.StatelessPublishService, *appregistry.MemoryObjectStore) {
	t.Helper()
	mem := appregistry.NewMemoryObjectStore()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	limits := appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}}
	return &appregistry.StatelessPublishService{
		Registry: "toolshed", StorageRoot: testPublishStorageRoot, PublicRoot: testPublishPublicRoot,
		Store: mem, Signer: signer, Writer: &appregistry.Writer{Store: mem}, Limits: limits, Now: now,
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

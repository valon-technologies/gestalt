package appregistry_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func TestStatelessPublishFinalizePreflightBeforePromotion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mem := appregistry.NewMemoryObjectStore()
	recorder := &promoteRecorder{MemoryObjectStore: mem}
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	limits := appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}}
	service := &appregistry.StatelessPublishService{
		Registry: "toolshed", StorageRoot: testPublishStorageRoot, PublicRoot: testPublishPublicRoot,
		Store: recorder, Signer: signer, Writer: &appregistry.Writer{Store: recorder}, Limits: limits,
	}
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.12")

	begin, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := appregistry.ApplyMemoryUpload(mem, begin.Uploads[0].UploadURL, artifactBytes, declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}

	layout, err := appregistry.ResolvePublishLayout(declaration.Manifest.Source, declaration.Manifest.Version)
	if err != nil {
		t.Fatalf("ResolvePublishLayout: %v", err)
	}
	entryURL := appregistry.StorageURL(testPublishStorageRoot, layout.EntryPath)
	finalRel, err := appregistry.PublishArtifactFinalRel(layout.ArtifactPrefix, declaration.Artifacts[0].Filename)
	if err != nil {
		t.Fatalf("PublishArtifactFinalRel: %v", err)
	}
	declarationDigest, err := appregistry.DeclarationDigest(declaration)
	if err != nil {
		t.Fatalf("DeclarationDigest: %v", err)
	}
	conflictingEntry, err := appregistry.BuildEntry(appregistry.BuildEntryInput{
		Manifest: declaration.Manifest, Version: declaration.Manifest.Version,
		SourceRef:    "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		ManifestPath: declaration.ManifestPath, PublicationKind: declaration.PublicationKind,
		PublishID: begin.PublishID, BuilderVersion: declaration.BuilderVersion,
		DeclarationDigest: declarationDigest, LocalSource: declaration.LocalSource,
		Release: declaration.ReleaseMetadata,
		Artifacts: []appregistry.PublishArtifact{{
			Target: declaration.Artifacts[0].Platform, Filename: declaration.Artifacts[0].Filename,
			StorageURL: appregistry.StorageURL(testPublishStorageRoot, finalRel),
			PublicURL:  appregistry.PublicURL(testPublishPublicRoot, finalRel),
			SHA256:     declaration.Artifacts[0].SHA256,
		}},
		PublishedAt: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildEntry: %v", err)
	}
	entryData, err := json.MarshalIndent(conflictingEntry, "", "  ")
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	entryPath, err := appregistry.WriteTempJSON("gestalt-orphan-entry-*", append(entryData, '\n'))
	if err != nil {
		t.Fatalf("WriteTempJSON: %v", err)
	}
	defer func() { _ = os.Remove(entryPath) }()
	if err := mem.WriteImmutableObject(appregistry.WriteImmutableObjectInput{
		LocalPath: entryPath, StorageURL: entryURL, SourceRef: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	}); err != nil {
		t.Fatalf("seed orphan entry: %v", err)
	}

	_, err = service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", PublishID: begin.PublishID, Declaration: declaration,
	})
	if !errors.Is(err, appregistry.ErrRegistryEntryConflict) {
		t.Fatalf("Finalize error = %v, want ErrRegistryEntryConflict", err)
	}
	if len(recorder.promotions) != 0 {
		t.Fatalf("PromoteObject calls = %#v, want none", recorder.promotions)
	}
	finalURL := appregistry.StorageURL(testPublishStorageRoot, finalRel)
	described, err := mem.DescribeObject(finalURL)
	if err != nil {
		t.Fatalf("DescribeObject(final): %v", err)
	}
	if described.Generation != 0 {
		t.Fatalf("final artifact created before preflight: %#v", described)
	}
}

func TestStatelessPublishFinalizeResumesMatchingPromotedArtifacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mem := appregistry.NewMemoryObjectStore()
	recorder := &promoteRecorder{MemoryObjectStore: mem}
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	limits := appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}}
	service := &appregistry.StatelessPublishService{
		Registry: "toolshed", StorageRoot: testPublishStorageRoot, PublicRoot: testPublishPublicRoot,
		Store: recorder, Signer: signer, Writer: &appregistry.Writer{Store: recorder}, Limits: limits,
	}
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.13")

	begin, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := appregistry.ApplyMemoryUpload(mem, begin.Uploads[0].UploadURL, artifactBytes, declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}

	layout, err := appregistry.ResolvePublishLayout(declaration.Manifest.Source, declaration.Manifest.Version)
	if err != nil {
		t.Fatalf("ResolvePublishLayout: %v", err)
	}
	digest, err := appregistry.DeclarationDigest(declaration)
	if err != nil {
		t.Fatalf("DeclarationDigest: %v", err)
	}
	stagingPrefix, err := appregistry.PublishStagingPrefix("g-issues", declaration.Manifest.Version, digest)
	if err != nil {
		t.Fatalf("PublishStagingPrefix: %v", err)
	}
	stagingPath, err := appregistry.PublishStagingArtifactPath(stagingPrefix, declaration.Artifacts[0].Platform, declaration.Artifacts[0].Filename)
	if err != nil {
		t.Fatalf("PublishStagingArtifactPath: %v", err)
	}
	stagingURL := appregistry.StorageURL(testPublishStorageRoot, stagingPath)
	stagingDescribed, err := mem.DescribeObject(stagingURL)
	if err != nil {
		t.Fatalf("DescribeObject(staging): %v", err)
	}
	finalRel, err := appregistry.PublishArtifactFinalRel(layout.ArtifactPrefix, declaration.Artifacts[0].Filename)
	if err != nil {
		t.Fatalf("PublishArtifactFinalRel: %v", err)
	}
	finalURL := appregistry.StorageURL(testPublishStorageRoot, finalRel)
	sourceRef := declaration.LocalSource.CommitSHA
	if err := mem.PromoteObject(appregistry.PromoteObjectInput{
		SourceURL: stagingURL, SourceGeneration: stagingDescribed.Generation,
		DestURL: finalURL, ExpectedSHA256: declaration.Artifacts[0].SHA256, SourceRef: sourceRef,
	}); err != nil {
		t.Fatalf("pre-promote artifact: %v", err)
	}
	promotedBefore, err := mem.DescribeObject(finalURL)
	if err != nil {
		t.Fatalf("DescribeObject(final before finalize): %v", err)
	}
	recorder.promotions = nil

	final, err := service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", PublishID: begin.PublishID, Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if final.State != appregistry.PublishStatePublished {
		t.Fatalf("finalize = %#v", final)
	}
	promotedAfter, err := mem.DescribeObject(finalURL)
	if err != nil {
		t.Fatalf("DescribeObject(final after finalize): %v", err)
	}
	if promotedAfter.Generation != promotedBefore.Generation {
		t.Fatalf("final artifact generation changed: before=%d after=%d", promotedBefore.Generation, promotedAfter.Generation)
	}
	loaded, err := appregistry.LoadPublishedState(mem, testPublishStorageRoot, "g-issues", declaration.Manifest.Version)
	if err != nil {
		t.Fatalf("LoadPublishedState: %v", err)
	}
	if loaded.State != appregistry.PublishedLoadVerified {
		t.Fatalf("published state = %v, want verified", loaded.State)
	}
}

func TestStatelessPublishRejectsMissingBuilderVersionBeforeSigning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newStatelessPublishHarness(t)
	declaration, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.7")
	declaration.BuilderVersion = ""
	_, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
	})
	if !errors.Is(err, appregistry.ErrPublishDeclarationInvalid) {
		t.Fatalf("Begin error = %v, want declaration invalid", err)
	}
}

func TestStatelessPublishEntryStableAcrossServiceInstances(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mem := appregistry.NewMemoryObjectStore()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	limits := appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}}
	newService := func() *appregistry.StatelessPublishService {
		return &appregistry.StatelessPublishService{
			Registry: "toolshed", StorageRoot: testPublishStorageRoot, PublicRoot: testPublishPublicRoot,
			Store: mem, Signer: signer, Writer: &appregistry.Writer{Store: mem}, Limits: limits,
		}
	}
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", "0.3.0-dev.8")
	declaration.BuilderVersion = "client-builder-1.2.3"

	serviceA := newService()
	serviceB := newService()
	beginA, err := serviceA.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Begin A: %v", err)
	}
	beginB, err := serviceB.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Begin B: %v", err)
	}
	if beginA.PublishID != beginB.PublishID {
		t.Fatalf("publish ids differ: %q vs %q", beginA.PublishID, beginB.PublishID)
	}
	if err := appregistry.ApplyMemoryUpload(mem, beginA.Uploads[0].UploadURL, artifactBytes, declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}
	finalA, err := serviceA.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", PublishID: beginA.PublishID, Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Finalize A: %v", err)
	}
	finalB, err := serviceB.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", PublishID: beginB.PublishID, Declaration: declaration,
	})
	if err != nil {
		t.Fatalf("Finalize B: %v", err)
	}
	if finalA.PublishID != finalB.PublishID {
		t.Fatalf("final publish ids differ: %q vs %q", finalA.PublishID, finalB.PublishID)
	}
	loaded, err := appregistry.LoadPublishedState(mem, testPublishStorageRoot, "g-issues", "0.3.0-dev.8")
	if err != nil {
		t.Fatalf("LoadPublishedState: %v", err)
	}
	if loaded.Entry.BuilderVersion != "client-builder-1.2.3" {
		t.Fatalf("entry builderVersion = %q, want client-builder-1.2.3", loaded.Entry.BuilderVersion)
	}
}

type promoteRecorder struct {
	*appregistry.MemoryObjectStore
	promotions []appregistry.PromoteObjectInput
}

func (r *promoteRecorder) PromoteObject(input appregistry.PromoteObjectInput) error {
	r.promotions = append(r.promotions, input)
	return r.MemoryObjectStore.PromoteObject(input)
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

func testPublishDeclarationMultiPlatform(t *testing.T, appName, version string) (*appregistry.PublishDeclaration, [][]byte) {
	t.Helper()
	artifactBytesA := []byte("artifact-a-" + version)
	sumA := sha256.Sum256(artifactBytesA)
	digestA := hex.EncodeToString(sumA[:])
	artifactBytesB := []byte("artifact-b-" + version)
	sumB := sha256.Sum256(artifactBytesB)
	digestB := hex.EncodeToString(sumB[:])
	release := &providerrelease.Metadata{
		Schema:        providerrelease.SchemaName,
		SchemaVersion: providerrelease.SchemaVersion,
		Package:       "github.com/valon-technologies/valon-tools/apps/" + appName,
		Kind:          providermanifestv1.KindApp,
		Version:       version,
		Runtime:       providerrelease.RuntimeExecutable,
		Artifacts: providerrelease.Artifacts{
			"linux/amd64":  {Path: "linux-amd64.tar.gz", SHA256: digestA},
			"darwin/arm64": {Path: "darwin-arm64.tar.gz", SHA256: digestB},
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
		BuilderVersion:  "0.0.1-test-builder",
		Artifacts: []appregistry.PublishDeclarationArtifact{
			{Platform: "linux/amd64", Filename: "linux-amd64.tar.gz", SHA256: digestA, Size: int64(len(artifactBytesA))},
			{Platform: "darwin/arm64", Filename: "darwin-arm64.tar.gz", SHA256: digestB, Size: int64(len(artifactBytesB))},
		},
	}, [][]byte{artifactBytesA, artifactBytesB}
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
		BuilderVersion:  "0.0.1-test-builder",
		Artifacts: []appregistry.PublishDeclarationArtifact{{
			Platform: "linux/amd64", Filename: "linux-amd64.tar.gz", SHA256: digest, Size: int64(len(artifactBytes)),
		}},
	}, artifactBytes
}

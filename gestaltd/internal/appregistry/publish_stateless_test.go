package appregistry_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	first, declaration := beginUploadedPublish(t, ctx, service, mem, "0.3.0-dev.1")
	if first.State != appregistry.PublishStateUploading || len(first.Uploads) != 1 {
		t.Fatalf("first begin = %#v", first)
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
	declB, _ := testPublishDeclaration(t, "g-issues", "0.3.0-dev.2")
	declB.Artifacts[0].SHA256 = strings.Repeat("b", 64)
	declB.ReleaseMetadata.Artifacts["linux/amd64"] = providerrelease.Artifact{
		Path: "linux-amd64.tar.gz", SHA256: strings.Repeat("b", 64),
	}

	beginA, declA := beginUploadedPublish(t, ctx, service, mem, "0.3.0-dev.2")
	if _, err := service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
		App: "g-issues", PublishID: beginA.PublishID, Declaration: declA,
	}); err != nil {
		t.Fatalf("Finalize A: %v", err)
	}

	_, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{
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
	service, mem, _ := newStatelessPublishHarnessWithNow(t, func() time.Time {
		return time.Now().UTC().Add(time.Duration(tick.Add(1)) * time.Second)
	}, false)
	begin, declaration := beginUploadedPublish(t, ctx, service, mem, "0.3.0-dev.3")

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
	begin, declaration := beginUploadedPublish(t, ctx, service, mem, "0.3.0-dev.6")

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

func TestStatelessPublishFinalizePromotionOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("orphan entry conflict skips promotion", func(t *testing.T) {
		t.Parallel()
		service, mem, recorder := newStatelessPublishHarnessWithRecorder(t)
		begin, declaration := beginUploadedPublish(t, ctx, service, mem, "0.3.0-dev.12")
		urls := testPublishArtifactURLs(t, declaration)
		seedConflictingOrphanEntry(t, mem, urls, declaration, begin.PublishID)

		_, err := service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
			App: "g-issues", PublishID: begin.PublishID, Declaration: declaration,
		})
		if !errors.Is(err, appregistry.ErrRegistryEntryConflict) {
			t.Fatalf("Finalize error = %v, want ErrRegistryEntryConflict", err)
		}
		if len(recorder.promotions) != 0 {
			t.Fatalf("PromoteObject calls = %#v, want none", recorder.promotions)
		}
		described, err := mem.DescribeObject(urls.finalURL)
		if err != nil {
			t.Fatalf("DescribeObject(final): %v", err)
		}
		if described.Generation != 0 {
			t.Fatalf("final artifact created before preflight: %#v", described)
		}
	})

	t.Run("matching pre-promoted artifacts resume", func(t *testing.T) {
		t.Parallel()
		service, mem, recorder := newStatelessPublishHarnessWithRecorder(t)
		begin, declaration := beginUploadedPublish(t, ctx, service, mem, "0.3.0-dev.13")
		urls := testPublishArtifactURLs(t, declaration)
		before := prePromoteStagingArtifact(t, mem, urls, declaration)
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
		after, err := mem.DescribeObject(urls.finalURL)
		if err != nil {
			t.Fatalf("DescribeObject(final): %v", err)
		}
		if after.Generation != before.Generation {
			t.Fatalf("final artifact generation changed: before=%d after=%d", before.Generation, after.Generation)
		}
		loaded, err := appregistry.LoadPublishedState(mem, testPublishStorageRoot, "g-issues", declaration.Manifest.Version)
		if err != nil {
			t.Fatalf("LoadPublishedState: %v", err)
		}
		if loaded.State != appregistry.PublishedLoadVerified {
			t.Fatalf("published state = %v, want verified", loaded.State)
		}
	})
}

func TestStatelessPublishMultiArtifactPromotion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	multiLimits := appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64", "darwin/arm64"}}

	cases := []struct {
		name, version    string
		uploaded         []string
		mismatchPlatform string
		genDriftPlatform string
		wantErr          error
		wantPromotions   int
		wantPublished    bool
		wantFinals       map[string]bool
	}{
		{
			name: "second missing", version: "0.3.0-dev.20", uploaded: []string{"darwin/arm64"},
			wantErr: appregistry.ErrPublishUploadMissing,
		},
		{
			name: "second mismatched", version: "0.3.0-dev.21", uploaded: []string{"linux/amd64", "darwin/arm64"},
			mismatchPlatform: "linux/amd64", wantErr: appregistry.ErrPublishUploadMismatch,
		},
		{
			name: "all valid", version: "0.3.0-dev.22", uploaded: []string{"linux/amd64", "darwin/arm64"},
			wantPromotions: 2, wantPublished: true,
			wantFinals: map[string]bool{"darwin/arm64": true, "linux/amd64": true},
		},
		{
			name: "generation change after snapshot", version: "0.3.0-dev.23", uploaded: []string{"linux/amd64", "darwin/arm64"},
			genDriftPlatform: "linux/amd64", wantErr: appregistry.ErrPublishUploadMismatch, wantPromotions: 2,
			wantFinals: map[string]bool{"darwin/arm64": true, "linux/amd64": false},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service, mem, store, begin, declaration, urls := setupMultiArtifactPublish(t, ctx, multiLimits, tc.version, tc.uploaded, tc.mismatchPlatform, tc.genDriftPlatform)
			resp, err := service.Finalize(ctx, "toolshed", appregistry.AdminPublishInput{
				App: "g-issues", PublishID: begin.PublishID, Declaration: declaration,
			})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Finalize error = %v, want %v", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Finalize: %v", err)
			}
			if len(store.promotions) != tc.wantPromotions {
				t.Fatalf("PromoteObject calls = %d, want %d", len(store.promotions), tc.wantPromotions)
			}
			if tc.wantPublished && resp.State != appregistry.PublishStatePublished {
				t.Fatalf("finalize = %#v", resp)
			}
			if tc.wantPromotions == 2 && tc.genDriftPlatform == "" {
				if store.promotions[0].SourceURL != urls["darwin/arm64"].staging || store.promotions[1].SourceURL != urls["linux/amd64"].staging {
					t.Fatalf("promotion order = %#v, want darwin/arm64 then linux/amd64", store.promotions)
				}
			}
			for platform, paths := range urls {
				wantFinal := tc.wantFinals[platform]
				described, err := mem.DescribeObject(paths.final)
				if err != nil {
					t.Fatalf("DescribeObject(%s final): %v", platform, err)
				}
				if hasFinal := described.Generation != 0; hasFinal != wantFinal {
					t.Fatalf("%s final exists = %v, want %v (%#v)", platform, hasFinal, wantFinal, described)
				}
			}
		})
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

type multiArtifactStore struct {
	appregistry.RegistryObjectStoreWithPromoter
	promotions                            []appregistry.PromoteObjectInput
	mismatchURL, mismatchSHA, genDriftURL string
	promoteCalls                          int
}

func (s *multiArtifactStore) DescribeObject(storageURL string) (appregistry.ObjectDescription, error) {
	described, err := s.RegistryObjectStoreWithPromoter.DescribeObject(storageURL)
	if err == nil && storageURL == s.mismatchURL {
		described.SHA256 = s.mismatchSHA
	}
	return described, err
}

func (s *multiArtifactStore) PromoteObject(input appregistry.PromoteObjectInput) error {
	s.promoteCalls++
	s.promotions = append(s.promotions, input)
	if s.genDriftURL != "" && s.promoteCalls == 2 && input.SourceURL == s.genDriftURL {
		return fmt.Errorf("%w: %s generation %d != %d", appregistry.ErrPublishUploadMismatch, input.SourceURL, input.SourceGeneration+1, input.SourceGeneration)
	}
	return s.RegistryObjectStoreWithPromoter.PromoteObject(input)
}

type promoteRecorder struct {
	*appregistry.MemoryObjectStore
	promotions []appregistry.PromoteObjectInput
}

func (r *promoteRecorder) PromoteObject(input appregistry.PromoteObjectInput) error {
	r.promotions = append(r.promotions, input)
	return r.MemoryObjectStore.PromoteObject(input)
}

type multiArtifactPaths struct {
	staging, final string
}

func setupMultiArtifactPublish(
	t *testing.T, ctx context.Context, limits appregistry.PublishLimits, version string, uploaded []string, mismatchPlatform, genDriftPlatform string,
) (*appregistry.StatelessPublishService, *appregistry.MemoryObjectStore, *multiArtifactStore, *appregistry.AdminPublishResponse, *appregistry.PublishDeclaration, map[string]multiArtifactPaths) {
	t.Helper()
	mem := appregistry.NewMemoryObjectStore()
	declaration, artifactBytes := testPublishDeclarationMultiPlatform(t, "g-issues", version)
	urls := multiArtifactURLMap(t, declaration)
	store := &multiArtifactStore{RegistryObjectStoreWithPromoter: mem, mismatchSHA: strings.Repeat("c", 64)}
	if mismatchPlatform != "" {
		store.mismatchURL = urls[mismatchPlatform].staging
	}
	if genDriftPlatform != "" {
		store.genDriftURL = urls[genDriftPlatform].staging
	}
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	service := &appregistry.StatelessPublishService{
		Registry: "toolshed", StorageRoot: testPublishStorageRoot, PublicRoot: testPublishPublicRoot,
		Store: store, Signer: signer, Writer: &appregistry.Writer{Store: store}, Limits: limits,
	}
	begin, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{App: "g-issues", Declaration: declaration})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	uploads := map[string]appregistry.AdminPublishUpload{}
	for _, upload := range begin.Uploads {
		uploads[upload.Platform] = upload
	}
	for _, platform := range uploaded {
		for i, artifact := range declaration.Artifacts {
			if artifact.Platform != platform {
				continue
			}
			upload, ok := uploads[platform]
			if !ok {
				t.Fatalf("missing upload URL for platform %q", platform)
			}
			if err := appregistry.ApplyMemoryUpload(mem, upload.UploadURL, artifactBytes[i], artifact.SHA256); err != nil {
				t.Fatalf("upload %s: %v", platform, err)
			}
		}
	}
	return service, mem, store, begin, declaration, urls
}

func multiArtifactURLMap(t *testing.T, declaration *appregistry.PublishDeclaration) map[string]multiArtifactPaths {
	t.Helper()
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
	urls := make(map[string]multiArtifactPaths, len(declaration.Artifacts))
	for _, artifact := range declaration.Artifacts {
		stagingPath, err := appregistry.PublishStagingArtifactPath(stagingPrefix, artifact.Platform, artifact.Filename)
		if err != nil {
			t.Fatalf("PublishStagingArtifactPath: %v", err)
		}
		finalRel, err := appregistry.PublishArtifactFinalRel(layout.ArtifactPrefix, artifact.Filename)
		if err != nil {
			t.Fatalf("PublishArtifactFinalRel: %v", err)
		}
		urls[artifact.Platform] = multiArtifactPaths{
			staging: appregistry.StorageURL(testPublishStorageRoot, stagingPath),
			final:   appregistry.StorageURL(testPublishStorageRoot, finalRel),
		}
	}
	return urls
}

type publishArtifactURLs struct {
	entryURL, stagingURL, finalURL, finalRel, digest string
}

func writeImmutableJSON(t *testing.T, mem *appregistry.MemoryObjectStore, storageURL, sourceRef, prefix string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	path, err := appregistry.WriteTempJSON(prefix, append(data, '\n'))
	if err != nil {
		t.Fatalf("WriteTempJSON: %v", err)
	}
	defer func() { _ = os.Remove(path) }()
	if err := mem.WriteImmutableObject(appregistry.WriteImmutableObjectInput{
		LocalPath: path, StorageURL: storageURL, SourceRef: sourceRef,
	}); err != nil {
		t.Fatalf("WriteImmutableObject(%s): %v", storageURL, err)
	}
}

func beginUploadedPublish(t *testing.T, ctx context.Context, service *appregistry.StatelessPublishService, mem *appregistry.MemoryObjectStore, version string) (*appregistry.AdminPublishResponse, *appregistry.PublishDeclaration) {
	t.Helper()
	declaration, artifactBytes := testPublishDeclaration(t, "g-issues", version)
	begin, err := service.Begin(ctx, "toolshed", appregistry.AdminPublishInput{App: "g-issues", Declaration: declaration})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := appregistry.ApplyMemoryUpload(mem, begin.Uploads[0].UploadURL, artifactBytes, declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}
	return begin, declaration
}

func testPublishArtifactURLs(t *testing.T, declaration *appregistry.PublishDeclaration) publishArtifactURLs {
	t.Helper()
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
	artifact := declaration.Artifacts[0]
	stagingPath, err := appregistry.PublishStagingArtifactPath(stagingPrefix, artifact.Platform, artifact.Filename)
	if err != nil {
		t.Fatalf("PublishStagingArtifactPath: %v", err)
	}
	finalRel, err := appregistry.PublishArtifactFinalRel(layout.ArtifactPrefix, artifact.Filename)
	if err != nil {
		t.Fatalf("PublishArtifactFinalRel: %v", err)
	}
	return publishArtifactURLs{
		entryURL:   appregistry.StorageURL(testPublishStorageRoot, layout.EntryPath),
		stagingURL: appregistry.StorageURL(testPublishStorageRoot, stagingPath),
		finalURL:   appregistry.StorageURL(testPublishStorageRoot, finalRel),
		finalRel:   finalRel,
		digest:     digest,
	}
}

func seedConflictingOrphanEntry(t *testing.T, mem *appregistry.MemoryObjectStore, urls publishArtifactURLs, declaration *appregistry.PublishDeclaration, publishID string) {
	t.Helper()
	const conflictingSourceRef = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	artifact := declaration.Artifacts[0]
	entry, err := appregistry.BuildEntry(appregistry.BuildEntryInput{
		Manifest: declaration.Manifest, Version: declaration.Manifest.Version,
		SourceRef: conflictingSourceRef, ManifestPath: declaration.ManifestPath,
		PublicationKind: declaration.PublicationKind, PublishID: publishID,
		BuilderVersion: declaration.BuilderVersion, DeclarationDigest: urls.digest,
		LocalSource: declaration.LocalSource, Release: declaration.ReleaseMetadata,
		Artifacts: []appregistry.PublishArtifact{{
			Target: artifact.Platform, Filename: artifact.Filename,
			StorageURL: urls.finalURL, PublicURL: appregistry.PublicURL(testPublishPublicRoot, urls.finalRel),
			SHA256: artifact.SHA256,
		}},
		PublishedAt: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildEntry: %v", err)
	}
	writeImmutableJSON(t, mem, urls.entryURL, conflictingSourceRef, "gestalt-orphan-entry-*", entry)
}

func prePromoteStagingArtifact(t *testing.T, mem *appregistry.MemoryObjectStore, urls publishArtifactURLs, declaration *appregistry.PublishDeclaration) appregistry.ObjectDescription {
	t.Helper()
	staging, err := mem.DescribeObject(urls.stagingURL)
	if err != nil {
		t.Fatalf("DescribeObject(staging): %v", err)
	}
	if err := mem.PromoteObject(appregistry.PromoteObjectInput{
		SourceURL: urls.stagingURL, SourceGeneration: staging.Generation,
		DestURL: urls.finalURL, ExpectedSHA256: declaration.Artifacts[0].SHA256,
		SourceRef: declaration.LocalSource.CommitSHA,
	}); err != nil {
		t.Fatalf("pre-promote artifact: %v", err)
	}
	described, err := mem.DescribeObject(urls.finalURL)
	if err != nil {
		t.Fatalf("DescribeObject(final): %v", err)
	}
	return described
}

func newStatelessPublishHarnessWithRecorder(t *testing.T) (*appregistry.StatelessPublishService, *appregistry.MemoryObjectStore, *promoteRecorder) {
	t.Helper()
	return newStatelessPublishHarnessWithNow(t, func() time.Time { return time.Now().UTC() }, true)
}

func newStatelessPublishHarness(t *testing.T) (*appregistry.StatelessPublishService, *appregistry.MemoryObjectStore) {
	t.Helper()
	service, mem, _ := newStatelessPublishHarnessWithNow(t, func() time.Time { return time.Now().UTC() }, false)
	return service, mem
}

func newStatelessPublishHarnessWithNow(t *testing.T, now func() time.Time, recordPromotions bool) (*appregistry.StatelessPublishService, *appregistry.MemoryObjectStore, *promoteRecorder) {
	t.Helper()
	mem := appregistry.NewMemoryObjectStore()
	store := appregistry.RegistryObjectStoreWithPromoter(mem)
	var recorder *promoteRecorder
	if recordPromotions {
		recorder = &promoteRecorder{MemoryObjectStore: mem}
		store = recorder
	}
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	limits := appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}}
	return &appregistry.StatelessPublishService{
		Registry: "toolshed", StorageRoot: testPublishStorageRoot, PublicRoot: testPublishPublicRoot,
		Store: store, Signer: signer, Writer: &appregistry.Writer{Store: store}, Limits: limits, Now: now,
	}, mem, recorder
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

package appregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestValidatePublishInput_RequiresSourceRefForGitHub(t *testing.T) {
	t.Parallel()

	manifest := testPublishManifest(t)
	if err := ValidatePublishInputWithOptions(manifest, "0.0.1", "", PublishValidationOptions{
		PublicationKind: PublicationKindGitHub,
	}); err == nil {
		t.Fatal("expected github publish to require sourceRef")
	}
}

func TestValidatePublishInput_AllowsMissingSourceRefForLocal(t *testing.T) {
	t.Parallel()

	manifest := testPublishManifest(t)
	if err := ValidatePublishInputWithOptions(manifest, "0.0.1", "", PublishValidationOptions{
		PublicationKind: PublicationKindLocal,
	}); err != nil {
		t.Fatalf("ValidatePublishInputWithOptions() = %v", err)
	}
}

func TestDecodeEntry_OldMetadataWithoutNewFields(t *testing.T) {
	t.Parallel()

	const legacy = `{
  "schemaVersion": 1,
  "app": "traffic-cop",
  "version": "0.0.1",
  "sourceRef": "651a5c30feb995c9364c38f63d0d5c3880bc2055",
  "manifestPath": "apps/traffic-cop/manifest.yaml",
  "repository": "github.com/valon-technologies/valon-tools",
  "publishedAt": "2026-07-24T19:04:32Z",
  "artifacts": {
    "linux/amd64": {
      "url": "https://example.com/artifact.tar.gz",
      "publicUrl": "https://example.com/artifact.tar.gz",
      "sha256": "deadbeef"
    }
  }
}`
	entry, err := DecodeEntry([]byte(legacy))
	if err != nil {
		t.Fatalf("DecodeEntry() = %v", err)
	}
	if entry.PublicationKind != "" || entry.PublishID != "" || entry.LocalSource != nil {
		t.Fatalf("legacy entry decoded unexpected new fields: %#v", entry)
	}
}

func TestDecodeEntry_LocalPublicationWithoutSourceRef(t *testing.T) {
	t.Parallel()

	entry := testPublishEntry(t)
	entry.SourceRef = ""
	entry.PublicationKind = PublicationKindLocal
	entry.LocalSource = &LocalSourceState{CommitSHA: "651a5c30feb995c9364c38f63d0d5c3880bc2055", Dirty: true}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	decoded, err := DecodeEntry(data)
	if err != nil {
		t.Fatalf("DecodeEntry() = %v", err)
	}
	if decoded.SourceRef != "" || decoded.PublicationKind != PublicationKindLocal || decoded.LocalSource == nil {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestWriter_Publish_IdempotentRetry(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	writer := &Writer{Store: store}
	manifest, entryPath, artifactPath := writePublishManifestFixture(t, store, "0.0.1")
	req := PublishRequest{Manifest: manifest, SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055"}

	result, err := writer.Publish(req, PublishProgress{})
	if err != nil {
		t.Fatalf("first Publish() = %v", err)
	}
	if result.Entry.Outcome != ObjectWriteOutcomeUploaded {
		t.Fatalf("first entry outcome = %q, want uploaded", result.Entry.Outcome)
	}
	if result.Retention != CatalogWriteOutcomeUpdated {
		t.Fatalf("first retention outcome = %q, want updated", result.Retention)
	}
	if result.Index != CatalogWriteOutcomeUpdated {
		t.Fatalf("first index outcome = %q, want updated", result.Index)
	}
	result, err = writer.Publish(req, PublishProgress{})
	if err != nil {
		t.Fatalf("retry Publish() = %v", err)
	}
	if result.Entry.Outcome != ObjectWriteOutcomeSkipped {
		t.Fatalf("retry entry outcome = %q, want skipped", result.Entry.Outcome)
	}
	if result.Retention != CatalogWriteOutcomeUnchanged {
		t.Fatalf("retry retention outcome = %q, want unchanged", result.Retention)
	}
	if result.Index != CatalogWriteOutcomeUnchanged {
		t.Fatalf("retry index outcome = %q, want unchanged", result.Index)
	}

	described, err := store.DescribeObject(manifest.EntryObject.StorageURL)
	if err != nil || described.Generation == 0 {
		t.Fatalf("entry object missing: %#v err=%v", described, err)
	}
	_, indexData, err := store.ReadObject(manifest.IndexObject.StorageURL)
	if err != nil {
		t.Fatalf("ReadObject(index): %v", err)
	}
	index, err := DecodeIndex(indexData)
	if err != nil {
		t.Fatalf("DecodeIndex(): %v", err)
	}
	if len(index.Apps[manifest.AppName].Versions) != 1 {
		t.Fatalf("index versions = %#v", index.Apps[manifest.AppName].Versions)
	}
	_ = entryPath
	_ = artifactPath
}

func TestWriter_Preflight_RejectsConflictingBytes(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	manifest, _, artifactPath := writePublishManifestFixture(t, store, "0.0.1")
	if err := store.WriteImmutableObject(WriteImmutableObjectInput{
		LocalPath:  artifactPath,
		StorageURL: manifest.ArtifactObjects[0].StorageURL,
		SourceRef:  "651a5c30feb995c9364c38f63d0d5c3880bc2055",
		SHA256:     "different",
	}); err != nil {
		t.Fatalf("seed conflicting artifact: %v", err)
	}
	writer := &Writer{Store: store}
	err := writer.Preflight(PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{})
	if err == nil {
		t.Fatal("expected conflicting artifact preflight to fail")
	}
}

func TestWriter_Publish_IndexCASPreservesConcurrentVersions(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	manifest, _, _ := writePublishManifestFixture(t, store, "0.0.2")

	existing := NewEmptyIndex()
	otherEntry := testPublishEntry(t)
	otherEntry.Version = "0.0.1"
	updated, changed, err := UpsertAppIndex(existing, otherEntry, AppVersionEntryPath(manifest.AppName, "0.0.1"), "", "")
	if err != nil || !changed {
		t.Fatalf("UpsertAppIndex(existing): changed=%v err=%v", changed, err)
	}
	indexData, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	indexPath, err := WriteTempJSON("gestalt-app-index-seed-*", append(indexData, '\n'))
	if err != nil {
		t.Fatalf("WriteTempJSON(): %v", err)
	}
	if err := store.WriteCatalogObject(WriteCatalogObjectInput{
		LocalPath:  indexPath,
		StorageURL: manifest.IndexObject.StorageURL,
		SourceRef:  "651a5c30feb995c9364c38f63d0d5c3880bc2055",
		Generation: 0,
	}); err != nil {
		t.Fatalf("seed index: %v", err)
	}

	failStore := &catalogConflictStore{
		RegistryObjectStore: store,
		failURL:             manifest.IndexObject.StorageURL,
		failRemaining:       1,
	}
	writer := &Writer{Store: failStore}
	if _, err := writer.Publish(PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{}); err != nil {
		t.Fatalf("Publish() = %v", err)
	}

	_, indexData, err = store.ReadObject(manifest.IndexObject.StorageURL)
	if err != nil {
		t.Fatalf("ReadObject(index): %v", err)
	}
	index, err := DecodeIndex(indexData)
	if err != nil {
		t.Fatalf("DecodeIndex(): %v", err)
	}
	versions := index.Apps[manifest.AppName].Versions
	if len(versions) != 2 {
		t.Fatalf("index versions = %#v, want 2", versions)
	}
	if _, ok := versions["0.0.1"]; !ok {
		t.Fatalf("missing concurrent version 0.0.1: %#v", versions)
	}
	if _, ok := versions["0.0.2"]; !ok {
		t.Fatalf("missing published version 0.0.2: %#v", versions)
	}
}

type catalogConflictStore struct {
	RegistryObjectStore
	failURL       string
	failRemaining int
}

func (s *catalogConflictStore) WriteCatalogObject(input WriteCatalogObjectInput) error {
	if s.failRemaining > 0 && input.StorageURL == s.failURL {
		s.failRemaining--
		return fmt.Errorf("%w: simulated index conflict", ErrObjectPreconditionFailed)
	}
	return s.RegistryObjectStore.WriteCatalogObject(input)
}

func writePublishManifestFixture(t *testing.T, store RegistryObjectStore, version string) (PublishManifest, string, string) {
	t.Helper()

	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "linux-amd64.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact-bytes"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	manifest := testPublishManifest(t)
	release := &providerrelease.Metadata{
		StaticValidation: &providerrelease.StaticValidation{
			Catalog: &catalog.Catalog{Name: "provider"},
		},
	}
	entryInput := BuildEntryInput{
		Manifest:         manifest,
		Version:          version,
		SourceRef:        "651a5c30feb995c9364c38f63d0d5c3880bc2055",
		ManifestPath:     "apps/traffic-cop/manifest.yaml",
		PublicationKind:  PublicationKindGitHub,
		Release:          release,
		PublishStartedAt: time.Date(2026, 7, 24, 19, 0, 0, 0, time.UTC),
	}
	plan, err := BuildPublishManifest(BuildPublishManifestInput{
		StorageRoot: "gs://gestalt-app-registry",
		PublicRoot:  "https://storage.googleapis.com/gestalt-app-registry",
		EntryInput:  entryInput,
		LocalArtifacts: []LocalPublishArtifact{{
			Target:    "linux/amd64",
			LocalPath: artifactPath,
		}},
	})
	if err != nil {
		t.Fatalf("BuildPublishManifest(): %v", err)
	}
	_ = store
	return plan, plan.EntryObject.LocalPath, artifactPath
}

func testPublishManifest(t *testing.T) *providermanifestv1.Manifest {
	t.Helper()
	return &providermanifestv1.Manifest{
		Kind:   providermanifestv1.KindApp,
		Source: "github.com/valon-technologies/valon-tools/apps/traffic-cop",
	}
}

func testPublishEntry(t *testing.T) Entry {
	t.Helper()
	return Entry{
		SchemaVersion: EntrySchemaVersion,
		App:           "traffic-cop",
		Version:       "0.0.1",
		SourceRef:     "651a5c30feb995c9364c38f63d0d5c3880bc2055",
		ManifestPath:  "apps/traffic-cop/manifest.yaml",
		Repository:    "github.com/valon-technologies/valon-tools",
		PublishedAt:   time.Date(2026, 7, 24, 19, 4, 32, 0, time.UTC),
		Artifacts: map[string]Artifact{
			"linux/amd64": {
				URL:       "https://example.com/artifact.tar.gz",
				PublicURL: "https://example.com/artifact.tar.gz",
				SHA256:    "deadbeef",
			},
		},
	}
}

func TestWriter_NeverDeletesObjects(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	writer := &Writer{Store: store}
	manifest, _, _ := writePublishManifestFixture(t, store, "0.0.1")
	if _, err := writer.Publish(PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{}); err != nil {
		t.Fatalf("Publish() = %v", err)
	}
	if _, ok := store.objects[manifest.EntryObject.StorageURL]; !ok {
		t.Fatal("writer removed entry object")
	}
}

func TestGcloudPreconditionFailed(t *testing.T) {
	t.Parallel()

	if !gcloudPreconditionFailed(errors.New("412 Precondition Failed")) {
		t.Fatal("expected gcloud precondition detection")
	}
	if !isObjectGenerationPreconditionFailed(fmt.Errorf("wrap: %w", ErrObjectPreconditionFailed)) {
		t.Fatal("expected memory store precondition detection")
	}
	if !isObjectGenerationPreconditionFailed(errors.New("412 Precondition Failed")) {
		t.Fatal("expected raw gcloud precondition detection")
	}
}

package appregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

type catalogWriteRecorder struct {
	RegistryObjectStore
	writes []string
}

func (s *catalogWriteRecorder) WriteCatalogObject(input WriteCatalogObjectInput) error {
	s.writes = append(s.writes, input.StorageURL)
	return s.RegistryObjectStore.WriteCatalogObject(input)
}

type retentionFailStore struct {
	RegistryObjectStore
	retentionURL string
}

func (s *retentionFailStore) WriteCatalogObject(input WriteCatalogObjectInput) error {
	if input.StorageURL == s.retentionURL {
		return fmt.Errorf("%w: simulated retention failure", ErrObjectPreconditionFailed)
	}
	return s.RegistryObjectStore.WriteCatalogObject(input)
}

func TestWriter_Publish_CommitsRetentionBeforeIndex(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	manifest, _, _ := writePublishManifestFixture(t, store, "0.0.3")
	recorder := &catalogWriteRecorder{RegistryObjectStore: store}
	writer := &Writer{Store: recorder}
	req := PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}

	result, err := writer.Publish(req, PublishProgress{})
	if err != nil {
		t.Fatalf("Publish() = %v", err)
	}
	if result.Retention != CatalogWriteOutcomeUpdated {
		t.Fatalf("retention outcome = %q, want updated", result.Retention)
	}
	if result.Index != CatalogWriteOutcomeUpdated {
		t.Fatalf("index outcome = %q, want updated", result.Index)
	}
	if len(recorder.writes) != 2 {
		t.Fatalf("catalog writes = %#v, want 2", recorder.writes)
	}
	retentionURL := RetentionStorageURL(manifest.IndexObject.StorageURL, manifest.AppName)
	if recorder.writes[0] != retentionURL {
		t.Fatalf("first catalog write = %q, want retention %q", recorder.writes[0], retentionURL)
	}
	if recorder.writes[1] != manifest.IndexObject.StorageURL {
		t.Fatalf("second catalog write = %q, want index %q", recorder.writes[1], manifest.IndexObject.StorageURL)
	}
}

func TestWriter_Publish_RetentionFailureLeavesIndexUnchanged(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	manifest, _, _ := writePublishManifestFixture(t, store, "0.0.4")
	retentionURL := RetentionStorageURL(manifest.IndexObject.StorageURL, manifest.AppName)
	failStore := &retentionFailStore{
		RegistryObjectStore: store,
		retentionURL:        retentionURL,
	}
	writer := &Writer{Store: failStore}
	req := PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}

	result, err := writer.Publish(req, PublishProgress{})
	if err == nil {
		t.Fatal("expected retention failure")
	}
	if result.Retention != CatalogWriteOutcomeNotAttempted {
		t.Fatalf("retention outcome = %q, want not_attempted", result.Retention)
	}
	if result.Index != CatalogWriteOutcomeNotAttempted {
		t.Fatalf("index outcome = %q, want not_attempted", result.Index)
	}
	if result.Entry.Outcome != ObjectWriteOutcomeUploaded {
		t.Fatalf("entry outcome = %q, want uploaded", result.Entry.Outcome)
	}

	_, indexData, err := store.ReadObject(manifest.IndexObject.StorageURL)
	if err != nil {
		t.Fatalf("ReadObject(index): %v", err)
	}
	if len(indexData) != 0 {
		t.Fatalf("index changed after retention failure: %s", string(indexData))
	}
}

func TestWriter_Publish_UsesEntryPublishedAtForIndexAndRetention(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	manifest, _, _ := writePublishManifestFixture(t, store, "0.0.5")
	publishedAt := time.Date(2026, 8, 17, 12, 34, 56, 0, time.UTC)
	manifest.Entry.PublishedAt = publishedAt
	entryData, err := json.MarshalIndent(manifest.Entry, "", "  ")
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	if err := os.WriteFile(manifest.EntryObject.LocalPath, append(entryData, '\n'), 0o644); err != nil {
		t.Fatalf("rewrite entry file: %v", err)
	}

	writer := &Writer{Store: store}
	result, err := writer.Publish(PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{})
	if err != nil {
		t.Fatalf("Publish() = %v", err)
	}
	if result.Index != CatalogWriteOutcomeUpdated || result.Retention != CatalogWriteOutcomeUpdated {
		t.Fatalf("result = %#v", result)
	}

	retentionURL := RetentionStorageURL(manifest.IndexObject.StorageURL, manifest.AppName)
	_, retentionData, err := store.ReadObject(retentionURL)
	if err != nil {
		t.Fatalf("ReadObject(retention): %v", err)
	}
	retention, err := DecodeRetentionIndex(retentionData)
	if err != nil {
		t.Fatalf("DecodeRetentionIndex(): %v", err)
	}
	retentionVersion, ok := retention.Versions["0.0.5"]
	if !ok {
		t.Fatalf("retention versions = %#v", retention.Versions)
	}
	if !retentionVersion.PublishedAt.Equal(publishedAt) {
		t.Fatalf("retention publishedAt = %v, want %v", retentionVersion.PublishedAt, publishedAt)
	}

	_, indexData, err := store.ReadObject(manifest.IndexObject.StorageURL)
	if err != nil {
		t.Fatalf("ReadObject(index): %v", err)
	}
	index, err := DecodeIndex(indexData)
	if err != nil {
		t.Fatalf("DecodeIndex(): %v", err)
	}
	indexVersion, ok := index.Apps[manifest.AppName].Versions["0.0.5"]
	if !ok {
		t.Fatalf("index versions = %#v", index.Apps[manifest.AppName].Versions)
	}
	if !indexVersion.PublishedAt.Equal(publishedAt) {
		t.Fatalf("index publishedAt = %v, want %v", indexVersion.PublishedAt, publishedAt)
	}
}

func TestWriter_Publish_ReturnsTypedOutcomeOnPartialFailure(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	manifest, _, _ := writePublishManifestFixture(t, store, "0.0.6")
	indexPath := manifest.IndexObject.StorageURL
	failStore := &catalogConflictStore{
		RegistryObjectStore: store,
		failURL:             indexPath,
		failRemaining:       1,
	}
	writer := &Writer{Store: failStore, CatalogAttempts: 1}
	result, err := writer.Publish(PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{})
	if err == nil {
		t.Fatal("expected index failure")
	}
	if result.Retention != CatalogWriteOutcomeUpdated {
		t.Fatalf("retention outcome = %q, want updated", result.Retention)
	}
	if result.Index != CatalogWriteOutcomeNotAttempted {
		t.Fatalf("index outcome = %q, want not_attempted", result.Index)
	}

	retentionURL := RetentionStorageURL(indexPath, manifest.AppName)
	_, retentionData, err := store.ReadObject(retentionURL)
	if err != nil {
		t.Fatalf("ReadObject(retention): %v", err)
	}
	var retention RetentionIndex
	if err := json.Unmarshal(retentionData, &retention); err != nil {
		t.Fatalf("unmarshal retention: %v", err)
	}
	if _, ok := retention.Versions["0.0.6"]; !ok {
		t.Fatalf("retention missing version after index failure: %#v", retention.Versions)
	}
	_, indexData, err := store.ReadObject(indexPath)
	if err != nil {
		t.Fatalf("ReadObject(index): %v", err)
	}
	if len(indexData) != 0 {
		t.Fatalf("index changed before successful commit: %s", string(indexData))
	}
}

func TestWriter_Publish_ConcurrentWritersUseWinningPublishedAt(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	manifestA, _, _ := writePublishManifestFixture(t, store, "0.0.7")
	winningPublishedAt := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	laterPublishedAt := time.Date(2026, 8, 17, 11, 0, 0, 0, time.UTC)
	manifestA.Entry.PublishedAt = winningPublishedAt
	entryData, err := json.MarshalIndent(manifestA.Entry, "", "  ")
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	if err := os.WriteFile(manifestA.EntryObject.LocalPath, append(entryData, '\n'), 0o644); err != nil {
		t.Fatalf("rewrite entry file: %v", err)
	}

	writer := &Writer{Store: store}
	if _, err := writer.Publish(PublishRequest{
		Manifest:  manifestA,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{}); err != nil {
		t.Fatalf("first Publish() = %v", err)
	}

	manifestB, _, _ := writePublishManifestFixture(t, store, "0.0.7")
	manifestB.Entry.PublishedAt = laterPublishedAt
	entryData, err = json.MarshalIndent(manifestB.Entry, "", "  ")
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	if err := os.WriteFile(manifestB.EntryObject.LocalPath, append(entryData, '\n'), 0o644); err != nil {
		t.Fatalf("rewrite entry file: %v", err)
	}
	result, err := writer.Publish(PublishRequest{
		Manifest:  manifestB,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{})
	if err != nil {
		t.Fatalf("second Publish() = %v", err)
	}
	if result.Entry.Outcome != ObjectWriteOutcomeSkipped {
		t.Fatalf("second entry outcome = %q, want skipped", result.Entry.Outcome)
	}

	_, indexData, err := store.ReadObject(manifestA.IndexObject.StorageURL)
	if err != nil {
		t.Fatalf("ReadObject(index): %v", err)
	}
	index, err := DecodeIndex(indexData)
	if err != nil {
		t.Fatalf("DecodeIndex(): %v", err)
	}
	indexVersion := index.Apps[manifestA.AppName].Versions["0.0.7"]
	if !indexVersion.PublishedAt.Equal(winningPublishedAt) {
		t.Fatalf("index publishedAt = %v, want winning %v", indexVersion.PublishedAt, winningPublishedAt)
	}

	retentionURL := RetentionStorageURL(manifestA.IndexObject.StorageURL, manifestA.AppName)
	_, retentionData, err := store.ReadObject(retentionURL)
	if err != nil {
		t.Fatalf("ReadObject(retention): %v", err)
	}
	retention, err := DecodeRetentionIndex(retentionData)
	if err != nil {
		t.Fatalf("DecodeRetentionIndex(): %v", err)
	}
	retentionVersion := retention.Versions["0.0.7"]
	if !retentionVersion.PublishedAt.Equal(winningPublishedAt) {
		t.Fatalf("retention publishedAt = %v, want winning %v", retentionVersion.PublishedAt, winningPublishedAt)
	}
}

func TestWriter_Publish_IndexIdentityMismatchFails(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	manifest, _, _ := writePublishManifestFixture(t, store, "0.0.8")
	metadataPath := AppVersionEntryPath(manifest.AppName, manifest.Version)
	index := NewEmptyIndex()
	entry := manifest.Entry
	if _, changed, err := UpsertAppIndex(index, entry, metadataPath, "", ""); err != nil || !changed {
		t.Fatalf("seed index: changed=%v err=%v", changed, err)
	}
	index.Apps[manifest.AppName].Versions[manifest.Version] = indexVersionFromEntry(entry, metadataPath)
	app := index.Apps[manifest.AppName]
	version := app.Versions[manifest.Version]
	version.SourceRef = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	app.Versions[manifest.Version] = version
	index.Apps[manifest.AppName] = app
	indexData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	indexPath, err := WriteTempJSON("gestalt-app-index-stale-*", append(indexData, '\n'))
	if err != nil {
		t.Fatalf("WriteTempJSON(): %v", err)
	}
	if err := store.WriteCatalogObject(WriteCatalogObjectInput{
		LocalPath:  indexPath,
		StorageURL: manifest.IndexObject.StorageURL,
		SourceRef:  "651a5c30feb995c9364c38f63d0d5c3880bc2055",
		Generation: 0,
	}); err != nil {
		t.Fatalf("seed stale index: %v", err)
	}

	writer := &Writer{Store: store}
	_, err = writer.Publish(PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{})
	if !errors.Is(err, ErrIndexVersionConflict) {
		t.Fatalf("Publish() = %v, want ErrIndexVersionConflict", err)
	}
}

func TestWriter_Publish_ConflictingEntryIdentityFails(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	manifest, _, _ := writePublishManifestFixture(t, store, "0.0.9")
	conflicting := manifest.Entry
	conflicting.SourceRef = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	entryData, err := json.MarshalIndent(conflicting, "", "  ")
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	entryPath, err := WriteTempJSON("gestalt-app-entry-conflict-*", append(entryData, '\n'))
	if err != nil {
		t.Fatalf("WriteTempJSON(): %v", err)
	}
	if err := store.WriteImmutableObject(WriteImmutableObjectInput{
		LocalPath:  entryPath,
		StorageURL: manifest.EntryObject.StorageURL,
		SourceRef:  "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}); err != nil {
		t.Fatalf("seed conflicting entry: %v", err)
	}

	writer := &Writer{Store: store}
	err = writer.Preflight(PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{})
	if !errors.Is(err, ErrRegistryEntryConflict) {
		t.Fatalf("Preflight() = %v, want ErrRegistryEntryConflict", err)
	}
	_, err = writer.Publish(PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{})
	if !errors.Is(err, ErrRegistryEntryConflict) {
		t.Fatalf("Publish() = %v, want ErrRegistryEntryConflict", err)
	}
}

func TestWriter_Publish_PartialEntryWithoutIndexRemainsRetryable(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	manifest, _, _ := writePublishManifestFixture(t, store, "0.0.10")
	retentionURL := RetentionStorageURL(manifest.IndexObject.StorageURL, manifest.AppName)
	failStore := &retentionFailStore{
		RegistryObjectStore: store,
		retentionURL:        retentionURL,
	}
	writer := &Writer{Store: failStore}
	result, err := writer.Publish(PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{})
	if err == nil {
		t.Fatal("expected retention failure")
	}
	if result.Entry.Outcome != ObjectWriteOutcomeUploaded {
		t.Fatalf("entry outcome = %q, want uploaded", result.Entry.Outcome)
	}
	if result.Index != CatalogWriteOutcomeNotAttempted {
		t.Fatalf("index outcome = %q, want not_attempted", result.Index)
	}
	described, err := store.DescribeObject(manifest.EntryObject.StorageURL)
	if err != nil || described.Generation == 0 {
		t.Fatalf("entry object missing after partial publish: %#v err=%v", described, err)
	}
	_, indexData, err := store.ReadObject(manifest.IndexObject.StorageURL)
	if err != nil {
		t.Fatalf("ReadObject(index): %v", err)
	}
	if len(indexData) != 0 {
		t.Fatalf("index changed after partial publish: %s", string(indexData))
	}

	writer = &Writer{Store: store}
	result, err = writer.Publish(PublishRequest{
		Manifest:  manifest,
		SourceRef: "651a5c30feb995c9364c38f63d0d5c3880bc2055",
	}, PublishProgress{})
	if err != nil {
		t.Fatalf("retry Publish() = %v", err)
	}
	if result.Entry.Outcome != ObjectWriteOutcomeSkipped {
		t.Fatalf("retry entry outcome = %q, want skipped", result.Entry.Outcome)
	}
	if result.Index != CatalogWriteOutcomeUpdated {
		t.Fatalf("retry index outcome = %q, want updated", result.Index)
	}
}

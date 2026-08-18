package appregistry

import (
	"encoding/json"
	"fmt"
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
	// Force retention to succeed first; only index write fails.
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

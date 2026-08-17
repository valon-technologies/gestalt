package appregistry_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestLoadPublishedStateAbsentWithoutIndex(t *testing.T) {
	t.Parallel()

	store := appregistry.NewMemoryObjectStore()
	loaded, err := appregistry.LoadPublishedState(store, testPublishStorageRoot, "g-issues", "0.3.0-dev.10")
	if err != nil {
		t.Fatalf("LoadPublishedState: %v", err)
	}
	if loaded.State != appregistry.PublishedLoadAbsent {
		t.Fatalf("state = %v, want absent", loaded.State)
	}
}

func TestLoadPublishedStateCorruptWhenEntryMissing(t *testing.T) {
	t.Parallel()

	store := appregistry.NewMemoryObjectStore()
	index := appregistry.NewEmptyIndex()
	entryPath := appregistry.AppVersionEntryPath("g-issues", "0.3.0-dev.11")
	index.Apps["g-issues"] = appregistry.AppVersions{
		Versions: map[string]appregistry.IndexVersion{
			"0.3.0-dev.11": {
				Metadata: entryPath, PublishedAt: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	indexData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	tmpPath, err := appregistry.WriteTempJSON("gestalt-index-*", indexData)
	if err != nil {
		t.Fatalf("WriteTempJSON: %v", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()
	indexURL := appregistry.StorageURL(testPublishStorageRoot, appregistry.AppIndexPath("g-issues"))
	if err := store.WriteCatalogObject(appregistry.WriteCatalogObjectInput{
		LocalPath: tmpPath, StorageURL: indexURL, SourceRef: "test", Generation: 0,
	}); err != nil {
		t.Fatalf("WriteCatalogObject: %v", err)
	}

	loaded, err := appregistry.LoadPublishedState(store, testPublishStorageRoot, "g-issues", "0.3.0-dev.11")
	if err != nil {
		t.Fatalf("LoadPublishedState: %v", err)
	}
	if loaded.State != appregistry.PublishedLoadCorrupt {
		t.Fatalf("state = %v, want corrupt", loaded.State)
	}
}

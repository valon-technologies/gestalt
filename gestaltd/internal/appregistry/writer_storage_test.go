package appregistry

import (
	"context"
	"testing"
)

var (
	_ RegistryObjectStore             = (*GcloudObjectStore)(nil)
	_ RegistryObjectStore             = (*GCSRegistryStore)(nil)
	_ RegistryObjectStore             = (*MemoryObjectStore)(nil)
	_ RegistryObjectPromoter          = (*GCSRegistryStore)(nil)
	_ RegistryObjectPromoter          = (*MemoryObjectStore)(nil)
	_ RegistryObjectStoreWithPromoter = (*GCSRegistryStore)(nil)
	_ RegistryObjectStoreWithPromoter = (*MemoryObjectStore)(nil)
)

func TestWriterNeedsOnlyRegistryObjectStore(t *testing.T) {
	t.Parallel()

	var store RegistryObjectStore = NewMemoryObjectStore()
	writer := &Writer{Store: store}
	if writer.Store == nil {
		t.Fatal("writer store is required")
	}
}

func TestStatelessFinalizeNeedsRegistryObjectStoreWithPromoter(t *testing.T) {
	t.Parallel()

	service := &StatelessPublishService{
		Registry: "toolshed", StorageRoot: "gs://example", PublicRoot: "https://example",
		Writer: &Writer{Store: NewMemoryObjectStore()},
	}
	_, err := service.Finalize(context.Background(), "toolshed", AdminPublishInput{App: "g-issues"})
	if err != ErrPublishUnavailable {
		t.Fatalf("Finalize without store = %v, want %v", err, ErrPublishUnavailable)
	}
}

func TestGcloudObjectStoreDoesNotImplementRegistryObjectPromoter(t *testing.T) {
	t.Parallel()

	var store RegistryObjectStore = &GcloudObjectStore{}
	if _, ok := any(store).(RegistryObjectPromoter); ok {
		t.Fatal("GcloudObjectStore must not implement RegistryObjectPromoter")
	}
}

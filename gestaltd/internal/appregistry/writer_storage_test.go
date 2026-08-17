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

func TestStatelessFinalizeNeedsRegistryObjectPromoter(t *testing.T) {
	t.Parallel()

	store := NewMemoryObjectStore()
	service := &StatelessPublishService{
		Registry: "toolshed", StorageRoot: "gs://example", PublicRoot: "https://example",
		Store: store, Writer: &Writer{Store: store},
	}
	_, err := service.Finalize(context.Background(), "toolshed", AdminPublishInput{App: "g-issues"})
	if err != ErrPublishUnavailable {
		t.Fatalf("Finalize without promoter = %v, want %v", err, ErrPublishUnavailable)
	}
}

func TestGcloudObjectStoreDoesNotImplementRegistryObjectPromoter(t *testing.T) {
	t.Parallel()

	var store RegistryObjectStore = &GcloudObjectStore{}
	if _, ok := any(store).(RegistryObjectPromoter); ok {
		t.Fatal("GcloudObjectStore must not implement RegistryObjectPromoter")
	}
}

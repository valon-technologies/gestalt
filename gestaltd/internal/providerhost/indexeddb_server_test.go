package providerhost

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func TestIndexedDBServerPrefixesStoreNamesPerPlugin(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	srv := NewIndexedDBServer(db, "roadmap")
	ctx := context.Background()
	record, err := gestalt.RecordToProto(map[string]any{"id": "snap-1"})
	if err != nil {
		t.Fatalf("RecordToProto: %v", err)
	}

	if _, err := srv.(*indexedDBServer).Put(ctx, &proto.RecordRequest{
		Store:  "snapshots",
		Record: record,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := db.ObjectStore("plugin_roadmap_snapshots").Get(ctx, "snap-1"); err != nil {
		t.Fatalf("expected prefixed object store record to exist")
	}
	if _, err := db.ObjectStore("snapshots").Get(ctx, "snap-1"); err == nil {
		t.Fatal("unprefixed object store should not contain the record")
	}
}

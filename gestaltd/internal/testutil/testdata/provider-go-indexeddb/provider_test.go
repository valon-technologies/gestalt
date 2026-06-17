package provider

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

func TestProvider_IndexGetMatchesIndexGetAllFirst(t *testing.T) {
	t.Parallel()

	p := New()
	ctx := context.Background()
	schema := gestalt.ObjectStoreOptions{
		Indexes: []gestalt.IndexSchema{{Name: "by_status", KeyPath: []string{"status"}}},
	}
	if err := p.CreateObjectStore(ctx, "items", schema); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	for _, rec := range []gestalt.Record{
		{"id": "c", "status": "active"},
		{"id": "a", "status": "active"},
		{"id": "b", "status": "active"},
	} {
		if err := p.Put(ctx, gestalt.IndexedDBRecordRequest{Store: "items", Record: rec}); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	query := indexeddb.ToQuery("active")
	req := gestalt.IndexedDBIndexQueryRequest{
		Store: "items",
		Index: "by_status",
		Query: query,
	}

	all, err := p.IndexGetAll(ctx, req)
	if err != nil {
		t.Fatalf("IndexGetAll: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("IndexGetAll len = %d, want 3", len(all))
	}
	wantIDs := []string{"a", "b", "c"}
	for i, id := range wantIDs {
		if fieldString(all[i], "id") != id {
			t.Fatalf("IndexGetAll[%d] id = %q, want %q (full order %v)", i, fieldString(all[i], "id"), id, all)
		}
	}

	got, err := p.IndexGet(ctx, req)
	if err != nil {
		t.Fatalf("IndexGet: %v", err)
	}
	if fieldString(got, "id") != fieldString(all[0], "id") {
		t.Fatalf("IndexGet id = %q, want %q (IndexGetAll[0])", fieldString(got, "id"), fieldString(all[0], "id"))
	}
}

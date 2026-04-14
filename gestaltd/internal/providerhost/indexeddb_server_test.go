package providerhost

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
)

func TestIndexedDBServerUsesStoreNamesAsProvided(t *testing.T) {
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

	if _, err := db.ObjectStore("snapshots").Get(ctx, "snap-1"); err != nil {
		t.Fatalf("expected object store record to exist: %v", err)
	}
}

func TestIndexedDBServerRecordsPluginMetricAttributes(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)

	db := metricutil.InstrumentIndexedDB(&coretesting.StubIndexedDB{}, "system")
	srv := NewIndexedDBServer(db, "roadmap")
	if err := metricutil.UnwrapIndexedDB(db).CreateObjectStore(ctx, "snapshots", indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{{Name: "by_type", KeyPath: []string{"type"}}},
	}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	if _, err := srv.(*indexedDBServer).Put(ctx, &proto.RecordRequest{
		Store: "snapshots",
		Record: func() *proto.Record {
			rec, err := gestalt.RecordToProto(map[string]any{"id": "snap-1", "type": "daily"})
			if err != nil {
				t.Fatalf("RecordToProto with type: %v", err)
			}
			return rec
		}(),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	value, err := gestalt.TypedValueFromAny("daily")
	if err != nil {
		t.Fatalf("TypedValueFromAny: %v", err)
	}
	if _, err := srv.(*indexedDBServer).IndexGet(ctx, &proto.IndexQueryRequest{
		Store:  "snapshots",
		Index:  "by_type",
		Values: []*proto.TypedValue{value},
	}); err != nil {
		t.Fatalf("IndexGet: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	putAttrs := map[string]string{
		"gestalt.db":           "system",
		"gestalt.plugin":       "roadmap",
		"gestalt.object_store": "snapshots",
		"gestalt.method":       "Put",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.indexeddb.count", 1, putAttrs)
	metrictest.RequireInt64SumOmitsAttr(t, rm, "gestaltd.indexeddb.count", putAttrs, "gestalt.store")
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.indexeddb.duration", putAttrs)

	indexAttrs := map[string]string{
		"gestalt.db":           "system",
		"gestalt.plugin":       "roadmap",
		"gestalt.object_store": "snapshots",
		"gestalt.method":       "Index.Get",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.indexeddb.count", 1, indexAttrs)
	metrictest.RequireInt64SumOmitsAttr(t, rm, "gestaltd.indexeddb.count", indexAttrs, "gestalt.store")
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.indexeddb.duration", indexAttrs)
}

package metricutil

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
)

func TestInstrumentIndexedDBRecordsDBAndObjectStoreAttributes(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := WithMeterProvider(context.Background(), metrics.Provider)

	db := InstrumentIndexedDB(&coretesting.StubIndexedDB{}, "system")
	if err := db.ObjectStore("users").Put(ctx, map[string]any{"id": "user-1"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := db.ObjectStore("users").Get(ctx, "missing"); err == nil {
		t.Fatal("Get missing record should fail")
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	dbAttrs := map[string]string{
		"db.system.name":     "gestaltd.indexeddb",
		"db.namespace":       "system",
		"db.collection.name": "users",
		"db.operation.name":  "put",
	}
	metrictest.RequireFloat64Histogram(t, rm, "db.client.operation.duration", dbAttrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "db.client.operation.duration", dbAttrs, "gestalt.db")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "db.client.operation.duration", dbAttrs, "gestalt.object_store")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "db.client.operation.duration", dbAttrs, "gestalt.method")
	metrictest.RequireFloat64Histogram(t, rm, "db.client.operation.duration", map[string]string{
		"db.system.name":     "gestaltd.indexeddb",
		"db.namespace":       "system",
		"db.collection.name": "users",
		"db.operation.name":  "get",
		"error.type":         "not_found",
	})

	metrictest.RequireNoMetric(t, rm, "gestaltd.indexeddb.count")
	metrictest.RequireNoMetric(t, rm, "gestaltd.indexeddb.error_count")
	metrictest.RequireNoMetric(t, rm, "gestaltd.indexeddb.duration")
}

func TestInstrumentIndexedDBRecordsTransactionScopedOperations(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := WithMeterProvider(context.Background(), metrics.Provider)

	db := InstrumentIndexedDB(&coretesting.StubIndexedDB{}, "system")
	if err := db.CreateObjectStore(ctx, "users", indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{{Name: "by_email", KeyPath: []string{"email"}}},
	}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	tx, err := db.Transaction(ctx, []string{"users"}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}
	txStore := tx.ObjectStore("users")
	if err := txStore.Put(ctx, indexeddb.Record{"id": "user-1", "email": "user@example.com"}); err != nil {
		t.Fatalf("transaction Put: %v", err)
	}
	if _, err := txStore.Index("by_email").Get(ctx, "user@example.com"); err != nil {
		t.Fatalf("transaction index Get: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireFloat64Histogram(t, rm, "db.client.operation.duration", map[string]string{
		"db.system.name":     "gestaltd.indexeddb",
		"db.namespace":       "system",
		"db.collection.name": "users",
		"db.operation.name":  "put",
	})
	metrictest.RequireFloat64Histogram(t, rm, "db.client.operation.duration", map[string]string{
		"db.system.name":                "gestaltd.indexeddb",
		"db.namespace":                  "system",
		"db.collection.name":            "users",
		"db.operation.name":             "index_get",
		"gestaltd.indexeddb.index.name": "by_email",
	})
}

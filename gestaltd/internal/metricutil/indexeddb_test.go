package metricutil

import (
	"context"
	"testing"

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

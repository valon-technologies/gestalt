package metricutil

import (
	"context"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
)

func TestInstrumentIndexedDBRecordsDBAndObjectStoreAttributes(t *testing.T) {
	metrics := metrictest.NewManualMeterProvider(t)
	ctx := WithMeterProvider(context.Background(), metrics.Provider)

	db := InstrumentIndexedDB(&coretesting.StubIndexedDB{}, "system")
	if err := db.ObjectStore("users").Put(ctx, map[string]any{"id": "user-1"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestalt.db":           "system",
		"gestalt.object_store": "users",
		"gestalt.method":       "Put",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.indexeddb.count", 1, attrs)
	metrictest.RequireInt64SumOmitsAttr(t, rm, "gestaltd.indexeddb.count", attrs, "gestalt.plugin")
	metrictest.RequireInt64SumOmitsAttr(t, rm, "gestaltd.indexeddb.count", attrs, "gestalt.store")
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.indexeddb.duration", attrs)
}

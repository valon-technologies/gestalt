package metricutil

import (
	"context"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
)

func TestInstrumentIndexedDBRecordsDBAndObjectStoreAttributes(t *testing.T) {
	reader := metrictest.UseManualMeterProvider(t)
	ctx := context.Background()

	db := InstrumentIndexedDB(&coretesting.StubIndexedDB{}, "system")
	if err := db.ObjectStore("users").Put(ctx, map[string]any{"id": "user-1"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rm := metrictest.CollectMetrics(t, reader)
	attrs := map[string]string{
		"gestalt.db":           "system",
		"gestalt.object_store": "users",
		"gestalt.method":       "Put",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.indexeddb.count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.indexeddb.duration", attrs)
}

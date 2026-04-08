package main

import (
	"context"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
)

func TestDatastoreReadinessEmitsPingMetric(t *testing.T) {
	t.Parallel()

	reader := metrictest.UseManualMeterProvider(t)
	check := datastoreReadiness(metrictest.NewNamedStubDatastore("ready-store", coretesting.StubDatastore{
		PingFn: func(context.Context) error { return nil },
	}))

	if got := check(); got != "" {
		t.Fatalf("readiness = %q, want ready", got)
	}

	rm := metrictest.CollectMetrics(t, reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.datastore.count", 1, map[string]string{
		"gestalt.provider": "ready-store",
		"gestalt.method":   "ping",
	})
}

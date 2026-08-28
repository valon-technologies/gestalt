package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestIngressTelemetryRequestMetrics(t *testing.T) {
	t.Parallel()

	t.Run("ready request labels unknown client without ingress kind", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		srv := newTestServer(t, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
		})
		testutil.CloseOnCleanup(t, srv)

		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/ready", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		attrs := map[string]string{
			"http.route":           "/ready",
			"gestaltd.client.kind": metricutil.ClientKindUnknown,
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
		metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", attrs, "gestaltd.ingress.kind")
	})

	t.Run("app invoke v1 labels cli client and ingress kind", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		const providerName = "ingress-metrics"
		srv := newTestServer(t, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
			cfg.Providers = testutil.NewProviderRegistry(t, &stubIntegrationWithCatalog{
				StubIntegration: coretesting.StubIntegration{
					N:        providerName,
					ConnMode: core.ConnectionModeNone,
					ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
						return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"operation":"` + operation + `"}`)}, nil
					},
				},
				catalog: &catalog.Catalog{
					Name: providerName,
					Operations: []catalog.CatalogOperation{
						{ID: "list", Method: http.MethodGet, Path: "/list"},
					},
				},
			})
		})
		testutil.CloseOnCleanup(t, srv)

		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/"+providerName+"/list", nil)
		req.Header.Set(metricutil.HeaderGestaltClient, metricutil.ClientKindCLI)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		attrs := map[string]string{
			"http.route":            "/api/v1/{integration}/{operation}",
			"gestaltd.ingress.kind": metricutil.IngressKindAppInvokeV1,
			"gestaltd.client.kind":  metricutil.ClientKindCLI,
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
	})

	t.Run("rejected app invoke retains ingress kind", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		srv := newTestServer(t, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
		})
		testutil.CloseOnCleanup(t, srv)

		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/not-a-provider/not-an-operation", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusOK {
			t.Fatal("expected rejected app invocation")
		}

		attrs := map[string]string{
			"http.route":            "/api/v1/{integration}/{operation}",
			"gestaltd.ingress.kind": metricutil.IngressKindAppInvokeV1,
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
	})

	t.Run("catalog route labels cli client without ingress kind", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		srv := newTestServer(t, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
		})
		testutil.CloseOnCleanup(t, srv)

		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/catalog/apps", nil)
		req.Header.Set(metricutil.HeaderGestaltClient, metricutil.ClientKindCLI)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		attrs := map[string]string{
			"http.route":           "/api/v1/catalog/apps",
			"gestaltd.client.kind": metricutil.ClientKindCLI,
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
		metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", attrs, "gestaltd.ingress.kind")
	})

	t.Run("mounted ui labels web client and ingress kind", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		srv := newTestServer(t, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
			cfg.MountedUIs = []server.MountedUI{{
				Path: "/telemetry-ui",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("ok"))
				}),
			}}
		})
		testutil.CloseOnCleanup(t, srv)

		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/telemetry-ui/", nil)
		req.Header.Set("Sec-Fetch-Dest", "document")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		attrs := map[string]string{
			"http.route":            "/telemetry-ui/*",
			"gestaltd.ingress.kind": metricutil.IngressKindMountedUI,
			"gestaltd.client.kind":  metricutil.ClientKindWeb,
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
	})

	t.Run("public rest labels cli client and ingress kind", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
		})

		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v2/identity/userinfo", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set(metricutil.HeaderGestaltClient, metricutil.ClientKindCLI)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		attrs := map[string]string{
			"http.route":            "/api/v2/*",
			"gestaltd.ingress.kind": metricutil.IngressKindPublicREST,
			"gestaltd.client.kind":  metricutil.ClientKindCLI,
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
	})
}

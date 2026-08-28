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
		metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", attrs, "gestaltd.subject.label")
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
			"http.route":             "/api/v1/{integration}/{operation}",
			"gestaltd.ingress.kind":  metricutil.IngressKindAppInvokeV1,
			"gestaltd.client.kind":   metricutil.ClientKindCLI,
			"gestaltd.subject.label": "anonymous@gestalt",
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
		metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", attrs, "gestaltd.client.app")
	})

	t.Run("app invoke v1 labels authenticated user and service account subjects", func(t *testing.T) {
		t.Parallel()

		const providerName = "ingress-subject-metrics"
		provider := &stubIntegrationWithCatalog{
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
		}

		for _, tc := range []struct {
			name         string
			auth         core.IdentityProvider
			token        string
			subjectLabel string
		}{
			{
				name: "user email",
				auth: coretesting.NamedIntrospectIdentityStub("stub", func(_ context.Context, token string) (*core.UserIdentity, error) {
					if token != "user-token" {
						return nil, core.ErrNotFound
					}
					return &core.UserIdentity{Email: "USER@example.com"}, nil
				}),
				token:        "user-token",
				subjectLabel: "user@example.com",
			},
			{
				name: "service account",
				auth: testAuthStubWithIntrospect(func(_ context.Context, token string) (*core.IntrospectResponse, error) {
					if token != "sa-token" {
						return &core.IntrospectResponse{Active: false}, nil
					}
					return testIntrospectActive("service_account:oauth-bot", ""), nil
				}),
				token:        "sa-token",
				subjectLabel: "oauth-bot",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				metrics := metrictest.NewManualMeterProvider(t)
				srv := newTestServer(t, func(cfg *server.Config) {
					cfg.MeterProvider = metrics.Provider
					cfg.Auth = tc.auth
					cfg.StateSecret = []byte("0123456789abcdef0123456789abcdef")
					cfg.Providers = testutil.NewProviderRegistry(t, provider)
				})
				testutil.CloseOnCleanup(t, srv)

				req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/"+providerName+"/list", nil)
				req.Header.Set("Authorization", "Bearer "+tc.token)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("request: %v", err)
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
				}

				attrs := map[string]string{
					"http.route":             "/api/v1/{integration}/{operation}",
					"gestaltd.ingress.kind":  metricutil.IngressKindAppInvokeV1,
					"gestaltd.client.kind":   metricutil.ClientKindUnknown,
					"gestaltd.subject.label": tc.subjectLabel,
				}
				rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
					return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
				})
				metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
			})
		}
	})

	t.Run("app invoke v1 labels unknown subject on auth failure", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		const providerName = "ingress-auth-failure"
		srv := newTestServer(t, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
			cfg.Auth = testAuthStubWithIntrospect(func(_ context.Context, _ string) (*core.IntrospectResponse, error) {
				return &core.IntrospectResponse{Active: false}, nil
			})
			cfg.StateSecret = []byte("0123456789abcdef0123456789abcdef")
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
		req.Header.Set("Authorization", "Bearer invalid-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}

		attrs := map[string]string{
			"http.route":             "/api/v1/{integration}/{operation}",
			"gestaltd.ingress.kind":  metricutil.IngressKindAppInvokeV1,
			"gestaltd.subject.label": metricutil.SubjectLabelUnknown,
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
	})

	t.Run("app invoke v1 labels matched and unknown web client apps", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		const providerName = "ingress-web-metrics"
		srv := newTestServer(t, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
			cfg.PublicBaseURL = "https://valon.tools"
			cfg.MountedUIs = []server.MountedUI{{
				Name: "app:telemetry-ui",
				Path: "/telemetry-ui",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("ok"))
				}),
			}}
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

		for _, tc := range []struct {
			name      string
			referer   string
			clientApp string
		}{
			{
				name:      "matched",
				referer:   "https://valon.tools/telemetry-ui/page",
				clientApp: "app:telemetry-ui",
			},
			{
				name:      "unknown",
				referer:   "https://valon.tools/other-ui/page",
				clientApp: metricutil.ClientAppUnknown,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/"+providerName+"/list", nil)
				req.Header.Set("Sec-Fetch-Site", "same-origin")
				req.Header.Set("Referer", tc.referer)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("request: %v", err)
				}
				defer func() { _ = resp.Body.Close() }()

				attrs := map[string]string{
					"http.route":            "/api/v1/{integration}/{operation}",
					"gestaltd.ingress.kind": metricutil.IngressKindAppInvokeV1,
					"gestaltd.client.kind":  metricutil.ClientKindWeb,
					"gestaltd.client.app":   tc.clientApp,
				}
				rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
					return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
				})
				metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
			})
		}
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
			"http.route":             "/api/v1/{integration}/{operation}",
			"gestaltd.ingress.kind":  metricutil.IngressKindAppInvokeV1,
			"gestaltd.subject.label": "anonymous@gestalt",
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
		metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", attrs, "gestaltd.subject.label")
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
		metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", attrs, "gestaltd.subject.label")
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
			"http.route":             "/api/v2/*",
			"gestaltd.ingress.kind":  metricutil.IngressKindPublicREST,
			"gestaltd.client.kind":   metricutil.ClientKindCLI,
			"gestaltd.subject.label": metricutil.SubjectLabelUnknown,
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
		metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", attrs, "gestaltd.client.app")
	})

	t.Run("public rest labels authenticated user subject", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		identity := testAuthStubForScopedBearer()
		identity.UserInfoFn = func(_ context.Context, _ *core.UserInfoRequest) (*core.UserInfoResponse, error) {
			return &core.UserInfoResponse{Email: "USER@example.com"}, nil
		}
		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
			cfg.Auth = identity
			cfg.PublicGatewayTransport.SetIdentityProvider(identity)
		})

		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v2/identity/userinfo", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+publicRESTTestBearer(""))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if got := resp.Header.Values("Grpc-Metadata-Gestaltd-Subject-Label"); len(got) != 0 {
			t.Fatalf("response exposed subject label metadata: %v", got)
		}

		attrs := map[string]string{
			"http.route":             "/api/v2/*",
			"gestaltd.ingress.kind":  metricutil.IngressKindPublicREST,
			"gestaltd.client.kind":   metricutil.ClientKindUnknown,
			"gestaltd.subject.label": "user@example.com",
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
	})

	t.Run("public rest web labels matched client app from referer", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
			cfg.MountedUIs = []server.MountedUI{{
				Name: "app:telemetry-ui",
				Path: "/telemetry-ui",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("ok"))
				}),
			}}
		})

		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v2/identity/userinfo", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Referer", "https://gestalt.test/telemetry-ui/page")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		attrs := map[string]string{
			"http.route":            "/api/v2/*",
			"gestaltd.ingress.kind": metricutil.IngressKindPublicREST,
			"gestaltd.client.kind":  metricutil.ClientKindWeb,
			"gestaltd.client.app":   "app:telemetry-ui",
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
	})

	t.Run("public rest web labels unknown client app without matching referer", func(t *testing.T) {
		t.Parallel()

		metrics := metrictest.NewManualMeterProvider(t)
		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.MeterProvider = metrics.Provider
			cfg.MountedUIs = []server.MountedUI{{
				Name: "app:telemetry-ui",
				Path: "/telemetry-ui",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("ok"))
				}),
			}}
		})

		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v2/identity/userinfo", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Referer", "https://gestalt.test/other-ui/page")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		attrs := map[string]string{
			"http.route":            "/api/v2/*",
			"gestaltd.ingress.kind": metricutil.IngressKindPublicREST,
			"gestaltd.client.kind":  metricutil.ClientKindWeb,
			"gestaltd.client.app":   metricutil.ClientAppUnknown,
		}
		rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
			return metrictest.HasFloat64Histogram(rm, "http.server.request.duration", attrs)
		})
		metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", attrs)
	})
}

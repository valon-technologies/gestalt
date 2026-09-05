package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func TestAppAdminUIObservabilityMiddlewareRecordsSuccess(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)

	handler := (&Server{}).appAdminUIObservabilityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/g-issues/admin/members", nil)
	req = req.WithContext(ctx)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("app", "g-issues")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.app_admin.app":       "g-issues",
		"gestaltd.app_admin.operation": "members_list",
		"gestaltd.app_admin.outcome":   "success",
		"gestaltd.subject.kind":        "unknown",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.count", 1, attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.app_admin.error_count", attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.app_admin.duration", attrs)
}

func TestAppAdminUIObservabilityMiddlewareRecordsValidationFailure(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)

	handler := (&Server{}).appAdminUIObservabilityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
	}))

	req := httptest.NewRequest(http.MethodPut, "/api/v1/apps/g-issues/admin/allowed-operations", nil)
	req = req.WithContext(ctx)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("app", "g-issues")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.app_admin.app":              "g-issues",
		"gestaltd.app_admin.operation":        "allowed_operations_save",
		"gestaltd.app_admin.outcome":          "failure",
		"gestaltd.app_admin.failure_category": "validation",
		"gestaltd.subject.kind":               "unknown",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.count", 1, attrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.error_count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.app_admin.duration", attrs)
}

func TestAppAdminUIObservabilityMiddlewareRecordsAuthorizationUnavailable(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)

	s := &Server{}
	handler := s.appAdminUIObservabilityMiddleware(s.appAdminAuthorizationMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not run when authorization is unavailable")
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/g-issues/admin/members", nil)
	req = req.WithContext(ctx)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("app", "g-issues")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.app_admin.app":              "g-issues",
		"gestaltd.app_admin.operation":        "members_list",
		"gestaltd.app_admin.outcome":          "failure",
		"gestaltd.app_admin.failure_category": "server",
		"gestaltd.subject.kind":               "unknown",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.count", 1, attrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.error_count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.app_admin.duration", attrs)
}

func TestAppAdminUIFailureCategoryFromStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		want   string
	}{
		{status: http.StatusUnauthorized, want: observability.AppAdminUIFailureAuth},
		{status: http.StatusForbidden, want: observability.AppAdminUIFailureAuth},
		{status: http.StatusBadRequest, want: observability.AppAdminUIFailureValidation},
		{status: http.StatusNotFound, want: observability.AppAdminUIFailureOther},
		{status: http.StatusInternalServerError, want: observability.AppAdminUIFailureServer},
	}
	for _, tc := range tests {
		if got := observability.AppAdminUIFailureCategoryHTTP(tc.status); got != tc.want {
			t.Fatalf("status %d category = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestAppAdminUIRouteSpecForRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method  string
		path    string
		surface string
		action  string
		ok      bool
	}{
		{method: http.MethodGet, path: "/api/v1/apps/demo/admin/members", surface: "members", action: "list", ok: true},
		{method: http.MethodGet, path: "/api/v1/apps/demo/admin/allowed-operations", surface: "allowed_operations", action: "list", ok: true},
		{method: http.MethodPut, path: "/api/v1/apps/demo/admin/allowed-operations", surface: "allowed_operations", action: "save", ok: true},
		{method: http.MethodGet, path: "/api/v1/apps/demo/admin/registry", ok: false},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		surface, action, ok := appAdminUIRouteSpecForRequest(req)
		if ok != tc.ok {
			t.Fatalf("%s %s ok = %v, want %v", tc.method, tc.path, ok, tc.ok)
		}
		if !tc.ok {
			continue
		}
		if surface != tc.surface || action != tc.action {
			t.Fatalf("%s %s spec = (%q, %q), want (%q, %q)", tc.method, tc.path, surface, action, tc.surface, tc.action)
		}
	}
}

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
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
		"gestaltd.app_admin.ui.app":     "g-issues",
		"gestaltd.app_admin.ui.surface": "members",
		"gestaltd.app_admin.ui.action":  "list",
		"gestaltd.app_admin.ui.outcome": "success",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.ui.count", 1, attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.app_admin.ui.error_count", attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.app_admin.ui.duration", attrs)
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
		"gestaltd.app_admin.ui.app":              "g-issues",
		"gestaltd.app_admin.ui.surface":          "allowed_operations",
		"gestaltd.app_admin.ui.action":           "save",
		"gestaltd.app_admin.ui.outcome":          "failure",
		"gestaltd.app_admin.ui.failure_category": "validation",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.ui.count", 1, attrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.ui.error_count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.app_admin.ui.duration", attrs)
}

func TestAppAdminUIFailureCategoryFromStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		want   string
	}{
		{status: http.StatusUnauthorized, want: appAdminUIFailureAuth},
		{status: http.StatusForbidden, want: appAdminUIFailureAuth},
		{status: http.StatusBadRequest, want: appAdminUIFailureValidation},
		{status: http.StatusNotFound, want: appAdminUIFailureOther},
		{status: http.StatusInternalServerError, want: appAdminUIFailureServer},
	}
	for _, tc := range tests {
		if got := appAdminUIFailureCategoryFromStatus(tc.status); got != tc.want {
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

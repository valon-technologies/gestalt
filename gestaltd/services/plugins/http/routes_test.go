package pluginshttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestMountAuthenticatedRoutes(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	MountAuthenticated(r, AuthenticatedHandlers{
		ListIntegrations:      testHandler("list-integrations"),
		DisconnectIntegration: testHandler("disconnect-integration"),
	})

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/integrations", "list-integrations"},
		{http.MethodDelete, "/integrations/github", "disconnect-integration"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.path, rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("X-Test-Handler"); got != tc.want {
			t.Fatalf("%s %s handler = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestMountOperationRoutes(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	MountOperations(r, OperationHandlers{
		ListOperations:   testHandler("list-operations"),
		ExecuteOperation: testHandler("execute-operation"),
		PluginRouteAuthMiddleware: func(param string) Middleware {
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Test-Middleware-Param", param)
					next.ServeHTTP(w, r)
				})
			}
		},
	})

	cases := []struct {
		method    string
		path      string
		want      string
		wantParam string
	}{
		{http.MethodGet, "/integrations/github/operations", "list-operations", "name"},
		{http.MethodGet, "/github/list", "execute-operation", "integration"},
		{http.MethodPost, "/github/list", "execute-operation", "integration"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.path, rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("X-Test-Handler"); got != tc.want {
			t.Fatalf("%s %s handler = %q, want %q", tc.method, tc.path, got, tc.want)
		}
		if got := rec.Header().Get("X-Test-Middleware-Param"); got != tc.wantParam {
			t.Fatalf("%s %s middleware param = %q, want %q", tc.method, tc.path, got, tc.wantParam)
		}
	}
}

func testHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Test-Handler", name)
		w.WriteHeader(http.StatusNoContent)
	}
}

package identityhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestMountRoutes(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	Mount(r, Handlers{
		StartOAuth:         testHandler("start-oauth"),
		ConnectManual:      testHandler("connect-manual"),
		CreateAPIToken:     testHandler("create-api-token"),
		ListAPITokens:      testHandler("list-api-tokens"),
		RevokeAllAPITokens: testHandler("revoke-all-api-tokens"),
		RevokeAPIToken:     testHandler("revoke-api-token"),
	})

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/auth/start-oauth", "start-oauth"},
		{http.MethodPost, "/auth/connect-manual", "connect-manual"},
		{http.MethodPost, "/tokens", "create-api-token"},
		{http.MethodGet, "/tokens", "list-api-tokens"},
		{http.MethodDelete, "/tokens", "revoke-all-api-tokens"},
		{http.MethodDelete, "/tokens/token-1", "revoke-api-token"},
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

func testHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Test-Handler", name)
		w.WriteHeader(http.StatusNoContent)
	}
}

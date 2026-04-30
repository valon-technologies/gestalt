package providerdevhttp

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
		CreateAttachment: testHandler("create-attachment"),
		ListAttachments:  testHandler("list-attachments"),
		GetAttachment:    testHandler("get-attachment"),
	})

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/provider-dev/attachments", "create-attachment"},
		{http.MethodGet, "/provider-dev/attachments", "list-attachments"},
		{http.MethodGet, "/provider-dev/attachments/attachment-1", "get-attachment"},
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

package apiexec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestDo_POSTExplicitContentTypeOverridesCustomHeaders(t *testing.T) {
	t.Parallel()

	var gotContentType string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	testutil.CloseOnCleanup(t, srv)

	_, err := Do(context.Background(), srv.Client(), Request{
		Method:      http.MethodPost,
		BaseURL:     srv.URL,
		Path:        "/items",
		Params:      map[string]any{"name": "widget"},
		ContentType: "application/json; charset=utf-8",
		CustomHeaders: map[string]string{
			"Content-Type": "text/plain",
			"X-Test":       "1",
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if gotContentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want %q", gotContentType, "application/json; charset=utf-8")
	}
	if gotBody["name"] != "widget" {
		t.Fatalf("body[name] = %v, want widget", gotBody["name"])
	}
}

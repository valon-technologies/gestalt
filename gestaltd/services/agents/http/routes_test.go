package agentshttp

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
		CreateSession:      testHandler("create-session"),
		ListSessions:       testHandler("list-sessions"),
		ListProviders:      testHandler("list-providers"),
		GetSession:         testHandler("get-session"),
		UpdateSession:      testHandler("update-session"),
		CreateTurn:         testHandler("create-turn"),
		ListTurns:          testHandler("list-turns"),
		GetTurn:            testHandler("get-turn"),
		CancelTurn:         testHandler("cancel-turn"),
		ListTurnEvents:     testHandler("list-turn-events"),
		StreamTurnEvents:   testHandler("stream-turn-events"),
		ListInteractions:   testHandler("list-interactions"),
		ResolveInteraction: testHandler("resolve-interaction"),
	})

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/agent/sessions", "create-session"},
		{http.MethodGet, "/agent/sessions", "list-sessions"},
		{http.MethodGet, "/agent/providers", "list-providers"},
		{http.MethodPost, "/agent/sessions/", "create-session"},
		{http.MethodGet, "/agent/sessions/", "list-sessions"},
		{http.MethodGet, "/agent/sessions/session-1", "get-session"},
		{http.MethodPatch, "/agent/sessions/session-1", "update-session"},
		{http.MethodPost, "/agent/sessions/session-1/turns", "create-turn"},
		{http.MethodGet, "/agent/sessions/session-1/turns", "list-turns"},
		{http.MethodGet, "/agent/turns/turn-1", "get-turn"},
		{http.MethodPost, "/agent/turns/turn-1/cancel", "cancel-turn"},
		{http.MethodGet, "/agent/turns/turn-1/events", "list-turn-events"},
		{http.MethodGet, "/agent/turns/turn-1/events/stream", "stream-turn-events"},
		{http.MethodGet, "/agent/turns/turn-1/interactions", "list-interactions"},
		{http.MethodPost, "/agent/turns/turn-1/interactions/interaction-1/resolve", "resolve-interaction"},
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

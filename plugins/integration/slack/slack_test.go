package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
)

func newMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/oauth.v2.access", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		grantType := r.FormValue("grant_type")
		w.Header().Set("Content-Type", "application/json")

		switch grantType {
		case "refresh_token":
			refresh := r.FormValue("refresh_token")
			if refresh == "valid-refresh" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":            true,
					"access_token":  "refreshed-access-token",
					"token_type":    "Bearer",
					"refresh_token": "",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "invalid_grant",
			})
		default:
			code := r.FormValue("code")
			if code == "valid-code" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":            true,
					"access_token":  "mock-access-token",
					"token_type":    "Bearer",
					"refresh_token": "mock-refresh-token",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "invalid_grant",
			})
		}
	})

	mux.HandleFunc("/api/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !validToken(r) {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"channels": []map[string]string{{"id": "C123", "name": "general"}},
		})
	})

	mux.HandleFunc("/api/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !validToken(r) {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"channel": "C123",
			"ts":      "1234567890.123456",
		})
	})

	mux.HandleFunc("/api/users.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !validToken(r) {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"members": []map[string]string{{"id": "U123", "name": "testuser"}},
		})
	})

	mux.HandleFunc("/api/search.messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !validToken(r) {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"messages": map[string]any{"matches": []any{}},
		})
	})

	mux.HandleFunc("/api/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !validToken(r) {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"messages": []map[string]string{{"text": "hello"}},
		})
	})

	return httptest.NewServer(mux)
}

func validToken(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	return auth == "Bearer valid-bearer-token"
}

func TestConformanceSuite(t *testing.T) {
	t.Parallel()

	srv := newMockServer(t)
	defer srv.Close()

	coretesting.RunIntegrationTests(t, func(t *testing.T, mockURL string) core.Integration {
		t.Helper()
		return New(Config{
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			RedirectURL:  mockURL + "/callback",
			BaseURL:      mockURL,
		})
	}, srv)
}

func TestSendMessage(t *testing.T) {
	t.Parallel()

	srv := newMockServer(t)
	defer srv.Close()

	s := New(Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  srv.URL + "/callback",
		BaseURL:      srv.URL,
	})

	result, err := s.Execute(context.Background(), "send_message", map[string]any{
		"channel": "C123",
		"text":    "hello world",
	}, "valid-bearer-token")
	if err != nil {
		t.Fatalf("Execute(send_message): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Errorf("Status = %d, want %d", result.Status, http.StatusOK)
	}
	if result.Body == "" {
		t.Error("Body is empty")
	}
}

func TestUnknownOperation(t *testing.T) {
	t.Parallel()

	s := New(Config{BaseURL: "http://localhost"})

	_, err := s.Execute(context.Background(), "nonexistent", nil, "tok")
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
}

func TestSlackErrorResponse(t *testing.T) {
	t.Parallel()

	srv := newMockServer(t)
	defer srv.Close()

	s := New(Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  srv.URL + "/callback",
		BaseURL:      srv.URL,
	})

	_, err := s.Execute(context.Background(), "list_channels", map[string]any{}, "invalid-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestListOperations(t *testing.T) {
	t.Parallel()

	s := New(Config{})
	ops := s.ListOperations()
	if len(ops) != 5 {
		t.Fatalf("len(ListOperations()) = %d, want 5", len(ops))
	}

	names := map[string]bool{}
	for _, op := range ops {
		names[op.Name] = true
	}
	for _, expected := range []string{"list_channels", "send_message", "list_users", "search_messages", "read_channel"} {
		if !names[expected] {
			t.Errorf("missing operation %q", expected)
		}
	}
}

func TestName(t *testing.T) {
	t.Parallel()

	s := New(Config{})
	if s.Name() != "slack" {
		t.Errorf("Name() = %q, want %q", s.Name(), "slack")
	}
}

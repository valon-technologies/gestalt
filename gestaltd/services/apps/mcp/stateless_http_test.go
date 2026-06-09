package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

func TestStatelessHTTPHandlerProtocolEdges(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		headers    map[string]string
		body       string
		wantStatus int
		wantError  bool
	}{
		{
			name:       "invalid accept",
			headers:    map[string]string{"Accept": "text/plain"},
			body:       `{"jsonrpc":"2.0","id":1,"method":"ping"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unsupported protocol version",
			headers:    map[string]string{mcpserver.HeaderKeyProtocolVersion: "1900-01-01"},
			body:       `{"jsonrpc":"2.0","id":1,"method":"ping"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "malformed json",
			body:       `{`,
			wantStatus: http.StatusOK,
			wantError:  true,
		},
		{
			name:       "tools list cursor unsupported",
			body:       `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":"next"}}`,
			wantStatus: http.StatusOK,
			wantError:  true,
		},
	}

	handler := NewStatelessHTTPHandler(Config{})
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !tc.wantError {
				return
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if _, ok := body["error"].(map[string]any); !ok {
				t.Fatalf("response = %v, want JSON-RPC error", body)
			}
		})
	}
}

func TestStatelessHTTPHandlerNotificationReturnsAccepted(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	NewStatelessHTTPHandler(Config{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty notification response", rec.Body.String())
	}
}

func TestNormalizedSessionCatalogInstanceTrimsWhitespace(t *testing.T) {
	t.Parallel()

	if got := normalizedSessionCatalogInstance("  prod  "); got != "prod" {
		t.Fatalf("normalized instance = %q, want prod", got)
	}
	if got := normalizedSessionCatalogInstance("   "); got != "" {
		t.Fatalf("blank instance = %q, want empty", got)
	}
}

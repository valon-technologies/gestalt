package sse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewWriter_SetsHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil writer")
	}

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", got, "text/event-stream")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-cache")
	}
	if got := rec.Header().Get("Connection"); got != "keep-alive" {
		t.Errorf("Connection = %q, want %q", got, "keep-alive")
	}
}

func TestNewWriter_FailsWithoutFlusher(t *testing.T) {
	w, err := NewWriter(noFlushWriter{})
	if err == nil {
		t.Fatal("expected error for non-flusher")
	}
	if w != nil {
		t.Fatal("expected nil writer on error")
	}
	if !strings.Contains(err.Error(), "flushing") {
		t.Errorf("error should mention flushing: %v", err)
	}
}

func TestWriteEvent_Format(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := w.WriteEvent("ping", "hello"); err != nil {
		t.Fatalf("WriteEvent: %v", err)
	}

	got := rec.Body.String()
	want := "event: ping\ndata: hello\n\n"
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestWriteJSON_MarshalsAndWrites(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := map[string]string{"key": "value"}
	if err := w.WriteJSON("data", payload); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	got := rec.Body.String()
	want := "event: data\ndata: {\"key\":\"value\"}\n\n"
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// noFlushWriter implements http.ResponseWriter but not http.Flusher.
type noFlushWriter struct{}

func (noFlushWriter) Header() http.Header        { return http.Header{} }
func (noFlushWriter) Write([]byte) (int, error)   { return 0, nil }
func (noFlushWriter) WriteHeader(int)             {}

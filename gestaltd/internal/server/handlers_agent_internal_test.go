package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteAgentProviderErrorMapsContextDeadlineToUnavailable(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	s := &Server{}

	s.writeAgentProviderError(context.Background(), w, "agent turn", "turn-1", context.DeadlineExceeded)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(w.Body.String(), "agent provider unavailable") {
		t.Fatalf("body = %q, want unavailable message", w.Body.String())
	}
}

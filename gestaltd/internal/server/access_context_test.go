package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireUserCallerRejectsNilPrincipal(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	if err := requireUserCaller(recorder, nil); err == nil {
		t.Fatal("requireUserCaller(nil) error = nil, want error")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

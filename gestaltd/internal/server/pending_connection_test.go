package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectionSuccessURLIncludesDuplicateResult(t *testing.T) {
	if got, want := connectionSuccessURL("slack", true), "/apps?alreadyConnected=true&connected=slack"; got != want {
		t.Fatalf("connectionSuccessURL() = %q, want %q", got, want)
	}
	if got, want := connectionSuccessURL("slack", false), "/apps?connected=slack"; got != want {
		t.Fatalf("connectionSuccessURL() = %q, want %q", got, want)
	}
}

func TestConnectionCompletePagePostsDuplicateResult(t *testing.T) {
	response := httptest.NewRecorder()
	writeConnectionCompletePage(response, "slack", true)

	body := response.Body.String()
	if !strings.Contains(body, `alreadyConnected: true`) {
		t.Fatalf("completion page did not post duplicate result: %s", body)
	}
	if !strings.Contains(body, `/apps?alreadyConnected=true&amp;connected=slack`) {
		t.Fatalf("completion page did not include duplicate fallback URL: %s", body)
	}
}

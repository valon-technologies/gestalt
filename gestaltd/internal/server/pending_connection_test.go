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
	if !strings.Contains(body, `<title>slack already connected</title>`) {
		t.Fatalf("completion page did not identify duplicate connection: %s", body)
	}
	if !strings.Contains(body, "This account was already connected.") {
		t.Fatalf("completion page did not explain duplicate connection: %s", body)
	}
	if !strings.Contains(body, `alreadyConnected: true`) {
		t.Fatalf("completion page did not post duplicate result: %s", body)
	}
	if !strings.Contains(body, `/apps?alreadyConnected=true&amp;connected=slack`) {
		t.Fatalf("completion page did not include duplicate fallback URL: %s", body)
	}
}

func TestConnectionCompletePagePostsNewConnectionResult(t *testing.T) {
	response := httptest.NewRecorder()
	writeConnectionCompletePage(response, "slack", false)

	body := response.Body.String()
	if !strings.Contains(body, `<title>slack connected</title>`) {
		t.Fatalf("completion page did not identify new connection: %s", body)
	}
	if strings.Contains(body, "already connected") {
		t.Fatalf("completion page used duplicate copy for new connection: %s", body)
	}
	if !strings.Contains(body, `alreadyConnected: false`) {
		t.Fatalf("completion page did not post new-connection result: %s", body)
	}
	if !strings.Contains(body, `/apps?connected=slack`) {
		t.Fatalf("completion page did not include new-connection fallback URL: %s", body)
	}
}

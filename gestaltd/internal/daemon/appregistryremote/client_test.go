package appregistryremote

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	t.Parallel()
	input := "Authorization: Bearer super-secret-token api_token=abc123"
	got := redactSecrets(input)
	if strings.Contains(got, "super-secret-token") || strings.Contains(got, "abc123") {
		t.Fatalf("redactSecrets() = %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("redactSecrets() = %q, want redacted markers", got)
	}
}

func TestAdminRegistryURL(t *testing.T) {
	t.Parallel()
	got := adminRegistryURL("https://valon.tools", "g-issues")
	want := "https://valon.tools/apps/g-issues/admin/registry"
	if got != want {
		t.Fatalf("adminRegistryURL() = %q, want %q", got, want)
	}
}

func TestParseAPIErrorUsesJSONMessage(t *testing.T) {
	t.Parallel()
	err := parseAPIError(403, []byte(`{"error":"forbidden publish"}`))
	if err == nil || !strings.Contains(err.Error(), "forbidden publish") {
		t.Fatalf("parseAPIError() = %v", err)
	}
	if strings.Contains(err.Error(), "forbidden publish") && strings.Contains(err.Error(), "Bearer") {
		t.Fatalf("parseAPIError leaked auth details: %v", err)
	}
}

func TestClientRequiresAuth(t *testing.T) {
	t.Parallel()
	client := &Client{BaseURL: "https://valon.tools", Token: ""}
	_, err := client.CreateSession(t.Context(), "demo", &CreateSessionRequest{})
	if err == nil || !strings.Contains(err.Error(), "credentials are required") {
		t.Fatalf("CreateSession() = %v, want credentials error", err)
	}
}

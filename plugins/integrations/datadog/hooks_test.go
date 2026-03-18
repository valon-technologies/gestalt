package datadog

import (
	"testing"
)

func TestDatadogTokenParser(t *testing.T) {
	t.Parallel()

	t.Run("valid keys", func(t *testing.T) {
		t.Parallel()
		authHeader, headers, err := datadogTokenParser(`{"api_key":"ak123","app_key":"apk456"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authHeader != "" {
			t.Errorf("expected empty auth header, got %q", authHeader)
		}
		if headers["DD-API-KEY"] != "ak123" {
			t.Errorf("DD-API-KEY = %q, want %q", headers["DD-API-KEY"], "ak123")
		}
		if headers["DD-APPLICATION-KEY"] != "apk456" {
			t.Errorf("DD-APPLICATION-KEY = %q, want %q", headers["DD-APPLICATION-KEY"], "apk456")
		}
	})

	t.Run("missing app_key", func(t *testing.T) {
		t.Parallel()
		if _, _, err := datadogTokenParser(`{"api_key":"ak123"}`); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		t.Parallel()
		if _, _, err := datadogTokenParser("not-json"); err == nil {
			t.Fatal("expected error")
		}
	})
}

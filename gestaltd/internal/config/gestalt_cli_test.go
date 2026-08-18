package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveGestaltCLIURLFromEnv(t *testing.T) {
	t.Setenv("GESTALT_URL", "valon.tools")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got, err := ResolveGestaltCLIURL()
	if err != nil {
		t.Fatalf("ResolveGestaltCLIURL() error = %v", err)
	}
	if got != "https://valon.tools" {
		t.Fatalf("ResolveGestaltCLIURL() = %q", got)
	}
}

func TestResolveGestaltCLITokenPrefersAPIKey(t *testing.T) {
	t.Setenv("GESTALT_API_KEY", "env-token")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "gestalt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gestalt", "credentials.json"), []byte(`{"api_token":"file-token"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveGestaltCLIToken()
	if err != nil {
		t.Fatalf("ResolveGestaltCLIToken() error = %v", err)
	}
	if got != "env-token" {
		t.Fatalf("ResolveGestaltCLIToken() = %q, want env precedence", got)
	}
}

func TestResolveGestaltCLITokenFromCredentialsFile(t *testing.T) {
	t.Setenv("GESTALT_API_KEY", "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "gestalt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gestalt", "credentials.json"), []byte(`{"api_token":"file-token"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveGestaltCLIToken()
	if err != nil {
		t.Fatalf("ResolveGestaltCLIToken() error = %v", err)
	}
	if got != "file-token" {
		t.Fatalf("ResolveGestaltCLIToken() = %q", got)
	}
}

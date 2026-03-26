package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type DevConfigOptions struct {
	Port         int
	BaseURL      string
	DatabasePath string
	ExtraYAML    string
}

func WriteDevConfig(t *testing.T, dir string, opts DevConfigOptions) string {
	t.Helper()

	port := opts.Port
	if port == 0 {
		port = FreePort(t)
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	}

	dbPath := opts.DatabasePath
	if dbPath == "" {
		dbPath = filepath.Join(dir, "gestalt.db")
	}

	var b strings.Builder
	fmt.Fprintf(&b, `auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: %s/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: %d
  base_url: %s
  dev_mode: true
  encryption_key: test-encryption-key
`, baseURL, dbPath, port, baseURL)

	if extra := strings.TrimSpace(opts.ExtraYAML); extra != "" {
		b.WriteByte('\n')
		b.WriteString(extra)
		b.WriteByte('\n')
	}

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatalf("write test config: %v", err)
	}

	return path
}

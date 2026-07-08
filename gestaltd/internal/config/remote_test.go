package config

import (
	"strings"
	"testing"
)

func TestNormalizeRemoteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"https://valon.tools/", "https://valon.tools"},
		{"  https://valon.tools/  ", "https://valon.tools"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := NormalizeRemoteURL(tc.in); got != tc.want {
			t.Fatalf("NormalizeRemoteURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadConfigRemoteFields(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v8
server:
  remote: https://valon.tools/
  remoteToken: ${REMOTE_TOKEN}
  providers:
    identity: local
    indexeddb: sqlite
  encryptionKey: server-key
providers:
  identity:
    local:
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/auth/google/v1.0.0/provider-release.yaml
  indexeddb:
    sqlite:
      source:
        path: ./providers/indexeddb/sqlite
apps:
  service-a:
    source:
      path: /tmp/manifest.yaml
`)

	cfg, err := LoadWithLookup(path, func(key string) (string, bool) {
		if key == "REMOTE_TOKEN" {
			return "gst_api_test", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("LoadWithLookup: %v", err)
	}
	if cfg.Server.Remote != "https://valon.tools" {
		t.Fatalf("Server.Remote = %q, want https://valon.tools", cfg.Server.Remote)
	}
	if cfg.Server.RemoteToken != "gst_api_test" {
		t.Fatalf("Server.RemoteToken = %q, want gst_api_test", cfg.Server.RemoteToken)
	}
}

func TestLoadConfigRemoteRequiresToken(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v8
server:
  remote: https://valon.tools
  providers:
    identity: local
    indexeddb: sqlite
  encryptionKey: server-key
providers:
  identity:
    local:
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/auth/google/v1.0.0/provider-release.yaml
  indexeddb:
    sqlite:
      source:
        path: ./providers/indexeddb/sqlite
apps:
  service-a:
    source:
      path: /tmp/manifest.yaml
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load: expected error for remote without token, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "server.remoteToken") {
		t.Fatalf("Load error = %q", got)
	}
}

func TestLoadConfigRemoteLayeredOverrides(t *testing.T) {
	t.Parallel()

	basePath := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v8
server:
  remote: https://base.example/
  remoteToken: base-token
  providers:
    identity: local
    indexeddb: sqlite
  encryptionKey: server-key
providers:
  identity:
    local:
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/auth/google/v1.0.0/provider-release.yaml
  indexeddb:
    sqlite:
      source:
        path: ./providers/indexeddb/sqlite
apps:
  service-a:
    source:
      path: /tmp/manifest.yaml
`)
	overridePath := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v8
server:
  remoteToken: override-token
`)

	cfg, err := LoadWithLookupPaths([]string{basePath, overridePath}, nil)
	if err != nil {
		t.Fatalf("LoadWithLookupPaths: %v", err)
	}
	if cfg.Server.Remote != "https://base.example" {
		t.Fatalf("Remote = %q, want https://base.example", cfg.Server.Remote)
	}
	if cfg.Server.RemoteToken != "override-token" {
		t.Fatalf("RemoteToken = %q, want override-token", cfg.Server.RemoteToken)
	}
}

func TestApplyServerRemoteOverrides(t *testing.T) {
	t.Parallel()

	t.Run("cli overrides config", func(t *testing.T) {
		server := ServerConfig{
			Remote:      "https://old.example/",
			RemoteToken: "old-token",
		}
		if err := ApplyServerRemoteOverrides(&server, "https://valon.tools/", "new-token"); err != nil {
			t.Fatalf("ApplyServerRemoteOverrides: %v", err)
		}
		if server.Remote != "https://valon.tools" {
			t.Fatalf("Remote = %q", server.Remote)
		}
		if server.RemoteToken != "new-token" {
			t.Fatalf("RemoteToken = %q", server.RemoteToken)
		}
	})

	t.Run("missing token when remote configured", func(t *testing.T) {
		server := ServerConfig{Remote: "https://valon.tools"}
		if err := ApplyServerRemoteOverrides(&server, "", ""); err == nil {
			t.Fatal("ApplyServerRemoteOverrides: expected error, got nil")
		}
	})
}

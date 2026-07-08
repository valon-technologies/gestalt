package config

import (
	"strings"
	"testing"
)

func TestLoadServerRemoteConfig(t *testing.T) {
	t.Parallel()

	t.Run("loads remote url and token", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
server:
  remote: https://valon.tools/
  remoteToken: gst_api_test
  encryptionKey: server-key
  providers:
    indexeddb: sqlite
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/indexeddb/sqlite
      config:
        path: ./gestalt.db
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Server.Remote != "https://valon.tools" {
			t.Fatalf("Server.Remote = %q, want %q", cfg.Server.Remote, "https://valon.tools")
		}
		if cfg.Server.RemoteToken != "gst_api_test" {
			t.Fatalf("Server.RemoteToken = %q, want %q", cfg.Server.RemoteToken, "gst_api_test")
		}
	})

	t.Run("layers remote config left to right", func(t *testing.T) {
		t.Parallel()
		basePath := mustWriteConfigFile(t, `
server:
  remote: https://old.example
  remoteToken: old-token
  encryptionKey: server-key
  providers:
    indexeddb: sqlite
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/indexeddb/sqlite
      config:
        path: ./gestalt.db
`)
		overridePath := mustWriteConfigFile(t, `
server:
  remote: https://valon.tools
  remoteToken: new-token
`)
		cfg, err := LoadPaths([]string{basePath, overridePath})
		if err != nil {
			t.Fatalf("LoadPaths: %v", err)
		}
		if cfg.Server.Remote != "https://valon.tools" {
			t.Fatalf("Server.Remote = %q, want %q", cfg.Server.Remote, "https://valon.tools")
		}
		if cfg.Server.RemoteToken != "new-token" {
			t.Fatalf("Server.RemoteToken = %q, want %q", cfg.Server.RemoteToken, "new-token")
		}
	})
}

func TestValidateServerRemote(t *testing.T) {
	t.Parallel()

	cfg := validRuntimeConfig(t)
	cfg.Server.Remote = "https://valon.tools"
	cfg.Server.RemoteToken = ""
	if err := ValidateStructure(cfg); err == nil {
		t.Fatal("expected missing remote token error")
	} else if !strings.Contains(err.Error(), "server.remoteToken is required") {
		t.Fatalf("error = %v, want remoteToken required", err)
	}

	cfg.Server.RemoteToken = "gst_api_test"
	if err := ValidateStructure(cfg); err != nil {
		t.Fatalf("ValidateStructure: %v", err)
	}
}

func TestApplyRemoteOverrides(t *testing.T) {
	t.Parallel()

	cfg := validRuntimeConfig(t)
	cfg.ApplyRemoteOverrides("https://valon.tools/", "gst_api_test")
	if cfg.Server.Remote != "https://valon.tools" {
		t.Fatalf("Server.Remote = %q, want normalized URL", cfg.Server.Remote)
	}
	if cfg.Server.RemoteToken != "gst_api_test" {
		t.Fatalf("Server.RemoteToken = %q", cfg.Server.RemoteToken)
	}
}

func validRuntimeConfig(t *testing.T) *Config {
	t.Helper()
	path := mustWriteConfigFile(t, `
server:
  encryptionKey: server-key
  providers:
    indexeddb: sqlite
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/indexeddb/sqlite
      config:
        path: ./gestalt.db
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

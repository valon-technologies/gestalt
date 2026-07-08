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

func TestLoadLocalRemoteOverlayWithoutToken(t *testing.T) {
	t.Parallel()

	basePath := mustWriteConfigFile(t, `
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
	localYAML := `
server:
  remote: https://valon.tools
`
	localPath := mustWriteConfigFile(t, localYAML)
	cfg, err := LoadPaths([]string{basePath, localPath})
	if err != nil {
		t.Fatalf("LoadPaths: %v", err)
	}
	if cfg.Server.Remote != "https://valon.tools" {
		t.Fatalf("Server.Remote = %q", cfg.Server.Remote)
	}
	if cfg.Server.RemoteToken != "" {
		t.Fatalf("local overlay must not set remoteToken, got %q", cfg.Server.RemoteToken)
	}
	if strings.Contains(localYAML, "gst_api_") {
		t.Fatal("local overlay must not contain gst_api_ token literals")
	}
	if err := ValidateServerRemoteCredentials(&cfg.Server); err == nil {
		t.Fatal("expected credentials validation to require token")
	}
}

func TestValidateServerRemote(t *testing.T) {
	t.Parallel()

	cfg := validRuntimeConfig(t)
	cfg.Server.Remote = "https://valon.tools"
	cfg.Server.RemoteToken = ""
	if err := ValidateStructure(cfg); err != nil {
		t.Fatalf("ValidateStructure with url-only remote: %v", err)
	}
	if err := ValidateServerRemoteCredentials(&cfg.Server); err == nil {
		t.Fatal("expected missing remote token error")
	} else if !strings.Contains(err.Error(), "server.remoteToken is required") {
		t.Fatalf("error = %v, want remoteToken required", err)
	}

	cfg.Server.RemoteToken = "gst_api_test"
	if err := ValidateServerRemoteCredentials(&cfg.Server); err != nil {
		t.Fatalf("ValidateServerRemoteCredentials: %v", err)
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

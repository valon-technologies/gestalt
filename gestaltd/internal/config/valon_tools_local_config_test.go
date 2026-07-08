package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func valonToolsLocalConfigPath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../docs/examples/valon-tools/deploy/local/config.yaml"))
}

func TestValonToolsLocalConfigHasNoCommittedToken(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(valonToolsLocalConfigPath(t))
	if err != nil {
		t.Fatalf("read local config: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "remoteToken") {
		t.Fatalf("local config must not set server.remoteToken:\n%s", text)
	}
	if strings.Contains(text, "gst_api_") {
		t.Fatalf("local config must not contain gst_api_ token literals:\n%s", text)
	}
}

func TestValonToolsLocalConfigLayersWithRemoteToken(t *testing.T) {
	t.Parallel()

	basePath := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v8
server:
  encryptionKey: test-encryption-key
`)
	localPath := valonToolsLocalConfigPath(t)
	tokenPath := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v8
server:
  remoteToken: gst_api_from_cli
`)

	cfg, err := LoadPaths([]string{basePath, localPath, tokenPath})
	if err != nil {
		t.Fatalf("LoadPaths: %v", err)
	}
	if got := cfg.Server.Remote; got != "https://valon.tools" {
		t.Fatalf("server.remote = %q, want https://valon.tools", got)
	}
	if got := cfg.Server.RemoteToken; got != "gst_api_from_cli" {
		t.Fatalf("server.remoteToken = %q", got)
	}
}

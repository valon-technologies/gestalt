package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func valonToolsLocalConfigPath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../docs/examples/valon-tools/deploy/local/config.yaml"))
}

func readValonToolsLocalConfig(t *testing.T) []byte {
	t.Helper()

	raw, err := os.ReadFile(valonToolsLocalConfigPath(t))
	if err != nil {
		t.Fatalf("read local config: %v", err)
	}
	return raw
}

func TestValonToolsLocalConfigHasNoCommittedToken(t *testing.T) {
	t.Parallel()

	text := string(readValonToolsLocalConfig(t))
	if strings.Contains(text, "remoteToken") {
		t.Fatalf("local config must not set server.remoteToken:\n%s", text)
	}
	if strings.Contains(text, "gst_api_") {
		t.Fatalf("local config must not contain gst_api_ token literals:\n%s", text)
	}
}

func TestValonToolsLocalConfigDeclaresRemoteURL(t *testing.T) {
	t.Parallel()

	var doc struct {
		APIVersion string `yaml:"apiVersion"`
		Server     struct {
			Remote string `yaml:"remote"`
		} `yaml:"server"`
	}
	if err := yaml.Unmarshal(readValonToolsLocalConfig(t), &doc); err != nil {
		t.Fatalf("unmarshal local config: %v", err)
	}
	if doc.APIVersion != "gestaltd.config/v8" {
		t.Fatalf("apiVersion = %q, want gestaltd.config/v8", doc.APIVersion)
	}
	if doc.Server.Remote != "https://valon.tools" {
		t.Fatalf("server.remote = %q, want https://valon.tools", doc.Server.Remote)
	}
}

func valonToolsCLIDocPath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../docs/content/reference/cli.mdx"))
}

func TestValonToolsDocumentedServeUsesRemoteTokenFlag(t *testing.T) {
	t.Parallel()

	cliDoc, err := os.ReadFile(valonToolsCLIDocPath(t))
	if err != nil {
		t.Fatalf("read cli docs: %v", err)
	}
	text := string(cliDoc)
	if !strings.Contains(text, `--remote-token "$GESTALT_API_KEY"`) {
		t.Fatalf("cli docs must document --remote-token \"$GESTALT_API_KEY\"")
	}
}

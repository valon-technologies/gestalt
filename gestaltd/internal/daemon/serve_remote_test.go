package daemon

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestApplyServeConfigOverridesRemoteFlags(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	if err := applyServeConfigOverrides(cfg, serveConfigOverrides{
		Remote:      "https://valon.tools/",
		RemoteToken: "gst_api_cli_token",
	}); err != nil {
		t.Fatalf("applyServeConfigOverrides: %v", err)
	}
	if got := cfg.Server.Remote; got != "https://valon.tools" {
		t.Fatalf("server.remote = %q", got)
	}
	if got := cfg.Server.RemoteToken; got != "gst_api_cli_token" {
		t.Fatalf("server.remoteToken = %q", got)
	}
}

func TestApplyServeConfigOverridesRequiresTokenWhenRemoteSet(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	err := applyServeConfigOverrides(cfg, serveConfigOverrides{
		Remote: "https://valon.tools",
	})
	if err == nil {
		t.Fatal("applyServeConfigOverrides: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "server.remoteToken is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

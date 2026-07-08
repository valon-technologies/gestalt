package daemon

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestFinalizeRemoteServeConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Server: config.ServerConfig{Remote: "https://valon.tools"}}
	err := finalizeRemoteServeConfig(cfg)
	if err == nil {
		t.Fatal("finalizeRemoteServeConfig: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "server.remoteToken is required") {
		t.Fatalf("unexpected error: %v", err)
	}

	config.ApplyRemoteOverrides(cfg, config.RemoteOverrides{Token: "gst_api_cli_token"})
	if err := finalizeRemoteServeConfig(cfg); err != nil {
		t.Fatalf("finalizeRemoteServeConfig with CLI token: %v", err)
	}
}

func TestLogConfigSummaryMasksRemoteToken(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "gst_api_secret_token",
		},
	}
	if got := maskSecret(cfg.Server.RemoteToken); got == "gst_api_secret_token" {
		t.Fatalf("maskSecret leaked token = %q", got)
	}
	if got := maskSecret(cfg.Server.RemoteToken); got != "***" {
		t.Fatalf("maskSecret = %q, want ***", got)
	}
}

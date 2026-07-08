package daemon

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestApplyServeRemoteOverrides(t *testing.T) {
	t.Parallel()

	t.Run("cli overrides config", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Server: config.ServerConfig{
				Remote:      "https://old.example.test/",
				RemoteToken: "old-token",
			},
		}
		if err := config.ApplyServeRemoteOverrides(cfg, "https://valon.tools/", "new-token"); err != nil {
			t.Fatalf("ApplyServeRemoteOverrides: %v", err)
		}
		if got := cfg.Server.Remote; got != "https://valon.tools" {
			t.Fatalf("server.remote = %q, want %q", got, "https://valon.tools")
		}
		if got := cfg.Server.RemoteToken; got != "new-token" {
			t.Fatalf("server.remoteToken = %q, want %q", got, "new-token")
		}
	})

	t.Run("missing token when remote configured", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Server: config.ServerConfig{
				Remote: "https://valon.tools",
			},
		}
		err := config.ApplyServeRemoteOverrides(cfg, "", "")
		if err == nil {
			t.Fatal("ApplyServeRemoteOverrides: expected error, got nil")
		}
		if !strings.Contains(err.Error(), "server.remoteToken is required when server.remote is set") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("token from cli satisfies remote from config", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Server: config.ServerConfig{
				Remote: "https://valon.tools",
			},
		}
		if err := config.ApplyServeRemoteOverrides(cfg, "", "cli-token"); err != nil {
			t.Fatalf("ApplyServeRemoteOverrides: %v", err)
		}
		if got := cfg.Server.RemoteToken; got != "cli-token" {
			t.Fatalf("server.remoteToken = %q, want %q", got, "cli-token")
		}
	})
}

func TestLogConfigSummaryMasksRemoteToken(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	logConfigSummary(nil, &config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "super-secret-token",
		},
	})

	out := buf.String()
	if strings.Contains(out, "super-secret-token") {
		t.Fatalf("log output leaked remote token: %s", out)
	}
	if !strings.Contains(out, "server_remote=https://valon.tools") {
		t.Fatalf("log output missing remote URL: %s", out)
	}
	if !strings.Contains(out, "server_remote_token=***") {
		t.Fatalf("log output missing masked remote token: %s", out)
	}
}

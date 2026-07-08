package daemon

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestApplyServeRemoteConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.ApplyRemoteOverrides("", "")
	if err := applyServeRemoteConfig(cfg, "https://valon.tools/", "gst_api_test"); err != nil {
		t.Fatalf("applyServeRemoteConfig: %v", err)
	}
	if cfg.Server.Remote != "https://valon.tools" {
		t.Fatalf("Server.Remote = %q, want normalized URL", cfg.Server.Remote)
	}
	if cfg.Server.RemoteToken != "gst_api_test" {
		t.Fatalf("Server.RemoteToken = %q", cfg.Server.RemoteToken)
	}

	cfg = &config.Config{}
	cfg.Server.Remote = "https://valon.tools"
	if err := applyServeRemoteConfig(cfg, "", ""); err == nil {
		t.Fatal("expected missing remote token error")
	} else if !strings.Contains(err.Error(), "server.remoteToken is required") {
		t.Fatalf("error = %v, want remoteToken required", err)
	}
}

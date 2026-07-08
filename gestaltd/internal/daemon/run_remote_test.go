package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/testutil/remotefake"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
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

func TestApplyServeRemoteConfigDialsFakeRemote(t *testing.T) {
	t.Parallel()

	remoteSrv, err := remotefake.Start()
	if err != nil {
		t.Fatalf("remotefake.Start: %v", err)
	}
	t.Cleanup(func() { _ = remoteSrv.Close() })

	cfg := &config.Config{}
	if err := applyServeRemoteConfig(cfg, remoteSrv.BaseURL(), "gst_api_test"); err != nil {
		t.Fatalf("applyServeRemoteConfig: %v", err)
	}
	clientSet, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:   cfg.Server.Remote,
		Token: cfg.Server.RemoteToken,
	})
	if err != nil {
		t.Fatalf("remote.NewClientSet: %v", err)
	}
	defer func() { _ = clientSet.Close() }()

	if _, err := clientSet.App.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "linear",
		Operation: "issues.list",
	}); err != nil {
		t.Fatalf("App.Invoke: %v", err)
	}
	if _, calls := remoteSrv.App.Snapshot(); len(calls) != 1 || calls[0].Auth != "Bearer gst_api_test" {
		t.Fatalf("remote calls = %#v", calls)
	}
}

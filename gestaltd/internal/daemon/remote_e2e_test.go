package daemon

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remotetest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestPlan6RemoteClientSetDialsFakeGestaltd(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote:      fake.URL(),
			RemoteToken: fake.Token,
		},
		Apps: map[string]*config.ProviderEntry{
			"linear": {},
		},
	}

	plan := bootstrap.NewPlacementPlan(cfg)
	if !plan.ShouldRouteRemote(bootstrap.RemoteProviderKindApp, "linear") {
		t.Fatal("expected linear to route remote")
	}

	clients, err := fake.NewClientSet(context.Background())
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	defer func() { _ = clients.Close() }()

	_, err = clients.App.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "linear",
		Operation: "issues.list",
	})
	if err != nil {
		t.Fatalf("App.Invoke: %v", err)
	}

	if len(fake.Recorder.AppInvokesSnapshot()) != 1 {
		t.Fatalf("remote invokes = %d, want 1", len(fake.Recorder.AppInvokesSnapshot()))
	}
}

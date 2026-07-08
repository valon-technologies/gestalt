package bootstrap

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
)

func prepareRemoteClients(ctx context.Context, cfg *config.Config) (*remote.ClientSet, error) {
	if cfg == nil || !cfg.Server.HasRemote() {
		return nil, nil
	}
	clientSet, err := remote.NewClientSet(ctx, remote.Config{
		URL:   cfg.Server.Remote,
		Token: cfg.Server.RemoteToken,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap: remote client: %w", err)
	}
	return clientSet, nil
}

func closeRemoteClients(clientSet *remote.ClientSet) error {
	if clientSet == nil || clientSet.Close == nil {
		return nil
	}
	return clientSet.Close()
}

func shouldRouteRemoteApp(deps Deps, name string) bool {
	return deps.Placement != nil && deps.Placement.ShouldRouteRemote(RemoteProviderKindApp, name)
}

func shouldRouteRemoteAgent(deps Deps, name string) bool {
	return deps.Placement != nil && deps.Placement.ShouldRouteRemote(RemoteProviderKindAgent, name)
}

func shouldRouteRemoteWorkflow(deps Deps, name string) bool {
	return deps.Placement != nil && deps.Placement.ShouldRouteRemote(RemoteProviderKindWorkflow, name)
}

func shouldRouteRemoteIndexedDB(deps Deps, name string) bool {
	return deps.Placement != nil && deps.Placement.ShouldRouteRemote(RemoteProviderKindIndexedDB, name)
}

package bootstrap

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/services/agents"
	indexeddbpkg "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/workflows"
)

type remoteProviderPublishResult struct {
	extraIndexedDBs []indexeddb.IndexedDB
}

func publishRemoteProviders(ctx context.Context, cfg *config.Config, deps *Deps) (remoteProviderPublishResult, error) {
	var out remoteProviderPublishResult
	if cfg == nil || deps.Placement == nil || !deps.Placement.RemoteConfigured() {
		return out, nil
	}
	clients := deps.RemoteClients
	if clients == nil {
		var err error
		clients, err = remote.NewClientSet(ctx, remote.Config{
			URL:   cfg.Server.Remote,
			Token: cfg.Server.RemoteToken,
		})
		if err != nil {
			return out, fmt.Errorf("bootstrap: remote client: %w", err)
		}
		deps.RemoteClients = clients
	}

	selectedIndexedDB, _, err := cfg.SelectedIndexedDBProvider()
	if err != nil {
		return out, err
	}

	for name := range cfg.Providers.Agent {
		if !deps.Placement.ShouldRouteRemote(RemoteProviderKindAgent, name) {
			continue
		}
		provider, err := agents.NewPublicRemote(clients.Agent, name)
		if err != nil {
			return out, fmt.Errorf("bootstrap: remote agent provider %q: %w", name, err)
		}
		if deps.AgentRuntime != nil {
			deps.AgentRuntime.PublishProvider(name, provider)
		}
	}

	for name := range cfg.Providers.Workflow {
		if !deps.Placement.ShouldRouteRemote(RemoteProviderKindWorkflow, name) {
			continue
		}
		provider, err := workflows.NewPublicRemote(clients.Workflow, name)
		if err != nil {
			return out, fmt.Errorf("bootstrap: remote workflow provider %q: %w", name, err)
		}
		if deps.WorkflowRuntime != nil {
			deps.WorkflowRuntime.PublishProvider(name, provider)
		}
	}

	if deps.IndexedDBs == nil {
		deps.IndexedDBs = map[string]indexeddb.IndexedDB{}
	}
	for name := range cfg.Providers.IndexedDB {
		if name == selectedIndexedDB {
			continue
		}
		if !deps.Placement.ShouldRouteRemote(RemoteProviderKindIndexedDB, name) {
			continue
		}
		store, err := indexeddbpkg.NewPublicRemote(clients.IndexedDB, name)
		if err != nil {
			return out, fmt.Errorf("bootstrap: remote indexeddb provider %q: %w", name, err)
		}
		store = metricutil.InstrumentIndexedDB(store, name)
		deps.IndexedDBs[name] = store
		out.extraIndexedDBs = append(out.extraIndexedDBs, store)
	}

	return out, nil
}

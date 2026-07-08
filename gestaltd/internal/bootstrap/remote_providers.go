package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/agents"
	indexeddbpkg "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/workflows"
)

func publishRemoteProviders(ctx context.Context, cfg *config.Config, deps *Deps) ([]indexeddb.IndexedDB, error) {
	if cfg == nil || strings.TrimSpace(cfg.Server.Remote) == "" {
		return nil, nil
	}
	clients := deps.RemoteClients
	if clients == nil {
		return nil, fmt.Errorf("bootstrap: remote client is required")
	}

	selectedIndexedDB, _, err := cfg.SelectedIndexedDBProvider()
	if err != nil {
		return nil, err
	}

	for name, entry := range cfg.Providers.Agent {
		if providerBuildsLocal(cfg, entry) {
			continue
		}
		provider, err := agents.NewRemote(ctx, agents.RemoteConfig{Client: clients.Agent, Name: name})
		if err != nil {
			return nil, fmt.Errorf("bootstrap: remote agent provider %q: %w", name, err)
		}
		deps.AgentRuntime.PublishProvider(name, provider)
	}

	for name, entry := range cfg.Providers.Workflow {
		if providerBuildsLocal(cfg, entry) {
			continue
		}
		provider, err := workflows.NewRemote(ctx, workflows.RemoteConfig{Client: clients.Workflow, Name: name})
		if err != nil {
			return nil, fmt.Errorf("bootstrap: remote workflow provider %q: %w", name, err)
		}
		deps.WorkflowRuntime.PublishProvider(name, provider)
	}

	var extraIndexedDBs []indexeddb.IndexedDB
	for name, entry := range cfg.Providers.IndexedDB {
		if name == selectedIndexedDB || providerBuildsLocal(cfg, entry) {
			continue
		}
		store, err := indexeddbpkg.NewPublicRemote(clients.IndexedDB, name)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: remote indexeddb provider %q: %w", name, err)
		}
		store = metricutil.InstrumentIndexedDB(store, name)
		deps.IndexedDBs[name] = store
		extraIndexedDBs = append(extraIndexedDBs, store)
	}

	return extraIndexedDBs, nil
}

package bootstrap

import (
	"context"
	"log/slog"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
)

func dialRemoteClients(ctx context.Context, cfg *config.Config) (*remote.ClientSet, error) {
	if cfg == nil {
		return nil, nil
	}
	cfg.Server.NormalizeRemote()
	if strings.TrimSpace(cfg.Server.Remote) == "" {
		return nil, nil
	}
	return remote.NewClientSet(ctx, remote.Config{
		URL:   cfg.Server.Remote,
		Token: cfg.Server.RemoteToken,
	})
}

func registerRemoteAgents(cfg *config.Config, deps Deps) {
	if cfg == nil || deps.AgentRuntime == nil || deps.Placement == nil || deps.RemoteClients == nil || deps.RemoteClients.Agent == nil {
		return
	}
	for name, entry := range cfg.Providers.Agent {
		name = strings.TrimSpace(name)
		if name == "" || entry == nil {
			continue
		}
		if !deps.Placement.ShouldRouteRemote(RemoteProviderKindAgent, name) {
			continue
		}
		provider := agentservice.NewGestaltRemoteProvider(deps.RemoteClients.Agent, name)
		if provider == nil {
			continue
		}
		deps.AgentRuntime.PublishProvider(name, provider)
		slog.Info("registered remote agent provider", "provider", name)
	}
}

func registerRemoteWorkflows(cfg *config.Config, deps Deps) {
	if cfg == nil || deps.WorkflowRuntime == nil || deps.Placement == nil || deps.RemoteClients == nil || deps.RemoteClients.Workflow == nil {
		return
	}
	for name, entry := range cfg.Providers.Workflow {
		name = strings.TrimSpace(name)
		if name == "" || entry == nil {
			continue
		}
		if !deps.Placement.ShouldRouteRemote(RemoteProviderKindWorkflow, name) {
			continue
		}
		provider := workflowservice.NewGestaltRemoteProvider(deps.RemoteClients.Workflow, name)
		if provider == nil {
			continue
		}
		deps.WorkflowRuntime.PublishProvider(name, provider)
		slog.Info("registered remote workflow provider", "provider", name)
	}
}

func remoteIndexedDBBindings(cfg *config.Config, placement *PlacementPlan, clients *remote.ClientSet, selectedName string) map[string]indexeddb.IndexedDB {
	if cfg == nil || placement == nil || clients == nil || clients.IndexedDB == nil {
		return nil
	}
	bindings := map[string]indexeddb.IndexedDB{}
	for name, entry := range cfg.Providers.IndexedDB {
		name = strings.TrimSpace(name)
		if name == "" || entry == nil || name == selectedName {
			continue
		}
		if !placement.ShouldRouteRemote(RemoteProviderKindIndexedDB, name) {
			continue
		}
		provider := indexeddbservice.NewGestaltRemoteProvider(clients.IndexedDB)
		if provider == nil {
			continue
		}
		bindings[name] = provider
		slog.Info("registered remote indexeddb provider", "provider", name)
	}
	if len(bindings) == 0 {
		return nil
	}
	return bindings
}

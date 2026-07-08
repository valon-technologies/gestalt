package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remoteroute"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
)

func dialRemoteClients(ctx context.Context, cfg *config.Config) (*remote.ClientSet, error) {
	return remoteroute.Dial(ctx, cfg)
}

func publishRemoteHostProviders(cfg *config.Config, deps Deps) error {
	if cfg == nil || deps.Placement == nil || deps.RemoteClients == nil {
		return nil
	}
	clients := deps.RemoteClients

	for name := range cfg.Providers.Agent {
		name = strings.TrimSpace(name)
		if name == "" || !deps.Placement.ShouldRouteRemote(RemoteProviderKindAgent, name) {
			continue
		}
		provider, err := agentservice.NewGestaltRemoteProvider(name, clients.Agent)
		if err != nil {
			return fmt.Errorf("remote agent %q: %w", name, err)
		}
		if deps.AgentRuntime != nil {
			deps.AgentRuntime.PublishProvider(name, provider)
		}
		slog.Info("registered remote agent provider", "provider", name)
	}

	for name := range cfg.Providers.Workflow {
		name = strings.TrimSpace(name)
		if name == "" || !deps.Placement.ShouldRouteRemote(RemoteProviderKindWorkflow, name) {
			continue
		}
		provider, err := workflowservice.NewGestaltRemoteProvider(name, clients.Workflow)
		if err != nil {
			return fmt.Errorf("remote workflow %q: %w", name, err)
		}
		if deps.WorkflowRuntime != nil {
			deps.WorkflowRuntime.PublishProvider(name, provider)
		}
		slog.Info("registered remote workflow provider", "provider", name)
	}

	return nil
}

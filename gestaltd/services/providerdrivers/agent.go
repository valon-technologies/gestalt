package providerdrivers

import (
	"context"
	"fmt"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"gopkg.in/yaml.v3"
)

func AgentFactory(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps AgentDeps) (coreagent.Provider, error) {
	var cfg componentprovider.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("agent provider: parsing config: %w", err)
	}
	prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindAgent,
		Subject:              "agent provider",
		SourceMissingMessage: "no Go, Rust, Python, or TypeScript agent provider source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig

	return agentservice.NewExecutable(ctx, agentservice.ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		Egress:       cfg.EgressPolicy(deps.EgressDefaultAction),
		HostBinary:   cfg.HostBinary,
		Cleanup:      prepared.Cleanup,
		HostServices: hostServices,
		Name:         name,
		Telemetry:    deps.Telemetry,
	})
}

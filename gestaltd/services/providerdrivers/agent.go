package providerdrivers

import (
	"context"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"gopkg.in/yaml.v3"
)

func AgentFactory(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps AgentDeps) (coreagent.Provider, error) {
	prepared, err := prepareYAMLExecution(node, componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindAgent,
		Subject:              "agent provider",
		SourceMissingMessage: "no Go, Rust, Python, or TypeScript agent provider source package found",
	})
	if err != nil {
		return nil, err
	}
	cfg := prepared.YAMLConfig

	return agentservice.NewExecutable(ctx, agentservice.ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Workdir:      cfg.Workdir,
		Env:          cfg.Env,
		Config:       cfg.Config,
		Egress:       cfg.EgressPolicy(deps.EgressDefaultAction),
		HostBinary:   cfg.HostBinary,
		Cleanup:      prepared.Cleanup,
		HostServices: hostServices,
		Name:         name,
		Telemetry:    deps.Telemetry,
		Gateway:      deps.Gateway,
	})
}

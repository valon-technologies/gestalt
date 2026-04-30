package provider

import (
	"context"
	"fmt"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"gopkg.in/yaml.v3"
)

var Factory bootstrap.AgentFactory = func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreagent.Provider, error) {
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

	return providerhost.NewExecutableAgent(ctx, providerhost.AgentExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		Egress:       cfg.EgressPolicy(deps.Egress.DefaultAction),
		HostBinary:   cfg.HostBinary,
		Cleanup:      prepared.Cleanup,
		HostServices: hostServices,
		Name:         name,
	})
}

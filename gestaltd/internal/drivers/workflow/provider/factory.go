package provider

import (
	"context"
	"fmt"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

var Factory bootstrap.WorkflowFactory = func(ctx context.Context, name string, node yaml.Node, hostServices []providerhost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
	var cfg componentprovider.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("workflow provider: parsing config: %w", err)
	}
	prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindWorkflow,
		Subject:              "workflow provider",
		SourceMissingMessage: "no Go, Rust, or Python workflow provider source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig

	return providerhost.NewExecutableWorkflow(ctx, providerhost.WorkflowExecConfig{
		Command:       cfg.Command,
		Args:          cfg.Args,
		Env:           cfg.Env,
		Config:        cfg.Config,
		AllowedHosts:  cfg.AllowedHosts,
		DefaultAction: deps.Egress.DefaultAction,
		HostBinary:    cfg.HostBinary,
		Cleanup:       prepared.Cleanup,
		HostServices:  hostServices,
		Name:          name,
	})
}

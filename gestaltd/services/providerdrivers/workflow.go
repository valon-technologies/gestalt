package providerdrivers

import (
	"context"
	"fmt"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"gopkg.in/yaml.v3"
)

var WorkflowFactory bootstrap.WorkflowFactory = func(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
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

	return workflowservice.NewExecutable(ctx, workflowservice.ExecConfig{
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

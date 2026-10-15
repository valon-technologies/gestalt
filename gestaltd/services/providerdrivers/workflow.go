package providerdrivers

import (
	"context"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"gopkg.in/yaml.v3"
)

func WorkflowFactory(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps WorkflowDeps) (coreworkflow.Provider, error) {
	prepared, err := prepareYAMLExecution(node, componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindWorkflow,
		Subject:              "workflow provider",
		SourceMissingMessage: "no Go, Rust, or Python workflow provider source package found",
	})
	if err != nil {
		return nil, err
	}
	cfg := prepared.YAMLConfig

	return workflowservice.NewExecutable(ctx, workflowservice.ExecConfig{
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

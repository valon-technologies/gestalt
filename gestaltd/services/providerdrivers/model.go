package providerdrivers

import (
	"context"
	"fmt"

	coremodel "github.com/valon-technologies/gestalt/server/core/model"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	modelservice "github.com/valon-technologies/gestalt/server/services/models"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"gopkg.in/yaml.v3"
)

func ModelFactory(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, deps ModelDeps) (coremodel.Provider, error) {
	var cfg componentprovider.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("model provider: parsing config: %w", err)
	}
	prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindModel,
		Subject:              "model provider",
		SourceMissingMessage: "no Go, Rust, Python, or TypeScript model provider source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig

	return modelservice.NewExecutable(ctx, modelservice.ExecConfig{
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

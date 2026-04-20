package provider

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

var Factory bootstrap.AuthorizationFactory = func(node yaml.Node, hostServices []providerhost.HostService, _ bootstrap.Deps) (core.AuthorizationProvider, error) {
	var cfg componentprovider.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("authorization provider: parsing config: %w", err)
	}
	prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindAuthorization,
		Subject:              "authorization provider",
		SourceMissingMessage: "no Go, Rust, or Python authorization provider source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig

	return providerhost.NewExecutableAuthorizationProvider(context.Background(), providerhost.AuthorizationExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		Cleanup:      prepared.Cleanup,
		HostServices: hostServices,
		Name:         cfg.Name,
	})
}

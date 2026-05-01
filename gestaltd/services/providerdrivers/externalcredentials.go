package providerdrivers

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	externalcredentialsservice "github.com/valon-technologies/gestalt/server/services/externalcredentials"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"gopkg.in/yaml.v3"
)

func ExternalCredentialsFactory(ctx context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService) (core.ExternalCredentialProvider, error) {
	var cfg componentprovider.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("external credentials provider: parsing config: %w", err)
	}
	prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindExternalCredentials,
		Subject:              "external credentials provider",
		SourceMissingMessage: "no Go external-credentials provider source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig

	return externalcredentialsservice.NewExecutable(ctx, externalcredentialsservice.ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		Egress:       cfg.EgressPolicy(""),
		HostBinary:   cfg.HostBinary,
		Cleanup:      prepared.Cleanup,
		HostServices: hostServices,
		Name:         name,
	})
}

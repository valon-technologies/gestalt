package providerdrivers

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	secretsservice "github.com/valon-technologies/gestalt/server/services/secrets"
	"gopkg.in/yaml.v3"
)

func SecretsProviderFactory(node yaml.Node) (core.SecretManager, error) {
	prepared, err := prepareYAMLExecution(node, componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindSecrets,
		Subject:              "secrets provider",
		SourceMissingMessage: "no Go, Rust, or Python secrets provider source package found",
	})
	if err != nil {
		return nil, err
	}
	cfg := prepared.YAMLConfig

	return secretsservice.NewExecutable(context.Background(), secretsservice.ExecConfig{
		Command:    cfg.Command,
		Args:       cfg.Args,
		Workdir:    cfg.Workdir,
		Env:        cfg.Env,
		Config:     cfg.Config,
		Egress:     cfg.EgressPolicy(""),
		HostBinary: cfg.HostBinary,
		Cleanup:    prepared.Cleanup,
		Name:       cfg.Name,
	})
}

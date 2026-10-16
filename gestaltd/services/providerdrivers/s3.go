package providerdrivers

import (
	"context"

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	s3service "github.com/valon-technologies/gestalt/server/services/s3"
	"gopkg.in/yaml.v3"
)

func S3Factory(node yaml.Node) (s3sdk.S3, error) {
	prepared, err := prepareYAMLExecution(node, componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindS3,
		Subject:              "s3 provider",
		SourceMissingMessage: "no Go, Rust, or Python s3 provider source package found",
	})
	if err != nil {
		return nil, err
	}
	cfg := prepared.YAMLConfig

	return s3service.NewExecutable(context.Background(), s3service.ExecConfig{
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

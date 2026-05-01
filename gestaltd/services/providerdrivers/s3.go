package providerdrivers

import (
	"context"
	"fmt"

	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	s3service "github.com/valon-technologies/gestalt/server/services/s3"
	"gopkg.in/yaml.v3"
)

func S3Factory(node yaml.Node) (s3store.Client, error) {
	var cfg componentprovider.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("s3 provider: parsing config: %w", err)
	}
	prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindS3,
		Subject:              "s3 provider",
		SourceMissingMessage: "no Go, Rust, or Python s3 provider source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig

	return s3service.NewExecutable(context.Background(), s3service.ExecConfig{
		Command:    cfg.Command,
		Args:       cfg.Args,
		Env:        cfg.Env,
		Config:     cfg.Config,
		Egress:     cfg.EgressPolicy(""),
		HostBinary: cfg.HostBinary,
		Cleanup:    prepared.Cleanup,
		Name:       cfg.Name,
	})
}

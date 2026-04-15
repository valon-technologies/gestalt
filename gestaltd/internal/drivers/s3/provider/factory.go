package provider

import (
	"context"
	"fmt"

	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

var Factory bootstrap.S3Factory = func(node yaml.Node) (s3store.Client, error) {
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

	return providerhost.NewExecutableS3(context.Background(), providerhost.S3ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		Cleanup:      prepared.Cleanup,
		Name:         cfg.Name,
	})
}

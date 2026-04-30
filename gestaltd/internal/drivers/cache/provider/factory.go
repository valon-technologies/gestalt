package provider

import (
	"context"
	"fmt"

	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/componentprovider"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	cacheservice "github.com/valon-technologies/gestalt/server/services/cache"
	"gopkg.in/yaml.v3"
)

var Factory bootstrap.CacheFactory = func(node yaml.Node) (corecache.Cache, error) {
	var cfg componentprovider.YAMLConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("cache provider: parsing config: %w", err)
	}
	prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindCache,
		Subject:              "cache provider",
		SourceMissingMessage: "no Go cache provider source package found",
		Config:               cfg,
	})
	if err != nil {
		return nil, err
	}
	cfg = prepared.YAMLConfig

	return cacheservice.NewExecutable(context.Background(), cacheservice.ExecConfig{
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

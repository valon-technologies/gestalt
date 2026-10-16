package providerdrivers

import (
	"context"

	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	cacheservice "github.com/valon-technologies/gestalt/server/services/cache"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"gopkg.in/yaml.v3"
)

func CacheFactory(node yaml.Node) (corecache.Cache, error) {
	prepared, err := prepareYAMLExecution(node, componentprovider.PrepareParams{
		Kind:                 providermanifestv1.KindCache,
		Subject:              "cache provider",
		SourceMissingMessage: "no Go cache provider source package found",
	})
	if err != nil {
		return nil, err
	}
	cfg := prepared.YAMLConfig

	return cacheservice.NewExecutable(context.Background(), cacheservice.ExecConfig{
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

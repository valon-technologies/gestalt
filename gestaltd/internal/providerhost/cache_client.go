package providerhost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	cacheservice "github.com/valon-technologies/gestalt/server/services/cache"
)

type CacheExecConfig = cacheservice.ExecConfig

func NewExecutableCache(ctx context.Context, cfg CacheExecConfig) (corecache.Cache, error) {
	return cacheservice.NewExecutable(ctx, cfg)
}

func NewCacheServer(cache corecache.Cache, pluginName string) proto.CacheServer {
	return cacheservice.NewServer(cache, pluginName)
}

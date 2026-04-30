// Package cache exposes cache provider transport primitives.
package cache

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

const DefaultSocketEnv = providerhost.DefaultCacheSocketEnv

type ExecConfig = providerhost.CacheExecConfig

func SocketEnv(name string) string {
	return providerhost.CacheSocketEnv(name)
}

func SocketTokenEnv(name string) string {
	return providerhost.CacheSocketTokenEnv(name)
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (corecache.Cache, error) {
	return providerhost.NewExecutableCache(ctx, cfg)
}

func NewServer(cache corecache.Cache, pluginName string) proto.CacheServer {
	return providerhost.NewCacheServer(cache, pluginName)
}

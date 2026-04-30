package providerhost

import cacheservice "github.com/valon-technologies/gestalt/server/services/cache"

const DefaultCacheSocketEnv = cacheservice.DefaultSocketEnv

func CacheSocketEnv(name string) string {
	return cacheservice.SocketEnv(name)
}

func CacheSocketTokenEnv(name string) string {
	return cacheservice.SocketTokenEnv(name)
}

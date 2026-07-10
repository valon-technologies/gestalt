package bootstrap

import (
	"github.com/valon-technologies/gestalt/server/internal/authzappresource"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

func appResourceResolverFromConfig(cfg *config.Config) *authzappresource.Resolver {
	dedicated := map[string]struct{}{}
	if cfg != nil {
		for _, model := range cfg.Authorization.Models {
			for name := range model.ResourceTypes {
				if authzappresource.IsDedicatedSingletonResourceType(name) {
					dedicated[name] = struct{}{}
				}
			}
		}
	}
	policies := map[string]string{}
	if cfg != nil {
		for appName, entry := range cfg.Apps {
			if entry == nil {
				continue
			}
			if policy := entry.AuthorizationPolicy; policy != "" {
				policies[appName] = policy
			}
		}
	}
	return authzappresource.NewResolver(dedicated, policies)
}

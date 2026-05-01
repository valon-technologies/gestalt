package server

import (
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/authorization"
)

func newTestAuthorizer(cfg config.AuthorizationConfig, pluginDefs map[string]*config.ProviderEntry) (*authorization.Authorizer, error) {
	return authorization.New(config.AuthorizationStaticConfig(cfg, pluginDefs))
}

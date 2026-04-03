package oidc

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"gopkg.in/yaml.v3"
)

var Factory bootstrap.AuthFactory = func(node yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
	var cfg Config
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("oidc auth: parsing config: %w", err)
	}
	if cfg.RedirectURL == "" && deps.BaseURL != "" {
		cfg.RedirectURL = deps.BaseURL + config.AuthCallbackPath
	}
	return New(cfg)
}

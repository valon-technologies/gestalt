package keychain

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

const defaultService = "gestaltd"

type yamlConfig struct {
	Service string `yaml:"service"`
}

var Factory bootstrap.SecretManagerFactory = func(node yaml.Node) (core.SecretManager, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("keychain secrets: parsing config: %w", err)
	}
	service := cfg.Service
	if service == "" {
		service = defaultService
	}
	return &Provider{service: service}, nil
}

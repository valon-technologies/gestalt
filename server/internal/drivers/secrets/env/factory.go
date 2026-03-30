package env

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	Prefix string `yaml:"prefix"`
}

var Factory bootstrap.SecretManagerFactory = func(node yaml.Node) (core.SecretManager, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("env secrets: parsing config: %w", err)
	}
	return &Provider{prefix: cfg.Prefix}, nil
}

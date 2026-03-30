package file

import (
	"fmt"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	Dir string `yaml:"dir"`
}

var Factory bootstrap.SecretManagerFactory = func(node yaml.Node) (core.SecretManager, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("file secrets: parsing config: %w", err)
	}
	if cfg.Dir == "" {
		return nil, fmt.Errorf("file secrets: dir is required")
	}
	abs, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("file secrets: resolving dir: %w", err)
	}
	return &Provider{dir: abs}, nil
}

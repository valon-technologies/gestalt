package sqlite

import (
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	Path string `yaml:"path"`
}

var Factory bootstrap.ChatStoreFactory = func(node yaml.Node, deps bootstrap.Deps) (core.ChatStore, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("sqlite chatstore: parsing config: %w", err)
	}
	if cfg.Path == "" {
		cfg.Path = "./chat.db"
	}
	return New(cfg.Path)
}

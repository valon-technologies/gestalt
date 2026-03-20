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

var Factory bootstrap.DatastoreFactory = func(node yaml.Node, deps bootstrap.Deps) (core.Datastore, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("sqlite: parsing config: %w", err)
	}
	if cfg.Path == "" {
		cfg.Path = "./gestalt.db"
	}
	return New(cfg.Path, deps.EncryptionKey)
}

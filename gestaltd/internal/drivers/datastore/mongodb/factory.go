package mongodb

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	URI      string `yaml:"uri"`
	Database string `yaml:"database"`
}

const defaultDatabase = "gestalt"

var Factory bootstrap.DatastoreFactory = func(node yaml.Node, deps bootstrap.Deps) (core.Datastore, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("mongodb: parsing config: %w", err)
	}
	if cfg.Database == "" {
		cfg.Database = defaultDatabase
	}
	return New(cfg.URI, cfg.Database, deps.EncryptionKey)
}

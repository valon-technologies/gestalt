package mysql

import (
	"fmt"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	DSN string `yaml:"dsn"`
}

var Factory bootstrap.DatastoreFactory = func(node yaml.Node, deps bootstrap.Deps) (core.Datastore, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("mysql: parsing config: %w", err)
	}
	return New(cfg.DSN, deps.EncryptionKey)
}

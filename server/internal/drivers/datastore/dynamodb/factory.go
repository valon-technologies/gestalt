package dynamodb

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	Table    string `yaml:"table"`
	Region   string `yaml:"region"`
	Endpoint string `yaml:"endpoint"`
}

var Factory bootstrap.DatastoreFactory = func(node yaml.Node, deps bootstrap.Deps) (core.Datastore, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("dynamodb: parsing config: %w", err)
	}
	if cfg.Table == "" {
		cfg.Table = "gestalt"
	}
	return New(Config{
		Table:               cfg.Table,
		Region:              cfg.Region,
		Endpoint:            cfg.Endpoint,
		EncryptionKey:       deps.EncryptionKey,
		LegacyEncryptionKey: deps.LegacyEncryptionKey,
	})
}

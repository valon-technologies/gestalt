package sqlstore

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type VersionedDSNConfig struct {
	DSN     string `yaml:"dsn"`
	Version string `yaml:"version"`
}

type DSNConfig struct {
	DSN string `yaml:"dsn"`
}

func NewDSNFactory(name string, newStore func(dsn string, encryptionKey []byte) (core.Datastore, error)) bootstrap.DatastoreFactory {
	return func(node yaml.Node, deps bootstrap.Deps) (core.Datastore, error) {
		var cfg DSNConfig
		if err := node.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("%s: parsing config: %w", name, err)
		}
		return newStore(cfg.DSN, deps.EncryptionKey)
	}
}

func NewVersionedDSNFactory(name string, newStore func(cfg VersionedDSNConfig, encryptionKey []byte) (core.Datastore, error)) bootstrap.DatastoreFactory {
	return func(node yaml.Node, deps bootstrap.Deps) (core.Datastore, error) {
		var cfg VersionedDSNConfig
		if err := node.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("%s: parsing config: %w", name, err)
		}
		return newStore(cfg, deps.EncryptionKey)
	}
}

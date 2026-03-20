package sqlstore

import (
	"fmt"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type dsnConfig struct {
	DSN string `yaml:"dsn"`
}

func NewDSNFactory(name string, newStore func(dsn string, encryptionKey []byte) (core.Datastore, error)) bootstrap.DatastoreFactory {
	return func(node yaml.Node, deps bootstrap.Deps) (core.Datastore, error) {
		var cfg dsnConfig
		if err := node.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("%s: parsing config: %w", name, err)
		}
		return newStore(cfg.DSN, deps.EncryptionKey)
	}
}

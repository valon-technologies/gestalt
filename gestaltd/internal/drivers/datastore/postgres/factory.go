package postgres

import (
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/sqlstore"
)

var Factory bootstrap.DatastoreFactory = sqlstore.NewVersionedDSNFactory("postgres", func(cfg sqlstore.VersionedDSNConfig, encryptionKey []byte) (core.Datastore, error) {
	return New(cfg.DSN, cfg.Version, encryptionKey)
})

package postgres

import (
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/plugins/datastore/sqlstore"
)

var Factory bootstrap.DatastoreFactory = sqlstore.NewDSNFactory("postgres", func(dsn string, encryptionKey []byte) (core.Datastore, error) {
	return New(dsn, encryptionKey)
})

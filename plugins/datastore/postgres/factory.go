package postgres

import (
	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/plugins/datastore/sqlstore"
)

var Factory bootstrap.DatastoreFactory = sqlstore.NewDSNFactory("postgres", func(dsn string, encryptionKey []byte) (core.Datastore, error) {
	return New(dsn, encryptionKey)
})

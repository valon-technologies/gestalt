package mysql

import (
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/plugins/datastore/sqlstore"
)

var Factory bootstrap.DatastoreFactory = sqlstore.NewDSNFactory("mysql", func(dsn string, encryptionKey []byte) (core.Datastore, error) {
	return New(dsn, encryptionKey)
})

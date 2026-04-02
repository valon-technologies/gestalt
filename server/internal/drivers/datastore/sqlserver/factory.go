package sqlserver

import (
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/sqlstore"
)

var Factory bootstrap.DatastoreFactory = sqlstore.NewDSNFactory("sqlserver", func(dsn string, encryptionKey []byte) (core.Datastore, error) {
	return New(dsn, encryptionKey)
})

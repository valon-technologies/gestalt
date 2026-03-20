package oracle

import (
	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/plugins/datastore/sqlstore"
)

var Factory bootstrap.DatastoreFactory = sqlstore.NewDSNFactory("oracle", func(dsn string, key []byte) (core.Datastore, error) {
	return New(dsn, key)
})

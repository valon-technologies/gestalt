package datastore

import (
	_ "github.com/go-sql-driver/mysql" // register mysql driver
	_ "github.com/jackc/pgx/v5/stdlib" // register pgx postgres driver
	_ "modernc.org/sqlite"             // register sqlite driver
)

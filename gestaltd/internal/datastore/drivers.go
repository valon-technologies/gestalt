package datastore

import (
	"database/sql"

	_ "github.com/go-sql-driver/mysql" // register mysql driver
	_ "github.com/jackc/pgx/v5/stdlib" // register pgx postgres driver
	sqlite "modernc.org/sqlite"        // register sqlite driver as "sqlite"
)

func init() {
	// xorm maps the "sqlite3" dialect to Go sql driver name "sqlite3", but
	// modernc.org/sqlite only registers as "sqlite". Register under "sqlite3"
	// so configs using driver: sqlite3 work with the pure-Go driver.
	sql.Register("sqlite3", &sqlite.Driver{})
}

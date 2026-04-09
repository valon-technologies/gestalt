package gestalt

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
)

var schemeDriverMap = map[string]string{
	"postgres":  "postgres",
	"postgresql": "postgres",
	"mysql":     "mysql",
	"sqlite":    "sqlite3",
	"sqlite3":   "sqlite3",
}

func OpenDB(alias string) (*sql.DB, error) {
	envKey := "GESTALT_DATASTORE_" + strings.ToUpper(alias)
	dsn := os.Getenv(envKey)
	if dsn == "" {
		return nil, fmt.Errorf("datastore %q not configured (env %s is empty)", alias, envKey)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	driver, ok := schemeDriverMap[u.Scheme]
	if !ok {
		return nil, fmt.Errorf("unsupported database scheme %q in dsn", u.Scheme)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return db, nil
}

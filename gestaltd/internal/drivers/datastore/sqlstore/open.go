package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/valon-technologies/gestalt/server/core/crypto"
)

const (
	DefaultMaxOpenConns    = 25
	DefaultMaxIdleConns    = 5
	DefaultConnMaxLifetime = 5 * time.Minute
)

func Open(driverName, dsn string, encryptionKey []byte, d Dialect) (*Store, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", driverName, err)
	}
	return openDB(db, driverName, encryptionKey, d)
}

func OpenDB(db *sql.DB, driverName string, encryptionKey []byte, d Dialect) (*Store, error) {
	return openDB(db, driverName, encryptionKey, d)
}

func OpenVersioned(driverName, dsn string, encryptionKey []byte, d Dialect, requestedVersion string, resolve func(context.Context, *sql.DB, string) (string, error)) (*Store, error) {
	s, err := Open(driverName, dsn, encryptionKey, d)
	if err != nil {
		return nil, err
	}
	if _, err := resolve(context.Background(), s.DB, requestedVersion); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func openDB(db *sql.DB, name string, encryptionKey []byte, d Dialect) (*Store, error) {
	db.SetMaxOpenConns(DefaultMaxOpenConns)
	db.SetMaxIdleConns(DefaultMaxIdleConns)
	db.SetConnMaxLifetime(DefaultConnMaxLifetime)

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging %s: %w", name, err)
	}

	enc, err := crypto.NewAESGCM(encryptionKey)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating encryptor: %w", err)
	}

	return New(db, enc, d), nil
}

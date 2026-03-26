package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/plugins/datastore/sqlstore"

	_ "modernc.org/sqlite" // register SQLite driver
)

// dialect implements sqlstore.Dialect for SQLite.
type dialect struct{}

func (dialect) Placeholder(int) string { return "?" }

func (dialect) UpsertTokenSQL() string {
	return `
		INSERT INTO integration_tokens
			(id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted,
			 scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, integration, instance) DO UPDATE SET
			access_token_encrypted = excluded.access_token_encrypted,
			refresh_token_encrypted = excluded.refresh_token_encrypted,
			scopes = excluded.scopes,
			expires_at = excluded.expires_at,
			last_refreshed_at = excluded.last_refreshed_at,
			refresh_error_count = excluded.refresh_error_count,
			metadata_json = excluded.metadata_json,
			updated_at = excluded.updated_at`
}

func (dialect) IsDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Store embeds sqlstore.Store and adds SQLite-specific behavior.
type Store struct {
	*sqlstore.Store
}

var _ core.Datastore = (*Store)(nil)
var _ core.StagedConnectionStore = (*Store)(nil)

func New(dbPath string, encryptionKey []byte) (*Store, error) {
	dsn := dbPath + "?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	enc, err := crypto.NewAESGCM(encryptionKey)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating encryptor: %w", err)
	}

	return &Store{Store: sqlstore.New(db, enc, dialect{})}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS integration_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			integration TEXT NOT NULL,
			instance TEXT NOT NULL,
			access_token_encrypted TEXT NOT NULL,
			refresh_token_encrypted TEXT NOT NULL DEFAULT '',
			scopes TEXT NOT NULL DEFAULT '',
			expires_at DATETIME,
			last_refreshed_at DATETIME,
			refresh_error_count INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE(user_id, integration, instance)
		);
		CREATE TABLE IF NOT EXISTS api_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL,
			hashed_token TEXT UNIQUE NOT NULL,
			scopes TEXT NOT NULL DEFAULT '',
			expires_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS staged_connections (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			integration TEXT NOT NULL,
			instance TEXT NOT NULL,
			access_token_encrypted TEXT NOT NULL,
			refresh_token_encrypted TEXT NOT NULL DEFAULT '',
			token_expires_at DATETIME,
			metadata_json TEXT NOT NULL DEFAULT '',
			candidates_json TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL
		);
	`)
	return err
}

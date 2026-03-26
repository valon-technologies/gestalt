package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/plugins/datastore/sqlstore"

	_ "modernc.org/sqlite" // register database/sql driver
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
var _ core.EgressClientStore = (*Store)(nil)
var _ core.EgressDenyRuleStore = (*Store)(nil)
var _ core.EgressCredentialGrantStore = (*Store)(nil)

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
		CREATE TABLE IF NOT EXISTS egress_clients (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			scope TEXT NOT NULL DEFAULT 'personal',
			scope_key TEXT NOT NULL DEFAULT '',
			created_by_id TEXT NOT NULL REFERENCES users(id),
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE(scope_key, name)
		);
		CREATE TABLE IF NOT EXISTS egress_client_tokens (
			id TEXT PRIMARY KEY,
			client_id TEXT NOT NULL REFERENCES egress_clients(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			hashed_token TEXT UNIQUE NOT NULL,
			expires_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS egress_deny_rules (
			id TEXT PRIMARY KEY,
			subject_kind TEXT NOT NULL DEFAULT '',
			subject_id TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			operation TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL DEFAULT '',
			path_prefix TEXT NOT NULL DEFAULT '',
			created_by_id TEXT NOT NULL REFERENCES users(id),
			description TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS egress_credential_grants (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL DEFAULT '',
			instance TEXT NOT NULL DEFAULT '',
			secret_ref TEXT NOT NULL DEFAULT '',
			auth_style TEXT NOT NULL DEFAULT '',
			subject_kind TEXT NOT NULL DEFAULT '',
			subject_id TEXT NOT NULL DEFAULT '',
			operation TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL DEFAULT '',
			path_prefix TEXT NOT NULL DEFAULT '',
			created_by_id TEXT NOT NULL REFERENCES users(id),
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)
	`)
	if err != nil {
		return err
	}
	return s.migrateEgressClientScope(ctx)
}

func (s *Store) egressClientHasScopeColumns(ctx context.Context) (hasScope, hasScopeKey bool, err error) {
	rows, err := s.DB.QueryContext(ctx, "PRAGMA table_info(egress_clients)")
	if err != nil {
		return false, false, fmt.Errorf("checking egress_clients schema: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			return false, false, fmt.Errorf("scanning table_info: %w", err)
		}
		if name == "scope" {
			hasScope = true
		}
		if name == "scope_key" {
			hasScopeKey = true
		}
	}
	return hasScope, hasScopeKey, rows.Err()
}

func (s *Store) migrateEgressClientScope(ctx context.Context) error {
	hasScope, hasScopeKey, err := s.egressClientHasScopeColumns(ctx)
	if err != nil {
		return err
	}
	if hasScope && hasScopeKey {
		return nil
	}

	if _, err := s.DB.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disabling foreign keys: %w", err)
	}
	defer func() { _, _ = s.DB.ExecContext(ctx, "PRAGMA foreign_keys = ON") }()

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning scope migration tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS egress_clients_new`); err != nil {
		return fmt.Errorf("cleaning up prior failed migration: %w", err)
	}

	stmts := []string{
		`CREATE TABLE egress_clients_new (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			scope TEXT NOT NULL DEFAULT 'personal',
			scope_key TEXT NOT NULL DEFAULT '',
			created_by_id TEXT NOT NULL REFERENCES users(id),
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE(scope_key, name)
		)`,
		`INSERT INTO egress_clients_new (id, name, description, scope, scope_key, created_by_id, created_at, updated_at)
		 SELECT id, name, description, 'personal', created_by_id, created_by_id, created_at, updated_at
		 FROM egress_clients`,
		`DROP TABLE egress_clients`,
		`ALTER TABLE egress_clients_new RENAME TO egress_clients`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrating egress_clients scope: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing scope migration: %w", err)
	}

	fkRows, err := s.DB.QueryContext(ctx, "PRAGMA foreign_key_check(egress_client_tokens)")
	if err != nil {
		return fmt.Errorf("foreign key check: %w", err)
	}
	defer func() { _ = fkRows.Close() }()
	if fkRows.Next() {
		return fmt.Errorf("foreign key violations found in egress_client_tokens after migration")
	}
	return fkRows.Err()
}

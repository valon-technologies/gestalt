package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/sqlstore"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/versioning"

	_ "github.com/jackc/pgx/v5/stdlib" // register database/sql driver
)

const providerName = "postgres"

var supportedVersions = []string{"15", "16", "17", "18"}

// dialect implements sqlstore.Dialect for PostgreSQL.
type dialect struct{}

func (dialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }

func (dialect) UpsertTokenSQL() string {
	return `
		INSERT INTO integration_tokens
			(id, user_id, integration, connection, instance, access_token_encrypted, refresh_token_encrypted,
			 scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT(user_id, integration, connection, instance) DO UPDATE SET
			access_token_encrypted = EXCLUDED.access_token_encrypted,
			refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
			scopes = EXCLUDED.scopes,
			expires_at = EXCLUDED.expires_at,
			last_refreshed_at = EXCLUDED.last_refreshed_at,
			refresh_error_count = EXCLUDED.refresh_error_count,
			metadata_json = EXCLUDED.metadata_json,
			updated_at = EXCLUDED.updated_at`
}

func (dialect) RegistrationDDL() string {
	return `CREATE TABLE IF NOT EXISTS oauth_registrations (
		id TEXT PRIMARY KEY,
		auth_server_url TEXT NOT NULL,
		redirect_uri TEXT NOT NULL,
		client_id TEXT NOT NULL,
		client_secret_encrypted TEXT,
		expires_at TIMESTAMPTZ,
		authorization_endpoint TEXT NOT NULL,
		token_endpoint TEXT NOT NULL,
		scopes_supported TEXT,
		discovered_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL,
		UNIQUE (auth_server_url, redirect_uri)
	)`
}

func (dialect) IsDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (dialect) NormalizeConnection(connection string) string   { return connection }
func (dialect) DenormalizeConnection(connection string) string { return connection }

// Store embeds sqlstore.Store and adds PostgreSQL-specific behavior.
type Store struct {
	*sqlstore.Store
}

var _ core.Datastore = (*Store)(nil)

func New(dsn string, encryptionKey []byte) (*Store, error) {
	s, err := sqlstore.OpenVersioned("pgx", dsn, encryptionKey, dialect{}, "", resolveVersion)
	if err != nil {
		return nil, err
	}
	return &Store{Store: s}, nil
}

func resolveVersion(ctx context.Context, db *sql.DB, requested string) (string, error) {
	return versioning.Resolve(ctx, providerName, requested, supportedVersions, func(ctx context.Context) (string, string, error) {
		var raw string
		if err := db.QueryRowContext(ctx, "SHOW server_version_num").Scan(&raw); err != nil {
			return "", "", fmt.Errorf("%s: detecting version: %w", providerName, err)
		}
		versionNum, err := strconv.Atoi(raw)
		if err != nil {
			return "", raw, fmt.Errorf("%s: parsing server_version_num %q: %w", providerName, raw, err)
		}
		return strconv.Itoa(versionNum / 10000), raw, nil
	})
}

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning migration tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`); err != nil {
		return fmt.Errorf("creating users table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS integration_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			integration TEXT NOT NULL,
			connection TEXT NOT NULL DEFAULT '',
			instance TEXT NOT NULL,
			access_token_encrypted TEXT NOT NULL,
			refresh_token_encrypted TEXT NOT NULL DEFAULT '',
			scopes TEXT NOT NULL DEFAULT '',
			expires_at TIMESTAMPTZ,
			last_refreshed_at TIMESTAMPTZ,
			refresh_error_count INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			UNIQUE(user_id, integration, connection, instance)
		)`); err != nil {
		return fmt.Errorf("creating integration_tokens table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS api_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL,
			hashed_token TEXT UNIQUE NOT NULL,
			scopes TEXT NOT NULL DEFAULT '',
			expires_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`); err != nil {
		return fmt.Errorf("creating api_tokens table: %w", err)
	}
	return tx.Commit()
}

// Package mysql implements core.Datastore backed by a MySQL database.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/plugins/datastore/sqlstore"
)

// dialect implements sqlstore.Dialect for MySQL.
type dialect struct{}

func (dialect) Placeholder(int) string { return "?" }

func (dialect) UpsertTokenSQL() string {
	return `
		INSERT INTO integration_tokens
			(id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted,
			 scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			access_token_encrypted = VALUES(access_token_encrypted),
			refresh_token_encrypted = VALUES(refresh_token_encrypted),
			scopes = VALUES(scopes),
			expires_at = VALUES(expires_at),
			last_refreshed_at = VALUES(last_refreshed_at),
			refresh_error_count = VALUES(refresh_error_count),
			metadata_json = VALUES(metadata_json),
			updated_at = VALUES(updated_at)`
}

func (dialect) IsDuplicateKeyError(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

// Store embeds sqlstore.Store and adds MySQL-specific behavior.
type Store struct {
	*sqlstore.Store
}

var _ core.Datastore = (*Store)(nil)
var _ core.StagedConnectionStore = (*Store)(nil)
var _ core.EgressClientStore = (*Store)(nil)

func New(dsn string, encryptionKey []byte) (*Store, error) {
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing dsn: %w", err)
	}
	cfg.ParseTime = true

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("opening mysql: %w", err)
	}

	s, err := sqlstore.OpenDB(db, "mysql", encryptionKey, dialect{})
	if err != nil {
		return nil, err
	}
	return &Store{Store: s}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			email VARCHAR(255) NOT NULL,
			display_name VARCHAR(255) NOT NULL DEFAULT '',
			created_at DATETIME(6) NOT NULL,
			updated_at DATETIME(6) NOT NULL,
			UNIQUE KEY idx_users_email (email)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS integration_tokens (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			user_id VARCHAR(36) NOT NULL,
			integration VARCHAR(255) NOT NULL,
			instance VARCHAR(255) NOT NULL,
			access_token_encrypted TEXT NOT NULL,
			refresh_token_encrypted TEXT NOT NULL,
			scopes TEXT NOT NULL,
			expires_at DATETIME(6) NULL,
			last_refreshed_at DATETIME(6) NULL,
			refresh_error_count INT NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL,
			created_at DATETIME(6) NOT NULL,
			updated_at DATETIME(6) NOT NULL,
			UNIQUE KEY idx_integration_tokens_user_integ_inst (user_id, integration, instance),
			CONSTRAINT fk_integration_tokens_user FOREIGN KEY (user_id) REFERENCES users(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS api_tokens (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			user_id VARCHAR(36) NOT NULL,
			name VARCHAR(255) NOT NULL,
			hashed_token VARCHAR(255) NOT NULL,
			scopes TEXT NOT NULL,
			expires_at DATETIME(6) NULL,
			created_at DATETIME(6) NOT NULL,
			updated_at DATETIME(6) NOT NULL,
			UNIQUE KEY idx_api_tokens_hashed (hashed_token),
			CONSTRAINT fk_api_tokens_user FOREIGN KEY (user_id) REFERENCES users(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS staged_connections (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			user_id VARCHAR(36) NOT NULL,
			integration VARCHAR(255) NOT NULL,
			instance VARCHAR(255) NOT NULL,
			access_token_encrypted TEXT NOT NULL,
			refresh_token_encrypted TEXT NOT NULL,
			token_expires_at DATETIME,
			metadata_json TEXT NOT NULL,
			candidates_json TEXT NOT NULL,
			created_at DATETIME(6) NOT NULL,
			expires_at DATETIME(6) NOT NULL,
			CONSTRAINT fk_staged_connections_user FOREIGN KEY (user_id) REFERENCES users(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS egress_clients (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			description TEXT NOT NULL,
			created_by_id VARCHAR(36) NOT NULL,
			created_at DATETIME(6) NOT NULL,
			updated_at DATETIME(6) NOT NULL,
			UNIQUE KEY idx_egress_clients_owner_name (created_by_id, name),
			CONSTRAINT fk_egress_clients_user FOREIGN KEY (created_by_id) REFERENCES users(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS egress_client_tokens (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			client_id VARCHAR(36) NOT NULL,
			name VARCHAR(255) NOT NULL,
			hashed_token VARCHAR(255) NOT NULL,
			expires_at DATETIME(6) NULL,
			created_at DATETIME(6) NOT NULL,
			updated_at DATETIME(6) NOT NULL,
			UNIQUE KEY idx_egress_client_tokens_hashed (hashed_token),
			CONSTRAINT fk_egress_client_tokens_client FOREIGN KEY (client_id) REFERENCES egress_clients(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}

	for i, stmt := range migrations {
		if _, err := s.DB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration %d: %w", i, err)
		}
	}
	return nil
}

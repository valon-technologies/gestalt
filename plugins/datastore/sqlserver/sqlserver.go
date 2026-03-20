package sqlserver

import (
	"context"
	"errors"
	"fmt"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/plugins/datastore/sqlstore"
)

const (
	driverName = "sqlserver"

	// SQL Server error numbers for duplicate key violations.
	errUniqueKeyViolation   = 2627
	errUniqueIndexViolation = 2601
)

// dialect implements sqlstore.Dialect for SQL Server.
type dialect struct{}

func (dialect) Placeholder(n int) string { return fmt.Sprintf("@p%d", n) }

func (dialect) UpsertTokenSQL() string {
	return `
		MERGE integration_tokens WITH (HOLDLOCK) AS tgt
		USING (VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9, @p10, @p11, @p12, @p13))
			AS src (id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted,
					scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at)
		ON tgt.user_id = src.user_id AND tgt.integration = src.integration AND tgt.instance = src.instance
		WHEN MATCHED THEN UPDATE SET
			tgt.access_token_encrypted = src.access_token_encrypted,
			tgt.refresh_token_encrypted = src.refresh_token_encrypted,
			tgt.scopes = src.scopes, tgt.expires_at = src.expires_at,
			tgt.last_refreshed_at = src.last_refreshed_at,
			tgt.refresh_error_count = src.refresh_error_count,
			tgt.metadata_json = src.metadata_json, tgt.updated_at = src.updated_at
		WHEN NOT MATCHED THEN INSERT
			(id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted,
			 scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at)
		VALUES (src.id, src.user_id, src.integration, src.instance, src.access_token_encrypted,
				src.refresh_token_encrypted, src.scopes, src.expires_at, src.last_refreshed_at,
				src.refresh_error_count, src.metadata_json, src.created_at, src.updated_at);`
}

func (dialect) IsDuplicateKeyError(err error) bool {
	var mssqlErr *mssql.Error
	if errors.As(err, &mssqlErr) {
		return mssqlErr.Number == errUniqueKeyViolation || mssqlErr.Number == errUniqueIndexViolation
	}
	return false
}

// Store embeds sqlstore.Store and adds SQL Server-specific behavior.
type Store struct {
	*sqlstore.Store
}

var _ core.Datastore = (*Store)(nil)

func New(dsn string, encryptionKey []byte) (*Store, error) {
	s, err := sqlstore.Open(driverName, dsn, encryptionKey, dialect{})
	if err != nil {
		return nil, err
	}
	return &Store{Store: s}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning migration tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	migrations := []struct {
		name string
		sql  string
	}{
		{"users", `
			IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'users')
			CREATE TABLE users (
				id NVARCHAR(36) NOT NULL PRIMARY KEY,
				email NVARCHAR(255) NOT NULL UNIQUE,
				display_name NVARCHAR(255) NOT NULL DEFAULT '',
				created_at DATETIME2(6) NOT NULL,
				updated_at DATETIME2(6) NOT NULL
			)`},
		{"integration_tokens", `
			IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'integration_tokens')
			CREATE TABLE integration_tokens (
				id NVARCHAR(36) NOT NULL PRIMARY KEY,
				user_id NVARCHAR(36) NOT NULL REFERENCES users(id),
				integration NVARCHAR(255) NOT NULL,
				instance NVARCHAR(255) NOT NULL,
				access_token_encrypted NVARCHAR(MAX) NOT NULL,
				refresh_token_encrypted NVARCHAR(MAX) NOT NULL DEFAULT '',
				scopes NVARCHAR(MAX) NOT NULL DEFAULT '',
				expires_at DATETIME2(6) NULL,
				last_refreshed_at DATETIME2(6) NOT NULL,
				refresh_error_count INT NOT NULL DEFAULT 0,
				metadata_json NVARCHAR(MAX) NOT NULL DEFAULT '',
				created_at DATETIME2(6) NOT NULL,
				updated_at DATETIME2(6) NOT NULL,
				UNIQUE(user_id, integration, instance)
			)`},
		{"api_tokens", `
			IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'api_tokens')
			CREATE TABLE api_tokens (
				id NVARCHAR(36) NOT NULL PRIMARY KEY,
				user_id NVARCHAR(36) NOT NULL REFERENCES users(id),
				name NVARCHAR(255) NOT NULL,
				hashed_token NVARCHAR(255) NOT NULL UNIQUE,
				scopes NVARCHAR(MAX) NOT NULL DEFAULT '',
				expires_at DATETIME2(6) NULL,
				created_at DATETIME2(6) NOT NULL,
				updated_at DATETIME2(6) NOT NULL
			)`},
	}

	for _, m := range migrations {
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("creating %s table: %w", m.name, err)
		}
	}
	return tx.Commit()
}

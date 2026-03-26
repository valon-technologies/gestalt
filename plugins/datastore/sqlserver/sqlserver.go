package sqlserver

import (
	"context"
	"errors"
	"fmt"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/plugins/datastore/sqlstore"
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

func (dialect) RegistrationDDL() string {
	return `IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'oauth_registrations')
		CREATE TABLE oauth_registrations (
			id NVARCHAR(36) NOT NULL PRIMARY KEY,
			auth_server_url NVARCHAR(255) NOT NULL,
			redirect_uri NVARCHAR(255) NOT NULL,
			client_id NVARCHAR(255) NOT NULL,
			client_secret_encrypted NVARCHAR(MAX),
			authorization_endpoint NVARCHAR(500) NOT NULL,
			token_endpoint NVARCHAR(500) NOT NULL,
			scopes_supported NVARCHAR(MAX),
			discovered_at DATETIME2(6) NOT NULL,
			created_at DATETIME2(6) NOT NULL,
			updated_at DATETIME2(6) NOT NULL,
			UNIQUE(auth_server_url, redirect_uri)
		)`
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
var _ core.StagedConnectionStore = (*Store)(nil)
var _ core.EgressClientStore = (*Store)(nil)

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
				last_refreshed_at DATETIME2(6) NULL,
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
		{"staged_connections", `
			IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'staged_connections')
			CREATE TABLE staged_connections (
				id NVARCHAR(36) NOT NULL PRIMARY KEY,
				user_id NVARCHAR(36) NOT NULL REFERENCES users(id),
				integration NVARCHAR(255) NOT NULL,
				instance NVARCHAR(255) NOT NULL,
				access_token_encrypted NVARCHAR(MAX) NOT NULL,
				refresh_token_encrypted NVARCHAR(MAX) NOT NULL DEFAULT '',
				token_expires_at DATETIME2,
				metadata_json NVARCHAR(MAX) NOT NULL DEFAULT '',
				candidates_json NVARCHAR(MAX) NOT NULL,
				created_at DATETIME2(6) NOT NULL,
				expires_at DATETIME2(6) NOT NULL
			)`},
		{"egress_clients", `
			IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'egress_clients')
			CREATE TABLE egress_clients (
				id NVARCHAR(36) NOT NULL PRIMARY KEY,
				name NVARCHAR(255) NOT NULL,
				description NVARCHAR(MAX) NOT NULL DEFAULT '',
				scope NVARCHAR(255) NOT NULL DEFAULT 'personal',
				scope_key NVARCHAR(255) NOT NULL DEFAULT '',
				created_by_id NVARCHAR(36) NOT NULL REFERENCES users(id),
				created_at DATETIME2(6) NOT NULL,
				updated_at DATETIME2(6) NOT NULL,
				CONSTRAINT uq_egress_clients_scope_name UNIQUE (scope_key, name)
			)`},
		{"egress_client_tokens", `
			IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'egress_client_tokens')
			CREATE TABLE egress_client_tokens (
				id NVARCHAR(36) NOT NULL PRIMARY KEY,
				client_id NVARCHAR(36) NOT NULL REFERENCES egress_clients(id) ON DELETE CASCADE,
				name NVARCHAR(255) NOT NULL,
				hashed_token NVARCHAR(255) NOT NULL UNIQUE,
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

	if err := tx.Commit(); err != nil {
		return err
	}
	return s.migrateEgressClientScope(ctx)
}

func (s *Store) migrateEgressClientScope(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `
		IF COL_LENGTH('egress_clients', 'scope') IS NULL
		  ALTER TABLE egress_clients ADD scope NVARCHAR(255) NOT NULL DEFAULT 'personal'`); err != nil {
		return fmt.Errorf("adding scope column: %w", err)
	}

	if _, err := s.DB.ExecContext(ctx, `
		IF COL_LENGTH('egress_clients', 'scope_key') IS NULL
		  ALTER TABLE egress_clients ADD scope_key NVARCHAR(255) NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("adding scope_key column: %w", err)
	}

	if _, err := s.DB.ExecContext(ctx, `
		UPDATE egress_clients SET scope_key = created_by_id WHERE scope = 'personal' AND scope_key = ''`); err != nil {
		return fmt.Errorf("backfilling scope_key: %w", err)
	}

	if _, err := s.DB.ExecContext(ctx, `
		DECLARE @old_uq NVARCHAR(255);
		SELECT @old_uq = kc.name FROM sys.key_constraints kc
		  JOIN sys.index_columns ic ON kc.unique_index_id = ic.index_id AND kc.parent_object_id = ic.object_id
		  WHERE kc.parent_object_id = OBJECT_ID('egress_clients') AND kc.type = 'UQ'
		  GROUP BY kc.name
		  HAVING COUNT(*) = 2
		    AND MAX(CASE WHEN COL_NAME(ic.object_id, ic.column_id) = 'created_by_id' THEN 1 ELSE 0 END) = 1
		    AND MAX(CASE WHEN COL_NAME(ic.object_id, ic.column_id) = 'name' THEN 1 ELSE 0 END) = 1;
		IF @old_uq IS NOT NULL EXEC('ALTER TABLE egress_clients DROP CONSTRAINT [' + @old_uq + ']')`); err != nil {
		return fmt.Errorf("dropping old constraint: %w", err)
	}

	if _, err := s.DB.ExecContext(ctx, `
		IF NOT EXISTS (SELECT 1 FROM sys.key_constraints WHERE name = 'uq_egress_clients_scope_name')
		  ALTER TABLE egress_clients ADD CONSTRAINT uq_egress_clients_scope_name UNIQUE (scope_key, name)`); err != nil {
		return fmt.Errorf("adding scope constraint: %w", err)
	}

	return nil
}

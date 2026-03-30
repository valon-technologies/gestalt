package oracle

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore/sqlstore"

	_ "github.com/sijms/go-ora/v2" // register database/sql driver
)

const oraUniqueViolation = "ORA-00001"

// dialect implements sqlstore.Dialect for Oracle.
type dialect struct{}

func (dialect) Placeholder(n int) string { return fmt.Sprintf(":%d", n) }

func (dialect) UpsertTokenSQL() string {
	return `MERGE INTO integration_tokens tgt
USING (SELECT :1 AS id, :2 AS user_id, :3 AS integration, :4 AS connection,
              :5 AS instance, :6 AS access_token_encrypted, :7 AS refresh_token_encrypted,
              :8 AS scopes, :9 AS expires_at, :10 AS last_refreshed_at,
              :11 AS refresh_error_count, :12 AS metadata_json,
              :13 AS created_at, :14 AS updated_at FROM DUAL) src
ON (tgt.user_id = src.user_id AND tgt.integration = src.integration AND tgt.connection = src.connection AND tgt.instance = src.instance)
WHEN MATCHED THEN UPDATE SET
    tgt.access_token_encrypted = src.access_token_encrypted,
    tgt.refresh_token_encrypted = src.refresh_token_encrypted,
    tgt.scopes = src.scopes, tgt.expires_at = src.expires_at,
    tgt.last_refreshed_at = src.last_refreshed_at,
    tgt.refresh_error_count = src.refresh_error_count,
    tgt.metadata_json = src.metadata_json, tgt.updated_at = src.updated_at
WHEN NOT MATCHED THEN INSERT
    (id, user_id, integration, connection, instance, access_token_encrypted, refresh_token_encrypted,
     scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at)
VALUES (src.id, src.user_id, src.integration, src.connection, src.instance, src.access_token_encrypted,
        src.refresh_token_encrypted, src.scopes, src.expires_at, src.last_refreshed_at,
        src.refresh_error_count, src.metadata_json, src.created_at, src.updated_at)`
}

func (dialect) RegistrationDDL() string {
	return `DECLARE
		v_count NUMBER;
	BEGIN
		SELECT COUNT(*) INTO v_count FROM user_tables WHERE table_name = 'OAUTH_REGISTRATIONS';
		IF v_count = 0 THEN
			EXECUTE IMMEDIATE 'CREATE TABLE oauth_registrations (
				id VARCHAR2(36) PRIMARY KEY,
				auth_server_url VARCHAR2(255) NOT NULL,
				redirect_uri VARCHAR2(255) NOT NULL,
				client_id VARCHAR2(255) NOT NULL,
				client_secret_encrypted CLOB,
				authorization_endpoint VARCHAR2(500) NOT NULL,
				token_endpoint VARCHAR2(500) NOT NULL,
				scopes_supported CLOB,
				discovered_at TIMESTAMP WITH TIME ZONE NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL,
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
				UNIQUE (auth_server_url, redirect_uri)
			)';
		END IF;
	END;`
}

func (dialect) IsDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), oraUniqueViolation)
}

// Store embeds sqlstore.Store and adds Oracle-specific behavior.
type Store struct {
	*sqlstore.Store
}

var _ core.Datastore = (*Store)(nil)
var _ core.StagedConnectionStore = (*Store)(nil)

func New(dsn string, encryptionKey []byte) (*Store, error) {
	s, err := sqlstore.Open("oracle", dsn, encryptionKey, dialect{})
	if err != nil {
		return nil, err
	}
	return &Store{Store: s}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	tables := []struct {
		name string
		ddl  string
	}{
		{
			name: "USERS",
			ddl: `CREATE TABLE users (
				id VARCHAR2(36) PRIMARY KEY,
				email VARCHAR2(255) NOT NULL,
				display_name VARCHAR2(255),
				created_at TIMESTAMP WITH TIME ZONE NOT NULL,
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL
			)`,
		},
		{
			name: "INTEGRATION_TOKENS",
			ddl: `CREATE TABLE integration_tokens (
				id VARCHAR2(36) PRIMARY KEY,
				user_id VARCHAR2(36) NOT NULL,
				integration VARCHAR2(128) NOT NULL,
				connection VARCHAR2(128) DEFAULT '' NOT NULL,
				instance VARCHAR2(128) NOT NULL,
				access_token_encrypted CLOB NOT NULL,
				refresh_token_encrypted CLOB,
				scopes CLOB,
				expires_at TIMESTAMP WITH TIME ZONE,
				last_refreshed_at TIMESTAMP WITH TIME ZONE,
				refresh_error_count NUMBER(10) DEFAULT 0 NOT NULL,
				metadata_json CLOB,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL,
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL
			)`,
		},
		{
			name: "API_TOKENS",
			ddl: `CREATE TABLE api_tokens (
				id VARCHAR2(36) PRIMARY KEY,
				user_id VARCHAR2(36) NOT NULL,
				name VARCHAR2(255) NOT NULL,
				hashed_token VARCHAR2(255) NOT NULL,
				scopes CLOB,
				expires_at TIMESTAMP WITH TIME ZONE,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL,
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL
			)`,
		},
		{
			name: "STAGED_CONNECTIONS",
			ddl: `CREATE TABLE staged_connections (
				id VARCHAR2(36) PRIMARY KEY,
				user_id VARCHAR2(36) NOT NULL,
				integration VARCHAR2(128) NOT NULL,
				connection VARCHAR2(128) DEFAULT '' NOT NULL,
				instance VARCHAR2(128) NOT NULL,
				access_token_encrypted CLOB NOT NULL,
				refresh_token_encrypted CLOB,
				token_expires_at TIMESTAMP,
				metadata_json CLOB,
				candidates_json CLOB NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL,
				expires_at TIMESTAMP WITH TIME ZONE NOT NULL
			)`,
		},
	}

	for _, tbl := range tables {
		exists, err := s.tableExists(ctx, tbl.name)
		if err != nil {
			return fmt.Errorf("checking table %s: %w", tbl.name, err)
		}
		if !exists {
			if _, err := s.DB.ExecContext(ctx, tbl.ddl); err != nil {
				return fmt.Errorf("creating table %s: %w", tbl.name, err)
			}
		}
	}

	constraints := []struct {
		name string
		ddl  string
	}{
		{
			name: "UQ_USERS_EMAIL",
			ddl:  "ALTER TABLE users ADD CONSTRAINT uq_users_email UNIQUE (email)",
		},
		{
			name: "UQ_IT_USER_INTEG_CONN_INST",
			ddl:  "ALTER TABLE integration_tokens ADD CONSTRAINT uq_it_user_integ_conn_inst UNIQUE (user_id, integration, connection, instance)",
		},
		{
			name: "FK_IT_USER",
			ddl:  "ALTER TABLE integration_tokens ADD CONSTRAINT fk_it_user FOREIGN KEY (user_id) REFERENCES users(id)",
		},
		{
			name: "UQ_API_TOKENS_HASHED",
			ddl:  "ALTER TABLE api_tokens ADD CONSTRAINT uq_api_tokens_hashed UNIQUE (hashed_token)",
		},
		{
			name: "FK_API_TOKENS_USER",
			ddl:  "ALTER TABLE api_tokens ADD CONSTRAINT fk_api_tokens_user FOREIGN KEY (user_id) REFERENCES users(id)",
		},
		{
			name: "FK_STAGED_CONN_USER",
			ddl:  "ALTER TABLE staged_connections ADD CONSTRAINT fk_staged_conn_user FOREIGN KEY (user_id) REFERENCES users(id)",
		},
	}

	for _, c := range constraints {
		exists, err := s.constraintExists(ctx, c.name)
		if err != nil {
			return fmt.Errorf("checking constraint %s: %w", c.name, err)
		}
		if !exists {
			if _, err := s.DB.ExecContext(ctx, c.ddl); err != nil {
				return fmt.Errorf("creating constraint %s: %w", c.name, err)
			}
		}
	}

	return nil
}

func (s *Store) tableExists(ctx context.Context, name string) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_tables WHERE table_name = :1", name,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) constraintExists(ctx context.Context, name string) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_constraints WHERE constraint_name = :1", strings.ToUpper(name),
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

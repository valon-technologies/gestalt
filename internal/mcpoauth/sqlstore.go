package mcpoauth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/core/crypto"
)

// SQLDialect abstracts database-specific SQL syntax. Matches the subset of
// sqlstore.Dialect needed for registration storage.
type SQLDialect interface {
	Placeholder(n int) string
}

// RegistrationDDLProvider is an optional interface that SQLDialect
// implementations can satisfy to provide database-specific DDL for the
// oauth_registrations table. When not implemented, Migrate uses generic DDL
// compatible with MySQL, PostgreSQL, and SQLite.
type RegistrationDDLProvider interface {
	RegistrationDDL() string
}

type SQLStore struct {
	db      *sql.DB
	enc     *crypto.AESGCMEncryptor
	dialect SQLDialect
}

func NewSQLStore(db *sql.DB, enc *crypto.AESGCMEncryptor, dialect SQLDialect) *SQLStore {
	return &SQLStore{db: db, enc: enc, dialect: dialect}
}

func (s *SQLStore) ph(n int) string { return s.dialect.Placeholder(n) }

const defaultRegistrationDDL = `CREATE TABLE IF NOT EXISTS oauth_registrations (
	id VARCHAR(36) PRIMARY KEY,
	auth_server_url VARCHAR(255) NOT NULL,
	redirect_uri VARCHAR(255) NOT NULL,
	client_id VARCHAR(255) NOT NULL,
	client_secret_encrypted TEXT,
	authorization_endpoint VARCHAR(500) NOT NULL,
	token_endpoint VARCHAR(500) NOT NULL,
	scopes_supported TEXT,
	discovered_at DATETIME NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL,
	UNIQUE (auth_server_url, redirect_uri)
)`

func (s *SQLStore) Migrate(ctx context.Context) error {
	ddl := defaultRegistrationDDL
	if p, ok := s.dialect.(RegistrationDDLProvider); ok {
		ddl = p.RegistrationDDL()
	}
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

func (s *SQLStore) GetRegistration(ctx context.Context, authServerURL, redirectURI string) (*Registration, error) {
	query := `SELECT id, auth_server_url, redirect_uri, client_id, client_secret_encrypted,
	       authorization_endpoint, token_endpoint, scopes_supported, discovered_at
	FROM oauth_registrations
	WHERE auth_server_url = ` + s.ph(1) + ` AND redirect_uri = ` + s.ph(2)

	row := s.db.QueryRowContext(ctx, query, authServerURL, redirectURI)

	var reg Registration
	var secretEnc sql.NullString
	var scopes sql.NullString
	var id string
	err := row.Scan(&id, &reg.AuthServerURL, &reg.RedirectURI, &reg.ClientID,
		&secretEnc, &reg.AuthorizationEndpoint, &reg.TokenEndpoint,
		&scopes, &reg.DiscoveredAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying registration: %w", err)
	}

	if secretEnc.String != "" {
		decrypted, err := s.enc.Decrypt(secretEnc.String)
		if err != nil {
			return nil, fmt.Errorf("decrypting client_secret: %w", err)
		}
		reg.ClientSecret = decrypted
	}
	reg.ScopesSupported = scopes.String

	return &reg, nil
}

func (s *SQLStore) StoreRegistration(ctx context.Context, reg *Registration) error {
	var secretEnc sql.NullString
	if reg.ClientSecret != "" {
		encrypted, err := s.enc.Encrypt(reg.ClientSecret)
		if err != nil {
			return fmt.Errorf("encrypting client_secret: %w", err)
		}
		secretEnc = sql.NullString{String: encrypted, Valid: true}
	}

	now := time.Now()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Try UPDATE first, then INSERT if no rows affected. This avoids
	// database-specific upsert syntax (ON DUPLICATE KEY / ON CONFLICT).
	res, err := tx.ExecContext(ctx, `UPDATE oauth_registrations SET
		client_id = `+s.ph(1)+`,
		client_secret_encrypted = `+s.ph(2)+`,
		authorization_endpoint = `+s.ph(3)+`,
		token_endpoint = `+s.ph(4)+`,
		scopes_supported = `+s.ph(5)+`,
		discovered_at = `+s.ph(6)+`,
		updated_at = `+s.ph(7)+`
		WHERE auth_server_url = `+s.ph(8)+` AND redirect_uri = `+s.ph(9),
		reg.ClientID, secretEnc, reg.AuthorizationEndpoint, reg.TokenEndpoint,
		reg.ScopesSupported, reg.DiscoveredAt, now,
		reg.AuthServerURL, reg.RedirectURI,
	)
	if err != nil {
		return fmt.Errorf("updating registration: %w", err)
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO oauth_registrations
			(id, auth_server_url, redirect_uri, client_id, client_secret_encrypted,
			 authorization_endpoint, token_endpoint, scopes_supported, discovered_at, created_at, updated_at)
			VALUES (`+s.ph(1)+`, `+s.ph(2)+`, `+s.ph(3)+`, `+s.ph(4)+`, `+s.ph(5)+`, `+s.ph(6)+`, `+s.ph(7)+`, `+s.ph(8)+`, `+s.ph(9)+`, `+s.ph(10)+`, `+s.ph(11)+`)`,
			uuid.NewString(), reg.AuthServerURL, reg.RedirectURI, reg.ClientID,
			secretEnc, reg.AuthorizationEndpoint, reg.TokenEndpoint,
			reg.ScopesSupported, reg.DiscoveredAt, now, now,
		)
		if err != nil {
			return fmt.Errorf("inserting registration: %w", err)
		}
	}

	return tx.Commit()
}

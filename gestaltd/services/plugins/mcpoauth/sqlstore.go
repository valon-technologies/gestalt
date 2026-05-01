package mcpoauth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core/crypto"
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
	expires_at DATETIME NULL,
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
	       expires_at, authorization_endpoint, token_endpoint, scopes_supported, discovered_at
	FROM oauth_registrations
	WHERE auth_server_url = ` + s.ph(1) + ` AND redirect_uri = ` + s.ph(2)

	row := s.db.QueryRowContext(ctx, query, authServerURL, redirectURI)

	var reg Registration
	var secretEnc sql.NullString
	var scopes sql.NullString
	var expiresAt sql.NullTime
	var id string
	err := row.Scan(&id, &reg.AuthServerURL, &reg.RedirectURI, &reg.ClientID,
		&secretEnc, &expiresAt, &reg.AuthorizationEndpoint, &reg.TokenEndpoint,
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
	if expiresAt.Valid {
		reg.ExpiresAt = &expiresAt.Time
	}
	reg.ScopesSupported = scopes.String

	return &reg, nil
}

func (s *SQLStore) DeleteRegistration(ctx context.Context, authServerURL, redirectURI string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM oauth_registrations WHERE auth_server_url = `+s.ph(1)+` AND redirect_uri = `+s.ph(2),
		authServerURL, redirectURI,
	)
	return err
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

	var expiresAt sql.NullTime
	if reg.ExpiresAt != nil {
		expiresAt = sql.NullTime{Time: *reg.ExpiresAt, Valid: true}
	}

	now := time.Now()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `UPDATE oauth_registrations SET
		client_id = `+s.ph(1)+`,
		client_secret_encrypted = `+s.ph(2)+`,
		expires_at = `+s.ph(3)+`,
		authorization_endpoint = `+s.ph(4)+`,
		token_endpoint = `+s.ph(5)+`,
		scopes_supported = `+s.ph(6)+`,
		discovered_at = `+s.ph(7)+`,
		updated_at = `+s.ph(8)+`
		WHERE auth_server_url = `+s.ph(9)+` AND redirect_uri = `+s.ph(10),
		reg.ClientID, secretEnc, expiresAt, reg.AuthorizationEndpoint, reg.TokenEndpoint,
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
			 expires_at, authorization_endpoint, token_endpoint, scopes_supported, discovered_at, created_at, updated_at)
			VALUES (`+s.ph(1)+`, `+s.ph(2)+`, `+s.ph(3)+`, `+s.ph(4)+`, `+s.ph(5)+`, `+s.ph(6)+`, `+s.ph(7)+`, `+s.ph(8)+`, `+s.ph(9)+`, `+s.ph(10)+`, `+s.ph(11)+`, `+s.ph(12)+`)`,
			uuid.NewString(), reg.AuthServerURL, reg.RedirectURI, reg.ClientID,
			secretEnc, expiresAt, reg.AuthorizationEndpoint, reg.TokenEndpoint,
			reg.ScopesSupported, reg.DiscoveredAt, now, now,
		)
		if err != nil {
			return fmt.Errorf("inserting registration: %w", err)
		}
	}

	return tx.Commit()
}

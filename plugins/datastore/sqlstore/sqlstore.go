// Package sqlstore provides a shared SQL-backed implementation of
// core.Datastore. Driver-specific packages (sqlite, postgres, mysql)
// supply a Dialect and connection setup, then embed *Store for the
// common query logic.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/core/crypto"
)

// Dialect captures the SQL differences between database engines.
type Dialect interface {
	// Placeholder returns the bind-parameter placeholder for the
	// n-th argument (1-based). For MySQL/SQLite this is always "?";
	// for PostgreSQL it is "$1", "$2", etc.
	Placeholder(n int) string

	// UpsertTokenSQL returns the full INSERT ... ON CONFLICT / ON
	// DUPLICATE KEY UPDATE statement for integration_tokens. The
	// statement must accept 13 bind parameters.
	UpsertTokenSQL() string

	// IsDuplicateKeyError reports whether err is a driver-specific
	// unique-constraint violation. Dialects that handle duplicates
	// via ON CONFLICT in the INSERT itself may always return false.
	IsDuplicateKeyError(err error) bool
}

// Store implements every core.Datastore method except Migrate (which
// remains driver-specific because DDL varies across engines).
type Store struct {
	DB      *sql.DB
	Enc     *crypto.AESGCMEncryptor
	Dialect Dialect
}

// New creates a Store. Callers are responsible for opening the *sql.DB
// and configuring connection pool settings before passing it in.
func New(db *sql.DB, enc *crypto.AESGCMEncryptor, dialect Dialect) *Store {
	return &Store{DB: db, Enc: enc, Dialect: dialect}
}

// Ping verifies the database connection is alive.
func (s *Store) Ping(ctx context.Context) error {
	return s.DB.PingContext(ctx)
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.DB.Close()
}

// ph is a shorthand for s.Dialect.Placeholder.
func (s *Store) ph(n int) string { return s.Dialect.Placeholder(n) }

// Scanner is the subset of *sql.Row / *sql.Rows used by scan helpers.
type Scanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanIntegrationToken(row Scanner) (*core.IntegrationToken, error) {
	var t core.IntegrationToken
	var accessEnc, refreshEnc sql.NullString
	var scopes, metadataJSON sql.NullString
	var expiresAt sql.NullTime

	if err := row.Scan(&t.ID, &t.UserID, &t.Integration, &t.Instance,
		&accessEnc, &refreshEnc,
		&scopes, &expiresAt, &t.LastRefreshedAt, &t.RefreshErrorCount,
		&metadataJSON, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}

	t.Scopes = scopes.String
	t.MetadataJSON = metadataJSON.String

	if expiresAt.Valid {
		t.ExpiresAt = &expiresAt.Time
	}

	var err error
	t.AccessToken, t.RefreshToken, err = s.Enc.DecryptTokenPair(accessEnc.String, refreshEnc.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanAPIToken(row Scanner) (*core.APIToken, error) {
	var t core.APIToken
	var scopes sql.NullString
	var expiresAt sql.NullTime

	if err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.HashedToken,
		&scopes, &expiresAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Scopes = scopes.String
	if expiresAt.Valid {
		t.ExpiresAt = &expiresAt.Time
	}
	return &t, nil
}

// NullableTime converts a *time.Time to a value suitable for a
// nullable SQL DATETIME/TIMESTAMPTZ column.
func NullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

// ---------------------------------------------------------------------------
// Datastore methods
// ---------------------------------------------------------------------------

func scanUser(row Scanner) (core.User, error) {
	var u core.User
	var displayName sql.NullString
	if err := row.Scan(&u.ID, &u.Email, &displayName, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return u, err
	}
	u.DisplayName = displayName.String
	return u, nil
}

func (s *Store) GetUser(ctx context.Context, id string) (*core.User, error) {
	row := s.DB.QueryRowContext(ctx,
		"SELECT id, email, display_name, created_at, updated_at FROM users WHERE id = "+s.ph(1),
		id,
	)
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, core.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user by id: %w", err)
	}
	return &user, nil
}

func (s *Store) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	row := s.DB.QueryRowContext(ctx,
		"SELECT id, email, display_name, created_at, updated_at FROM users WHERE email = "+s.ph(1),
		email,
	)
	user, err := scanUser(row)
	if err == nil {
		return &user, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("querying user: %w", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	user = core.User{
		ID:        uuid.NewString(),
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = s.DB.ExecContext(ctx,
		"INSERT INTO users (id, email, display_name, created_at, updated_at) VALUES ("+
			s.ph(1)+", "+s.ph(2)+", "+s.ph(3)+", "+s.ph(4)+", "+s.ph(5)+")",
		user.ID, user.Email, user.DisplayName, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		if s.Dialect.IsDuplicateKeyError(err) {
			reRow := s.DB.QueryRowContext(ctx,
				"SELECT id, email, display_name, created_at, updated_at FROM users WHERE email = "+s.ph(1),
				email,
			)
			user, err2 := scanUser(reRow)
			if err2 != nil {
				return nil, fmt.Errorf("re-querying user after duplicate key: %w", err2)
			}
			return &user, nil
		}
		return nil, fmt.Errorf("inserting user: %w", err)
	}
	return &user, nil
}

func (s *Store) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	accessEnc, refreshEnc, err := s.Enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("encrypting token pair: %w", err)
	}

	_, err = s.DB.ExecContext(ctx, s.Dialect.UpsertTokenSQL(),
		token.ID, token.UserID, token.Integration, token.Instance,
		accessEnc, refreshEnc,
		token.Scopes, NullableTime(token.ExpiresAt), token.LastRefreshedAt,
		token.RefreshErrorCount, token.MetadataJSON, token.CreatedAt, token.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upserting integration token: %w", err)
	}
	return nil
}

func (s *Store) Token(ctx context.Context, userID, integration, instance string) (*core.IntegrationToken, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted,
		       scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at
		FROM integration_tokens
		WHERE user_id = `+s.ph(1)+` AND integration = `+s.ph(2)+` AND instance = `+s.ph(3),
		userID, integration, instance,
	)
	t, err := s.scanIntegrationToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying token: %w", err)
	}
	return t, nil
}

func (s *Store) ListTokens(ctx context.Context, userID string) ([]*core.IntegrationToken, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted,
		       scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at
		FROM integration_tokens WHERE user_id = `+s.ph(1), userID)
	if err != nil {
		return nil, fmt.Errorf("listing tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tokens []*core.IntegrationToken
	for rows.Next() {
		t, err := s.scanIntegrationToken(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning token row: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (s *Store) DeleteToken(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM integration_tokens WHERE id = "+s.ph(1), id)
	if err != nil {
		return fmt.Errorf("deleting token: %w", err)
	}
	return nil
}

func (s *Store) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO api_tokens (id, user_id, name, hashed_token, scopes, expires_at, created_at, updated_at)
		VALUES (`+s.ph(1)+`, `+s.ph(2)+`, `+s.ph(3)+`, `+s.ph(4)+`, `+s.ph(5)+`, `+s.ph(6)+`, `+s.ph(7)+`, `+s.ph(8)+`)`,
		token.ID, token.UserID, token.Name, token.HashedToken,
		token.Scopes, NullableTime(token.ExpiresAt), token.CreatedAt, token.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting api token: %w", err)
	}
	return nil
}

func (s *Store) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, name, hashed_token, scopes, expires_at, created_at, updated_at
		FROM api_tokens WHERE hashed_token = `+s.ph(1),
		hashedToken,
	)
	t, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("validating api token: %w", err)
	}
	return t, nil
}

func (s *Store) ListAPITokens(ctx context.Context, userID string) ([]*core.APIToken, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, name, hashed_token, scopes, expires_at, created_at, updated_at
		FROM api_tokens WHERE user_id = `+s.ph(1), userID)
	if err != nil {
		return nil, fmt.Errorf("listing api tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tokens []*core.APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning api token row: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (s *Store) RevokeAPIToken(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM api_tokens WHERE id = "+s.ph(1), id)
	if err != nil {
		return fmt.Errorf("revoking api token: %w", err)
	}
	return nil
}

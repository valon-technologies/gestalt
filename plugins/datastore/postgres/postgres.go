package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/core/crypto"

	_ "github.com/jackc/pgx/v5/stdlib" // register database/sql driver
)

var _ core.Datastore = (*Store)(nil)

type Store struct {
	db  *sql.DB
	enc *crypto.AESGCMEncryptor
}

func New(dsn string, encryptionKey []byte) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening postgres: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	enc, err := crypto.NewAESGCM(encryptionKey)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating encryptor: %w", err)
	}

	return &Store{db: db, enc: enc}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
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
			instance TEXT NOT NULL,
			access_token_encrypted TEXT NOT NULL,
			refresh_token_encrypted TEXT NOT NULL DEFAULT '',
			scopes TEXT NOT NULL DEFAULT '',
			expires_at TIMESTAMPTZ,
			last_refreshed_at TIMESTAMPTZ NOT NULL,
			refresh_error_count INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			UNIQUE(user_id, integration, instance)
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

func (s *Store) Close() error {
	return s.db.Close()
}

type scanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanIntegrationToken(row scanner) (*core.IntegrationToken, error) {
	var t core.IntegrationToken
	var accessEnc, refreshEnc string
	var expiresAt sql.NullTime

	if err := row.Scan(&t.ID, &t.UserID, &t.Integration, &t.Instance,
		&accessEnc, &refreshEnc,
		&t.Scopes, &expiresAt, &t.LastRefreshedAt, &t.RefreshErrorCount,
		&t.MetadataJSON, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}

	if expiresAt.Valid {
		t.ExpiresAt = &expiresAt.Time
	}

	var err error
	t.AccessToken, err = s.enc.Decrypt(accessEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypting access token: %w", err)
	}
	t.RefreshToken, err = s.enc.Decrypt(refreshEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypting refresh token: %w", err)
	}
	return &t, nil
}

func scanAPIToken(row scanner) (*core.APIToken, error) {
	var t core.APIToken
	var expiresAt sql.NullTime

	if err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.HashedToken,
		&t.Scopes, &expiresAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		t.ExpiresAt = &expiresAt.Time
	}
	return &t, nil
}

func (s *Store) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	var user core.User
	err := s.db.QueryRowContext(ctx,
		"SELECT id, email, display_name, created_at, updated_at FROM users WHERE email = $1",
		email,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
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

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO users (id, email, display_name, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)",
		user.ID, user.Email, user.DisplayName, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			err2 := s.db.QueryRowContext(ctx,
				"SELECT id, email, display_name, created_at, updated_at FROM users WHERE email = $1",
				email,
			).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
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
	accessEnc, err := s.enc.Encrypt(token.AccessToken)
	if err != nil {
		return fmt.Errorf("encrypting access token: %w", err)
	}
	refreshEnc, err := s.enc.Encrypt(token.RefreshToken)
	if err != nil {
		return fmt.Errorf("encrypting refresh token: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO integration_tokens
			(id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted,
			 scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT(user_id, integration, instance) DO UPDATE SET
			access_token_encrypted = EXCLUDED.access_token_encrypted,
			refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
			scopes = EXCLUDED.scopes,
			expires_at = EXCLUDED.expires_at,
			last_refreshed_at = EXCLUDED.last_refreshed_at,
			refresh_error_count = EXCLUDED.refresh_error_count,
			metadata_json = EXCLUDED.metadata_json,
			updated_at = EXCLUDED.updated_at`,
		token.ID, token.UserID, token.Integration, token.Instance,
		accessEnc, refreshEnc,
		token.Scopes, nullableTime(token.ExpiresAt), token.LastRefreshedAt,
		token.RefreshErrorCount, token.MetadataJSON, token.CreatedAt, token.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upserting integration token: %w", err)
	}
	return nil
}

func (s *Store) Token(ctx context.Context, userID, integration, instance string) (*core.IntegrationToken, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted,
		       scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at
		FROM integration_tokens
		WHERE user_id = $1 AND integration = $2 AND instance = $3`,
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
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted,
		       scopes, expires_at, last_refreshed_at, refresh_error_count, metadata_json, created_at, updated_at
		FROM integration_tokens WHERE user_id = $1`, userID)
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
	_, err := s.db.ExecContext(ctx, "DELETE FROM integration_tokens WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("deleting token: %w", err)
	}
	return nil
}

func (s *Store) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_tokens (id, user_id, name, hashed_token, scopes, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		token.ID, token.UserID, token.Name, token.HashedToken,
		token.Scopes, nullableTime(token.ExpiresAt), token.CreatedAt, token.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting api token: %w", err)
	}
	return nil
}

func (s *Store) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, hashed_token, scopes, expires_at, created_at, updated_at
		FROM api_tokens WHERE hashed_token = $1`,
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
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, hashed_token, scopes, expires_at, created_at, updated_at
		FROM api_tokens WHERE user_id = $1`, userID)
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
	_, err := s.db.ExecContext(ctx, "DELETE FROM api_tokens WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("revoking api token: %w", err)
	}
	return nil
}

func isDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

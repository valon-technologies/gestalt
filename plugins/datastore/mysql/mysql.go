// Package mysql implements core.Datastore backed by a MySQL database.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/core/crypto"
)

type Store struct {
	db  *sql.DB
	enc *crypto.AESGCMEncryptor
}

func New(dsn string, encryptionKey []byte) (*Store, error) {
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing dsn: %w", err)
	}
	cfg.ParseTime = true
	finalDSN := cfg.FormatDSN()

	db, err := sql.Open("mysql", finalDSN)
	if err != nil {
		return nil, fmt.Errorf("opening mysql: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	enc, err := crypto.NewAESGCM(encryptionKey)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating encryptor: %w", err)
	}

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pinging mysql: %w", err)
	}

	return &Store{db: db, enc: enc}, nil
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
			last_refreshed_at DATETIME(6) NOT NULL,
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
	}

	for i, stmt := range migrations {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration %d: %w", i, err)
		}
	}
	return nil
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

func (s *Store) GetUser(ctx context.Context, id string) (*core.User, error) {
	var user core.User
	err := s.db.QueryRowContext(ctx,
		"SELECT id, email, display_name, created_at, updated_at FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, core.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user by id: %w", err)
	}
	return &user, nil
}

func (s *Store) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	// Fast path: user already exists.
	var user core.User
	err := s.db.QueryRowContext(ctx,
		"SELECT id, email, display_name, created_at, updated_at FROM users WHERE email = ?",
		email,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	if err == nil {
		return &user, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("querying user: %w", err)
	}

	// User does not exist; attempt insert.
	now := time.Now().UTC().Truncate(time.Second)
	user = core.User{
		ID:        uuid.NewString(),
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO users (id, email, display_name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		user.ID, user.Email, user.DisplayName, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		// Concurrent insert race: another goroutine created the row first.
		if isDuplicateKeyError(err) {
			err2 := s.db.QueryRowContext(ctx,
				"SELECT id, email, display_name, created_at, updated_at FROM users WHERE email = ?",
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			access_token_encrypted = VALUES(access_token_encrypted),
			refresh_token_encrypted = VALUES(refresh_token_encrypted),
			scopes = VALUES(scopes),
			expires_at = VALUES(expires_at),
			last_refreshed_at = VALUES(last_refreshed_at),
			refresh_error_count = VALUES(refresh_error_count),
			metadata_json = VALUES(metadata_json),
			updated_at = VALUES(updated_at)`,
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
		WHERE user_id = ? AND integration = ? AND instance = ?`,
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
		FROM integration_tokens WHERE user_id = ?`, userID)
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
	_, err := s.db.ExecContext(ctx, "DELETE FROM integration_tokens WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting token: %w", err)
	}
	return nil
}

func (s *Store) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_tokens (id, user_id, name, hashed_token, scopes, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
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
		FROM api_tokens WHERE hashed_token = ?`,
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
		FROM api_tokens WHERE user_id = ?`, userID)
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
	_, err := s.db.ExecContext(ctx, "DELETE FROM api_tokens WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("revoking api token: %w", err)
	}
	return nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func isDuplicateKeyError(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

var _ core.Datastore = (*Store)(nil)

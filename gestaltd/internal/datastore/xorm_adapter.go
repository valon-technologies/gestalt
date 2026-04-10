package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"xorm.io/xorm"
)

type userRow struct {
	ID          string    `xorm:"varchar(36) pk 'id'"`
	Email       string    `xorm:"varchar(255) unique notnull 'email'"`
	DisplayName string    `xorm:"varchar(255) 'display_name'"`
	CreatedAt   time.Time `xorm:"created 'created_at'"`
	UpdatedAt   time.Time `xorm:"updated 'updated_at'"`
}

func (userRow) TableName() string { return "users" }

type integrationTokenRow struct {
	ID                 string     `xorm:"varchar(36) pk 'id'"`
	UserID             string     `xorm:"varchar(36) notnull index 'user_id'"`
	Integration        string     `xorm:"varchar(128) notnull 'integration'"`
	Connection         string     `xorm:"varchar(128) notnull 'connection'"`
	Instance           string     `xorm:"varchar(128) 'instance'"`
	AccessTokenSealed  string     `xorm:"text 'access_token_sealed'"`
	RefreshTokenSealed string     `xorm:"text 'refresh_token_sealed'"`
	Scopes             string     `xorm:"text 'scopes'"`
	ExpiresAt          *time.Time `xorm:"'expires_at'"`
	LastRefreshedAt    *time.Time `xorm:"'last_refreshed_at'"`
	RefreshErrorCount  int        `xorm:"'refresh_error_count'"`
	MetadataJSON       string     `xorm:"text 'metadata_json'"`
	CreatedAt          time.Time  `xorm:"created 'created_at'"`
	UpdatedAt          time.Time  `xorm:"updated 'updated_at'"`
}

func (integrationTokenRow) TableName() string { return "integration_tokens" }

type apiTokenRow struct {
	ID          string     `xorm:"varchar(36) pk 'id'"`
	UserID      string     `xorm:"varchar(36) notnull 'user_id'"`
	Name        string     `xorm:"varchar(255) 'name'"`
	HashedToken string     `xorm:"varchar(255) unique notnull 'hashed_token'"`
	Scopes      string     `xorm:"text 'scopes'"`
	ExpiresAt   *time.Time `xorm:"'expires_at'"`
	CreatedAt   time.Time  `xorm:"created 'created_at'"`
	UpdatedAt   time.Time  `xorm:"updated 'updated_at'"`
}

func (apiTokenRow) TableName() string { return "api_tokens" }

type XORMAdapter struct {
	engine *xorm.Engine
	enc    *corecrypto.AESGCMEncryptor
}

func NewXORMAdapter(engine *xorm.Engine, enc *corecrypto.AESGCMEncryptor) (*XORMAdapter, error) {
	if err := engine.Sync2(new(userRow), new(integrationTokenRow), new(apiTokenRow)); err != nil {
		return nil, fmt.Errorf("sync schema: %w", err)
	}
	return &XORMAdapter{engine: engine, enc: enc}, nil
}

func (a *XORMAdapter) GetUser(ctx context.Context, id string) (*core.User, error) {
	var row userRow
	found, err := a.engine.Context(ctx).Where("id = ?", id).Get(&row)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if !found {
		return nil, core.ErrNotFound
	}
	return userRowToCore(&row), nil
}

func (a *XORMAdapter) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	var row userRow
	found, err := a.engine.Context(ctx).Where("email = ?", email).Get(&row)
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	if found {
		return userRowToCore(&row), nil
	}

	row = userRow{
		ID:    uuid.New().String(),
		Email: email,
	}
	if _, err := a.engine.Context(ctx).Insert(&row); err != nil {
		// Race condition: another request may have created the user concurrently.
		// Retry the lookup instead of failing on unique constraint violation.
		var retryRow userRow
		found, retryErr := a.engine.Context(ctx).Where("email = ?", email).Get(&retryRow)
		if retryErr != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		if found {
			return userRowToCore(&retryRow), nil
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return userRowToCore(&row), nil
}

func (a *XORMAdapter) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	accessEnc, refreshEnc, err := a.enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("encrypt token pair: %w", err)
	}
	if token.ID == "" {
		token.ID = uuid.New().String()
	}

	row := integrationTokenRow{
		ID:                 token.ID,
		UserID:             token.UserID,
		Integration:        token.Integration,
		Connection:         token.Connection,
		Instance:           token.Instance,
		AccessTokenSealed:  accessEnc,
		RefreshTokenSealed: refreshEnc,
		Scopes:             token.Scopes,
		ExpiresAt:          token.ExpiresAt,
		LastRefreshedAt:    token.LastRefreshedAt,
		RefreshErrorCount:  token.RefreshErrorCount,
		MetadataJSON:       token.MetadataJSON,
	}

	var existing integrationTokenRow
	found, err := a.engine.Context(ctx).ID(token.ID).Get(&existing)
	if err != nil {
		return fmt.Errorf("check existing token: %w", err)
	}
	if found {
		if _, err := a.engine.Context(ctx).ID(token.ID).AllCols().Update(&row); err != nil {
			return fmt.Errorf("update token: %w", err)
		}
	} else {
		if _, err := a.engine.Context(ctx).Insert(&row); err != nil {
			return fmt.Errorf("insert token: %w", err)
		}
	}
	return nil
}

func (a *XORMAdapter) Token(ctx context.Context, userID, integration, connection, instance string) (*core.IntegrationToken, error) {
	var row integrationTokenRow
	found, err := a.engine.Context(ctx).
		Where("user_id = ? AND integration = ? AND connection = ? AND instance = ?", userID, integration, connection, instance).
		Get(&row)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	if !found {
		return nil, core.ErrNotFound
	}
	return a.tokenRowToCore(&row)
}

func (a *XORMAdapter) ListTokens(ctx context.Context, userID string) ([]*core.IntegrationToken, error) {
	var rows []integrationTokenRow
	if err := a.engine.Context(ctx).Where("user_id = ?", userID).Find(&rows); err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return a.tokenRowsToCore(rows)
}

func (a *XORMAdapter) ListTokensForIntegration(ctx context.Context, userID, integration string) ([]*core.IntegrationToken, error) {
	var rows []integrationTokenRow
	if err := a.engine.Context(ctx).Where("user_id = ? AND integration = ?", userID, integration).Find(&rows); err != nil {
		return nil, fmt.Errorf("list tokens for integration: %w", err)
	}
	return a.tokenRowsToCore(rows)
}

func (a *XORMAdapter) ListTokensForConnection(ctx context.Context, userID, integration, connection string) ([]*core.IntegrationToken, error) {
	var rows []integrationTokenRow
	if err := a.engine.Context(ctx).Where("user_id = ? AND integration = ? AND connection = ?", userID, integration, connection).Find(&rows); err != nil {
		return nil, fmt.Errorf("list tokens for connection: %w", err)
	}
	return a.tokenRowsToCore(rows)
}

func (a *XORMAdapter) DeleteToken(ctx context.Context, id string) error {
	if _, err := a.engine.Context(ctx).ID(id).Delete(&integrationTokenRow{}); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	return nil
}

func (a *XORMAdapter) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	row := apiTokenRow{
		ID:          token.ID,
		UserID:      token.UserID,
		Name:        token.Name,
		HashedToken: token.HashedToken,
		Scopes:      token.Scopes,
		ExpiresAt:   token.ExpiresAt,
	}
	if _, err := a.engine.Context(ctx).Insert(&row); err != nil {
		return fmt.Errorf("store api token: %w", err)
	}
	return nil
}

func (a *XORMAdapter) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	var row apiTokenRow
	found, err := a.engine.Context(ctx).Where("hashed_token = ?", hashedToken).Get(&row)
	if err != nil {
		return nil, fmt.Errorf("validate api token: %w", err)
	}
	if !found {
		return nil, core.ErrNotFound
	}
	if row.ExpiresAt != nil && row.ExpiresAt.Before(time.Now()) {
		return nil, core.ErrNotFound
	}
	return apiTokenRowToCore(&row), nil
}

func (a *XORMAdapter) ListAPITokens(ctx context.Context, userID string) ([]*core.APIToken, error) {
	var rows []apiTokenRow
	if err := a.engine.Context(ctx).Where("user_id = ?", userID).Find(&rows); err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	out := make([]*core.APIToken, len(rows))
	for i := range rows {
		out[i] = apiTokenRowToCore(&rows[i])
	}
	return out, nil
}

func (a *XORMAdapter) RevokeAPIToken(ctx context.Context, userID, id string) error {
	affected, err := a.engine.Context(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&apiTokenRow{})
	if err != nil {
		return fmt.Errorf("revoke api token: %w", err)
	}
	if affected == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (a *XORMAdapter) RevokeAllAPITokens(ctx context.Context, userID string) (int64, error) {
	affected, err := a.engine.Context(ctx).Where("user_id = ?", userID).Delete(&apiTokenRow{})
	if err != nil {
		return 0, fmt.Errorf("revoke all api tokens: %w", err)
	}
	return affected, nil
}

func (a *XORMAdapter) Ping(_ context.Context) error {
	return a.engine.Ping()
}

func (a *XORMAdapter) Migrate(_ context.Context) error {
	return nil
}

func (a *XORMAdapter) Close() error {
	return a.engine.Close()
}

func userRowToCore(r *userRow) *core.User {
	return &core.User{
		ID:          r.ID,
		Email:       r.Email,
		DisplayName: r.DisplayName,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func (a *XORMAdapter) tokenRowToCore(r *integrationTokenRow) (*core.IntegrationToken, error) {
	access, refresh, err := a.enc.DecryptTokenPair(r.AccessTokenSealed, r.RefreshTokenSealed)
	if err != nil {
		return nil, fmt.Errorf("decrypt token pair: %w", err)
	}
	return &core.IntegrationToken{
		ID:                r.ID,
		UserID:            r.UserID,
		Integration:       r.Integration,
		Connection:        r.Connection,
		Instance:          r.Instance,
		AccessToken:       access,
		RefreshToken:      refresh,
		Scopes:            r.Scopes,
		ExpiresAt:         r.ExpiresAt,
		LastRefreshedAt:   r.LastRefreshedAt,
		RefreshErrorCount: r.RefreshErrorCount,
		MetadataJSON:      r.MetadataJSON,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}, nil
}

func (a *XORMAdapter) tokenRowsToCore(rows []integrationTokenRow) ([]*core.IntegrationToken, error) {
	out := make([]*core.IntegrationToken, 0, len(rows))
	for i := range rows {
		t, err := a.tokenRowToCore(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func apiTokenRowToCore(r *apiTokenRow) *core.APIToken {
	return &core.APIToken{
		ID:          r.ID,
		UserID:      r.UserID,
		Name:        r.Name,
		HashedToken: r.HashedToken,
		Scopes:      r.Scopes,
		ExpiresAt:   r.ExpiresAt,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

var _ core.Datastore = (*XORMAdapter)(nil)

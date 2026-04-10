package coredata

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/datastore"
)

// CompatDatastore implements core.Datastore using the IndexedDB-inspired
// datastore abstraction. It delegates to UserService, TokenService, and
// APITokenService which operate on ObjectStore and Index.
type CompatDatastore struct {
	ds        datastore.Datastore
	Users     *UserService
	Tokens    *TokenService
	APITokens *APITokenService
}

func New(ds datastore.Datastore, enc *corecrypto.AESGCMEncryptor) (*CompatDatastore, error) {
	ctx := context.Background()
	if err := ds.CreateObjectStore(ctx, StoreUsers, UsersSchema); err != nil {
		return nil, fmt.Errorf("create users store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreIntegrationTokens, IntegrationTokensSchema); err != nil {
		return nil, fmt.Errorf("create integration_tokens store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAPITokens, APITokensSchema); err != nil {
		return nil, fmt.Errorf("create api_tokens store: %w", err)
	}
	return &CompatDatastore{
		ds:        ds,
		Users:     NewUserService(ds),
		Tokens:    NewTokenService(ds, enc),
		APITokens: NewAPITokenService(ds),
	}, nil
}

func (d *CompatDatastore) Store() datastore.Datastore { return d.ds }

func (d *CompatDatastore) GetUser(ctx context.Context, id string) (*core.User, error) {
	return d.Users.GetUser(ctx, id)
}
func (d *CompatDatastore) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	return d.Users.FindOrCreateUser(ctx, email)
}
func (d *CompatDatastore) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	return d.Tokens.StoreToken(ctx, token)
}
func (d *CompatDatastore) Token(ctx context.Context, userID, integration, connection, instance string) (*core.IntegrationToken, error) {
	return d.Tokens.Token(ctx, userID, integration, connection, instance)
}
func (d *CompatDatastore) ListTokens(ctx context.Context, userID string) ([]*core.IntegrationToken, error) {
	return d.Tokens.ListTokens(ctx, userID)
}
func (d *CompatDatastore) ListTokensForIntegration(ctx context.Context, userID, integration string) ([]*core.IntegrationToken, error) {
	return d.Tokens.ListTokensForIntegration(ctx, userID, integration)
}
func (d *CompatDatastore) ListTokensForConnection(ctx context.Context, userID, integration, connection string) ([]*core.IntegrationToken, error) {
	return d.Tokens.ListTokensForConnection(ctx, userID, integration, connection)
}
func (d *CompatDatastore) DeleteToken(ctx context.Context, id string) error {
	return d.Tokens.DeleteToken(ctx, id)
}
func (d *CompatDatastore) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	return d.APITokens.StoreAPIToken(ctx, token)
}
func (d *CompatDatastore) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	return d.APITokens.ValidateAPIToken(ctx, hashedToken)
}
func (d *CompatDatastore) ListAPITokens(ctx context.Context, userID string) ([]*core.APIToken, error) {
	return d.APITokens.ListAPITokens(ctx, userID)
}
func (d *CompatDatastore) RevokeAPIToken(ctx context.Context, userID, id string) error {
	return d.APITokens.RevokeAPIToken(ctx, userID, id)
}
func (d *CompatDatastore) RevokeAllAPITokens(ctx context.Context, userID string) (int64, error) {
	return d.APITokens.RevokeAllAPITokens(ctx, userID)
}
func (d *CompatDatastore) Ping(ctx context.Context) error {
	return d.ds.Ping(ctx)
}

func (d *CompatDatastore) Migrate(ctx context.Context) error {
	return nil
}

func (d *CompatDatastore) Close() error {
	return d.ds.Close()
}

func (d *CompatDatastore) Name() string {
	type named interface{ Name() string }
	if n, ok := d.ds.(named); ok {
		return n.Name()
	}
	return "unknown"
}

var _ core.Datastore = (*CompatDatastore)(nil)

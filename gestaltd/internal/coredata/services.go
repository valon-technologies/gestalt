package coredata

import (
	"context"
	"fmt"

	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type Services struct {
	Users                *UserService
	Tokens               *TokenService
	APITokens            *APITokenService
	PluginAuthorizations *PluginAuthorizationService
	AdminAuthorizations  *AdminAuthorizationService
	DB                   indexeddb.IndexedDB
}

func New(ds indexeddb.IndexedDB, enc *corecrypto.AESGCMEncryptor) (*Services, error) {
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
	users := NewUserService(ds)
	if err := users.BackfillNormalizedEmails(ctx); err != nil {
		return nil, fmt.Errorf("backfill users store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StorePluginAuthorizationMemberships, PluginAuthorizationMembershipsSchema); err != nil {
		return nil, fmt.Errorf("create plugin_authorization_memberships store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAdminAuthorizationMemberships, AdminAuthorizationMembershipsSchema); err != nil {
		return nil, fmt.Errorf("create admin_authorization_memberships store: %w", err)
	}
	return &Services{
		Users:                users,
		Tokens:               NewTokenService(ds, enc),
		APITokens:            NewAPITokenService(ds),
		PluginAuthorizations: NewPluginAuthorizationService(ds),
		AdminAuthorizations:  NewAdminAuthorizationService(ds),
		DB:                   ds,
	}, nil
}

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}

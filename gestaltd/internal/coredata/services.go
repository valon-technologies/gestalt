package coredata

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type Services struct {
	Users                *UserService
	Tokens               *TokenService
	APITokens            *APITokenService
	PluginAuthorizations *PluginAuthorizationService
	DB                   indexeddb.IndexedDB
}

func New(ds indexeddb.IndexedDB, enc *corecrypto.AESGCMEncryptor) (*Services, error) {
	ctx := context.Background()
	users, err := initUserService(ctx, ds)
	if err != nil {
		return nil, err
	}
	if err := ds.CreateObjectStore(ctx, StoreIntegrationTokens, IntegrationTokensSchema); err != nil {
		return nil, fmt.Errorf("create integration_tokens store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAPITokens, APITokensSchema); err != nil {
		return nil, fmt.Errorf("create api_tokens store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StorePluginAuthorizationMemberships, PluginAuthorizationMembershipsSchema); err != nil {
		return nil, fmt.Errorf("create plugin_authorization_memberships store: %w", err)
	}
	return &Services{
		Users:                users,
		Tokens:               NewTokenService(ds, enc),
		APITokens:            NewAPITokenService(ds),
		PluginAuthorizations: NewPluginAuthorizationService(ds),
		DB:                   ds,
	}, nil
}

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}

func initUserService(ctx context.Context, ds indexeddb.IndexedDB) (*UserService, error) {
	if err := ds.CreateObjectStore(ctx, StoreUsers, UsersSchema); err == nil {
		users := NewUserService(ds, true)
		if err := users.BackfillNormalizedEmails(ctx); err != nil {
			return nil, fmt.Errorf("backfill users store: %w", err)
		}
		return users, nil
	} else if !isLegacyUsersSchemaError(err) {
		return nil, fmt.Errorf("create users store: %w", err)
	}

	if err := ds.CreateObjectStore(ctx, StoreUsers, LegacyUsersSchema); err != nil {
		return nil, fmt.Errorf("create users store: %w", err)
	}
	slog.Warn("coredata: users store missing normalized_email support; continuing in legacy compatibility mode")
	return NewUserService(ds, false), nil
}

func isLegacyUsersSchemaError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "normalized_email")
}

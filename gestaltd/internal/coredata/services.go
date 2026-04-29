package coredata

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/runtimelogs"
)

type Services struct {
	Users               *UserService
	ExternalCredentials core.ExternalCredentialProvider
	APITokens           *APITokenService
	RuntimeSessionLogs  runtimelogs.Store
	DB                  indexeddb.IndexedDB
}

func New(ds indexeddb.IndexedDB) (*Services, error) {
	return NewWithContext(context.Background(), ds)
}

func NewWithContext(ctx context.Context, ds indexeddb.IndexedDB) (*Services, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ds.CreateObjectStore(ctx, StoreUsers, UsersSchema); err != nil {
		return nil, fmt.Errorf("create users store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAPITokens, APITokensSchema); err != nil {
		return nil, fmt.Errorf("create api_tokens store: %w", err)
	}

	runtimeSessionLogs := runtimelogs.NewMemoryStore()

	users := NewUserService(ds)
	apiTokens := NewAPITokenService(ds)
	return &Services{
		ExternalCredentials: nil,
		Users:               users,
		APITokens:           apiTokens,
		RuntimeSessionLogs:  runtimeSessionLogs,
		DB:                  ds,
	}, nil
}

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}

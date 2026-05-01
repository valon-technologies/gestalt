package coredata

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
)

type Services struct {
	Users               *UserService
	ExternalCredentials core.ExternalCredentialProvider
	APITokens           *APITokenService
	ManagedSubjects     *ManagedSubjectService
	MCPOAuthGrants      *MCPOAuthGrantService
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
	if err := ds.CreateObjectStore(ctx, StoreManagedSubjects, ManagedSubjectsSchema); err != nil {
		return nil, fmt.Errorf("create managed_subjects store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreMCPOAuthGrants, MCPOAuthGrantsSchema); err != nil {
		return nil, fmt.Errorf("create mcp_oauth_grants store: %w", err)
	}

	runtimeSessionLogs := runtimelogs.NewMemoryStore()

	users := NewUserService(ds)
	apiTokens := NewAPITokenService(ds)
	managedSubjects := NewManagedSubjectService(ds)
	mcpOAuthGrants := NewMCPOAuthGrantService(ds)
	return &Services{
		ExternalCredentials: nil,
		Users:               users,
		APITokens:           apiTokens,
		ManagedSubjects:     managedSubjects,
		MCPOAuthGrants:      mcpOAuthGrants,
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

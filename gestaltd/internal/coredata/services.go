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
	AgentSessions       *AgentSessionMetadataService
	AgentRunMetadata    *AgentRunMetadataService
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
	if err := ds.CreateObjectStore(ctx, StoreAgentSessionMetadata, AgentSessionMetadataSchema); err != nil {
		return nil, fmt.Errorf("create agent_session_metadata store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAgentSessionIdempotency, AgentSessionIdempotencySchema); err != nil {
		return nil, fmt.Errorf("create agent_session_idempotency store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAgentRunMetadata, AgentRunMetadataSchema); err != nil {
		return nil, fmt.Errorf("create agent_run_metadata store: %w", err)
	}
	if err := ds.CreateObjectStore(ctx, StoreAgentRunIdempotency, AgentRunIdempotencySchema); err != nil {
		return nil, fmt.Errorf("create agent_run_idempotency store: %w", err)
	}

	agentSessions := NewAgentSessionMetadataService(ds)
	agentRunMetadata := NewAgentRunMetadataService(ds)
	runtimeSessionLogs := runtimelogs.NewMemoryStore()

	users := NewUserService(ds)
	if err := users.BackfillNormalizedEmails(ctx); err != nil {
		return nil, fmt.Errorf("backfill users store: %w", err)
	}
	apiTokens := NewAPITokenService(ds)
	return &Services{
		ExternalCredentials: nil,
		Users:               users,
		APITokens:           apiTokens,
		AgentSessions:       agentSessions,
		AgentRunMetadata:    agentRunMetadata,
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

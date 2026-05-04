package agentmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const (
	indexedDBAgentSessionRouteStore = "agent_session_routes"
	indexedDBAgentTurnRouteStore    = "agent_turn_routes"
)

var (
	indexedDBAgentSessionRouteSchema = indexeddb.ObjectStoreSchema{
		Columns: []indexeddb.ColumnDef{
			{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
			{Name: "provider", Type: indexeddb.TypeString, NotNull: true},
		},
	}
	indexedDBAgentTurnRouteSchema = indexeddb.ObjectStoreSchema{
		Columns: []indexeddb.ColumnDef{
			{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
			{Name: "session_id", Type: indexeddb.TypeString, NotNull: true},
			{Name: "provider", Type: indexeddb.TypeString, NotNull: true},
		},
	}
)

type indexedDBRouteStore struct {
	db indexeddb.IndexedDB
}

func NewIndexedDBRouteStore(ctx context.Context, db indexeddb.IndexedDB) (RouteStore, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return nil, fmt.Errorf("agent route store requires an IndexedDB provider")
	}
	if err := db.CreateObjectStore(ctx, indexedDBAgentSessionRouteStore, indexedDBAgentSessionRouteSchema); err != nil && !errors.Is(err, indexeddb.ErrAlreadyExists) {
		return nil, fmt.Errorf("create agent session route store: %w", err)
	}
	if err := db.CreateObjectStore(ctx, indexedDBAgentTurnRouteStore, indexedDBAgentTurnRouteSchema); err != nil && !errors.Is(err, indexeddb.ErrAlreadyExists) {
		return nil, fmt.Errorf("create agent turn route store: %w", err)
	}
	return &indexedDBRouteStore{db: db}, nil
}

func (s *indexedDBRouteStore) LookupSession(ctx context.Context, sessionID string) (AgentRoute, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || s.db == nil || sessionID == "" {
		return AgentRoute{}, false, nil
	}
	record, err := s.db.ObjectStore(indexedDBAgentSessionRouteStore).Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return AgentRoute{}, false, nil
		}
		return AgentRoute{}, false, fmt.Errorf("lookup agent session route %q: %w", sessionID, err)
	}
	providerName := strings.TrimSpace(stringValue(record["provider"]))
	if providerName == "" {
		return AgentRoute{}, false, fmt.Errorf("agent session route %q has no provider", sessionID)
	}
	return AgentRoute{ProviderName: providerName, SessionID: sessionID}, true, nil
}

func (s *indexedDBRouteStore) RememberSession(ctx context.Context, sessionID, providerName string) error {
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if s == nil || s.db == nil || sessionID == "" || providerName == "" {
		return nil
	}
	if err := s.db.ObjectStore(indexedDBAgentSessionRouteStore).Put(ctx, indexeddb.Record{
		"id":       sessionID,
		"provider": providerName,
	}); err != nil {
		return fmt.Errorf("remember agent session route %q: %w", sessionID, err)
	}
	return nil
}

func (s *indexedDBRouteStore) ForgetSession(ctx context.Context, sessionID, providerName string) error {
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if s == nil || s.db == nil || sessionID == "" {
		return nil
	}
	return s.forgetMatching(ctx, indexedDBAgentSessionRouteStore, sessionID, providerName, "agent session")
}

func (s *indexedDBRouteStore) LookupTurn(ctx context.Context, turnID string) (AgentRoute, bool, error) {
	turnID = strings.TrimSpace(turnID)
	if s == nil || s.db == nil || turnID == "" {
		return AgentRoute{}, false, nil
	}
	record, err := s.db.ObjectStore(indexedDBAgentTurnRouteStore).Get(ctx, turnID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return AgentRoute{}, false, nil
		}
		return AgentRoute{}, false, fmt.Errorf("lookup agent turn route %q: %w", turnID, err)
	}
	providerName := strings.TrimSpace(stringValue(record["provider"]))
	sessionID := strings.TrimSpace(stringValue(record["session_id"]))
	if providerName == "" {
		return AgentRoute{}, false, fmt.Errorf("agent turn route %q has no provider", turnID)
	}
	if sessionID == "" {
		return AgentRoute{}, false, fmt.Errorf("agent turn route %q has no session id", turnID)
	}
	return AgentRoute{ProviderName: providerName, SessionID: sessionID}, true, nil
}

func (s *indexedDBRouteStore) RememberTurn(ctx context.Context, turnID, sessionID, providerName string) error {
	turnID = strings.TrimSpace(turnID)
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if s == nil || s.db == nil || turnID == "" || sessionID == "" || providerName == "" {
		return nil
	}
	if err := s.db.ObjectStore(indexedDBAgentTurnRouteStore).Put(ctx, indexeddb.Record{
		"id":         turnID,
		"session_id": sessionID,
		"provider":   providerName,
	}); err != nil {
		return fmt.Errorf("remember agent turn route %q: %w", turnID, err)
	}
	return nil
}

func (s *indexedDBRouteStore) ForgetTurn(ctx context.Context, turnID, providerName string) error {
	turnID = strings.TrimSpace(turnID)
	providerName = strings.TrimSpace(providerName)
	if s == nil || s.db == nil || turnID == "" {
		return nil
	}
	return s.forgetMatching(ctx, indexedDBAgentTurnRouteStore, turnID, providerName, "agent turn")
}

func (s *indexedDBRouteStore) forgetMatching(ctx context.Context, storeName, id, providerName, label string) error {
	store := s.db.ObjectStore(storeName)
	record, err := store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("lookup %s route %q before delete: %w", label, id, err)
	}
	if providerName != "" && strings.TrimSpace(stringValue(record["provider"])) != providerName {
		return nil
	}
	if err := store.Delete(ctx, id); err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("delete %s route %q: %w", label, id, err)
	}
	return nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}

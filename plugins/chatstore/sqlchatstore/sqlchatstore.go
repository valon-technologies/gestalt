package sqlchatstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/core"
)

type Scanner interface {
	Scan(dest ...any) error
}

type Store struct {
	DB *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{DB: db}
}

func (s *Store) Close() error {
	return s.DB.Close()
}

func scanAgent(row Scanner) (*core.Agent, error) {
	var a core.Agent
	var providersJSON string
	if err := row.Scan(&a.ID, &a.OwnerID, &a.Name, &a.SystemPrompt, &a.Model,
		&providersJSON, &a.Temperature, &a.MaxTokens, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	if providersJSON != "" {
		if err := json.Unmarshal([]byte(providersJSON), &a.Providers); err != nil {
			return nil, fmt.Errorf("unmarshaling agent providers: %w", err)
		}
	}
	return &a, nil
}

func scanConversation(row Scanner) (*core.Conversation, error) {
	var c core.Conversation
	if err := row.Scan(&c.ID, &c.AgentID, &c.UserID, &c.Title, &c.Status,
		&c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func scanMessage(row Scanner) (*core.Message, error) {
	var m core.Message
	var toolCalls, toolCallID sql.NullString
	if err := row.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content,
		&toolCalls, &toolCallID, &m.InputTokens, &m.OutputTokens, &m.CreatedAt); err != nil {
		return nil, err
	}
	m.ToolCalls = toolCalls.String
	m.ToolCallID = toolCallID.String
	return &m, nil
}

func (s *Store) GetAgent(ctx context.Context, id string) (*core.Agent, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, owner_id, name, system_prompt, model, providers, temperature, max_tokens, created_at, updated_at
		FROM agents WHERE id = ?`, id)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, core.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying agent: %w", err)
	}
	return a, nil
}

func (s *Store) ListAgents(ctx context.Context, ownerID string) ([]*core.Agent, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, owner_id, name, system_prompt, model, providers, temperature, max_tokens, created_at, updated_at
		FROM agents WHERE owner_id = ? ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var agents []*core.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning agent row: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func marshalProviders(providers []string) (string, error) {
	if providers == nil {
		providers = []string{}
	}
	b, err := json.Marshal(providers)
	if err != nil {
		return "", fmt.Errorf("marshaling agent providers: %w", err)
	}
	return string(b), nil
}

func (s *Store) CreateAgent(ctx context.Context, agent *core.Agent) error {
	now := time.Now().UTC().Truncate(time.Second)
	agent.ID = uuid.NewString()
	agent.CreatedAt = now
	agent.UpdatedAt = now

	providersJSON, err := marshalProviders(agent.Providers)
	if err != nil {
		return err
	}

	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO agents (id, owner_id, name, system_prompt, model, providers, temperature, max_tokens, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		agent.ID, agent.OwnerID, agent.Name, agent.SystemPrompt, agent.Model,
		providersJSON, agent.Temperature, agent.MaxTokens, agent.CreatedAt, agent.UpdatedAt)
	if err != nil {
		return fmt.Errorf("inserting agent: %w", err)
	}
	return nil
}

func (s *Store) UpdateAgent(ctx context.Context, agent *core.Agent) error {
	agent.UpdatedAt = time.Now().UTC().Truncate(time.Second)

	providersJSON, err := marshalProviders(agent.Providers)
	if err != nil {
		return err
	}

	result, err := s.DB.ExecContext(ctx,
		`UPDATE agents SET name = ?, system_prompt = ?, model = ?, providers = ?, temperature = ?, max_tokens = ?, updated_at = ?
		WHERE id = ?`,
		agent.Name, agent.SystemPrompt, agent.Model, providersJSON,
		agent.Temperature, agent.MaxTokens, agent.UpdatedAt, agent.ID)
	if err != nil {
		return fmt.Errorf("updating agent: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("updating agent: %w", err)
	}
	if n == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting agent: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("deleting agent: %w", err)
	}
	if n == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *Store) GetConversation(ctx context.Context, id string) (*core.Conversation, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, agent_id, user_id, title, status, created_at, updated_at
		FROM conversations WHERE id = ?`, id)
	c, err := scanConversation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, core.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying conversation: %w", err)
	}
	return c, nil
}

func (s *Store) ListConversations(ctx context.Context, userID string, limit, offset int) ([]*core.Conversation, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, agent_id, user_id, title, status, created_at, updated_at
		FROM conversations WHERE user_id = ? ORDER BY updated_at DESC LIMIT ? OFFSET ?`,
		userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing conversations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var convs []*core.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning conversation row: %w", err)
		}
		convs = append(convs, c)
	}
	return convs, rows.Err()
}

func (s *Store) CreateConversation(ctx context.Context, conv *core.Conversation) error {
	now := time.Now().UTC().Truncate(time.Second)
	conv.ID = uuid.NewString()
	conv.CreatedAt = now
	conv.UpdatedAt = now
	if conv.Status == "" {
		conv.Status = core.ConversationActive
	}

	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO conversations (id, agent_id, user_id, title, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		conv.ID, conv.AgentID, conv.UserID, conv.Title, conv.Status, conv.CreatedAt, conv.UpdatedAt)
	if err != nil {
		return fmt.Errorf("inserting conversation: %w", err)
	}
	return nil
}

func (s *Store) UpdateConversation(ctx context.Context, conv *core.Conversation) error {
	conv.UpdatedAt = time.Now().UTC().Truncate(time.Second)

	result, err := s.DB.ExecContext(ctx,
		`UPDATE conversations SET title = ?, status = ?, updated_at = ? WHERE id = ?`,
		conv.Title, conv.Status, conv.UpdatedAt, conv.ID)
	if err != nil {
		return fmt.Errorf("updating conversation: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("updating conversation: %w", err)
	}
	if n == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *Store) ListMessages(ctx context.Context, conversationID string) ([]*core.Message, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, tool_calls, tool_call_id, input_tokens, output_tokens, created_at
		FROM messages WHERE conversation_id = ? ORDER BY created_at ASC`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var msgs []*core.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning message row: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) AppendMessage(ctx context.Context, msg *core.Message) error {
	now := time.Now().UTC().Truncate(time.Second)
	msg.ID = uuid.NewString()
	msg.CreatedAt = now

	var toolCalls, toolCallID any
	if msg.ToolCalls != "" {
		toolCalls = msg.ToolCalls
	}
	if msg.ToolCallID != "" {
		toolCallID = msg.ToolCallID
	}

	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO messages (id, conversation_id, role, content, tool_calls, tool_call_id, input_tokens, output_tokens, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.ConversationID, msg.Role, msg.Content,
		toolCalls, toolCallID, msg.InputTokens, msg.OutputTokens, msg.CreatedAt)
	if err != nil {
		return fmt.Errorf("inserting message: %w", err)
	}
	return nil
}

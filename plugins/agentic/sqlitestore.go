package agentic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/core"

	_ "modernc.org/sqlite" // register database/sql driver
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dsn := dbPath + "?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			name TEXT NOT NULL,
			system_prompt TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			providers TEXT NOT NULL DEFAULT '[]',
			temperature REAL NOT NULL DEFAULT 0,
			max_tokens INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_agents_owner_id ON agents(owner_id);

		CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_conversations_user_updated ON conversations(user_id, updated_at);

		CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			tool_calls TEXT NOT NULL DEFAULT '',
			tool_call_id TEXT NOT NULL DEFAULT '',
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_messages_conv_created ON messages(conversation_id, created_at);
	`)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) GetAgent(ctx context.Context, id string) (*Agent, error) {
	row := s.db.QueryRowContext(ctx,
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

func (s *SQLiteStore) ListAgents(ctx context.Context, ownerID string) ([]*Agent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, name, system_prompt, model, providers, temperature, max_tokens, created_at, updated_at
		 FROM agents WHERE owner_id = ? ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var agents []*Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning agent: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (s *SQLiteStore) CreateAgent(ctx context.Context, agent *Agent) error {
	if agent.ID == "" {
		agent.ID = uuid.NewString()
	}
	now := time.Now().UTC().Truncate(time.Second)
	agent.CreatedAt = now
	agent.UpdatedAt = now

	providersJSON, err := json.Marshal(agent.Providers)
	if err != nil {
		return fmt.Errorf("marshaling providers: %w", err)
	}
	if agent.Providers == nil {
		providersJSON = []byte("[]")
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO agents (id, owner_id, name, system_prompt, model, providers, temperature, max_tokens, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		agent.ID, agent.OwnerID, agent.Name, agent.SystemPrompt, agent.Model,
		string(providersJSON), agent.Temperature, agent.MaxTokens,
		agent.CreatedAt, agent.UpdatedAt)
	if err != nil {
		return fmt.Errorf("inserting agent: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateAgent(ctx context.Context, agent *Agent) error {
	agent.UpdatedAt = time.Now().UTC().Truncate(time.Second)

	providersJSON, err := json.Marshal(agent.Providers)
	if err != nil {
		return fmt.Errorf("marshaling providers: %w", err)
	}
	if agent.Providers == nil {
		providersJSON = []byte("[]")
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE agents SET name = ?, system_prompt = ?, model = ?, providers = ?,
		 temperature = ?, max_tokens = ?, updated_at = ? WHERE id = ?`,
		agent.Name, agent.SystemPrompt, agent.Model, string(providersJSON),
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

func (s *SQLiteStore) DeleteAgent(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id)
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

func (s *SQLiteStore) GetConversation(ctx context.Context, id string) (*Conversation, error) {
	row := s.db.QueryRowContext(ctx,
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

func (s *SQLiteStore) ListConversations(ctx context.Context, userID string, limit, offset int) ([]*Conversation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, user_id, title, status, created_at, updated_at
		 FROM conversations WHERE user_id = ? ORDER BY updated_at DESC LIMIT ? OFFSET ?`,
		userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing conversations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var convs []*Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning conversation: %w", err)
		}
		convs = append(convs, c)
	}
	return convs, rows.Err()
}

func (s *SQLiteStore) CreateConversation(ctx context.Context, conv *Conversation) error {
	if conv.ID == "" {
		conv.ID = uuid.NewString()
	}
	now := time.Now().UTC().Truncate(time.Second)
	conv.CreatedAt = now
	conv.UpdatedAt = now
	if conv.Status == "" {
		conv.Status = ConversationActive
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO conversations (id, agent_id, user_id, title, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		conv.ID, conv.AgentID, conv.UserID, conv.Title, conv.Status,
		conv.CreatedAt, conv.UpdatedAt)
	if err != nil {
		return fmt.Errorf("inserting conversation: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateConversation(ctx context.Context, conv *Conversation) error {
	conv.UpdatedAt = time.Now().UTC().Truncate(time.Second)

	result, err := s.db.ExecContext(ctx,
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

func (s *SQLiteStore) ListMessages(ctx context.Context, conversationID string) ([]*Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, tool_calls, tool_call_id, input_tokens, output_tokens, created_at
		 FROM messages WHERE conversation_id = ? ORDER BY created_at ASC`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var msgs []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *SQLiteStore) AppendMessage(ctx context.Context, msg *Message) error {
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC().Truncate(time.Second)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (id, conversation_id, role, content, tool_calls, tool_call_id, input_tokens, output_tokens, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.ConversationID, msg.Role, msg.Content, msg.ToolCalls,
		msg.ToolCallID, msg.InputTokens, msg.OutputTokens, msg.CreatedAt)
	if err != nil {
		return fmt.Errorf("inserting message: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAgent(row scanner) (*Agent, error) {
	var a Agent
	var providersJSON string
	if err := row.Scan(&a.ID, &a.OwnerID, &a.Name, &a.SystemPrompt, &a.Model,
		&providersJSON, &a.Temperature, &a.MaxTokens, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	if providersJSON != "" {
		if err := json.Unmarshal([]byte(providersJSON), &a.Providers); err != nil {
			return nil, fmt.Errorf("unmarshaling providers: %w", err)
		}
	}
	if a.Providers == nil {
		a.Providers = []string{}
	}
	return &a, nil
}

func scanConversation(row scanner) (*Conversation, error) {
	var c Conversation
	if err := row.Scan(&c.ID, &c.AgentID, &c.UserID, &c.Title, &c.Status,
		&c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func scanMessage(row scanner) (*Message, error) {
	var m Message
	if err := row.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.ToolCalls,
		&m.ToolCallID, &m.InputTokens, &m.OutputTokens, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

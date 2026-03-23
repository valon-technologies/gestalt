package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/plugins/chatstore/sqlchatstore"

	_ "modernc.org/sqlite"
)

type Store struct {
	*sqlchatstore.Store
}

var _ core.ChatStore = (*Store)(nil)

func New(dbPath string) (*Store, error) {
	dsn := dbPath + "?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	return &Store{Store: sqlchatstore.New(db)}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
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
		CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL REFERENCES agents(id),
			user_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL REFERENCES conversations(id),
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			tool_calls TEXT,
			tool_call_id TEXT,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_agents_owner ON agents(owner_id);
		CREATE INDEX IF NOT EXISTS idx_conversations_user ON conversations(user_id, updated_at);
		CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id, created_at)
	`)
	return err
}

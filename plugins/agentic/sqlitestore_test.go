package agentic

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/core"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAgentCRUD(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	agent := &Agent{
		OwnerID:      "test-user",
		Name:         "test-agent",
		SystemPrompt: "you are helpful",
		Model:        "gpt-4",
		Providers:    []string{"slack", "github"},
		Temperature:  0.7,
		MaxTokens:    1024,
	}

	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if agent.ID == "" {
		t.Fatal("expected ID to be set")
	}

	got, err := s.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "test-agent" {
		t.Fatalf("got name %q, want %q", got.Name, "test-agent")
	}
	if len(got.Providers) != 2 || got.Providers[0] != "slack" {
		t.Fatalf("unexpected providers: %v", got.Providers)
	}

	got.Name = "updated-agent"
	if err := s.UpdateAgent(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, err := s.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Name != "updated-agent" {
		t.Fatalf("got name %q after update, want %q", got2.Name, "updated-agent")
	}

	agents, err := s.ListAgents(ctx, "test-user")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}

	if err := s.DeleteAgent(ctx, agent.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetAgent(ctx, agent.ID)
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestAgentNotFound(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetAgent(ctx, "nonexistent")
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if err := s.DeleteAgent(ctx, "nonexistent"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAgentNilProviders(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	agent := &Agent{
		OwnerID: "test-user",
		Name:    "no-providers",
	}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers == nil {
		t.Fatal("expected non-nil providers slice")
	}
	if len(got.Providers) != 0 {
		t.Fatalf("expected empty providers, got %v", got.Providers)
	}
}

func TestConversationCRUD(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	conv := &Conversation{
		AgentID: "agent-1",
		UserID:  "test-user",
		Title:   "test conversation",
	}

	if err := s.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	if conv.ID == "" {
		t.Fatal("expected ID to be set")
	}
	if conv.Status != ConversationActive {
		t.Fatalf("expected status %q, got %q", ConversationActive, conv.Status)
	}

	got, err := s.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "test conversation" {
		t.Fatalf("got title %q, want %q", got.Title, "test conversation")
	}

	got.Title = "updated title"
	got.Status = ConversationArchived
	if err := s.UpdateConversation(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, err := s.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Title != "updated title" || got2.Status != ConversationArchived {
		t.Fatalf("unexpected state after update: title=%q status=%q", got2.Title, got2.Status)
	}
}

func TestConversationNotFound(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetConversation(ctx, "nonexistent")
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestConversationPagination(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		conv := &Conversation{
			AgentID: "agent-1",
			UserID:  "test-user",
			Title:   "conversation",
		}
		if err := s.CreateConversation(ctx, conv); err != nil {
			t.Fatal(err)
		}
	}

	page1, err := s.ListConversations(ctx, "test-user", 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 3 {
		t.Fatalf("got %d conversations, want 3", len(page1))
	}

	page2, err := s.ListConversations(ctx, "test-user", 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 {
		t.Fatalf("got %d conversations, want 2", len(page2))
	}
}

func TestMessageLifecycle(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	conv := &Conversation{AgentID: "agent-1", UserID: "test-user"}
	if err := s.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}

	userMsg := &Message{
		ConversationID: conv.ID,
		Role:           RoleUser,
		Content:        "hello",
	}
	if err := s.AppendMessage(ctx, userMsg); err != nil {
		t.Fatal(err)
	}

	assistantMsg := &Message{
		ConversationID: conv.ID,
		Role:           RoleAssistant,
		Content:        "hi there",
		InputTokens:    10,
		OutputTokens:   5,
	}
	if err := s.AppendMessage(ctx, assistantMsg); err != nil {
		t.Fatal(err)
	}

	msgs, err := s.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != RoleUser || msgs[1].Role != RoleAssistant {
		t.Fatalf("unexpected message order: %q, %q", msgs[0].Role, msgs[1].Role)
	}
}

func TestIdempotentMigration(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.Migrate(ctx); err != nil {
		t.Fatal("second migration should be idempotent:", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("third migration should be idempotent:", err)
	}
}

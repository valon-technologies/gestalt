package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/core"
)

const (
	testOwner = "owner-001"
	testUser  = "user-001"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test-chat.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	return s
}

func TestMigrateIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second migrate should be idempotent: %v", err)
	}
}

func TestAgentCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	agent := &core.Agent{
		OwnerID:      testOwner,
		Name:         "test-agent",
		SystemPrompt: "you are helpful",
		Model:        "test-model-v1",
		Providers:    []string{"provider-a", "provider-b"},
		Temperature:  0.7,
		MaxTokens:    1024,
	}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if agent.ID == "" {
		t.Fatal("expected ID to be set after create")
	}

	got, err := s.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.Name != "test-agent" {
		t.Errorf("name = %q, want %q", got.Name, "test-agent")
	}
	if got.Model != "test-model-v1" {
		t.Errorf("model = %q, want %q", got.Model, "test-model-v1")
	}
	if len(got.Providers) != 2 || got.Providers[0] != "provider-a" {
		t.Errorf("providers = %v, want [provider-a provider-b]", got.Providers)
	}
	if got.Temperature != 0.7 {
		t.Errorf("temperature = %v, want 0.7", got.Temperature)
	}

	agents, err := s.ListAgents(ctx, testOwner)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len(agents) = %d, want 1", len(agents))
	}

	agent.Name = "updated-agent"
	if err := s.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	got, err = s.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent after update: %v", err)
	}
	if got.Name != "updated-agent" {
		t.Errorf("name after update = %q, want %q", got.Name, "updated-agent")
	}

	if err := s.DeleteAgent(ctx, agent.ID); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	_, err = s.GetAgent(ctx, agent.ID)
	if err != core.ErrNotFound {
		t.Errorf("GetAgent after delete: got %v, want ErrNotFound", err)
	}
}

func TestAgentNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetAgent(ctx, "nonexistent")
	if err != core.ErrNotFound {
		t.Errorf("GetAgent: got %v, want ErrNotFound", err)
	}

	err = s.UpdateAgent(ctx, &core.Agent{ID: "nonexistent", Name: "x"})
	if err != core.ErrNotFound {
		t.Errorf("UpdateAgent: got %v, want ErrNotFound", err)
	}

	err = s.DeleteAgent(ctx, "nonexistent")
	if err != core.ErrNotFound {
		t.Errorf("DeleteAgent: got %v, want ErrNotFound", err)
	}
}

func TestConversationCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	agent := &core.Agent{OwnerID: testOwner, Name: "conv-agent", Providers: []string{}}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	conv := &core.Conversation{
		AgentID: agent.ID,
		UserID:  testUser,
		Title:   "test conversation",
	}
	if err := s.CreateConversation(ctx, conv); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if conv.ID == "" {
		t.Fatal("expected ID to be set after create")
	}
	if conv.Status != core.ConversationActive {
		t.Errorf("status = %q, want %q", conv.Status, core.ConversationActive)
	}

	got, err := s.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.Title != "test conversation" {
		t.Errorf("title = %q, want %q", got.Title, "test conversation")
	}

	convs, err := s.ListConversations(ctx, testUser, 10, 0)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("len(convs) = %d, want 1", len(convs))
	}

	conv.Title = "updated title"
	conv.Status = core.ConversationArchived
	if err := s.UpdateConversation(ctx, conv); err != nil {
		t.Fatalf("UpdateConversation: %v", err)
	}
	got, err = s.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation after update: %v", err)
	}
	if got.Title != "updated title" {
		t.Errorf("title after update = %q, want %q", got.Title, "updated title")
	}
	if got.Status != core.ConversationArchived {
		t.Errorf("status after update = %q, want %q", got.Status, core.ConversationArchived)
	}
}

func TestConversationNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetConversation(ctx, "nonexistent")
	if err != core.ErrNotFound {
		t.Errorf("GetConversation: got %v, want ErrNotFound", err)
	}

	err = s.UpdateConversation(ctx, &core.Conversation{ID: "nonexistent", Title: "x"})
	if err != core.ErrNotFound {
		t.Errorf("UpdateConversation: got %v, want ErrNotFound", err)
	}
}

func TestMessageAppendAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	agent := &core.Agent{OwnerID: testOwner, Name: "msg-agent", Providers: []string{}}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	conv := &core.Conversation{AgentID: agent.ID, UserID: testUser, Title: "msg test"}
	if err := s.CreateConversation(ctx, conv); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	userMsg := &core.Message{
		ConversationID: conv.ID,
		Role:           core.RoleUser,
		Content:        "hello",
	}
	if err := s.AppendMessage(ctx, userMsg); err != nil {
		t.Fatalf("AppendMessage (user): %v", err)
	}
	if userMsg.ID == "" {
		t.Fatal("expected message ID to be set")
	}

	assistantMsg := &core.Message{
		ConversationID: conv.ID,
		Role:           core.RoleAssistant,
		Content:        "hi there",
		InputTokens:    10,
		OutputTokens:   5,
	}
	if err := s.AppendMessage(ctx, assistantMsg); err != nil {
		t.Fatalf("AppendMessage (assistant): %v", err)
	}

	toolMsg := &core.Message{
		ConversationID: conv.ID,
		Role:           core.RoleTool,
		Content:        `{"result": "ok"}`,
		ToolCallID:     "call-123",
	}
	if err := s.AppendMessage(ctx, toolMsg); err != nil {
		t.Fatalf("AppendMessage (tool): %v", err)
	}

	msgs, err := s.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %d, want 3", len(msgs))
	}

	if msgs[0].Role != core.RoleUser || msgs[0].Content != "hello" {
		t.Errorf("msg[0] = {role:%s content:%s}, want {role:user content:hello}", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].InputTokens != 10 || msgs[1].OutputTokens != 5 {
		t.Errorf("msg[1] tokens = {in:%d out:%d}, want {in:10 out:5}", msgs[1].InputTokens, msgs[1].OutputTokens)
	}
	if msgs[2].ToolCallID != "call-123" {
		t.Errorf("msg[2] tool_call_id = %q, want %q", msgs[2].ToolCallID, "call-123")
	}
}

func TestListConversationsPagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	agent := &core.Agent{OwnerID: testOwner, Name: "page-agent", Providers: []string{}}
	if err := s.CreateAgent(ctx, agent); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	for i := 0; i < 5; i++ {
		conv := &core.Conversation{AgentID: agent.ID, UserID: testUser, Title: "conv"}
		if err := s.CreateConversation(ctx, conv); err != nil {
			t.Fatalf("CreateConversation: %v", err)
		}
	}

	page, err := s.ListConversations(ctx, testUser, 2, 0)
	if err != nil {
		t.Fatalf("ListConversations page 1: %v", err)
	}
	if len(page) != 2 {
		t.Errorf("page 1 len = %d, want 2", len(page))
	}

	page, err = s.ListConversations(ctx, testUser, 2, 3)
	if err != nil {
		t.Fatalf("ListConversations page 2: %v", err)
	}
	if len(page) != 2 {
		t.Errorf("offset=3 limit=2 len = %d, want 2", len(page))
	}
}

func TestListAgentsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	agents, err := s.ListAgents(ctx, "no-such-owner")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected empty list, got %v", agents)
	}
}

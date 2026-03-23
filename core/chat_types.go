package core

import "time"

type Agent struct {
	ID           string
	OwnerID      string
	Name         string
	SystemPrompt string
	Model        string
	Providers    []string
	Temperature  float64
	MaxTokens    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Conversation struct {
	ID        string
	AgentID   string
	UserID    string
	Title     string
	Status    ConversationStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ConversationStatus string

const (
	ConversationActive   ConversationStatus = "active"
	ConversationArchived ConversationStatus = "archived"
)

type Message struct {
	ID             string
	ConversationID string
	Role           MessageRole
	Content        string
	ToolCalls      string
	ToolCallID     string
	InputTokens    int
	OutputTokens   int
	CreatedAt      time.Time
}

type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

type ChatEventType string

const (
	ChatEventTextDelta  ChatEventType = "text_delta"
	ChatEventToolUse    ChatEventType = "tool_use"
	ChatEventToolResult ChatEventType = "tool_result"
	ChatEventComplete   ChatEventType = "complete"
	ChatEventError      ChatEventType = "error"
)

type ChatEvent struct {
	Type           ChatEventType
	ConversationID string
	Text           string
	ToolCallID     string
	ToolName       string
	ToolInput      string
	Error          string
	InputTokens    int
	OutputTokens   int
}

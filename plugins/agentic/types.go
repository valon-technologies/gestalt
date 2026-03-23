package agentic

import "time"

type Agent struct {
	ID           string    `json:"id"`
	OwnerID      string    `json:"owner_id"`
	Name         string    `json:"name"`
	SystemPrompt string    `json:"system_prompt"`
	Model        string    `json:"model"`
	Providers    []string  `json:"providers"`
	Temperature  float64   `json:"temperature"`
	MaxTokens    int       `json:"max_tokens"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Conversation struct {
	ID        string             `json:"id"`
	AgentID   string             `json:"agent_id"`
	UserID    string             `json:"user_id"`
	Title     string             `json:"title"`
	Status    ConversationStatus `json:"status"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

type ConversationStatus string

const (
	ConversationActive   ConversationStatus = "active"
	ConversationArchived ConversationStatus = "archived"
)

type Message struct {
	ID             string      `json:"id"`
	ConversationID string      `json:"conversation_id"`
	Role           MessageRole `json:"role"`
	Content        string      `json:"content"`
	ToolCalls      string      `json:"tool_calls,omitempty"`
	ToolCallID     string      `json:"tool_call_id,omitempty"`
	InputTokens    int         `json:"input_tokens,omitempty"`
	OutputTokens   int         `json:"output_tokens,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
}

type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

type ChatEvent struct {
	Type           ChatEventType `json:"type"`
	ConversationID string        `json:"conversation_id"`
	Text           string        `json:"text,omitempty"`
	ToolCallID     string        `json:"tool_call_id,omitempty"`
	ToolName       string        `json:"tool_name,omitempty"`
	ToolInput      string        `json:"tool_input,omitempty"`
	Error          string        `json:"error,omitempty"`
	InputTokens    int           `json:"input_tokens,omitempty"`
	OutputTokens   int           `json:"output_tokens,omitempty"`
}

type ChatEventType string

const (
	ChatEventTextDelta  ChatEventType = "text_delta"
	ChatEventToolUse    ChatEventType = "tool_use"
	ChatEventToolResult ChatEventType = "tool_result"
	ChatEventComplete   ChatEventType = "complete"
	ChatEventError      ChatEventType = "error"
)

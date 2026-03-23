package core

import "context"

type ChatStore interface {
	GetAgent(ctx context.Context, id string) (*Agent, error)
	ListAgents(ctx context.Context, ownerID string) ([]*Agent, error)
	CreateAgent(ctx context.Context, agent *Agent) error
	UpdateAgent(ctx context.Context, agent *Agent) error
	DeleteAgent(ctx context.Context, id string) error

	GetConversation(ctx context.Context, id string) (*Conversation, error)
	ListConversations(ctx context.Context, userID string, limit, offset int) ([]*Conversation, error)
	CreateConversation(ctx context.Context, conv *Conversation) error
	UpdateConversation(ctx context.Context, conv *Conversation) error

	ListMessages(ctx context.Context, conversationID string) ([]*Message, error)
	AppendMessage(ctx context.Context, msg *Message) error

	Migrate(ctx context.Context) error
	Close() error
}

type ChatDispatcher interface {
	SendMessage(ctx context.Context, conversationID, content, userID string) (<-chan ChatEvent, error)
	CancelConversation(ctx context.Context, conversationID string) error
}

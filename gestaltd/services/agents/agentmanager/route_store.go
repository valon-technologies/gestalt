package agentmanager

import "context"

type AgentRoute struct {
	ProviderName string
	SessionID    string
}

type RouteStore interface {
	LookupSession(ctx context.Context, sessionID string) (AgentRoute, bool, error)
	RememberSession(ctx context.Context, sessionID, providerName string) error
	ForgetSession(ctx context.Context, sessionID, providerName string) error
	LookupTurn(ctx context.Context, turnID string) (AgentRoute, bool, error)
	RememberTurn(ctx context.Context, turnID, sessionID, providerName string) error
	ForgetTurn(ctx context.Context, turnID, providerName string) error
}

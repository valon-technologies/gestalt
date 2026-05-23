package agent

import (
	"context"
	"fmt"
)

// Provider is the authoritative agent data boundary. Read methods for
// sessions, turns, turn events, and interactions must be served from
// provider-owned control-plane state and must not require a live execution
// sandbox, pod IP, cached tunnel, or other transport attachment. Execution
// methods may require runtime transport and should return an unavailable error
// when the provider has durable state but cannot attach to the execution
// runtime.
type Provider interface {
	CreateSession(ctx context.Context, req CreateSessionRequest) (*Session, error)
	GetSession(ctx context.Context, req GetSessionRequest) (*Session, error)
	ListSessions(ctx context.Context, req ListSessionsRequest) ([]*Session, error)
	UpdateSession(ctx context.Context, req UpdateSessionRequest) (*Session, error)
	CreateTurn(ctx context.Context, req CreateTurnRequest) (*Turn, error)
	GetTurn(ctx context.Context, req GetTurnRequest) (*Turn, error)
	ListTurns(ctx context.Context, req ListTurnsRequest) ([]*Turn, error)
	CancelTurn(ctx context.Context, req CancelTurnRequest) (*Turn, error)
	ListTurnEvents(ctx context.Context, req ListTurnEventsRequest) ([]*TurnEvent, error)
	GetInteraction(ctx context.Context, req GetInteractionRequest) (*Interaction, error)
	ListInteractions(ctx context.Context, req ListInteractionsRequest) ([]*Interaction, error)
	ResolveInteraction(ctx context.Context, req ResolveInteractionRequest) (*Interaction, error)
	GetCapabilities(ctx context.Context, req GetCapabilitiesRequest) (*ProviderCapabilities, error)
	Ping(ctx context.Context) error
	Close() error
}

type WorkspaceProvider interface {
	Provider
	SupportsWorkspaceRequests() bool
}

type Host interface {
	ListTools(ctx context.Context, req ListToolsRequest) (*ListToolsResponse, error)
	ExecuteTool(ctx context.Context, req ExecuteToolRequest) (*ExecuteToolResponse, error)
}

type UnimplementedProvider struct{}

func (UnimplementedProvider) CreateSession(context.Context, CreateSessionRequest) (*Session, error) {
	return nil, fmt.Errorf("agent provider create session is not implemented")
}

func (UnimplementedProvider) GetSession(context.Context, GetSessionRequest) (*Session, error) {
	return nil, fmt.Errorf("agent provider get session is not implemented")
}

func (UnimplementedProvider) ListSessions(context.Context, ListSessionsRequest) ([]*Session, error) {
	return nil, fmt.Errorf("agent provider list sessions is not implemented")
}

func (UnimplementedProvider) UpdateSession(context.Context, UpdateSessionRequest) (*Session, error) {
	return nil, fmt.Errorf("agent provider update session is not implemented")
}

func (UnimplementedProvider) CreateTurn(context.Context, CreateTurnRequest) (*Turn, error) {
	return nil, fmt.Errorf("agent provider create turn is not implemented")
}

func (UnimplementedProvider) GetTurn(context.Context, GetTurnRequest) (*Turn, error) {
	return nil, fmt.Errorf("agent provider get turn is not implemented")
}

func (UnimplementedProvider) ListTurns(context.Context, ListTurnsRequest) ([]*Turn, error) {
	return nil, fmt.Errorf("agent provider list turns is not implemented")
}

func (UnimplementedProvider) CancelTurn(context.Context, CancelTurnRequest) (*Turn, error) {
	return nil, fmt.Errorf("agent provider cancel turn is not implemented")
}

func (UnimplementedProvider) ListTurnEvents(context.Context, ListTurnEventsRequest) ([]*TurnEvent, error) {
	return nil, fmt.Errorf("agent provider list turn events is not implemented")
}

func (UnimplementedProvider) GetInteraction(context.Context, GetInteractionRequest) (*Interaction, error) {
	return nil, fmt.Errorf("agent provider get interaction is not implemented")
}

func (UnimplementedProvider) ListInteractions(context.Context, ListInteractionsRequest) ([]*Interaction, error) {
	return nil, fmt.Errorf("agent provider list interactions is not implemented")
}

func (UnimplementedProvider) ResolveInteraction(context.Context, ResolveInteractionRequest) (*Interaction, error) {
	return nil, fmt.Errorf("agent provider resolve interaction is not implemented")
}

func (UnimplementedProvider) GetCapabilities(context.Context, GetCapabilitiesRequest) (*ProviderCapabilities, error) {
	return nil, fmt.Errorf("agent provider get capabilities is not implemented")
}

func (UnimplementedProvider) Ping(context.Context) error {
	return fmt.Errorf("agent provider ping is not implemented")
}

func (UnimplementedProvider) Close() error {
	return nil
}

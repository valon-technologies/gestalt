package agent

import (
	"context"
	"fmt"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// Provider is the authoritative agent data boundary. Read methods for
// sessions, turns, turn events, and interactions must be served from
// provider-owned control-plane state and must not require a live execution
// sandbox, pod IP, cached tunnel, or other transport attachment. Execution
// methods may require runtime transport and should return an unavailable error
// when the provider has durable state but cannot attach to the execution
// runtime.
type Provider interface {
	// CreateSession mints the session id: the returned Session carries a
	// non-empty, stable id of the provider's choosing. Creation must be
	// idempotent on the request's idempotency_key scoped per subject
	// (created_by_subject_id); an empty key always creates a new session.
	CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*Session, error)
	GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*Session, error)
	ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) ([]*Session, error)
	UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (*Session, error)
	CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*Turn, error)
	GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*Turn, error)
	ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) ([]*Turn, error)
	CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*Turn, error)
	ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) ([]*TurnEvent, error)
	GetInteraction(ctx context.Context, req *proto.GetAgentProviderInteractionRequest) (*Interaction, error)
	ListInteractions(ctx context.Context, req *proto.ListAgentProviderInteractionsRequest) ([]*Interaction, error)
	ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*Interaction, error)
	GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest) (*ProviderCapabilities, error)
	Ping(ctx context.Context) error
	Close() error
}

type WorkspaceProvider interface {
	Provider
	SupportsWorkspaceRequests() bool
}

type UnimplementedProvider struct{}

func (UnimplementedProvider) CreateSession(context.Context, *proto.CreateAgentProviderSessionRequest) (*Session, error) {
	return nil, fmt.Errorf("agent provider create session is not implemented")
}

func (UnimplementedProvider) GetSession(context.Context, *proto.GetAgentProviderSessionRequest) (*Session, error) {
	return nil, fmt.Errorf("agent provider get session is not implemented")
}

func (UnimplementedProvider) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest) ([]*Session, error) {
	return nil, fmt.Errorf("agent provider list sessions is not implemented")
}

func (UnimplementedProvider) UpdateSession(context.Context, *proto.UpdateAgentProviderSessionRequest) (*Session, error) {
	return nil, fmt.Errorf("agent provider update session is not implemented")
}

func (UnimplementedProvider) CreateTurn(context.Context, *proto.CreateAgentProviderTurnRequest) (*Turn, error) {
	return nil, fmt.Errorf("agent provider create turn is not implemented")
}

func (UnimplementedProvider) GetTurn(context.Context, *proto.GetAgentProviderTurnRequest) (*Turn, error) {
	return nil, fmt.Errorf("agent provider get turn is not implemented")
}

func (UnimplementedProvider) ListTurns(context.Context, *proto.ListAgentProviderTurnsRequest) ([]*Turn, error) {
	return nil, fmt.Errorf("agent provider list turns is not implemented")
}

func (UnimplementedProvider) CancelTurn(context.Context, *proto.CancelAgentProviderTurnRequest) (*Turn, error) {
	return nil, fmt.Errorf("agent provider cancel turn is not implemented")
}

func (UnimplementedProvider) ListTurnEvents(context.Context, *proto.ListAgentProviderTurnEventsRequest) ([]*TurnEvent, error) {
	return nil, fmt.Errorf("agent provider list turn events is not implemented")
}

func (UnimplementedProvider) GetInteraction(context.Context, *proto.GetAgentProviderInteractionRequest) (*Interaction, error) {
	return nil, fmt.Errorf("agent provider get interaction is not implemented")
}

func (UnimplementedProvider) ListInteractions(context.Context, *proto.ListAgentProviderInteractionsRequest) ([]*Interaction, error) {
	return nil, fmt.Errorf("agent provider list interactions is not implemented")
}

func (UnimplementedProvider) ResolveInteraction(context.Context, *proto.ResolveAgentProviderInteractionRequest) (*Interaction, error) {
	return nil, fmt.Errorf("agent provider resolve interaction is not implemented")
}

func (UnimplementedProvider) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*ProviderCapabilities, error) {
	return nil, fmt.Errorf("agent provider get capabilities is not implemented")
}

func (UnimplementedProvider) Ping(context.Context) error {
	return fmt.Errorf("agent provider ping is not implemented")
}

func (UnimplementedProvider) Close() error {
	return nil
}

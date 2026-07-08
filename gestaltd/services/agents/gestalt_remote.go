package agents

import (
	"context"
	"fmt"
	"strings"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type gestaltRemoteAgent struct {
	name   string
	client proto.AgentClient
}

// NewGestaltRemoteProvider routes agent operations through a remote gestaltd public Agent API.
func NewGestaltRemoteProvider(name string, client proto.AgentClient) (coreagent.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("agent provider name is required")
	}
	if client == nil {
		return nil, fmt.Errorf("agent provider client is required")
	}
	return &gestaltRemoteAgent{name: name, client: client}, nil
}

func setAgentProviderName(name string, req gproto.Message) gproto.Message {
	if req == nil {
		return nil
	}
	cloned := gproto.Clone(req)
	msg := cloned.ProtoReflect()
	field := msg.Descriptor().Fields().ByName("provider_name")
	if field != nil && field.Kind() == protoreflect.StringKind {
		msg.Set(field, protoreflect.ValueOfString(strings.TrimSpace(name)))
	}
	return cloned
}

func (p *gestaltRemoteAgent) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderSessionCreateContext(ctx)
	defer cancel()
	resp, err := p.client.CreateSession(ctx, setAgentProviderName(p.name, req).(*proto.CreateAgentProviderSessionRequest))
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (p *gestaltRemoteAgent) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.GetSession(ctx, setAgentProviderName(p.name, req).(*proto.GetAgentProviderSessionRequest))
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (p *gestaltRemoteAgent) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.ListSessions(ctx, setAgentProviderName(p.name, req).(*proto.ListAgentProviderSessionsRequest))
	if err != nil {
		return nil, err
	}
	sessions := make([]*coreagent.Session, 0, len(resp.GetSessions()))
	for _, session := range resp.GetSessions() {
		value, err := agentSessionFromProto(session)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, value)
	}
	return sessions, nil
}

func (p *gestaltRemoteAgent) UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.UpdateSession(ctx, setAgentProviderName(p.name, req).(*proto.UpdateAgentProviderSessionRequest))
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (p *gestaltRemoteAgent) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.CreateTurn(ctx, setAgentProviderName(p.name, req).(*proto.CreateAgentProviderTurnRequest))
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (p *gestaltRemoteAgent) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.GetTurn(ctx, setAgentProviderName(p.name, req).(*proto.GetAgentProviderTurnRequest))
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (p *gestaltRemoteAgent) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.ListTurns(ctx, setAgentProviderName(p.name, req).(*proto.ListAgentProviderTurnsRequest))
	if err != nil {
		return nil, err
	}
	turns := make([]*coreagent.Turn, 0, len(resp.GetTurns()))
	for _, turn := range resp.GetTurns() {
		value, err := agentTurnFromProto(turn)
		if err != nil {
			return nil, err
		}
		turns = append(turns, value)
	}
	return turns, nil
}

func (p *gestaltRemoteAgent) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.CancelTurn(ctx, setAgentProviderName(p.name, req).(*proto.CancelAgentProviderTurnRequest))
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (p *gestaltRemoteAgent) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.ListTurnEvents(ctx, setAgentProviderName(p.name, req).(*proto.ListAgentProviderTurnEventsRequest))
	if err != nil {
		return nil, err
	}
	return agentTurnEventsFromProto(resp.GetEvents()), nil
}

func (p *gestaltRemoteAgent) GetInteraction(ctx context.Context, req *proto.GetAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.GetInteraction(ctx, setAgentProviderName(p.name, req).(*proto.GetAgentProviderInteractionRequest))
	if err != nil {
		return nil, err
	}
	return agentInteractionFromProto(resp)
}

func (p *gestaltRemoteAgent) ListInteractions(ctx context.Context, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.ListInteractions(ctx, setAgentProviderName(p.name, req).(*proto.ListAgentProviderInteractionsRequest))
	if err != nil {
		return nil, err
	}
	return agentInteractionsFromProto(resp.GetInteractions())
}

func (p *gestaltRemoteAgent) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.ResolveInteraction(ctx, setAgentProviderName(p.name, req).(*proto.ResolveAgentProviderInteractionRequest))
	if err != nil {
		return nil, err
	}
	return agentInteractionFromProto(resp)
}

func (p *gestaltRemoteAgent) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := p.client.GetCapabilities(ctx, setAgentProviderName(p.name, req).(*proto.GetAgentProviderCapabilitiesRequest))
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return &coreagent.ProviderCapabilities{}, nil
		}
		return nil, err
	}
	return agentProviderCapabilitiesFromProto(resp), nil
}

func (p *gestaltRemoteAgent) Ping(ctx context.Context) error {
	_, err := p.GetCapabilities(ctx, &proto.GetAgentProviderCapabilitiesRequest{})
	return err
}

func (p *gestaltRemoteAgent) Close() error { return nil }

var _ coreagent.Provider = (*gestaltRemoteAgent)(nil)

package agents

import (
	"context"
	"strings"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// publicRemoteAgent routes agent provider calls to a remote gestaltd public Agent API.
type publicRemoteAgent struct {
	name   string
	client proto.AgentClient
}

// NewPublicRemote constructs a gestaltd-to-gestaltd agent provider without runtime lifecycle.
func NewPublicRemote(name string, client proto.AgentClient) coreagent.Provider {
	return &publicRemoteAgent{
		name:   strings.TrimSpace(name),
		client: client,
	}
}

func (r *publicRemoteAgent) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderSessionCreateContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.CreateAgentProviderSessionRequest{}), r.name)
	resp, err := r.client.CreateSession(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (r *publicRemoteAgent) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.GetAgentProviderSessionRequest{}), r.name)
	resp, err := r.client.GetSession(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (r *publicRemoteAgent) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.ListAgentProviderSessionsRequest{}), r.name)
	resp, err := r.client.ListSessions(ctx, providerReq)
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

func (r *publicRemoteAgent) UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.UpdateAgentProviderSessionRequest{}), r.name)
	resp, err := r.client.UpdateSession(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (r *publicRemoteAgent) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.CreateAgentProviderTurnRequest{}), r.name)
	resp, err := r.client.CreateTurn(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (r *publicRemoteAgent) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.GetAgentProviderTurnRequest{}), r.name)
	resp, err := r.client.GetTurn(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (r *publicRemoteAgent) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.ListAgentProviderTurnsRequest{}), r.name)
	resp, err := r.client.ListTurns(ctx, providerReq)
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

func (r *publicRemoteAgent) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.CancelAgentProviderTurnRequest{}), r.name)
	resp, err := r.client.CancelTurn(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (r *publicRemoteAgent) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.ListAgentProviderTurnEventsRequest{}), r.name)
	resp, err := r.client.ListTurnEvents(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentTurnEventsFromProto(resp.GetEvents()), nil
}

func (r *publicRemoteAgent) GetInteraction(ctx context.Context, req *proto.GetAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.GetAgentProviderInteractionRequest{}), r.name)
	resp, err := r.client.GetInteraction(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentInteractionFromProto(resp)
}

func (r *publicRemoteAgent) ListInteractions(ctx context.Context, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.ListAgentProviderInteractionsRequest{}), r.name)
	resp, err := r.client.ListInteractions(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentInteractionsFromProto(resp.GetInteractions())
}

func (r *publicRemoteAgent) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.ResolveAgentProviderInteractionRequest{}), r.name)
	resp, err := r.client.ResolveInteraction(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentInteractionFromProto(resp)
}

func (r *publicRemoteAgent) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := setProviderName(cloneAgentRequest(req, &proto.GetAgentProviderCapabilitiesRequest{}), r.name)
	resp, err := r.client.GetCapabilities(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentProviderCapabilitiesFromProto(resp), nil
}

func (r *publicRemoteAgent) Ping(context.Context) error { return nil }

func (r *publicRemoteAgent) Close() error { return nil }

var _ coreagent.Provider = (*publicRemoteAgent)(nil)

func setProviderName[T gproto.Message](req T, name string) T {
	name = strings.TrimSpace(name)
	if name == "" {
		return req
	}
	msg := req.ProtoReflect()
	field := msg.Descriptor().Fields().ByName("provider_name")
	if field == nil || field.Kind() != protoreflect.StringKind {
		return req
	}
	if strings.TrimSpace(msg.Get(field).String()) == "" {
		msg.Set(field, protoreflect.ValueOfString(name))
	}
	return req
}

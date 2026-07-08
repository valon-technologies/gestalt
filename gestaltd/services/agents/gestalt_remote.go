package agents

import (
	"context"
	"strings"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type gestaltRemoteAgent struct {
	client proto.AgentClient
	name   string
}

// NewGestaltRemoteProvider routes agent provider calls through a remote gestaltd public Agent API.
func NewGestaltRemoteProvider(client proto.AgentClient, name string) coreagent.Provider {
	name = strings.TrimSpace(name)
	if client == nil || name == "" {
		return nil
	}
	return &gestaltRemoteAgent{client: client, name: name}
}

func (r *gestaltRemoteAgent) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	providerReq := cloneGestaltAgentReq(req, &proto.CreateAgentProviderSessionRequest{}, r.name)
	resp, err := r.client.CreateSession(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return agentSessionFromProto(resp)
}

func (r *gestaltRemoteAgent) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	providerReq := cloneGestaltAgentReq(req, &proto.GetAgentProviderSessionRequest{}, r.name)
	resp, err := r.client.GetSession(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return agentSessionFromProto(resp)
}

func (r *gestaltRemoteAgent) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	providerReq := cloneGestaltAgentReq(req, &proto.ListAgentProviderSessionsRequest{}, r.name)
	resp, err := r.client.ListSessions(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
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

func (r *gestaltRemoteAgent) UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	providerReq := cloneGestaltAgentReq(req, &proto.UpdateAgentProviderSessionRequest{}, r.name)
	resp, err := r.client.UpdateSession(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return agentSessionFromProto(resp)
}

func (r *gestaltRemoteAgent) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneGestaltAgentReq(req, &proto.CreateAgentProviderTurnRequest{}, r.name)
	resp, err := r.client.CreateTurn(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return agentTurnFromProto(resp)
}

func (r *gestaltRemoteAgent) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneGestaltAgentReq(req, &proto.GetAgentProviderTurnRequest{}, r.name)
	resp, err := r.client.GetTurn(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return agentTurnFromProto(resp)
}

func (r *gestaltRemoteAgent) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneGestaltAgentReq(req, &proto.ListAgentProviderTurnsRequest{}, r.name)
	resp, err := r.client.ListTurns(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
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

func (r *gestaltRemoteAgent) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneGestaltAgentReq(req, &proto.CancelAgentProviderTurnRequest{}, r.name)
	resp, err := r.client.CancelTurn(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return agentTurnFromProto(resp)
}

func (r *gestaltRemoteAgent) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneGestaltAgentReq(req, &proto.ListAgentProviderTurnEventsRequest{}, r.name)
	resp, err := r.client.ListTurnEvents(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return agentTurnEventsFromProto(resp.GetEvents()), nil
}

func (r *gestaltRemoteAgent) GetInteraction(ctx context.Context, req *proto.GetAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneGestaltAgentReq(req, &proto.GetAgentProviderInteractionRequest{}, r.name)
	resp, err := r.client.GetInteraction(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return agentInteractionFromProto(resp)
}

func (r *gestaltRemoteAgent) ListInteractions(ctx context.Context, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneGestaltAgentReq(req, &proto.ListAgentProviderInteractionsRequest{}, r.name)
	resp, err := r.client.ListInteractions(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return agentInteractionsFromProto(resp.GetInteractions())
}

func (r *gestaltRemoteAgent) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneGestaltAgentReq(req, &proto.ResolveAgentProviderInteractionRequest{}, r.name)
	resp, err := r.client.ResolveInteraction(ctx, providerReq)
	if err != nil {
		return nil, remote.StatusError(err)
	}
	return agentInteractionFromProto(resp)
}

func (r *gestaltRemoteAgent) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneGestaltAgentReq(req, &proto.GetAgentProviderCapabilitiesRequest{}, r.name)
	resp, err := r.client.GetCapabilities(ctx, providerReq)
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return &coreagent.ProviderCapabilities{}, nil
		}
		return nil, remote.StatusError(err)
	}
	return agentProviderCapabilitiesFromProto(resp), nil
}

func (r *gestaltRemoteAgent) Ping(ctx context.Context) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := r.client.GetCapabilities(ctx, &proto.GetAgentProviderCapabilitiesRequest{})
	return remote.StatusError(err)
}

func (r *gestaltRemoteAgent) Close() error { return nil }

func cloneGestaltAgentReq[T interface {
	gproto.Message
	comparable
}](req T, empty T, providerName string) T {
	cloned := cloneAgentRequest(req, empty)
	setAgentProviderName(cloned, providerName)
	return cloned
}

func setAgentProviderName(req gproto.Message, providerName string) {
	if req == nil {
		return
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return
	}
	msg := req.ProtoReflect()
	field := msg.Descriptor().Fields().ByName("provider_name")
	if field == nil || field.Kind() != protoreflect.StringKind {
		return
	}
	if strings.TrimSpace(msg.Get(field).String()) != "" {
		return
	}
	msg.Set(field, protoreflect.ValueOfString(providerName))
}

var _ coreagent.Provider = (*gestaltRemoteAgent)(nil)

package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// ServeAgentProvider starts a gRPC server for an [AgentProvider].
func ServeAgentProvider(ctx context.Context, provider AgentProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAgent, provider))
		proto.RegisterAgentProviderServer(srv, agentProviderServer{provider: provider})
	})
}

type agentProviderServer struct {
	proto.UnimplementedAgentProviderServer
	provider AgentProvider
}

func (s agentProviderServer) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	session, err := s.provider.CreateSession(ctx, createAgentProviderSessionRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent create session", err)
	}
	resp, err := agentSessionToProto(session)
	return resp, providerRPCError("agent create session", err)
}

func (s agentProviderServer) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*proto.AgentSession, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	session, err := s.provider.GetSession(ctx, getAgentProviderSessionRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent get session", err)
	}
	resp, err := agentSessionToProto(session)
	return resp, providerRPCError("agent get session", err)
}

func (s agentProviderServer) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) (*proto.ListAgentProviderSessionsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	resp, err := s.provider.ListSessions(ctx, listAgentProviderSessionsRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent list sessions", err)
	}
	pbResp, err := listAgentProviderSessionsResponseToProto(resp)
	return pbResp, providerRPCError("agent list sessions", err)
}

func (s agentProviderServer) UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	session, err := s.provider.UpdateSession(ctx, updateAgentProviderSessionRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent update session", err)
	}
	resp, err := agentSessionToProto(session)
	return resp, providerRPCError("agent update session", err)
}

func (s agentProviderServer) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	providerReq, err := createAgentProviderTurnRequestFromProto(req)
	if err != nil {
		return nil, providerRPCError("agent create turn", err)
	}
	turn, err := s.provider.CreateTurn(ctx, providerReq)
	if err != nil {
		return nil, providerRPCError("agent create turn", err)
	}
	resp, err := agentTurnToProto(turn)
	return resp, providerRPCError("agent create turn", err)
}

func (s agentProviderServer) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	turn, err := s.provider.GetTurn(ctx, getAgentProviderTurnRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent get turn", err)
	}
	resp, err := agentTurnToProto(turn)
	return resp, providerRPCError("agent get turn", err)
}

func (s agentProviderServer) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) (*proto.ListAgentProviderTurnsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	resp, err := s.provider.ListTurns(ctx, listAgentProviderTurnsRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent list turns", err)
	}
	pbResp, err := listAgentProviderTurnsResponseToProto(resp)
	return pbResp, providerRPCError("agent list turns", err)
}

func (s agentProviderServer) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	turn, err := s.provider.CancelTurn(ctx, cancelAgentProviderTurnRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent cancel turn", err)
	}
	resp, err := agentTurnToProto(turn)
	return resp, providerRPCError("agent cancel turn", err)
}

func (s agentProviderServer) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) (*proto.ListAgentProviderTurnEventsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	resp, err := s.provider.ListTurnEvents(ctx, listAgentProviderTurnEventsRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent list turn events", err)
	}
	pbResp, err := listAgentProviderTurnEventsResponseToProto(resp)
	return pbResp, providerRPCError("agent list turn events", err)
}

func (s agentProviderServer) GetInteraction(ctx context.Context, req *proto.GetAgentProviderInteractionRequest) (*proto.AgentInteraction, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	interaction, err := s.provider.GetInteraction(ctx, getAgentProviderInteractionRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent get interaction", err)
	}
	resp, err := agentInteractionToProto(interaction)
	return resp, providerRPCError("agent get interaction", err)
}

func (s agentProviderServer) ListInteractions(ctx context.Context, req *proto.ListAgentProviderInteractionsRequest) (*proto.ListAgentProviderInteractionsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	resp, err := s.provider.ListInteractions(ctx, listAgentProviderInteractionsRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent list interactions", err)
	}
	pbResp, err := listAgentProviderInteractionsResponseToProto(resp)
	return pbResp, providerRPCError("agent list interactions", err)
}

func (s agentProviderServer) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*proto.AgentInteraction, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	interaction, err := s.provider.ResolveInteraction(ctx, resolveAgentProviderInteractionRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("agent resolve interaction", err)
	}
	resp, err := agentInteractionToProto(interaction)
	return resp, providerRPCError("agent resolve interaction", err)
}

func (s agentProviderServer) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest) (*proto.AgentProviderCapabilities, error) {
	capabilities, err := s.provider.GetCapabilities(ctx, &GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		return nil, providerRPCError("agent get capabilities", err)
	}
	return agentProviderCapabilitiesToProto(capabilities), providerRPCError("agent get capabilities", err)
}

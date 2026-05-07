package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
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
	session, err := s.provider.CreateSession(ctx, req)
	return session, providerRPCError("agent create session", err)
}

func (s agentProviderServer) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*proto.AgentSession, error) {
	session, err := s.provider.GetSession(ctx, req)
	return session, providerRPCError("agent get session", err)
}

func (s agentProviderServer) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) (*proto.ListAgentProviderSessionsResponse, error) {
	resp, err := s.provider.ListSessions(ctx, req)
	return resp, providerRPCError("agent list sessions", err)
}

func (s agentProviderServer) UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	session, err := s.provider.UpdateSession(ctx, req)
	return session, providerRPCError("agent update session", err)
}

func (s agentProviderServer) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	turn, err := s.provider.CreateTurn(ctx, req)
	return turn, providerRPCError("agent create turn", err)
}

func (s agentProviderServer) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	turn, err := s.provider.GetTurn(ctx, req)
	return turn, providerRPCError("agent get turn", err)
}

func (s agentProviderServer) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) (*proto.ListAgentProviderTurnsResponse, error) {
	resp, err := s.provider.ListTurns(ctx, req)
	return resp, providerRPCError("agent list turns", err)
}

func (s agentProviderServer) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	turn, err := s.provider.CancelTurn(ctx, req)
	return turn, providerRPCError("agent cancel turn", err)
}

func (s agentProviderServer) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) (*proto.ListAgentProviderTurnEventsResponse, error) {
	resp, err := s.provider.ListTurnEvents(ctx, req)
	return resp, providerRPCError("agent list turn events", err)
}

func (s agentProviderServer) GetInteraction(ctx context.Context, req *proto.GetAgentProviderInteractionRequest) (*proto.AgentInteraction, error) {
	interaction, err := s.provider.GetInteraction(ctx, req)
	return interaction, providerRPCError("agent get interaction", err)
}

func (s agentProviderServer) ListInteractions(ctx context.Context, req *proto.ListAgentProviderInteractionsRequest) (*proto.ListAgentProviderInteractionsResponse, error) {
	resp, err := s.provider.ListInteractions(ctx, req)
	return resp, providerRPCError("agent list interactions", err)
}

func (s agentProviderServer) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*proto.AgentInteraction, error) {
	interaction, err := s.provider.ResolveInteraction(ctx, req)
	return interaction, providerRPCError("agent resolve interaction", err)
}

func (s agentProviderServer) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest) (*proto.AgentProviderCapabilities, error) {
	capabilities, err := s.provider.GetCapabilities(ctx, req)
	return capabilities, providerRPCError("agent get capabilities", err)
}

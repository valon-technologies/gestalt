package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// ServeAgentContractProvider serves the target durable AgentProvider protocol.
func ServeAgentContractProvider(ctx context.Context, provider AgentContractProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAgent, provider))
		proto.RegisterAgentProviderServer(srv, agentContractProviderServer{provider: provider})
	})
}

type agentContractProviderServer struct {
	proto.UnimplementedAgentProviderServer
	provider AgentContractProvider
}

func (s agentContractProviderServer) CreateSession(ctx context.Context, req *proto.AgentProviderCreateSessionRequest) (*proto.AgentResource, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.CreateContractSession(ctx, req)
	return value, providerRPCError("agent contract create session", err)
}

func (s agentContractProviderServer) GetSession(ctx context.Context, req *proto.AgentProviderGetSessionRequest) (*proto.AgentResource, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.GetContractSession(ctx, req)
	return value, providerRPCError("agent contract get session", err)
}

func (s agentContractProviderServer) ListSessions(ctx context.Context, req *proto.AgentProviderListSessionsRequest) (*proto.AgentProviderListSessionsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.ListContractSessions(ctx, req)
	return value, providerRPCError("agent contract list sessions", err)
}

func (s agentContractProviderServer) ArchiveSession(ctx context.Context, req *proto.AgentProviderArchiveSessionRequest) (*proto.AgentResource, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.ArchiveContractSession(ctx, req)
	return value, providerRPCError("agent contract archive session", err)
}

func (s agentContractProviderServer) CreateConfigRevision(ctx context.Context, req *proto.AgentProviderCreateConfigRevisionRequest) (*proto.AgentResolvedConfigRevision, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.CreateContractConfigRevision(ctx, req)
	return value, providerRPCError("agent contract create config revision", err)
}

func (s agentContractProviderServer) CreateRun(ctx context.Context, req *proto.AgentProviderCreateRunRequest) (*proto.AgentRunResource, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.CreateContractRun(ctx, req)
	return value, providerRPCError("agent contract create run", err)
}

func (s agentContractProviderServer) GetRun(ctx context.Context, req *proto.AgentProviderGetRunRequest) (*proto.AgentRunResource, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.GetContractRun(ctx, req)
	return value, providerRPCError("agent contract get run", err)
}

func (s agentContractProviderServer) ListRuns(ctx context.Context, req *proto.AgentProviderListRunsRequest) (*proto.ListAgentRunsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.ListContractRuns(ctx, req)
	return value, providerRPCError("agent contract list runs", err)
}

func (s agentContractProviderServer) CancelRun(ctx context.Context, req *proto.AgentProviderCancelRunRequest) (*proto.AgentRunResource, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.CancelContractRun(ctx, req)
	return value, providerRPCError("agent contract cancel run", err)
}

func (s agentContractProviderServer) ListRunEvents(ctx context.Context, req *proto.AgentProviderListRunEventsRequest) (*proto.ListAgentRunEventsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.ListContractRunEvents(ctx, req)
	return value, providerRPCError("agent contract list run events", err)
}

func (s agentContractProviderServer) GetInteraction(ctx context.Context, req *proto.AgentProviderGetInteractionRequest) (*proto.AgentRunInteraction, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.GetContractInteraction(ctx, req)
	return value, providerRPCError("agent contract get interaction", err)
}

func (s agentContractProviderServer) ListInteractions(ctx context.Context, req *proto.AgentProviderListInteractionsRequest) (*proto.ListAgentRunInteractionsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.ListContractInteractions(ctx, req)
	return value, providerRPCError("agent contract list interactions", err)
}

func (s agentContractProviderServer) ResolveInteraction(ctx context.Context, req *proto.AgentProviderResolveInteractionRequest) (*proto.AgentRunInteraction, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	value, err := s.provider.ResolveContractInteraction(ctx, req)
	return value, providerRPCError("agent contract resolve interaction", err)
}

func (s agentContractProviderServer) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderContractCapabilitiesRequest) (*proto.AgentProviderContractCapabilities, error) {
	value, err := s.provider.GetContractCapabilities(ctx, req)
	return value, providerRPCError("agent contract get capabilities", err)
}

var _ proto.AgentProviderServer = agentContractProviderServer{}

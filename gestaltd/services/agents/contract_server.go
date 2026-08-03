package agents

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

// ContractServer exposes the target authenticated AgentService surface. The
// authenticated principal always comes from the transport context; request
// context fields are correlation/delegation data, not caller identity.
type ContractServer struct {
	proto.UnimplementedAgentServiceServer

	manager agentmanager.ContractService
}

func NewContractServer(manager agentmanager.ContractService) *ContractServer {
	return &ContractServer{manager: manager}
}

func (s *ContractServer) CreateAgent(ctx context.Context, req *proto.CreateAgentRequest) (*proto.AgentResource, error) {
	value, err := s.manager.CreateAgent(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) GetAgent(ctx context.Context, req *proto.GetAgentRequest) (*proto.AgentResource, error) {
	value, err := s.manager.GetAgent(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) ListAgents(ctx context.Context, req *proto.ListAgentsRequest) (*proto.ListAgentsResponse, error) {
	value, err := s.manager.ListAgents(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) ArchiveAgent(ctx context.Context, req *proto.ArchiveAgentRequest) (*proto.AgentResource, error) {
	value, err := s.manager.ArchiveAgent(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) CreateConfigRevision(
	ctx context.Context,
	req *proto.CreateAgentConfigRevisionRequest,
) (*proto.AgentConfigRevision, error) {
	value, err := s.manager.CreateConfigRevision(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) CreateRun(ctx context.Context, req *proto.CreateAgentRunRequest) (*proto.AgentRunResource, error) {
	value, err := s.manager.CreateRun(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) GetRun(ctx context.Context, req *proto.GetAgentRunRequest) (*proto.AgentRunResource, error) {
	value, err := s.manager.GetRun(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) ListRuns(ctx context.Context, req *proto.ListAgentRunsRequest) (*proto.ListAgentRunsResponse, error) {
	value, err := s.manager.ListRuns(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) CancelRun(ctx context.Context, req *proto.CancelAgentRunRequest) (*proto.AgentRunResource, error) {
	value, err := s.manager.CancelRun(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) ListRunEvents(
	ctx context.Context,
	req *proto.ListAgentRunEventsRequest,
) (*proto.ListAgentRunEventsResponse, error) {
	value, err := s.manager.ListRunEvents(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) GetInteraction(
	ctx context.Context,
	req *proto.GetAgentRunInteractionRequest,
) (*proto.AgentRunInteraction, error) {
	value, err := s.manager.GetRunInteraction(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) ListInteractions(
	ctx context.Context,
	req *proto.ListAgentRunInteractionsRequest,
) (*proto.ListAgentRunInteractionsResponse, error) {
	value, err := s.manager.ListRunInteractions(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

func (s *ContractServer) ResolveInteraction(
	ctx context.Context,
	req *proto.ResolveAgentRunInteractionRequest,
) (*proto.AgentRunInteraction, error) {
	value, err := s.manager.ResolveRunInteraction(ctx, principal.FromContext(ctx), req)
	return value, agentManagerStatusError(err)
}

var _ proto.AgentServiceServer = (*ContractServer)(nil)

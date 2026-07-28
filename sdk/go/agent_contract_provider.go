package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// AgentContractProvider implements the provider-owned durable lifecycle
// protocol. The Contract suffix keeps it additive with the original alpha
// AgentProvider until the coordinated cutover.
type AgentContractProvider interface {
	Provider
	CreateContractSession(context.Context, *AgentProviderCreateSessionRequest) (*AgentResource, error)
	GetContractSession(context.Context, *AgentProviderGetSessionRequest) (*AgentResource, error)
	ListContractSessions(context.Context, *AgentProviderListSessionsRequest) (*AgentProviderListSessionsResponse, error)
	ArchiveContractSession(context.Context, *AgentProviderArchiveSessionRequest) (*AgentResource, error)
	CreateContractConfigRevision(context.Context, *AgentProviderCreateConfigRevisionRequest) (*AgentResolvedConfigRevision, error)

	CreateContractRun(context.Context, *AgentProviderCreateRunRequest) (*AgentRunResource, error)
	GetContractRun(context.Context, *AgentProviderGetRunRequest) (*AgentRunResource, error)
	ListContractRuns(context.Context, *AgentProviderListRunsRequest) (*ListAgentRunsResponse, error)
	CancelContractRun(context.Context, *AgentProviderCancelRunRequest) (*AgentRunResource, error)
	ListContractRunEvents(context.Context, *AgentProviderListRunEventsRequest) (*ListAgentRunEventsResponse, error)

	GetContractInteraction(context.Context, *AgentProviderGetInteractionRequest) (*AgentRunInteraction, error)
	ListContractInteractions(context.Context, *AgentProviderListInteractionsRequest) (*ListAgentRunInteractionsResponse, error)
	ResolveContractInteraction(context.Context, *AgentProviderResolveInteractionRequest) (*AgentRunInteraction, error)

	GetContractCapabilities(context.Context, *GetAgentProviderContractCapabilitiesRequest) (*AgentProviderContractCapabilities, error)
}

// UnimplementedAgentContractProvider can be embedded while incrementally
// implementing the target provider contract.
type UnimplementedAgentContractProvider struct{}

func (UnimplementedAgentContractProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (UnimplementedAgentContractProvider) CreateContractSession(context.Context, *AgentProviderCreateSessionRequest) (*AgentResource, error) {
	return nil, Unimplemented("agent contract create session is not implemented")
}

func (UnimplementedAgentContractProvider) GetContractSession(context.Context, *AgentProviderGetSessionRequest) (*AgentResource, error) {
	return nil, Unimplemented("agent contract get session is not implemented")
}

func (UnimplementedAgentContractProvider) ListContractSessions(context.Context, *AgentProviderListSessionsRequest) (*AgentProviderListSessionsResponse, error) {
	return nil, Unimplemented("agent contract list sessions is not implemented")
}

func (UnimplementedAgentContractProvider) ArchiveContractSession(context.Context, *AgentProviderArchiveSessionRequest) (*AgentResource, error) {
	return nil, Unimplemented("agent contract archive session is not implemented")
}

func (UnimplementedAgentContractProvider) CreateContractConfigRevision(context.Context, *AgentProviderCreateConfigRevisionRequest) (*AgentResolvedConfigRevision, error) {
	return nil, Unimplemented("agent contract create config revision is not implemented")
}

func (UnimplementedAgentContractProvider) CreateContractRun(context.Context, *AgentProviderCreateRunRequest) (*AgentRunResource, error) {
	return nil, Unimplemented("agent contract create run is not implemented")
}

func (UnimplementedAgentContractProvider) GetContractRun(context.Context, *AgentProviderGetRunRequest) (*AgentRunResource, error) {
	return nil, Unimplemented("agent contract get run is not implemented")
}

func (UnimplementedAgentContractProvider) ListContractRuns(context.Context, *AgentProviderListRunsRequest) (*ListAgentRunsResponse, error) {
	return nil, Unimplemented("agent contract list runs is not implemented")
}

func (UnimplementedAgentContractProvider) CancelContractRun(context.Context, *AgentProviderCancelRunRequest) (*AgentRunResource, error) {
	return nil, Unimplemented("agent contract cancel run is not implemented")
}

func (UnimplementedAgentContractProvider) ListContractRunEvents(context.Context, *AgentProviderListRunEventsRequest) (*ListAgentRunEventsResponse, error) {
	return nil, Unimplemented("agent contract list run events is not implemented")
}

func (UnimplementedAgentContractProvider) GetContractInteraction(context.Context, *AgentProviderGetInteractionRequest) (*AgentRunInteraction, error) {
	return nil, Unimplemented("agent contract get interaction is not implemented")
}

func (UnimplementedAgentContractProvider) ListContractInteractions(context.Context, *AgentProviderListInteractionsRequest) (*ListAgentRunInteractionsResponse, error) {
	return nil, Unimplemented("agent contract list interactions is not implemented")
}

func (UnimplementedAgentContractProvider) ResolveContractInteraction(context.Context, *AgentProviderResolveInteractionRequest) (*AgentRunInteraction, error) {
	return nil, Unimplemented("agent contract resolve interaction is not implemented")
}

func (UnimplementedAgentContractProvider) GetContractCapabilities(context.Context, *GetAgentProviderContractCapabilitiesRequest) (*AgentProviderContractCapabilities, error) {
	return nil, Unimplemented("agent contract get capabilities is not implemented")
}

type AgentResource = proto.AgentResource
type AgentRunResource = proto.AgentRunResource
type AgentRunEvent = proto.AgentRunEvent
type AgentRunInteraction = proto.AgentRunInteraction
type AgentResolvedConfigRevision = proto.AgentResolvedConfigRevision
type AgentProviderContractCapabilities = proto.AgentProviderContractCapabilities

type AgentProviderCreateSessionRequest = proto.AgentProviderCreateSessionRequest
type AgentProviderGetSessionRequest = proto.AgentProviderGetSessionRequest
type AgentProviderListSessionsRequest = proto.AgentProviderListSessionsRequest
type AgentProviderListSessionsResponse = proto.AgentProviderListSessionsResponse
type AgentProviderArchiveSessionRequest = proto.AgentProviderArchiveSessionRequest
type AgentProviderCreateConfigRevisionRequest = proto.AgentProviderCreateConfigRevisionRequest
type AgentProviderCreateRunRequest = proto.AgentProviderCreateRunRequest
type AgentProviderGetRunRequest = proto.AgentProviderGetRunRequest
type AgentProviderListRunsRequest = proto.AgentProviderListRunsRequest
type AgentProviderCancelRunRequest = proto.AgentProviderCancelRunRequest
type AgentProviderListRunEventsRequest = proto.AgentProviderListRunEventsRequest
type AgentProviderGetInteractionRequest = proto.AgentProviderGetInteractionRequest
type AgentProviderListInteractionsRequest = proto.AgentProviderListInteractionsRequest
type AgentProviderResolveInteractionRequest = proto.AgentProviderResolveInteractionRequest
type GetAgentProviderContractCapabilitiesRequest = proto.GetAgentProviderContractCapabilitiesRequest
type ListAgentRunsResponse = proto.ListAgentRunsResponse
type ListAgentRunEventsResponse = proto.ListAgentRunEventsResponse
type ListAgentRunInteractionsResponse = proto.ListAgentRunInteractionsResponse

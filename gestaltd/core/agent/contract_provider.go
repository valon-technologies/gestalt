package agent

import (
	"context"
	"errors"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

var ErrContractUnsupported = errors.New("agent provider does not support the target contract")

const ContractProtocolVersion int32 = 1

// ContractProvider is the target provider-owned durable lifecycle boundary.
// Method names are distinct from the alpha Provider interface so both
// protocols can coexist on one provider during the coordinated cutover.
type ContractProvider interface {
	CreateContractSession(context.Context, *proto.AgentProviderCreateSessionRequest) (*proto.AgentResource, error)
	GetContractSession(context.Context, *proto.AgentProviderGetSessionRequest) (*proto.AgentResource, error)
	ListContractSessions(context.Context, *proto.AgentProviderListSessionsRequest) (*proto.AgentProviderListSessionsResponse, error)
	ArchiveContractSession(context.Context, *proto.AgentProviderArchiveSessionRequest) (*proto.AgentResource, error)
	CreateContractConfigRevision(context.Context, *proto.AgentProviderCreateConfigRevisionRequest) (*proto.AgentResolvedConfigRevision, error)

	CreateContractRun(context.Context, *proto.AgentProviderCreateRunRequest) (*proto.AgentRunResource, error)
	GetContractRun(context.Context, *proto.AgentProviderGetRunRequest) (*proto.AgentRunResource, error)
	ListContractRuns(context.Context, *proto.AgentProviderListRunsRequest) (*proto.ListAgentRunsResponse, error)
	CancelContractRun(context.Context, *proto.AgentProviderCancelRunRequest) (*proto.AgentRunResource, error)
	ListContractRunEvents(context.Context, *proto.AgentProviderListRunEventsRequest) (*proto.ListAgentRunEventsResponse, error)

	GetContractInteraction(context.Context, *proto.AgentProviderGetInteractionRequest) (*proto.AgentRunInteraction, error)
	ListContractInteractions(context.Context, *proto.AgentProviderListInteractionsRequest) (*proto.ListAgentRunInteractionsResponse, error)
	ResolveContractInteraction(context.Context, *proto.AgentProviderResolveInteractionRequest) (*proto.AgentRunInteraction, error)

	GetContractCapabilities(context.Context, *proto.GetAgentProviderContractCapabilitiesRequest) (*proto.AgentProviderContractCapabilities, error)
}

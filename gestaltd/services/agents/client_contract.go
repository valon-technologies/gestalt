package agents

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (r *remoteAgent) CreateContractSession(
	ctx context.Context,
	req *proto.AgentProviderCreateSessionRequest,
) (*proto.AgentResource, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderSessionCreateContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderCreateSessionRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.CreateSession(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) GetContractSession(
	ctx context.Context,
	req *proto.AgentProviderGetSessionRequest,
) (*proto.AgentResource, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderGetSessionRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.GetSession(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) ListContractSessions(
	ctx context.Context,
	req *proto.AgentProviderListSessionsRequest,
) (*proto.AgentProviderListSessionsResponse, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderListSessionsRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.ListSessions(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) ArchiveContractSession(
	ctx context.Context,
	req *proto.AgentProviderArchiveSessionRequest,
) (*proto.AgentResource, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderArchiveSessionRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.ArchiveSession(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) CreateContractConfigRevision(
	ctx context.Context,
	req *proto.AgentProviderCreateConfigRevisionRequest,
) (*proto.AgentResolvedConfigRevision, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderCreateConfigRevisionRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.CreateConfigRevision(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) CreateContractRun(
	ctx context.Context,
	req *proto.AgentProviderCreateRunRequest,
) (*proto.AgentRunResource, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderCreateRunRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.CreateRun(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) GetContractRun(
	ctx context.Context,
	req *proto.AgentProviderGetRunRequest,
) (*proto.AgentRunResource, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderGetRunRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.GetRun(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) ListContractRuns(
	ctx context.Context,
	req *proto.AgentProviderListRunsRequest,
) (*proto.ListAgentRunsResponse, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderListRunsRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.ListRuns(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) CancelContractRun(
	ctx context.Context,
	req *proto.AgentProviderCancelRunRequest,
) (*proto.AgentRunResource, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderCancelRunRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.CancelRun(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) ListContractRunEvents(
	ctx context.Context,
	req *proto.AgentProviderListRunEventsRequest,
) (*proto.ListAgentRunEventsResponse, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderListRunEventsRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.ListRunEvents(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) GetContractInteraction(
	ctx context.Context,
	req *proto.AgentProviderGetInteractionRequest,
) (*proto.AgentRunInteraction, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderGetInteractionRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.GetInteraction(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) ListContractInteractions(
	ctx context.Context,
	req *proto.AgentProviderListInteractionsRequest,
) (*proto.ListAgentRunInteractionsResponse, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderListInteractionsRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.ListInteractions(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) ResolveContractInteraction(
	ctx context.Context,
	req *proto.AgentProviderResolveInteractionRequest,
) (*proto.AgentRunInteraction, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	value := cloneAgentRequest(req, &proto.AgentProviderResolveInteractionRequest{})
	if err := attachAgentProviderRequestContext(ctx, value, r.name); err != nil {
		return nil, err
	}
	resp, err := r.contract.ResolveInteraction(ctx, value)
	return resp, contractError(err)
}

func (r *remoteAgent) GetContractCapabilities(
	ctx context.Context,
	req *proto.GetAgentProviderContractCapabilitiesRequest,
) (*proto.AgentProviderContractCapabilities, error) {
	if err := r.requireContract(); err != nil {
		return nil, err
	}
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.contract.GetCapabilities(
		ctx,
		cloneAgentRequest(req, &proto.GetAgentProviderContractCapabilitiesRequest{}),
	)
	return resp, contractError(err)
}

func (r *remoteAgent) requireContract() error {
	if r == nil || r.contract == nil {
		return coreagent.ErrContractUnsupported
	}
	return nil
}

func contractError(err error) error {
	if err == nil {
		return nil
	}
	if status.Code(err) == codes.Unimplemented {
		return errors.Join(coreagent.ErrContractUnsupported, err)
	}
	if status.Code(err) == codes.NotFound {
		return errors.Join(core.ErrNotFound, err)
	}
	return err
}

var _ coreagent.ContractProvider = (*remoteAgent)(nil)

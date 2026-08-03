package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func (p *hostedAgentProviderPool) CreateContractSession(
	ctx context.Context,
	req *proto.AgentProviderCreateSessionRequest,
) (*proto.AgentResource, error) {
	if req == nil {
		req = &proto.AgentProviderCreateSessionRequest{}
	}
	createKey := hostedAgentCreateKey(req.GetCreatedBySubjectId(), req.GetIdempotencyKey())
	preferred := p.createKeyBackend(createKey)
	backend, release, err := p.acquireBackendForNewWork(ctx, preferred, preferred != nil)
	if err != nil {
		return nil, err
	}
	for owner := p.claimCreateKeyBackend(createKey, backend); owner != backend; owner = p.claimCreateKeyBackend(createKey, backend) {
		release()
		backend, release, err = p.acquireBackend(ctx, owner, true)
		if err != nil {
			return nil, err
		}
	}
	provider, err := hostedContractProvider(backend)
	if err != nil {
		release()
		return nil, err
	}
	resource, err := provider.CreateContractSession(ctx, req)
	release()
	p.maybeProbeAfterCallError(backend, err)
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(resource.GetId())
	if sessionID == "" {
		return nil, fmt.Errorf("agent provider returned session without id")
	}
	p.recordSessionBackend(sessionID, backend)
	return resource, nil
}

func (p *hostedAgentProviderPool) GetContractSession(
	ctx context.Context,
	req *proto.AgentProviderGetSessionRequest,
) (*proto.AgentResource, error) {
	if req == nil {
		req = &proto.AgentProviderGetSessionRequest{}
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	resource, backend, err := hostedContractRead(
		ctx,
		p,
		p.sessionBackend(sessionID),
		func(provider coreagent.ContractProvider) (*proto.AgentResource, error) {
			return provider.GetContractSession(ctx, req)
		},
	)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			p.deleteSessionBackend(sessionID)
		}
		return nil, err
	}
	p.recordSessionBackend(resource.GetId(), backend)
	return resource, nil
}

func (p *hostedAgentProviderPool) ListContractSessions(
	ctx context.Context,
	req *proto.AgentProviderListSessionsRequest,
) (*proto.AgentProviderListSessionsResponse, error) {
	if req == nil {
		req = &proto.AgentProviderListSessionsRequest{}
	}
	if len(req.GetSessionIds()) > 0 {
		response := &proto.AgentProviderListSessionsResponse{}
		for _, sessionID := range req.GetSessionIds() {
			resource, err := p.GetContractSession(ctx, &proto.AgentProviderGetSessionRequest{
				SessionId: sessionID,
				Context:   req.GetContext(),
			})
			if err != nil {
				if errors.Is(err, core.ErrNotFound) {
					continue
				}
				return nil, err
			}
			if req.GetState() != proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED &&
				resource.GetState() != req.GetState() {
				continue
			}
			response.Sessions = append(response.Sessions, resource)
		}
		return response, nil
	}
	response, _, err := hostedContractRead(
		ctx,
		p,
		nil,
		func(provider coreagent.ContractProvider) (*proto.AgentProviderListSessionsResponse, error) {
			return provider.ListContractSessions(ctx, req)
		},
	)
	return response, err
}

func (p *hostedAgentProviderPool) ArchiveContractSession(
	ctx context.Context,
	req *proto.AgentProviderArchiveSessionRequest,
) (*proto.AgentResource, error) {
	if req == nil {
		req = &proto.AgentProviderArchiveSessionRequest{}
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	backend, err := p.contractSessionBackend(ctx, sessionID, req.GetContext())
	if err != nil {
		return nil, err
	}
	resource, _, err := hostedContractCall(
		ctx,
		p,
		backend,
		true,
		func(provider coreagent.ContractProvider) (*proto.AgentResource, error) {
			return provider.ArchiveContractSession(ctx, req)
		},
	)
	if err == nil {
		p.deleteSessionBackend(sessionID)
	}
	return resource, err
}

func (p *hostedAgentProviderPool) CreateContractConfigRevision(
	ctx context.Context,
	req *proto.AgentProviderCreateConfigRevisionRequest,
) (*proto.AgentResolvedConfigRevision, error) {
	if req == nil {
		req = &proto.AgentProviderCreateConfigRevisionRequest{}
	}
	backend, err := p.contractSessionBackend(ctx, req.GetSessionId(), req.GetContext())
	if err != nil {
		return nil, err
	}
	revision, _, err := hostedContractCall(
		ctx,
		p,
		backend,
		true,
		func(provider coreagent.ContractProvider) (*proto.AgentResolvedConfigRevision, error) {
			return provider.CreateContractConfigRevision(ctx, req)
		},
	)
	return revision, err
}

func (p *hostedAgentProviderPool) CreateContractRun(
	ctx context.Context,
	req *proto.AgentProviderCreateRunRequest,
) (*proto.AgentRunResource, error) {
	if req == nil {
		req = &proto.AgentProviderCreateRunRequest{}
	}
	runID := strings.TrimSpace(req.GetRunId())
	preferred := p.turnBackend(runID)
	allowDraining := preferred != nil
	if preferred == nil {
		preferred = p.sessionBackend(req.GetSessionId())
	}
	backend, release, err := p.acquireBackendForNewWork(ctx, preferred, allowDraining)
	if err != nil {
		return nil, err
	}
	provider, err := hostedContractProvider(backend)
	if err != nil {
		release()
		return nil, err
	}
	run, err := provider.CreateContractRun(ctx, req)
	release()
	p.maybeProbeAfterCallError(backend, err)
	if err != nil {
		return nil, err
	}
	if run == nil || strings.TrimSpace(run.GetId()) == "" {
		return nil, fmt.Errorf("agent provider returned run without id")
	}
	p.recordTurnBackend(run.GetId(), backend)
	return run, nil
}

func (p *hostedAgentProviderPool) GetContractRun(
	ctx context.Context,
	req *proto.AgentProviderGetRunRequest,
) (*proto.AgentRunResource, error) {
	if req == nil {
		req = &proto.AgentProviderGetRunRequest{}
	}
	runID := strings.TrimSpace(req.GetRunId())
	preferred := p.turnBackend(runID)
	if preferred == nil {
		preferred = p.sessionBackend(req.GetSessionId())
	}
	run, backend, err := hostedContractRead(
		ctx,
		p,
		preferred,
		func(provider coreagent.ContractProvider) (*proto.AgentRunResource, error) {
			return provider.GetContractRun(ctx, req)
		},
	)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			p.deleteTurnBackend(runID)
		}
		return nil, err
	}
	p.recordTurnBackend(run.GetId(), backend)
	return run, nil
}

func (p *hostedAgentProviderPool) ListContractRuns(
	ctx context.Context,
	req *proto.AgentProviderListRunsRequest,
) (*proto.ListAgentRunsResponse, error) {
	if req == nil {
		req = &proto.AgentProviderListRunsRequest{}
	}
	response, backend, err := hostedContractRead(
		ctx,
		p,
		p.sessionBackend(req.GetSessionId()),
		func(provider coreagent.ContractProvider) (*proto.ListAgentRunsResponse, error) {
			return provider.ListContractRuns(ctx, req)
		},
	)
	if err != nil {
		return nil, err
	}
	for _, run := range response.GetRuns() {
		if run != nil && strings.TrimSpace(run.GetId()) != "" {
			p.recordTurnBackend(run.GetId(), backend)
		}
	}
	return response, nil
}

func (p *hostedAgentProviderPool) CancelContractRun(
	ctx context.Context,
	req *proto.AgentProviderCancelRunRequest,
) (*proto.AgentRunResource, error) {
	if req == nil {
		req = &proto.AgentProviderCancelRunRequest{}
	}
	runID := strings.TrimSpace(req.GetRunId())
	backend, err := p.contractRunBackend(ctx, req.GetSessionId(), runID, req.GetContext())
	if err != nil {
		return nil, err
	}
	run, backend, err := hostedContractCall(
		ctx,
		p,
		backend,
		true,
		func(provider coreagent.ContractProvider) (*proto.AgentRunResource, error) {
			return provider.CancelContractRun(ctx, req)
		},
	)
	if err == nil && run != nil {
		p.recordTurnBackend(run.GetId(), backend)
	}
	return run, err
}

func (p *hostedAgentProviderPool) ListContractRunEvents(
	ctx context.Context,
	req *proto.AgentProviderListRunEventsRequest,
) (*proto.ListAgentRunEventsResponse, error) {
	if req == nil {
		req = &proto.AgentProviderListRunEventsRequest{}
	}
	backend, err := p.contractRunBackend(ctx, req.GetSessionId(), req.GetRunId(), req.GetContext())
	if err != nil {
		return nil, err
	}
	response, _, err := hostedContractCall(
		ctx,
		p,
		backend,
		true,
		func(provider coreagent.ContractProvider) (*proto.ListAgentRunEventsResponse, error) {
			return provider.ListContractRunEvents(ctx, req)
		},
	)
	return response, err
}

func (p *hostedAgentProviderPool) GetContractInteraction(
	ctx context.Context,
	req *proto.AgentProviderGetInteractionRequest,
) (*proto.AgentRunInteraction, error) {
	if req == nil {
		req = &proto.AgentProviderGetInteractionRequest{}
	}
	interactionID := strings.TrimSpace(req.GetInteractionId())
	preferred := p.interactionBackend(interactionID)
	if preferred == nil {
		preferred = p.turnBackend(req.GetRunId())
	}
	interaction, backend, err := hostedContractRead(
		ctx,
		p,
		preferred,
		func(provider coreagent.ContractProvider) (*proto.AgentRunInteraction, error) {
			return provider.GetContractInteraction(ctx, req)
		},
	)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			p.deleteInteractionBackend(interactionID)
		}
		return nil, err
	}
	p.recordInteractionBackend(interaction.GetId(), backend)
	return interaction, nil
}

func (p *hostedAgentProviderPool) ListContractInteractions(
	ctx context.Context,
	req *proto.AgentProviderListInteractionsRequest,
) (*proto.ListAgentRunInteractionsResponse, error) {
	if req == nil {
		req = &proto.AgentProviderListInteractionsRequest{}
	}
	preferred := p.turnBackend(req.GetRunId())
	if preferred == nil {
		preferred = p.sessionBackend(req.GetSessionId())
	}
	response, backend, err := hostedContractRead(
		ctx,
		p,
		preferred,
		func(provider coreagent.ContractProvider) (*proto.ListAgentRunInteractionsResponse, error) {
			return provider.ListContractInteractions(ctx, req)
		},
	)
	if err != nil {
		return nil, err
	}
	for _, interaction := range response.GetInteractions() {
		if interaction != nil && strings.TrimSpace(interaction.GetId()) != "" {
			p.recordInteractionBackend(interaction.GetId(), backend)
		}
	}
	return response, nil
}

func (p *hostedAgentProviderPool) ResolveContractInteraction(
	ctx context.Context,
	req *proto.AgentProviderResolveInteractionRequest,
) (*proto.AgentRunInteraction, error) {
	if req == nil {
		req = &proto.AgentProviderResolveInteractionRequest{}
	}
	interactionID := strings.TrimSpace(req.GetInteractionId())
	backend := p.interactionBackend(interactionID)
	if backend == nil {
		interaction, err := p.GetContractInteraction(ctx, &proto.AgentProviderGetInteractionRequest{
			SessionId:     req.GetSessionId(),
			RunId:         req.GetRunId(),
			InteractionId: interactionID,
			Context:       req.GetContext(),
		})
		if err != nil {
			return nil, err
		}
		backend = p.interactionBackend(interaction.GetId())
	}
	if backend == nil {
		return nil, core.ErrNotFound
	}
	interaction, backend, err := hostedContractCall(
		ctx,
		p,
		backend,
		true,
		func(provider coreagent.ContractProvider) (*proto.AgentRunInteraction, error) {
			return provider.ResolveContractInteraction(ctx, req)
		},
	)
	if err == nil && interaction != nil {
		p.recordInteractionBackend(interaction.GetId(), backend)
	}
	return interaction, err
}

func (p *hostedAgentProviderPool) GetContractCapabilities(
	ctx context.Context,
	req *proto.GetAgentProviderContractCapabilitiesRequest,
) (*proto.AgentProviderContractCapabilities, error) {
	if req == nil {
		req = &proto.GetAgentProviderContractCapabilitiesRequest{}
	}
	capabilities, _, err := hostedContractRead(
		ctx,
		p,
		nil,
		func(provider coreagent.ContractProvider) (*proto.AgentProviderContractCapabilities, error) {
			return provider.GetContractCapabilities(ctx, req)
		},
	)
	return capabilities, err
}

func (p *hostedAgentProviderPool) contractSessionBackend(
	ctx context.Context,
	sessionID string,
	reqContext *proto.RequestContext,
) (*hostedAgentPoolBackend, error) {
	sessionID = strings.TrimSpace(sessionID)
	if backend := p.sessionBackend(sessionID); backend != nil {
		return backend, nil
	}
	resource, err := p.GetContractSession(ctx, &proto.AgentProviderGetSessionRequest{
		SessionId: sessionID,
		Context:   reqContext,
	})
	if err != nil {
		return nil, err
	}
	backend := p.sessionBackend(resource.GetId())
	if backend == nil {
		return nil, core.ErrNotFound
	}
	return backend, nil
}

func (p *hostedAgentProviderPool) contractRunBackend(
	ctx context.Context,
	sessionID string,
	runID string,
	reqContext *proto.RequestContext,
) (*hostedAgentPoolBackend, error) {
	runID = strings.TrimSpace(runID)
	if backend := p.turnBackend(runID); backend != nil {
		return backend, nil
	}
	run, err := p.GetContractRun(ctx, &proto.AgentProviderGetRunRequest{
		SessionId: strings.TrimSpace(sessionID),
		RunId:     runID,
		Context:   reqContext,
	})
	if err != nil {
		return nil, err
	}
	backend := p.turnBackend(run.GetId())
	if backend == nil {
		return nil, core.ErrNotFound
	}
	return backend, nil
}

func hostedContractProvider(
	backend *hostedAgentPoolBackend,
) (coreagent.ContractProvider, error) {
	if backend == nil || backend.provider == nil {
		return nil, coreagent.ErrContractUnsupported
	}
	provider, ok := backend.provider.(coreagent.ContractProvider)
	if !ok {
		return nil, coreagent.ErrContractUnsupported
	}
	return provider, nil
}

func hostedContractCall[T any](
	ctx context.Context,
	p *hostedAgentProviderPool,
	backend *hostedAgentPoolBackend,
	allowDraining bool,
	call func(coreagent.ContractProvider) (T, error),
) (T, *hostedAgentPoolBackend, error) {
	var zero T
	acquired, release, err := p.acquireBackend(ctx, backend, allowDraining)
	if err != nil {
		return zero, nil, err
	}
	provider, err := hostedContractProvider(acquired)
	if err != nil {
		release()
		return zero, nil, err
	}
	value, err := call(provider)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	if err != nil {
		return zero, acquired, err
	}
	return value, acquired, nil
}

func hostedContractRead[T any](
	ctx context.Context,
	p *hostedAgentProviderPool,
	preferred *hostedAgentPoolBackend,
	call func(coreagent.ContractProvider) (T, error),
) (T, *hostedAgentPoolBackend, error) {
	var (
		zero         T
		retryableErr error
		notFoundErr  error
	)
	tried := map[*hostedAgentPoolBackend]struct{}{}
	candidates := make([]*hostedAgentPoolBackend, 0, 1)
	if preferred != nil {
		candidates = append(candidates, preferred)
	}
	candidates = append(candidates, p.availableBackends(true)...)
	for _, backend := range candidates {
		if backend == nil {
			continue
		}
		if _, ok := tried[backend]; ok {
			continue
		}
		tried[backend] = struct{}{}
		value, acquired, err := hostedContractCall(ctx, p, backend, true, call)
		switch {
		case err == nil:
			return value, acquired, nil
		case errors.Is(err, core.ErrNotFound):
			notFoundErr = err
		case isHostedAgentReadRetryableError(err):
			retryableErr = err
		default:
			return zero, acquired, err
		}
	}
	if retryableErr != nil {
		return zero, nil, retryableErr
	}
	if notFoundErr != nil {
		return zero, nil, notFoundErr
	}
	return zero, nil, core.ErrNotFound
}

func (p *hostedAgentProviderPool) recordInteractionBackend(
	interactionID string,
	backend *hostedAgentPoolBackend,
) {
	interactionID = strings.TrimSpace(interactionID)
	if p == nil || interactionID == "" || backend == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.interactionBackends == nil {
		p.interactionBackends = map[string]*hostedAgentPoolBackend{}
	}
	p.interactionBackends[interactionID] = backend
}

var _ coreagent.ContractProvider = (*hostedAgentProviderPool)(nil)

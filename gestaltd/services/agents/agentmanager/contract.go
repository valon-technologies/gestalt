package agentmanager

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentroute"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrAgentRoutesNotConfigured       = errors.New("agent route store is not configured")
	ErrAgentContractVersionMismatch   = errors.New("agent provider contract version mismatch")
	ErrAgentContractFeatureNotReady   = errors.New("agent contract feature is not implemented")
	ErrAgentConfigRevisionReadMissing = errors.New("agent provider contract cannot read canonical config revisions")
)

const (
	contractListDefaultPageSize = 50
	contractListMaxPageSize     = 200
)

// ContractService is the target authenticated AgentService lifecycle. It is
// separate from Service while the original alpha protocol remains available.
type ContractService interface {
	CreateAgent(context.Context, *principal.Principal, *proto.CreateAgentRequest) (*proto.AgentResource, error)
	GetAgent(context.Context, *principal.Principal, *proto.GetAgentRequest) (*proto.AgentResource, error)
	ListAgents(context.Context, *principal.Principal, *proto.ListAgentsRequest) (*proto.ListAgentsResponse, error)
	ArchiveAgent(context.Context, *principal.Principal, *proto.ArchiveAgentRequest) (*proto.AgentResource, error)
	CreateConfigRevision(context.Context, *principal.Principal, *proto.CreateAgentConfigRevisionRequest) (*proto.AgentConfigRevision, error)

	CreateRun(context.Context, *principal.Principal, *proto.CreateAgentRunRequest) (*proto.AgentRunResource, error)
	GetRun(context.Context, *principal.Principal, *proto.GetAgentRunRequest) (*proto.AgentRunResource, error)
	ListRuns(context.Context, *principal.Principal, *proto.ListAgentRunsRequest) (*proto.ListAgentRunsResponse, error)
	CancelRun(context.Context, *principal.Principal, *proto.CancelAgentRunRequest) (*proto.AgentRunResource, error)
	ListRunEvents(context.Context, *principal.Principal, *proto.ListAgentRunEventsRequest) (*proto.ListAgentRunEventsResponse, error)

	GetRunInteraction(context.Context, *principal.Principal, *proto.GetAgentRunInteractionRequest) (*proto.AgentRunInteraction, error)
	ListRunInteractions(context.Context, *principal.Principal, *proto.ListAgentRunInteractionsRequest) (*proto.ListAgentRunInteractionsResponse, error)
	ResolveRunInteraction(context.Context, *principal.Principal, *proto.ResolveAgentRunInteractionRequest) (*proto.AgentRunInteraction, error)
}

type contractRoute struct {
	route    *agentroute.Route
	provider coreagent.ContractProvider
}

func (m *Manager) CreateAgent(
	ctx context.Context,
	p *principal.Principal,
	req *proto.CreateAgentRequest,
) (*proto.AgentResource, error) {
	if req == nil || req.GetConfig() == nil {
		return nil, fmt.Errorf("%w: agent config is required", invocation.ErrInvalidInvocation)
	}
	p, subjectID, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	if m == nil || m.routes == nil {
		return nil, ErrAgentRoutesNotConfigured
	}

	config, err := normalizeContractConfig(req.GetConfig())
	if err != nil {
		return nil, err
	}
	fingerprint, err := contractFingerprint(config)
	if err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(req.GetIdempotencyKey())
	if idempotencyKey != "" {
		existing, lookupErr := m.routes.FindByIdempotency(ctx, subjectID, idempotencyKey)
		switch {
		case lookupErr == nil:
			if existing.RequestFingerprint != fingerprint {
				return nil, fmt.Errorf("%w: idempotency key was used with different agent config", agentroute.ErrConflict)
			}
			return m.getContractAgentForRoute(ctx, p, req.GetContext(), existing)
		case !errors.Is(lookupErr, agentroute.ErrNotFound):
			return nil, lookupErr
		}
	}

	providerName, provider, err := m.resolveContractProvider(ctx, p, config.GetProviderName())
	if err != nil {
		return nil, err
	}
	capabilities, err := provider.GetContractCapabilities(ctx, &proto.GetAgentProviderContractCapabilitiesRequest{})
	if err != nil {
		return nil, err
	}
	if capabilities == nil || capabilities.GetProtocolVersion() != coreagent.ContractProtocolVersion {
		got := int32(0)
		if capabilities != nil {
			got = capabilities.GetProtocolVersion()
		}
		return nil, fmt.Errorf(
			"%w: provider %q reports version %d, require %d",
			ErrAgentContractVersionMismatch,
			providerName,
			got,
			coreagent.ContractProtocolVersion,
		)
	}
	if err := validateContractCreationFeatures(config, capabilities); err != nil {
		return nil, err
	}

	agentID := contractResourceID("agent", subjectID, idempotencyKey)
	revisionID := contractStableResourceID("revision", agentID, "initial")
	now := timestamppb.New(time.Now().UTC())
	initialConfig := &proto.AgentResolvedConfigRevision{
		Id:             revisionID,
		Model:          config.GetModel(),
		Instructions:   config.GetInstructions(),
		HistoryPolicy:  &proto.AgentHistoryPolicy{Strategy: "provider_default"},
		CreatedAt:      now,
		ResolvedTools:  nil,
		ResolvedSkills: nil,
	}
	callCtx, providerReqContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), providerName)
	if err != nil {
		return nil, err
	}
	resource, err := provider.CreateContractSession(callCtx, &proto.AgentProviderCreateSessionRequest{
		SessionId:          agentID,
		IdempotencyKey:     idempotencyKey,
		InitialConfig:      initialConfig,
		CreatedBySubjectId: subjectID,
		Context:            providerReqContext,
	})
	if err != nil {
		return nil, err
	}
	resource, err = normalizeContractAgentResource(resource, agentID, providerName, subjectID, revisionID)
	if err != nil {
		return nil, err
	}

	route, _, err := m.routes.Create(ctx, agentroute.CreateRequest{
		Route: agentroute.Route{
			AgentID:            agentID,
			OwnerSubjectID:     subjectID,
			ProviderName:       providerName,
			ConfigRevision:     revisionID,
			RequestFingerprint: fingerprint,
			State:              agentroute.StateActive,
		},
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	if route.AgentID != resource.GetId() || route.ConfigRevision != resource.GetConfigRevision() {
		return nil, fmt.Errorf("persisted agent route does not match provider resource")
	}
	return resource, nil
}

func (m *Manager) GetAgent(
	ctx context.Context,
	p *principal.Principal,
	req *proto.GetAgentRequest,
) (*proto.AgentResource, error) {
	if req == nil {
		req = &proto.GetAgentRequest{}
	}
	p, _, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	owned, err := m.contractRoute(ctx, p, req.GetAgentId())
	if err != nil {
		return nil, err
	}
	return m.getContractAgentForRoute(ctx, p, req.GetContext(), owned.route)
}

func (m *Manager) ListAgents(
	ctx context.Context,
	p *principal.Principal,
	req *proto.ListAgentsRequest,
) (*proto.ListAgentsResponse, error) {
	if req == nil {
		req = &proto.ListAgentsRequest{}
	}
	p, subjectID, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	if m == nil || m.routes == nil {
		return nil, ErrAgentRoutesNotConfigured
	}
	state, err := contractRouteState(req.GetState())
	if err != nil {
		return nil, err
	}
	routes, err := m.routes.ListOwned(ctx, subjectID, state)
	if err != nil {
		return nil, err
	}
	if len(req.GetAgentIds()) > 0 {
		allowed := make(map[string]struct{}, len(req.GetAgentIds()))
		for _, id := range req.GetAgentIds() {
			if id = strings.TrimSpace(id); id != "" {
				allowed[id] = struct{}{}
			}
		}
		filtered := routes[:0]
		for _, route := range routes {
			if _, ok := allowed[route.AgentID]; ok {
				filtered = append(filtered, route)
			}
		}
		routes = filtered
	}
	offset, err := decodeContractPageToken(req.GetPageToken())
	if err != nil || offset > len(routes) {
		return nil, fmt.Errorf("%w: invalid page token", invocation.ErrInvalidInvocation)
	}
	pageSize, err := contractPageSize(req.GetPageSize())
	if err != nil {
		return nil, err
	}
	end := min(offset+pageSize, len(routes))
	response := &proto.ListAgentsResponse{Agents: make([]*proto.AgentResource, 0, end-offset)}
	for _, route := range routes[offset:end] {
		resource, getErr := m.getContractAgentForRoute(ctx, p, req.GetContext(), route)
		if getErr != nil {
			return nil, getErr
		}
		response.Agents = append(response.Agents, resource)
	}
	if end < len(routes) {
		response.NextPageToken = encodeContractPageToken(end)
	}
	return response, nil
}

func (m *Manager) ArchiveAgent(
	ctx context.Context,
	p *principal.Principal,
	req *proto.ArchiveAgentRequest,
) (*proto.AgentResource, error) {
	if req == nil {
		req = &proto.ArchiveAgentRequest{}
	}
	p, _, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	owned, err := m.contractRoute(ctx, p, req.GetAgentId())
	if err != nil {
		return nil, err
	}
	callCtx, requestContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.route.ProviderName)
	if err != nil {
		return nil, err
	}
	resource, err := owned.provider.ArchiveContractSession(callCtx, &proto.AgentProviderArchiveSessionRequest{
		SessionId:      owned.route.AgentID,
		IdempotencyKey: strings.TrimSpace(req.GetIdempotencyKey()),
		Context:        requestContext,
	})
	if err != nil {
		return nil, err
	}
	resource, err = normalizeContractAgentResource(
		resource,
		owned.route.AgentID,
		owned.route.ProviderName,
		owned.route.OwnerSubjectID,
		owned.route.ConfigRevision,
	)
	if err != nil {
		return nil, err
	}
	if resource.GetState() != proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED {
		return nil, fmt.Errorf("agent provider did not archive session %q", owned.route.AgentID)
	}
	if _, err := m.routes.Archive(ctx, owned.route.AgentID, owned.route.OwnerSubjectID); err != nil {
		return nil, err
	}
	return resource, nil
}

func (m *Manager) CreateConfigRevision(
	context.Context,
	*principal.Principal,
	*proto.CreateAgentConfigRevisionRequest,
) (*proto.AgentConfigRevision, error) {
	// Applying a partial public update requires reading the provider's
	// canonical resolved revision first. The current draft provider protocol
	// has only createConfigRevision, so attempting to synthesize the full
	// revision here would either make Gestalt canonical or drop configuration.
	return nil, fmt.Errorf(
		"%w: %w",
		ErrAgentContractFeatureNotReady,
		ErrAgentConfigRevisionReadMissing,
	)
}

func (m *Manager) CreateRun(
	ctx context.Context,
	p *principal.Principal,
	req *proto.CreateAgentRunRequest,
) (*proto.AgentRunResource, error) {
	if req == nil {
		req = &proto.CreateAgentRunRequest{}
	}
	p, subjectID, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.GetMessage()) == "" {
		return nil, fmt.Errorf("%w: message is required", invocation.ErrInvalidInvocation)
	}
	owned, err := m.contractRoute(ctx, p, req.GetAgentId())
	if err != nil {
		return nil, err
	}
	if owned.route.State != agentroute.StateActive {
		return nil, fmt.Errorf("%w: archived agent cannot create runs", agentroute.ErrConflict)
	}
	idempotencyKey := strings.TrimSpace(req.GetIdempotencyKey())
	runID := contractResourceID("run", owned.route.AgentID+"\x00"+subjectID, idempotencyKey)
	callCtx, requestContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.route.ProviderName)
	if err != nil {
		return nil, err
	}
	run, err := owned.provider.CreateContractRun(callCtx, &proto.AgentProviderCreateRunRequest{
		SessionId:          owned.route.AgentID,
		RunId:              runID,
		IdempotencyKey:     idempotencyKey,
		Message:            req.GetMessage(),
		ConfigRevision:     owned.route.ConfigRevision,
		ExecutionRef:       contractStableResourceID("execution", owned.route.AgentID, runID),
		AuthorityRef:       owned.route.AuthorityRef,
		CreatedBySubjectId: subjectID,
		Context:            requestContext,
	})
	if err != nil {
		return nil, err
	}
	return normalizeContractRun(run, owned.route, runID, owned.route.ConfigRevision)
}

func (m *Manager) GetRun(
	ctx context.Context,
	p *principal.Principal,
	req *proto.GetAgentRunRequest,
) (*proto.AgentRunResource, error) {
	if req == nil {
		req = &proto.GetAgentRunRequest{}
	}
	p, _, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	owned, err := m.contractRoute(ctx, p, req.GetAgentId())
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, core.ErrNotFound
	}
	callCtx, requestContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.route.ProviderName)
	if err != nil {
		return nil, err
	}
	run, err := owned.provider.GetContractRun(callCtx, &proto.AgentProviderGetRunRequest{
		SessionId: owned.route.AgentID,
		RunId:     runID,
		Context:   requestContext,
	})
	if err != nil {
		return nil, err
	}
	return normalizeContractRun(run, owned.route, runID, "")
}

func (m *Manager) ListRuns(
	ctx context.Context,
	p *principal.Principal,
	req *proto.ListAgentRunsRequest,
) (*proto.ListAgentRunsResponse, error) {
	if req == nil {
		req = &proto.ListAgentRunsRequest{}
	}
	p, _, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	if _, err := contractPageSize(req.GetPageSize()); err != nil {
		return nil, err
	}
	owned, err := m.contractRoute(ctx, p, req.GetAgentId())
	if err != nil {
		return nil, err
	}
	callCtx, requestContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.route.ProviderName)
	if err != nil {
		return nil, err
	}
	response, err := owned.provider.ListContractRuns(callCtx, &proto.AgentProviderListRunsRequest{
		SessionId: owned.route.AgentID,
		PageSize:  req.GetPageSize(),
		PageToken: strings.TrimSpace(req.GetPageToken()),
		Context:   requestContext,
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("agent provider returned nil run list")
	}
	for _, run := range response.GetRuns() {
		if _, err := normalizeContractRun(run, owned.route, strings.TrimSpace(run.GetId()), ""); err != nil {
			return nil, err
		}
	}
	return response, nil
}

func (m *Manager) CancelRun(
	ctx context.Context,
	p *principal.Principal,
	req *proto.CancelAgentRunRequest,
) (*proto.AgentRunResource, error) {
	if req == nil {
		req = &proto.CancelAgentRunRequest{}
	}
	p, _, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	owned, err := m.contractRoute(ctx, p, req.GetAgentId())
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, core.ErrNotFound
	}
	callCtx, requestContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.route.ProviderName)
	if err != nil {
		return nil, err
	}
	run, err := owned.provider.CancelContractRun(callCtx, &proto.AgentProviderCancelRunRequest{
		SessionId:      owned.route.AgentID,
		RunId:          runID,
		Reason:         strings.TrimSpace(req.GetReason()),
		IdempotencyKey: strings.TrimSpace(req.GetIdempotencyKey()),
		Context:        requestContext,
	})
	if err != nil {
		return nil, err
	}
	return normalizeContractRun(run, owned.route, runID, "")
}

func (m *Manager) ListRunEvents(
	ctx context.Context,
	p *principal.Principal,
	req *proto.ListAgentRunEventsRequest,
) (*proto.ListAgentRunEventsResponse, error) {
	if req == nil {
		req = &proto.ListAgentRunEventsRequest{}
	}
	p, _, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	if _, err := contractPageSize(req.GetPageSize()); err != nil {
		return nil, err
	}
	owned, err := m.contractRoute(ctx, p, req.GetAgentId())
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, core.ErrNotFound
	}
	callCtx, requestContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.route.ProviderName)
	if err != nil {
		return nil, err
	}
	response, err := owned.provider.ListContractRunEvents(callCtx, &proto.AgentProviderListRunEventsRequest{
		SessionId:   owned.route.AgentID,
		RunId:       runID,
		AfterCursor: strings.TrimSpace(req.GetAfterCursor()),
		PageSize:    req.GetPageSize(),
		Context:     requestContext,
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("agent provider returned nil event list")
	}
	for _, event := range response.GetEvents() {
		if event == nil ||
			strings.TrimSpace(event.GetAgentId()) != owned.route.AgentID ||
			strings.TrimSpace(event.GetRunId()) != runID ||
			strings.TrimSpace(event.GetId()) == "" ||
			strings.TrimSpace(event.GetCursor()) == "" {
			return nil, fmt.Errorf("agent provider returned invalid run event")
		}
	}
	return response, nil
}

func (m *Manager) GetRunInteraction(
	ctx context.Context,
	p *principal.Principal,
	req *proto.GetAgentRunInteractionRequest,
) (*proto.AgentRunInteraction, error) {
	if req == nil {
		req = &proto.GetAgentRunInteractionRequest{}
	}
	p, _, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	owned, runID, interactionID, err := m.contractInteractionRoute(ctx, p, req.GetAgentId(), req.GetRunId(), req.GetInteractionId())
	if err != nil {
		return nil, err
	}
	callCtx, requestContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.route.ProviderName)
	if err != nil {
		return nil, err
	}
	interaction, err := owned.provider.GetContractInteraction(callCtx, &proto.AgentProviderGetInteractionRequest{
		SessionId:     owned.route.AgentID,
		RunId:         runID,
		InteractionId: interactionID,
		Context:       requestContext,
	})
	if err != nil {
		return nil, err
	}
	return normalizeContractInteraction(interaction, owned.route.AgentID, runID, interactionID)
}

func (m *Manager) ListRunInteractions(
	ctx context.Context,
	p *principal.Principal,
	req *proto.ListAgentRunInteractionsRequest,
) (*proto.ListAgentRunInteractionsResponse, error) {
	if req == nil {
		req = &proto.ListAgentRunInteractionsRequest{}
	}
	p, _, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	if _, err := contractPageSize(req.GetPageSize()); err != nil {
		return nil, err
	}
	owned, err := m.contractRoute(ctx, p, req.GetAgentId())
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, core.ErrNotFound
	}
	callCtx, requestContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.route.ProviderName)
	if err != nil {
		return nil, err
	}
	response, err := owned.provider.ListContractInteractions(callCtx, &proto.AgentProviderListInteractionsRequest{
		SessionId: owned.route.AgentID,
		RunId:     runID,
		State:     req.GetState(),
		PageSize:  req.GetPageSize(),
		PageToken: strings.TrimSpace(req.GetPageToken()),
		Context:   requestContext,
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("agent provider returned nil interaction list")
	}
	for _, interaction := range response.GetInteractions() {
		if _, err := normalizeContractInteraction(interaction, owned.route.AgentID, runID, strings.TrimSpace(interaction.GetId())); err != nil {
			return nil, err
		}
	}
	return response, nil
}

func (m *Manager) ResolveRunInteraction(
	ctx context.Context,
	p *principal.Principal,
	req *proto.ResolveAgentRunInteractionRequest,
) (*proto.AgentRunInteraction, error) {
	if req == nil {
		req = &proto.ResolveAgentRunInteractionRequest{}
	}
	p, _, err := contractPrincipal(p)
	if err != nil {
		return nil, err
	}
	if req.GetResolution() == nil {
		return nil, fmt.Errorf("%w: interaction resolution is required", invocation.ErrInvalidInvocation)
	}
	owned, runID, interactionID, err := m.contractInteractionRoute(ctx, p, req.GetAgentId(), req.GetRunId(), req.GetInteractionId())
	if err != nil {
		return nil, err
	}
	callCtx, requestContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.route.ProviderName)
	if err != nil {
		return nil, err
	}
	interaction, err := owned.provider.ResolveContractInteraction(callCtx, &proto.AgentProviderResolveInteractionRequest{
		SessionId:      owned.route.AgentID,
		RunId:          runID,
		InteractionId:  interactionID,
		IdempotencyKey: strings.TrimSpace(req.GetIdempotencyKey()),
		Resolution:     gproto.Clone(req.GetResolution()).(*proto.AgentInteractionResolution),
		Context:        requestContext,
	})
	if err != nil {
		return nil, err
	}
	return normalizeContractInteraction(interaction, owned.route.AgentID, runID, interactionID)
}

func (m *Manager) contractRoute(
	ctx context.Context,
	p *principal.Principal,
	agentID string,
) (*contractRoute, error) {
	if m == nil || m.routes == nil {
		return nil, ErrAgentRoutesNotConfigured
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, core.ErrNotFound
	}
	subjectID := strings.TrimSpace(principalSubjectID(p))
	route, err := m.routes.GetOwned(ctx, agentID, subjectID)
	if err != nil {
		if errors.Is(err, agentroute.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, err
	}
	_, provider, err := m.resolveContractProvider(ctx, p, route.ProviderName)
	if err != nil {
		return nil, err
	}
	return &contractRoute{route: route, provider: provider}, nil
}

func (m *Manager) resolveContractProvider(
	ctx context.Context,
	p *principal.Principal,
	providerName string,
) (string, coreagent.ContractProvider, error) {
	resolvedName, raw, err := m.resolveProvider(ctx, providerName)
	if err != nil {
		return "", nil, err
	}
	if !m.allowsAgentProvider(ctx, p, resolvedName) {
		return "", nil, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, resolvedName)
	}
	provider, ok := raw.(coreagent.ContractProvider)
	if !ok {
		return "", nil, fmt.Errorf("%w: provider %q", coreagent.ErrContractUnsupported, resolvedName)
	}
	return resolvedName, provider, nil
}

func (m *Manager) getContractAgentForRoute(
	ctx context.Context,
	p *principal.Principal,
	existingContext *proto.RequestContext,
	route *agentroute.Route,
) (*proto.AgentResource, error) {
	_, provider, err := m.resolveContractProvider(ctx, p, route.ProviderName)
	if err != nil {
		return nil, err
	}
	callCtx, requestContext, err := agentProviderRequestContext(ctx, p, existingContext, route.ProviderName)
	if err != nil {
		return nil, err
	}
	resource, err := provider.GetContractSession(callCtx, &proto.AgentProviderGetSessionRequest{
		SessionId: route.AgentID,
		Context:   requestContext,
	})
	if err != nil {
		return nil, err
	}
	return normalizeContractAgentResource(
		resource,
		route.AgentID,
		route.ProviderName,
		route.OwnerSubjectID,
		route.ConfigRevision,
	)
}

func (m *Manager) contractInteractionRoute(
	ctx context.Context,
	p *principal.Principal,
	agentID string,
	runID string,
	interactionID string,
) (*contractRoute, string, string, error) {
	owned, err := m.contractRoute(ctx, p, agentID)
	if err != nil {
		return nil, "", "", err
	}
	runID = strings.TrimSpace(runID)
	interactionID = strings.TrimSpace(interactionID)
	if runID == "" || interactionID == "" {
		return nil, "", "", core.ErrNotFound
	}
	return owned, runID, interactionID, nil
}

func contractPrincipal(p *principal.Principal) (*principal.Principal, string, error) {
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, "", ErrAgentSubjectRequired
	}
	return p, subjectID, nil
}

func normalizeContractConfig(config *proto.AgentConfigInput) (*proto.AgentConfigInput, error) {
	config = gproto.Clone(config).(*proto.AgentConfigInput)
	config.ProviderName = strings.TrimSpace(config.GetProviderName())
	config.Model = strings.TrimSpace(config.GetModel())
	config.Instructions = strings.TrimSpace(config.GetInstructions())
	tools := config.GetTools()
	if tools != nil {
		if tools.GetDisabled() && len(tools.GetRefs()) > 0 {
			return nil, fmt.Errorf("%w: disabled tools cannot include refs", invocation.ErrInvalidInvocation)
		}
		for _, ref := range tools.GetRefs() {
			if ref == nil {
				return nil, fmt.Errorf("%w: tool refs cannot be null", invocation.ErrInvalidInvocation)
			}
		}
	}
	if skills := config.GetSkills(); skills != nil {
		for _, ref := range skills.GetRefs() {
			if ref == nil {
				return nil, fmt.Errorf("%w: skill refs cannot be null", invocation.ErrInvalidInvocation)
			}
		}
	}
	return config, nil
}

func validateContractCreationFeatures(
	config *proto.AgentConfigInput,
	capabilities *proto.AgentProviderContractCapabilities,
) error {
	if tools := config.GetTools(); tools != nil && len(tools.GetRefs()) > 0 {
		if !capabilities.GetTools() {
			return fmt.Errorf("%w: provider does not support tools", invocation.ErrInvalidInvocation)
		}
		return fmt.Errorf("%w: tool resolution", ErrAgentContractFeatureNotReady)
	}
	if skills := config.GetSkills(); skills != nil && len(skills.GetRefs()) > 0 {
		if !capabilities.GetSkills() {
			return fmt.Errorf("%w: provider does not support skills", invocation.ErrInvalidInvocation)
		}
		return fmt.Errorf("%w: skill resolution", ErrAgentContractFeatureNotReady)
	}
	if config.GetWorkspace() != nil {
		if !capabilities.GetWorkspaces() {
			return fmt.Errorf("%w: provider does not support workspaces", invocation.ErrInvalidInvocation)
		}
		return fmt.Errorf("%w: workspace materialization", ErrAgentContractFeatureNotReady)
	}
	return nil
}

func normalizeContractAgentResource(
	resource *proto.AgentResource,
	agentID string,
	providerName string,
	ownerSubjectID string,
	configRevision string,
) (*proto.AgentResource, error) {
	if resource == nil {
		return nil, core.ErrNotFound
	}
	resource = gproto.Clone(resource).(*proto.AgentResource)
	if strings.TrimSpace(resource.GetId()) != agentID {
		return nil, fmt.Errorf("agent provider returned unexpected session id %q", resource.GetId())
	}
	gotProvider := strings.TrimSpace(resource.GetProviderName())
	if gotProvider != "" && gotProvider != providerName {
		return nil, fmt.Errorf("agent provider returned unexpected provider name %q", gotProvider)
	}
	resource.ProviderName = providerName
	if strings.TrimSpace(resource.GetCreatedBySubjectId()) != ownerSubjectID {
		return nil, core.ErrNotFound
	}
	if strings.TrimSpace(resource.GetConfigRevision()) != configRevision {
		return nil, fmt.Errorf(
			"agent provider returned config revision %q, want %q",
			resource.GetConfigRevision(),
			configRevision,
		)
	}
	if resource.GetState() == proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED {
		return nil, fmt.Errorf("agent provider returned session without state")
	}
	return resource, nil
}

func normalizeContractRun(
	run *proto.AgentRunResource,
	route *agentroute.Route,
	runID string,
	expectedConfigRevision string,
) (*proto.AgentRunResource, error) {
	if run == nil {
		return nil, core.ErrNotFound
	}
	run = gproto.Clone(run).(*proto.AgentRunResource)
	if strings.TrimSpace(run.GetId()) != runID ||
		strings.TrimSpace(run.GetAgentId()) != route.AgentID {
		return nil, core.ErrNotFound
	}
	gotProvider := strings.TrimSpace(run.GetProviderName())
	if gotProvider != "" && gotProvider != route.ProviderName {
		return nil, fmt.Errorf("agent provider returned unexpected provider name %q", gotProvider)
	}
	run.ProviderName = route.ProviderName
	runConfigRevision := strings.TrimSpace(run.GetConfigRevision())
	if runConfigRevision == "" {
		return nil, fmt.Errorf("agent provider returned run without a config revision")
	}
	if expectedConfigRevision != "" && runConfigRevision != expectedConfigRevision {
		return nil, fmt.Errorf("agent provider returned run for an unexpected config revision")
	}
	if run.GetStatus() == proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_UNSPECIFIED {
		return nil, fmt.Errorf("agent provider returned run without status")
	}
	return run, nil
}

func normalizeContractInteraction(
	interaction *proto.AgentRunInteraction,
	agentID string,
	runID string,
	interactionID string,
) (*proto.AgentRunInteraction, error) {
	if interaction == nil {
		return nil, core.ErrNotFound
	}
	interaction = gproto.Clone(interaction).(*proto.AgentRunInteraction)
	if strings.TrimSpace(interaction.GetId()) != interactionID ||
		strings.TrimSpace(interaction.GetAgentId()) != agentID ||
		strings.TrimSpace(interaction.GetRunId()) != runID {
		return nil, core.ErrNotFound
	}
	if interaction.GetKind() == proto.AgentInteractionKind_AGENT_INTERACTION_KIND_UNSPECIFIED ||
		interaction.GetState() == proto.AgentInteractionState_AGENT_INTERACTION_STATE_UNSPECIFIED {
		return nil, fmt.Errorf("agent provider returned invalid interaction")
	}
	return interaction, nil
}

func contractFingerprint(message gproto.Message) (string, error) {
	encoded, err := (gproto.MarshalOptions{Deterministic: true}).Marshal(message)
	if err != nil {
		return "", fmt.Errorf("encode agent idempotency fingerprint: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

func contractResourceID(prefix, scope, idempotencyKey string) string {
	if strings.TrimSpace(idempotencyKey) == "" {
		return prefix + "_" + uuid.NewString()
	}
	return contractStableResourceID(prefix, scope, idempotencyKey)
}

func contractStableResourceID(prefix string, parts ...string) string {
	return prefix + "_" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, "\x00"))).String()
}

func contractRouteState(state proto.AgentSessionState) (agentroute.State, error) {
	switch state {
	case proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED:
		return "", nil
	case proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE:
		return agentroute.StateActive, nil
	case proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED:
		return agentroute.StateArchived, nil
	default:
		return "", fmt.Errorf("%w: invalid agent state", invocation.ErrInvalidInvocation)
	}
}

func contractPageSize(value int32) (int, error) {
	switch {
	case value < 0:
		return 0, fmt.Errorf("%w: page size must be non-negative", invocation.ErrInvalidInvocation)
	case value == 0:
		return contractListDefaultPageSize, nil
	case value > contractListMaxPageSize:
		return 0, fmt.Errorf(
			"%w: page size cannot exceed %d",
			invocation.ErrInvalidInvocation,
			contractListMaxPageSize,
		)
	default:
		return int(value), nil
	}
}

func encodeContractPageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeContractPageToken(token string) (int, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid offset")
	}
	return offset, nil
}

var _ ContractService = (*Manager)(nil)

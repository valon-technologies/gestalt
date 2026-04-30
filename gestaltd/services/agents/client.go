package agents

import (
	"context"
	"fmt"
	"io"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	Egress       egress.Policy
	HostBinary   string
	Cleanup      func()
	HostServices []runtimehost.HostService
	Name         string
	Telemetry    metricutil.TelemetryProviders
}

var startAgentProviderProcess = runtimehost.StartPluginProcess

type remoteAgent struct {
	client  proto.AgentProviderClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

type RemoteConfig struct {
	Client  proto.AgentProviderClient
	Runtime proto.ProviderLifecycleClient
	Closer  io.Closer
	Config  map[string]any
	Name    string
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (coreagent.Provider, error) {
	proc, err := startAgentProviderProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		HostServices: cfg.HostServices,
		ProviderName: cfg.Name,
		Telemetry:    cfg.Telemetry,
	})
	if err != nil {
		return nil, err
	}

	return NewRemote(ctx, RemoteConfig{
		Client:  proto.NewAgentProviderClient(proc.Conn()),
		Runtime: proc.Lifecycle(),
		Closer:  proc,
		Config:  cfg.Config,
		Name:    cfg.Name,
	})
}

func NewRemote(ctx context.Context, cfg RemoteConfig) (coreagent.Provider, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("agent provider client is required")
	}
	if cfg.Runtime == nil {
		return nil, fmt.Errorf("agent provider lifecycle client is required")
	}
	if _, err := runtimehost.ConfigureRuntimeProvider(ctx, cfg.Runtime, proto.ProviderKind_PROVIDER_KIND_AGENT, cfg.Name, cfg.Config); err != nil {
		if cfg.Closer != nil {
			_ = cfg.Closer.Close()
		}
		return nil, err
	}
	return &remoteAgent{client: cfg.Client, runtime: cfg.Runtime, closer: cfg.Closer}, nil
}

func (r *remoteAgent) CreateSession(ctx context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	metadata, err := structFromMap(req.Metadata)
	if err != nil {
		return nil, err
	}
	providerOptions, err := structFromMap(req.ProviderOptions)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		SessionId:       req.SessionID,
		IdempotencyKey:  req.IdempotencyKey,
		Model:           req.Model,
		ClientRef:       req.ClientRef,
		Metadata:        metadata,
		ProviderOptions: providerOptions,
		CreatedBy:       agentActorToProto(req.CreatedBy),
		Subject:         agentSubjectContextToProto(req.Subject),
	})
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (r *remoteAgent) GetSession(ctx context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetSession(ctx, &proto.GetAgentProviderSessionRequest{
		SessionId: req.SessionID,
		Subject:   agentSubjectContextToProto(req.Subject),
	})
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (r *remoteAgent) ListSessions(ctx context.Context, req coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListSessions(ctx, &proto.ListAgentProviderSessionsRequest{
		Subject:     agentSubjectContextToProto(req.Subject),
		SessionIds:  append([]string(nil), req.SessionIDs...),
		State:       agentSessionStateToProto(req.State),
		Limit:       int32(req.Limit),
		SummaryOnly: req.SummaryOnly,
	})
	if err != nil {
		return nil, err
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

func (r *remoteAgent) UpdateSession(ctx context.Context, req coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	metadata, err := structFromMap(req.Metadata)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.UpdateSession(ctx, &proto.UpdateAgentProviderSessionRequest{
		SessionId: req.SessionID,
		ClientRef: req.ClientRef,
		State:     agentSessionStateToProto(req.State),
		Metadata:  metadata,
		Subject:   agentSubjectContextToProto(req.Subject),
	})
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (r *remoteAgent) CreateTurn(ctx context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	messages, err := agentMessagesToProto(req.Messages)
	if err != nil {
		return nil, err
	}
	tools, err := agentToolsToProto(req.Tools)
	if err != nil {
		return nil, err
	}
	responseSchema, err := structFromMap(req.ResponseSchema)
	if err != nil {
		return nil, err
	}
	metadata, err := structFromMap(req.Metadata)
	if err != nil {
		return nil, err
	}
	providerOptions, err := structFromMap(req.ProviderOptions)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.CreateTurn(ctx, &proto.CreateAgentProviderTurnRequest{
		TurnId:          req.TurnID,
		SessionId:       req.SessionID,
		IdempotencyKey:  req.IdempotencyKey,
		Model:           req.Model,
		Messages:        messages,
		Tools:           tools,
		ResponseSchema:  responseSchema,
		Metadata:        metadata,
		ProviderOptions: providerOptions,
		CreatedBy:       agentActorToProto(req.CreatedBy),
		ExecutionRef:    req.ExecutionRef,
		ToolRefs:        agentToolRefsToProto(req.ToolRefs),
		ToolSource:      agentToolSourceModeToProto(req.ToolSource),
		Subject:         agentSubjectContextToProto(req.Subject),
		ToolGrant:       req.ToolGrant,
	})
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (r *remoteAgent) GetTurn(ctx context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetTurn(ctx, &proto.GetAgentProviderTurnRequest{
		TurnId:  req.TurnID,
		Subject: agentSubjectContextToProto(req.Subject),
	})
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (r *remoteAgent) ListTurns(ctx context.Context, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListTurns(ctx, &proto.ListAgentProviderTurnsRequest{
		SessionId:   req.SessionID,
		Subject:     agentSubjectContextToProto(req.Subject),
		TurnIds:     append([]string(nil), req.TurnIDs...),
		Status:      agentExecutionStatusToProto(req.Status),
		Limit:       int32(req.Limit),
		SummaryOnly: req.SummaryOnly,
	})
	if err != nil {
		return nil, err
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

func (r *remoteAgent) CancelTurn(ctx context.Context, req coreagent.CancelTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.CancelTurn(ctx, &proto.CancelAgentProviderTurnRequest{
		TurnId:  req.TurnID,
		Reason:  req.Reason,
		Subject: agentSubjectContextToProto(req.Subject),
	})
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (r *remoteAgent) ListTurnEvents(ctx context.Context, req coreagent.ListTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListTurnEvents(ctx, &proto.ListAgentProviderTurnEventsRequest{
		TurnId:   req.TurnID,
		AfterSeq: req.AfterSeq,
		Limit:    int32(req.Limit),
		Subject:  agentSubjectContextToProto(req.Subject),
	})
	if err != nil {
		return nil, err
	}
	return agentTurnEventsFromProto(resp.GetEvents()), nil
}

func (r *remoteAgent) GetInteraction(ctx context.Context, req coreagent.GetInteractionRequest) (*coreagent.Interaction, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetInteraction(ctx, &proto.GetAgentProviderInteractionRequest{
		InteractionId: req.InteractionID,
		Subject:       agentSubjectContextToProto(req.Subject),
	})
	if err != nil {
		return nil, err
	}
	return agentInteractionFromProto(resp)
}

func (r *remoteAgent) ListInteractions(ctx context.Context, req coreagent.ListInteractionsRequest) ([]*coreagent.Interaction, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListInteractions(ctx, &proto.ListAgentProviderInteractionsRequest{
		TurnId:  req.TurnID,
		Subject: agentSubjectContextToProto(req.Subject),
	})
	if err != nil {
		return nil, err
	}
	return agentInteractionsFromProto(resp.GetInteractions())
}

func (r *remoteAgent) ResolveInteraction(ctx context.Context, req coreagent.ResolveInteractionRequest) (*coreagent.Interaction, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resolution, err := structFromMap(req.Resolution)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.ResolveInteraction(ctx, &proto.ResolveAgentProviderInteractionRequest{
		InteractionId: req.InteractionID,
		Resolution:    resolution,
		Subject:       agentSubjectContextToProto(req.Subject),
	})
	if err != nil {
		return nil, err
	}
	return agentInteractionFromProto(resp)
}

func (r *remoteAgent) GetCapabilities(ctx context.Context, req coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetCapabilities(ctx, &proto.GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return &coreagent.ProviderCapabilities{}, nil
		}
		return nil, err
	}
	return agentProviderCapabilitiesFromProto(resp), nil
}

func (r *remoteAgent) Ping(ctx context.Context) error {
	if err := runtimehost.CheckRuntimeProviderHealth(ctx, r.runtime); err != nil {
		return err
	}
	capabilitiesCtx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetCapabilities(capabilitiesCtx, &proto.GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		return fmt.Errorf("agent provider capabilities check failed: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("agent provider capabilities check returned nil response")
	}
	return nil
}

func (r *remoteAgent) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

var _ coreagent.Provider = (*remoteAgent)(nil)

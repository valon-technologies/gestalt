package agents

import (
	"context"
	"fmt"
	"io"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

type ExecConfig struct {
	Command      string
	Args         []string
	Workdir      string
	Env          map[string]string
	Config       map[string]any
	Egress       egress.Policy
	HostBinary   string
	Cleanup      func()
	HostServices []runtimehost.HostService
	Name         string
	Telemetry    metricutil.TelemetryProviders
}

var startAgentProviderProcess = runtimehost.StartAppProcess

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
		Workdir:      cfg.Workdir,
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

func (r *remoteAgent) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderSessionCreateContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.CreateAgentProviderSessionRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.CreateSession(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (r *remoteAgent) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.GetAgentProviderSessionRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.GetSession(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (r *remoteAgent) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.ListAgentProviderSessionsRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.ListSessions(ctx, providerReq)
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

func (r *remoteAgent) UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.UpdateAgentProviderSessionRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.UpdateSession(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp)
}

func (r *remoteAgent) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.CreateAgentProviderTurnRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.CreateTurn(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (r *remoteAgent) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.GetAgentProviderTurnRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.GetTurn(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (r *remoteAgent) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.ListAgentProviderTurnsRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.ListTurns(ctx, providerReq)
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

func (r *remoteAgent) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.CancelAgentProviderTurnRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.CancelTurn(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp)
}

func (r *remoteAgent) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.ListAgentProviderTurnEventsRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.ListTurnEvents(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentTurnEventsFromProto(resp.GetEvents()), nil
}

func (r *remoteAgent) GetInteraction(ctx context.Context, req *proto.GetAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.GetAgentProviderInteractionRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.GetInteraction(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentInteractionFromProto(resp)
}

func (r *remoteAgent) ListInteractions(ctx context.Context, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.ListAgentProviderInteractionsRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.ListInteractions(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentInteractionsFromProto(resp.GetInteractions())
}

func (r *remoteAgent) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	providerReq := cloneAgentRequest(req, &proto.ResolveAgentProviderInteractionRequest{})
	providerReq.InvocationToken = appaccess.InvocationTokenFromContext(ctx)
	resp, err := r.client.ResolveInteraction(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	return agentInteractionFromProto(resp)
}

func (r *remoteAgent) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetCapabilities(ctx, cloneAgentRequest(req, &proto.GetAgentProviderCapabilitiesRequest{}))
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return &coreagent.ProviderCapabilities{}, nil
		}
		return nil, err
	}
	return agentProviderCapabilitiesFromProto(resp), nil
}

func cloneAgentRequest[T interface {
	gproto.Message
	comparable
}](req T, empty T) T {
	var zero T
	if req == zero {
		return empty
	}
	return gproto.Clone(req).(T)
}

func (r *remoteAgent) Ping(ctx context.Context) error {
	if err := runtimehost.CheckRuntimeProviderHealth(ctx, r.runtime); err != nil {
		return err
	}
	capabilitiesCtx, cancel := runtimehost.ProviderCallContext(ctx)
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

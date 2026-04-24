package providerhost

import (
	"context"
	"fmt"
	"io"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type AgentExecConfig struct {
	Command       string
	Args          []string
	Env           map[string]string
	Config        map[string]any
	AllowedHosts  []string
	DefaultAction egress.PolicyAction
	HostBinary    string
	Cleanup       func()
	HostServices  []HostService
	Name          string
}

var startAgentProviderProcess = startProviderProcess

type remoteAgent struct {
	client  proto.AgentProviderClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

type RemoteAgentConfig struct {
	Client  proto.AgentProviderClient
	Runtime proto.ProviderLifecycleClient
	Closer  io.Closer
	Config  map[string]any
	Name    string
}

func NewExecutableAgent(ctx context.Context, cfg AgentExecConfig) (coreagent.Provider, error) {
	execCfg := ExecConfig{
		Command:       cfg.Command,
		Args:          cfg.Args,
		Env:           cfg.Env,
		Config:        cfg.Config,
		AllowedHosts:  cfg.AllowedHosts,
		DefaultAction: cfg.DefaultAction,
		HostBinary:    cfg.HostBinary,
		Cleanup:       cfg.Cleanup,
		HostServices:  cfg.HostServices,
		ProviderName:  cfg.Name,
	}
	proc, err := startAgentProviderProcess(ctx, execCfg.processConfig())
	if err != nil {
		return nil, err
	}

	return NewRemoteAgent(ctx, RemoteAgentConfig{
		Client:  proto.NewAgentProviderClient(proc.conn),
		Runtime: proto.NewProviderLifecycleClient(proc.conn),
		Closer:  proc,
		Config:  cfg.Config,
		Name:    cfg.Name,
	})
}

func NewRemoteAgent(ctx context.Context, cfg RemoteAgentConfig) (coreagent.Provider, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("agent provider client is required")
	}
	if cfg.Runtime == nil {
		return nil, fmt.Errorf("agent provider lifecycle client is required")
	}
	if _, err := ConfigureRuntimeProvider(ctx, cfg.Runtime, proto.ProviderKind_PROVIDER_KIND_AGENT, cfg.Name, cfg.Config); err != nil {
		if cfg.Closer != nil {
			_ = cfg.Closer.Close()
		}
		return nil, err
	}
	return &remoteAgent{client: cfg.Client, runtime: cfg.Runtime, closer: cfg.Closer}, nil
}

func (r *remoteAgent) StartRun(ctx context.Context, req coreagent.StartRunRequest) (*coreagent.Run, error) {
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
	resp, err := r.client.StartRun(ctx, &proto.StartAgentProviderRunRequest{
		RunId:           req.RunID,
		IdempotencyKey:  req.IdempotencyKey,
		ProviderName:    req.ProviderName,
		Model:           req.Model,
		Messages:        messages,
		Tools:           tools,
		ResponseSchema:  responseSchema,
		SessionRef:      req.SessionRef,
		Metadata:        metadata,
		ProviderOptions: providerOptions,
		CreatedBy:       agentActorToProto(req.CreatedBy),
		ExecutionRef:    req.ExecutionRef,
	})
	if err != nil {
		return nil, err
	}
	return agentRunFromProto(resp)
}

func (r *remoteAgent) GetRun(ctx context.Context, req coreagent.GetRunRequest) (*coreagent.Run, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.GetRun(ctx, &proto.GetAgentProviderRunRequest{RunId: req.RunID})
	if err != nil {
		return nil, err
	}
	return agentRunFromProto(resp)
}

func (r *remoteAgent) ListRuns(ctx context.Context, req coreagent.ListRunsRequest) ([]*coreagent.Run, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListRuns(ctx, &proto.ListAgentProviderRunsRequest{})
	if err != nil {
		return nil, err
	}
	runs := make([]*coreagent.Run, 0, len(resp.GetRuns()))
	for _, run := range resp.GetRuns() {
		value, err := agentRunFromProto(run)
		if err != nil {
			return nil, err
		}
		runs = append(runs, value)
	}
	return runs, nil
}

func (r *remoteAgent) CancelRun(ctx context.Context, req coreagent.CancelRunRequest) (*coreagent.Run, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.CancelRun(ctx, &proto.CancelAgentProviderRunRequest{
		RunId:  req.RunID,
		Reason: req.Reason,
	})
	if err != nil {
		return nil, err
	}
	return agentRunFromProto(resp)
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

func (r *remoteAgent) ResumeRun(ctx context.Context, req coreagent.ResumeRunRequest) (*coreagent.Run, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resolution, err := structFromMap(req.Resolution)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.ResumeRun(ctx, &proto.ResumeAgentProviderRunRequest{
		RunId:         req.RunID,
		InteractionId: req.InteractionID,
		Resolution:    resolution,
	})
	if err != nil {
		return nil, err
	}
	return agentRunFromProto(resp)
}

func (r *remoteAgent) Ping(ctx context.Context) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteAgent) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

var _ coreagent.Provider = (*remoteAgent)(nil)

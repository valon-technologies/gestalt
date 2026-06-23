package runtimeprovider

import (
	"context"
	"fmt"
	"sync"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecutableConfig struct {
	Name         string
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	Egress       egress.Policy
	HostBinary   string
	HostServices []runtimehost.HostService
	Telemetry    metricutil.TelemetryProviders
}

type executableProvider struct {
	proc      *runtimehost.AppProcess
	runtime   proto.RuntimeClient
	lifecycle proto.ProviderLifecycleClient

	name      string
	telemetry metricutil.TelemetryProviders
	mu        sync.Mutex
	sessions  map[string]*proto.RuntimeSession
}

func NewExecutableProvider(ctx context.Context, cfg ExecutableConfig) (Provider, error) {
	proc, err := runtimehost.StartAppProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Egress:       cloneRuntimeEgressPolicy(cfg.Egress),
		HostBinary:   cfg.HostBinary,
		HostServices: cfg.HostServices,
		ProviderName: cfg.Name,
		Telemetry:    cfg.Telemetry,
	})
	if err != nil {
		return nil, err
	}

	lifecycle := proto.NewProviderLifecycleClient(proc.Conn())
	if _, err := runtimehost.ConfigureRuntimeProvider(ctx, lifecycle, proto.ProviderKind_PROVIDER_KIND_RUNTIME, cfg.Name, cfg.Config); err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &executableProvider{
		proc:      proc,
		runtime:   proto.NewRuntimeClient(proc.Conn()),
		lifecycle: lifecycle,
		name:      cfg.Name,
		telemetry: cfg.Telemetry,
		sessions:  make(map[string]*proto.RuntimeSession),
	}, nil
}

func cloneRuntimeEgressPolicy(policy egress.Policy) egress.Policy {
	return egress.Policy{
		AllowedHosts:  append([]string(nil), policy.AllowedHosts...),
		DefaultAction: policy.DefaultAction,
	}
}

func (p *executableProvider) Support(ctx context.Context) (*proto.RuntimeSupport, error) {
	resp, err := p.runtime.GetSupport(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("get runtime support: %w", err)
	}
	return resp, nil
}

func (p *executableProvider) StartSession(ctx context.Context, req *proto.StartRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	resp, err := p.runtime.StartSession(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("start runtime session: %w", err)
	}
	p.trackSession(resp)
	return resp, nil
}

func (p *executableProvider) ListSessions(ctx context.Context, req *proto.ListRuntimeSessionsRequest) (*proto.ListRuntimeSessionsResponse, error) {
	if p == nil {
		return nil, fmt.Errorf("runtime provider is not configured")
	}
	resp, err := p.runtime.ListSessions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list runtime sessions: %w", err)
	}
	for _, protoSession := range resp.GetSessions() {
		if protoSession == nil || protoSession.GetId() == "" {
			continue
		}
		p.trackSession(protoSession)
	}
	return resp, nil
}

func (p *executableProvider) GetSession(ctx context.Context, req *proto.GetRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	resp, err := p.runtime.GetSession(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get runtime session: %w", err)
	}
	p.trackSession(resp)
	return resp, nil
}

func (p *executableProvider) StopSession(ctx context.Context, req *proto.StopRuntimeSessionRequest) error {
	_, err := p.runtime.StopSession(ctx, req)
	p.mu.Lock()
	delete(p.sessions, req.GetSessionId())
	p.mu.Unlock()
	if err != nil {
		return fmt.Errorf("stop runtime session: %w", err)
	}
	return nil
}

func (p *executableProvider) PrepareWorkspace(ctx context.Context, req *proto.PrepareRuntimeWorkspaceRequest) (*proto.PrepareRuntimeWorkspaceResponse, error) {
	resp, err := p.runtime.PrepareWorkspace(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("prepare runtime workspace: %w", err)
	}
	return resp, nil
}

func (p *executableProvider) RemoveWorkspace(ctx context.Context, req *proto.RemoveRuntimeWorkspaceRequest) error {
	_, err := p.runtime.RemoveWorkspace(ctx, req)
	if err != nil {
		return fmt.Errorf("remove runtime workspace: %w", err)
	}
	return nil
}

func (p *executableProvider) StartApp(ctx context.Context, req *proto.StartHostedAppRequest) (*proto.HostedApp, error) {
	resp, err := p.runtime.StartApp(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("start hosted app: %w", err)
	}
	p.mu.Lock()
	if session, ok := p.sessions[req.GetSessionId()]; ok && session != nil {
		session.State = SessionStateRunning
	}
	p.mu.Unlock()
	return resp, nil
}

func (p *executableProvider) Close() error {
	if p == nil || p.proc == nil {
		return nil
	}
	p.mu.Lock()
	p.sessions = nil
	p.mu.Unlock()
	return p.proc.Close()
}

func (p *executableProvider) trackSession(session *proto.RuntimeSession) {
	if p == nil || session == nil || session.GetId() == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions == nil {
		p.sessions = make(map[string]*proto.RuntimeSession)
	}
	p.sessions[session.GetId()] = gproto.Clone(session).(*proto.RuntimeSession)
}

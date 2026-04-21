package pluginruntime

import (
	"context"
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecutableConfig struct {
	Name         string
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	AllowedHosts []string
	HostBinary   string
}

type executableProvider struct {
	proc      *providerhost.PluginProcess
	runtime   proto.PluginRuntimeProviderClient
	lifecycle proto.ProviderLifecycleClient
}

func NewExecutableProvider(ctx context.Context, cfg ExecutableConfig) (Provider, error) {
	proc, err := providerhost.StartPluginProcess(ctx, providerhost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
	})
	if err != nil {
		return nil, err
	}

	lifecycle := proto.NewProviderLifecycleClient(proc.Conn())
	if _, err := providerhost.ConfigureRuntimeProvider(ctx, lifecycle, proto.ProviderKind_PROVIDER_KIND_RUNTIME, cfg.Name, cfg.Config); err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &executableProvider{
		proc:      proc,
		runtime:   proto.NewPluginRuntimeProviderClient(proc.Conn()),
		lifecycle: lifecycle,
	}, nil
}

func (p *executableProvider) Capabilities(ctx context.Context) (Capabilities, error) {
	resp, err := p.runtime.GetCapabilities(ctx, &emptypb.Empty{})
	if err != nil {
		return Capabilities{}, fmt.Errorf("get runtime capabilities: %w", err)
	}
	return capabilitiesFromProto(resp.GetCapabilities()), nil
}

func (p *executableProvider) StartSession(ctx context.Context, req StartSessionRequest) (*Session, error) {
	resp, err := p.runtime.StartSession(ctx, &proto.StartPluginRuntimeSessionRequest{
		PluginName: req.PluginName,
		Template:   req.Template,
		Image:      req.Image,
		Metadata:   cloneStringMap(req.Metadata),
	})
	if err != nil {
		return nil, fmt.Errorf("start runtime session: %w", err)
	}
	return sessionFromProto(resp), nil
}

func (p *executableProvider) GetSession(ctx context.Context, req GetSessionRequest) (*Session, error) {
	resp, err := p.runtime.GetSession(ctx, &proto.GetPluginRuntimeSessionRequest{
		SessionId: req.SessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("get runtime session: %w", err)
	}
	return sessionFromProto(resp), nil
}

func (p *executableProvider) StopSession(ctx context.Context, req StopSessionRequest) error {
	_, err := p.runtime.StopSession(ctx, &proto.StopPluginRuntimeSessionRequest{
		SessionId: req.SessionID,
	})
	if err != nil {
		return fmt.Errorf("stop runtime session: %w", err)
	}
	return nil
}

func (p *executableProvider) BindHostService(ctx context.Context, req BindHostServiceRequest) (*HostServiceBinding, error) {
	resp, err := p.runtime.BindHostService(ctx, &proto.BindPluginRuntimeHostServiceRequest{
		SessionId:      req.SessionID,
		EnvVar:         req.EnvVar,
		HostSocketPath: req.HostSocketPath,
	})
	if err != nil {
		return nil, fmt.Errorf("bind runtime host service: %w", err)
	}
	return &HostServiceBinding{
		ID:         resp.GetId(),
		SessionID:  resp.GetSessionId(),
		EnvVar:     resp.GetEnvVar(),
		SocketPath: resp.GetSocketPath(),
	}, nil
}

func (p *executableProvider) StartPlugin(ctx context.Context, req StartPluginRequest) (*HostedPlugin, error) {
	resp, err := p.runtime.StartPlugin(ctx, &proto.StartHostedPluginRequest{
		SessionId:     req.SessionID,
		PluginName:    req.PluginName,
		Command:       req.Command,
		Args:          append([]string(nil), req.Args...),
		Env:           cloneStringMap(req.Env),
		BundleDir:     req.BundleDir,
		AllowedHosts:  append([]string(nil), req.AllowedHosts...),
		DefaultAction: string(req.DefaultAction),
		HostBinary:    req.HostBinary,
	})
	if err != nil {
		return nil, fmt.Errorf("start hosted plugin: %w", err)
	}
	return &HostedPlugin{
		ID:         resp.GetId(),
		SessionID:  resp.GetSessionId(),
		PluginName: resp.GetPluginName(),
		DialTarget: resp.GetDialTarget(),
	}, nil
}

func (p *executableProvider) Close() error {
	if p == nil || p.proc == nil {
		return nil
	}
	return p.proc.Close()
}

func capabilitiesFromProto(src *proto.PluginRuntimeCapabilities) Capabilities {
	if src == nil {
		return Capabilities{}
	}
	return Capabilities{
		HostedPluginRuntime: src.GetHostedPluginRuntime(),
		HostServiceTunnels:  src.GetHostServiceTunnels(),
		ProviderGRPCTunnel:  src.GetProviderGrpcTunnel(),
		HostnameProxyEgress: src.GetHostnameProxyEgress(),
		CIDREgress:          src.GetCidrEgress(),
		HostPathExecution:   src.GetHostPathExecution(),
		ExecutionGOOS:       src.GetExecutionGoos(),
		ExecutionGOARCH:     src.GetExecutionGoarch(),
	}
}

func sessionFromProto(src *proto.PluginRuntimeSession) *Session {
	if src == nil {
		return nil
	}
	return &Session{
		ID:       src.GetId(),
		State:    SessionState(src.GetState()),
		Metadata: cloneStringMap(src.GetMetadata()),
	}
}

package pluginruntime

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	mu       sync.Mutex
	sessions map[string]*Session
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
		sessions:  make(map[string]*Session),
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
	session := sessionFromProto(resp)
	p.trackSession(session)
	return session, nil
}

func (p *executableProvider) ListSessions(ctx context.Context) ([]Session, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin runtime is not configured")
	}

	p.mu.Lock()
	sessionIDs := make([]string, 0, len(p.sessions))
	for sessionID := range p.sessions {
		sessionIDs = append(sessionIDs, sessionID)
	}
	p.mu.Unlock()
	slices.Sort(sessionIDs)

	out := make([]Session, 0, len(sessionIDs))
	refreshed := make(map[string]*Session, len(sessionIDs))
	stale := make([]string, 0)
	for _, sessionID := range sessionIDs {
		resp, err := p.runtime.GetSession(ctx, &proto.GetPluginRuntimeSessionRequest{
			SessionId: sessionID,
		})
		if err != nil {
			if status.Code(err) == codes.NotFound {
				stale = append(stale, sessionID)
				continue
			}
			return nil, fmt.Errorf("list runtime sessions: get session %q: %w", sessionID, err)
		}
		session := sessionFromProto(resp)
		if session == nil {
			stale = append(stale, sessionID)
			continue
		}
		refreshed[sessionID] = cloneHostedSession(session)
		out = append(out, *session)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sessionID := range stale {
		delete(p.sessions, sessionID)
	}
	for sessionID, session := range refreshed {
		p.sessions[sessionID] = session
	}
	return out, nil
}

func (p *executableProvider) GetSession(ctx context.Context, req GetSessionRequest) (*Session, error) {
	resp, err := p.runtime.GetSession(ctx, &proto.GetPluginRuntimeSessionRequest{
		SessionId: req.SessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("get runtime session: %w", err)
	}
	session := sessionFromProto(resp)
	p.trackSession(session)
	return session, nil
}

func (p *executableProvider) StopSession(ctx context.Context, req StopSessionRequest) error {
	_, err := p.runtime.StopSession(ctx, &proto.StopPluginRuntimeSessionRequest{
		SessionId: req.SessionID,
	})
	if err != nil {
		return fmt.Errorf("stop runtime session: %w", err)
	}
	p.mu.Lock()
	delete(p.sessions, req.SessionID)
	p.mu.Unlock()
	return nil
}

func (p *executableProvider) BindHostService(ctx context.Context, req BindHostServiceRequest) (*HostServiceBinding, error) {
	resp, err := p.runtime.BindHostService(ctx, &proto.BindPluginRuntimeHostServiceRequest{
		SessionId:      req.SessionID,
		EnvVar:         req.EnvVar,
		HostSocketPath: req.HostSocketPath,
		Relay:          hostServiceRelayToProto(req.Relay),
	})
	if err != nil {
		return nil, fmt.Errorf("bind runtime host service: %w", err)
	}
	relay := hostServiceRelayFromProto(resp.GetRelay())
	socketPath := ""
	if strings.HasPrefix(relay.DialTarget, "unix://") {
		socketPath = strings.TrimPrefix(relay.DialTarget, "unix://")
	}
	return &HostServiceBinding{
		ID:         resp.GetId(),
		SessionID:  resp.GetSessionId(),
		EnvVar:     resp.GetEnvVar(),
		SocketPath: socketPath,
		Relay:      relay,
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
	p.mu.Lock()
	if session, ok := p.sessions[req.SessionID]; ok && session != nil {
		session.State = SessionStateRunning
	}
	p.mu.Unlock()
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
	p.mu.Lock()
	p.sessions = nil
	p.mu.Unlock()
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

func (p *executableProvider) trackSession(session *Session) {
	if p == nil || session == nil || session.ID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions == nil {
		p.sessions = make(map[string]*Session)
	}
	p.sessions[session.ID] = cloneHostedSession(session)
}

func cloneHostedSession(session *Session) *Session {
	if session == nil {
		return nil
	}
	return &Session{
		ID:       session.ID,
		State:    session.State,
		Metadata: cloneStringMap(session.Metadata),
	}
}

func hostServiceRelayToProto(src HostServiceRelay) *proto.PluginRuntimeHostServiceRelay {
	if src.DialTarget == "" {
		return nil
	}
	return &proto.PluginRuntimeHostServiceRelay{
		DialTarget: src.DialTarget,
	}
}

func hostServiceRelayFromProto(src *proto.PluginRuntimeHostServiceRelay) HostServiceRelay {
	if src == nil {
		return HostServiceRelay{}
	}
	return HostServiceRelay{
		DialTarget: src.GetDialTarget(),
	}
}

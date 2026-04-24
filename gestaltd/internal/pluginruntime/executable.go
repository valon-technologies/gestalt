package pluginruntime

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	startPluginDiagnosticsTailEntries = 40
	startPluginDiagnosticsTimeout     = 2 * time.Second
)

type ExecutableConfig struct {
	Name         string
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	AllowedHosts []string
	HostBinary   string
	Telemetry    metricutil.TelemetryProviders
}

type executableProvider struct {
	proc      *providerhost.PluginProcess
	runtime   proto.PluginRuntimeProviderClient
	lifecycle proto.ProviderLifecycleClient

	telemetry metricutil.TelemetryProviders
	mu        sync.Mutex
	sessions  map[string]*Session
}

func NewExecutableProvider(ctx context.Context, cfg ExecutableConfig) (Provider, error) {
	proc, err := providerhost.StartPluginProcess(ctx, providerhost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		ProviderName: cfg.Name,
		Telemetry:    cfg.Telemetry,
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
		telemetry: cfg.Telemetry,
		sessions:  make(map[string]*Session),
	}, nil
}

func (p *executableProvider) Support(ctx context.Context) (Support, error) {
	resp, err := p.runtime.GetSupport(ctx, &emptypb.Empty{})
	if err != nil {
		return Support{}, fmt.Errorf("get runtime support: %w", err)
	}
	return supportFromProto(resp), nil
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

func (p *executableProvider) GetSessionDiagnostics(ctx context.Context, req GetSessionDiagnosticsRequest) (*SessionDiagnostics, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin runtime is not configured")
	}
	tailEntries := normalizeSessionDiagnosticsTailEntries(req.TailEntries)

	resp, err := p.runtime.GetSessionDiagnostics(ctx, &proto.GetPluginRuntimeSessionDiagnosticsRequest{
		SessionId:   req.SessionID,
		TailEntries: int32(tailEntries),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, fmt.Errorf("%w: runtime session %q is not available", ErrSessionUnavailable, req.SessionID)
		}
		if status.Code(err) == codes.Unimplemented {
			session, sessionErr := p.GetSession(ctx, GetSessionRequest{SessionID: req.SessionID})
			if sessionErr != nil {
				if status.Code(sessionErr) == codes.NotFound {
					return nil, fmt.Errorf("%w: runtime session %q is not available", ErrSessionUnavailable, req.SessionID)
				}
				return nil, fmt.Errorf("%w: %v", ErrDiagnosticsUnavailable, sessionErr)
			}
			return &SessionDiagnostics{Session: *session}, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrDiagnosticsUnavailable, err)
	}

	session := sessionFromProto(resp.GetSession())
	if session == nil {
		return nil, fmt.Errorf("%w: runtime session %q is not available", ErrSessionUnavailable, req.SessionID)
	}
	p.trackSession(session)
	return diagnosticsFromProto(resp), nil
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
		SessionId: req.SessionID,
		EnvVar:    req.EnvVar,
		Relay:     hostServiceRelayToProto(req.Relay),
	})
	if err != nil {
		return nil, fmt.Errorf("bind runtime host service: %w", err)
	}
	relay := hostServiceRelayFromProto(resp.GetRelay())
	return &HostServiceBinding{
		ID:        resp.GetId(),
		SessionID: resp.GetSessionId(),
		EnvVar:    resp.GetEnvVar(),
		Relay:     relay,
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
		return nil, fmt.Errorf("start hosted plugin: %w", p.enrichStartPluginError(req.SessionID, err))
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

func supportFromProto(src *proto.PluginRuntimeSupport) Support {
	if src == nil {
		return Support{}
	}
	return Support{
		CanHostPlugins:    src.GetCanHostPlugins(),
		HostServiceAccess: hostServiceAccessFromProto(src.GetHostServiceAccess()),
		EgressMode:        egressModeFromProto(src.GetEgressMode()),
		LaunchMode:        launchModeFromProto(src.GetLaunchMode()),
		ExecutionTarget:   executionTargetFromProto(src.GetExecutionTarget()),
	}
}

func (p *executableProvider) enrichStartPluginError(sessionID string, err error) error {
	if p == nil || strings.TrimSpace(sessionID) == "" || err == nil {
		return err
	}
	diagCtx, cancel := context.WithTimeout(context.Background(), startPluginDiagnosticsTimeout)
	defer cancel()
	diagnostics, diagErr := p.GetSessionDiagnostics(diagCtx, GetSessionDiagnosticsRequest{
		SessionID:   sessionID,
		TailEntries: startPluginDiagnosticsTailEntries,
	})
	if diagErr != nil || diagnostics == nil || len(diagnostics.Logs) == 0 {
		return err
	}
	return enrichErrorWithDiagnostics(err, diagnostics)
}

func enrichErrorWithDiagnostics(err error, diagnostics *SessionDiagnostics) error {
	if err == nil || diagnostics == nil {
		return err
	}
	lines := make([]string, 0, len(diagnostics.Logs)+1)
	for _, entry := range diagnostics.Logs {
		stream := strings.TrimSpace(string(entry.Stream))
		if stream == "" {
			stream = "runtime"
		}
		message := strings.TrimSpace(entry.Message)
		if message == "" {
			continue
		}
		lines = append(lines, "["+stream+"] "+message)
	}
	if diagnostics.Truncated {
		lines = append(lines, "[truncated]")
	}
	if len(lines) == 0 {
		return err
	}
	return fmt.Errorf("%w; recent runtime logs:\n%s", err, strings.Join(lines, "\n"))
}

func hostServiceAccessFromProto(src proto.PluginRuntimeHostServiceAccess) HostServiceAccess {
	switch src {
	case proto.PluginRuntimeHostServiceAccess_PLUGIN_RUNTIME_HOST_SERVICE_ACCESS_DIRECT:
		return HostServiceAccessDirect
	default:
		return HostServiceAccessNone
	}
}

func egressModeFromProto(src proto.PluginRuntimeEgressMode) EgressMode {
	switch src {
	case proto.PluginRuntimeEgressMode_PLUGIN_RUNTIME_EGRESS_MODE_HOSTNAME:
		return EgressModeHostname
	case proto.PluginRuntimeEgressMode_PLUGIN_RUNTIME_EGRESS_MODE_CIDR:
		return EgressModeCIDR
	default:
		return EgressModeNone
	}
}

func launchModeFromProto(src proto.PluginRuntimeLaunchMode) LaunchMode {
	switch src {
	case proto.PluginRuntimeLaunchMode_PLUGIN_RUNTIME_LAUNCH_MODE_HOST_PATH:
		return LaunchModeHostPath
	default:
		return LaunchModeBundle
	}
}

func executionTargetFromProto(src *proto.PluginRuntimeExecutionTarget) ExecutionTarget {
	if src == nil {
		return ExecutionTarget{}
	}
	return ExecutionTarget{
		GOOS:   strings.TrimSpace(src.GetGoos()),
		GOARCH: strings.TrimSpace(src.GetGoarch()),
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

func diagnosticsFromProto(src *proto.PluginRuntimeSessionDiagnostics) *SessionDiagnostics {
	if src == nil {
		return nil
	}
	session := sessionFromProto(src.GetSession())
	if session == nil {
		return nil
	}
	logs := make([]LogEntry, 0, len(src.GetLogs()))
	for _, entry := range src.GetLogs() {
		if entry == nil {
			continue
		}
		logs = append(logs, LogEntry{
			Stream:     logStreamFromProto(entry.GetStream()),
			Message:    entry.GetMessage(),
			ObservedAt: entry.GetObservedAt().AsTime(),
		})
	}
	return &SessionDiagnostics{
		Session:   *session,
		Logs:      logs,
		Truncated: src.GetTruncated(),
	}
}

func logStreamFromProto(src proto.PluginRuntimeLogStream) LogStream {
	switch src {
	case proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_STDOUT:
		return LogStreamStdout
	case proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_STDERR:
		return LogStreamStderr
	default:
		return LogStreamRuntime
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

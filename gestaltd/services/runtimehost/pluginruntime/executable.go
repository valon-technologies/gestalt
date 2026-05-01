package pluginruntime

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	SessionLogs  runtimelogs.Store
}

type executableProvider struct {
	proc      *runtimehost.PluginProcess
	runtime   proto.PluginRuntimeProviderClient
	lifecycle proto.ProviderLifecycleClient

	name        string
	telemetry   metricutil.TelemetryProviders
	sessionLogs runtimelogs.Store
	mu          sync.Mutex
	sessions    map[string]*Session
}

func NewExecutableProvider(ctx context.Context, cfg ExecutableConfig) (Provider, error) {
	proc, err := runtimehost.StartPluginProcess(ctx, runtimehost.ProcessConfig{
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
		proc:        proc,
		runtime:     proto.NewPluginRuntimeProviderClient(proc.Conn()),
		lifecycle:   lifecycle,
		name:        cfg.Name,
		telemetry:   cfg.Telemetry,
		sessionLogs: cfg.SessionLogs,
		sessions:    make(map[string]*Session),
	}, nil
}

func cloneRuntimeEgressPolicy(policy egress.Policy) egress.Policy {
	return egress.Policy{
		AllowedHosts:  append([]string(nil), policy.AllowedHosts...),
		DefaultAction: policy.DefaultAction,
	}
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
		PluginName:    req.PluginName,
		Template:      req.Template,
		Image:         req.Image,
		ImagePullAuth: imagePullAuthToProto(req.ImagePullAuth),
		Metadata:      cloneStringMap(req.Metadata),
	})
	if err != nil {
		return nil, fmt.Errorf("start runtime session: %w", err)
	}
	session := sessionFromProto(resp)
	p.trackSession(session)
	if p.sessionLogs != nil && session != nil {
		metadata := cloneStringMap(session.Metadata)
		if len(metadata) == 0 {
			metadata = cloneStringMap(req.Metadata)
		}
		if err := p.sessionLogs.RegisterSession(ctx, runtimelogs.SessionRegistration{
			RuntimeProviderName: p.name,
			SessionID:           session.ID,
			Metadata:            metadata,
		}); err != nil {
			slog.WarnContext(ctx, "failed to register runtime session logs", "runtime_provider", p.name, "session", session.ID, "error", err)
		}
	}
	return session, nil
}

func imagePullAuthToProto(auth *ImagePullAuth) *proto.PluginRuntimeImagePullAuth {
	if auth == nil {
		return nil
	}
	return &proto.PluginRuntimeImagePullAuth{
		DockerConfigJson: auth.DockerConfigJSON,
	}
}

func (p *executableProvider) ListSessions(ctx context.Context) ([]Session, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin runtime is not configured")
	}

	resp, err := p.runtime.ListSessions(ctx, &proto.ListPluginRuntimeSessionsRequest{})
	if err == nil {
		out := make([]Session, 0, len(resp.GetSessions()))
		refreshed := make(map[string]*Session, len(resp.GetSessions()))
		for _, protoSession := range resp.GetSessions() {
			session := sessionFromProto(protoSession)
			if session == nil || session.ID == "" {
				continue
			}
			refreshed[session.ID] = cloneHostedSession(session)
			out = append(out, *session)
		}
		p.mu.Lock()
		p.sessions = refreshed
		p.mu.Unlock()
		return out, nil
	}
	if status.Code(err) != codes.Unimplemented {
		return nil, fmt.Errorf("list runtime sessions: %w", err)
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
	if p.sessionLogs != nil {
		_ = p.sessionLogs.MarkSessionStopped(ctx, p.name, req.SessionID, time.Now().UTC())
	}
	return nil
}

func (p *executableProvider) StartPlugin(ctx context.Context, req StartPluginRequest) (*HostedPlugin, error) {
	resp, err := p.runtime.StartPlugin(ctx, &proto.StartHostedPluginRequest{
		SessionId:     req.SessionID,
		PluginName:    req.PluginName,
		Command:       req.Command,
		Args:          append([]string(nil), req.Args...),
		Env:           cloneStringMap(req.Env),
		AllowedHosts:  append([]string(nil), req.Egress.AllowedHosts...),
		DefaultAction: string(req.Egress.DefaultAction),
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

func (p *executableProvider) enrichStartPluginError(sessionID string, err error) error {
	if p == nil || p.sessionLogs == nil || sessionID == "" {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	logs, logErr := p.sessionLogs.TailSessionLogs(ctx, p.name, sessionID, 20)
	if logErr != nil || len(logs) == 0 {
		return err
	}
	var b strings.Builder
	for _, entry := range logs {
		if entry.Message == "" {
			continue
		}
		b.WriteString("[")
		b.WriteString(string(entry.Stream))
		b.WriteString("] ")
		b.WriteString(entry.Message)
		if !strings.HasSuffix(entry.Message, "\n") {
			b.WriteByte('\n')
		}
	}
	if b.Len() == 0 {
		return err
	}
	return fmt.Errorf("%w\nrecent runtime logs:\n%s", err, b.String())
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
	// host_service_access is a legacy runtime-advertised capability. gestaltd
	// now derives host-service access from its own public relay configuration.
	return Support{
		CanHostPlugins: src.GetCanHostPlugins(),
		EgressMode:     egressModeFromProto(src.GetEgressMode()),
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

func sessionFromProto(src *proto.PluginRuntimeSession) *Session {
	if src == nil {
		return nil
	}
	return &Session{
		ID:           src.GetId(),
		State:        SessionState(src.GetState()),
		Metadata:     cloneStringMap(src.GetMetadata()),
		Lifecycle:    sessionLifecycleFromProto(src.GetLifecycle()),
		StateReason:  strings.TrimSpace(src.GetStateReason()),
		StateMessage: strings.TrimSpace(src.GetStateMessage()),
	}
}

func sessionLifecycleFromProto(src *proto.PluginRuntimeSessionLifecycle) *SessionLifecycle {
	if src == nil {
		return nil
	}
	lifecycle := &SessionLifecycle{
		StartedAt:          timeFromProto(src.GetStartedAt()),
		RecommendedDrainAt: timeFromProto(src.GetRecommendedDrainAt()),
		ExpiresAt:          timeFromProto(src.GetExpiresAt()),
	}
	if lifecycle.StartedAt == nil && lifecycle.RecommendedDrainAt == nil && lifecycle.ExpiresAt == nil {
		return nil
	}
	return lifecycle
}

func timeFromProto(src *timestamppb.Timestamp) *time.Time {
	if src == nil {
		return nil
	}
	ts := src.AsTime().UTC()
	if ts.IsZero() {
		return nil
	}
	return &ts
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
		ID:           session.ID,
		State:        session.State,
		Metadata:     cloneStringMap(session.Metadata),
		Lifecycle:    cloneSessionLifecycle(session.Lifecycle),
		StateReason:  session.StateReason,
		StateMessage: session.StateMessage,
	}
}

func cloneSessionLifecycle(lifecycle *SessionLifecycle) *SessionLifecycle {
	if lifecycle == nil {
		return nil
	}
	return &SessionLifecycle{
		StartedAt:          cloneTimePtr(lifecycle.StartedAt),
		RecommendedDrainAt: cloneTimePtr(lifecycle.RecommendedDrainAt),
		ExpiresAt:          cloneTimePtr(lifecycle.ExpiresAt),
	}
}

func cloneTimePtr(src *time.Time) *time.Time {
	if src == nil {
		return nil
	}
	out := src.UTC()
	return &out
}

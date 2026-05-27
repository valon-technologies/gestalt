package runtimeprovider

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
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
	proc      *runtimehost.AppProcess
	runtime   proto.RuntimeProviderClient
	lifecycle proto.ProviderLifecycleClient

	name        string
	telemetry   metricutil.TelemetryProviders
	sessionLogs runtimelogs.Store
	mu          sync.Mutex
	sessions    map[string]*Session
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
		proc:        proc,
		runtime:     proto.NewRuntimeProviderClient(proc.Conn()),
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
	support := supportFromProto(resp)
	return support, nil
}

func (p *executableProvider) StartSession(ctx context.Context, req StartSessionRequest) (*Session, error) {
	resp, err := p.runtime.StartSession(ctx, &proto.StartRuntimeSessionRequest{
		AppName:       req.AppName,
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

func imagePullAuthToProto(auth *ImagePullAuth) *proto.RuntimeImagePullAuth {
	if auth == nil {
		return nil
	}
	return &proto.RuntimeImagePullAuth{
		DockerConfigJson: auth.DockerConfigJSON,
	}
}

func (p *executableProvider) ListSessions(ctx context.Context, req ListSessionsRequest) (*ListSessionsResponse, error) {
	if p == nil {
		return nil, fmt.Errorf("runtime provider is not configured")
	}
	req, err := NormalizeListSessionsRequestForForwarding(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.runtime.ListSessions(ctx, &proto.ListRuntimeSessionsRequest{
		PageSize:  int32(req.PageSize),
		PageToken: req.PageToken,
	})
	if err != nil {
		return nil, fmt.Errorf("list runtime sessions: %w", err)
	}
	out := make([]Session, 0, len(resp.GetSessions()))
	for _, protoSession := range resp.GetSessions() {
		session := sessionFromProto(protoSession)
		if session == nil || session.ID == "" {
			continue
		}
		p.trackSession(session)
		out = append(out, *session)
	}
	return &ListSessionsResponse{
		Sessions:      out,
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}

func (p *executableProvider) GetSession(ctx context.Context, req GetSessionRequest) (*Session, error) {
	resp, err := p.runtime.GetSession(ctx, &proto.GetRuntimeSessionRequest{
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
	_, err := p.runtime.StopSession(ctx, &proto.StopRuntimeSessionRequest{
		SessionId: req.SessionID,
	})
	p.mu.Lock()
	delete(p.sessions, req.SessionID)
	p.mu.Unlock()
	if p.sessionLogs != nil {
		_ = p.sessionLogs.MarkSessionStopped(ctx, p.name, req.SessionID, time.Now().UTC())
	}
	if err != nil {
		return fmt.Errorf("stop runtime session: %w", err)
	}
	return nil
}

func (p *executableProvider) PrepareWorkspace(ctx context.Context, req PrepareWorkspaceRequest) (*PreparedWorkspace, error) {
	resp, err := p.runtime.PrepareWorkspace(ctx, &proto.PrepareRuntimeWorkspaceRequest{
		SessionId:      req.SessionID,
		AgentSessionId: req.AgentSessionID,
		Workspace:      workspaceToProto(req.Workspace),
	})
	if err != nil {
		return nil, fmt.Errorf("prepare runtime workspace: %w", err)
	}
	return preparedWorkspaceFromProto(resp.GetWorkspace()), nil
}

func (p *executableProvider) RemoveWorkspace(ctx context.Context, req RemoveWorkspaceRequest) error {
	_, err := p.runtime.RemoveWorkspace(ctx, &proto.RemoveRuntimeWorkspaceRequest{
		SessionId:      req.SessionID,
		AgentSessionId: req.AgentSessionID,
	})
	if err != nil {
		return fmt.Errorf("remove runtime workspace: %w", err)
	}
	return nil
}

func (p *executableProvider) StartApp(ctx context.Context, req StartAppRequest) (*HostedApp, error) {
	resp, err := p.runtime.StartApp(ctx, &proto.StartHostedAppRequest{
		SessionId:     req.SessionID,
		AppName:       req.AppName,
		Command:       req.Command,
		Args:          append([]string(nil), req.Args...),
		Env:           cloneStringMap(req.Env),
		AllowedHosts:  append([]string(nil), req.Egress.AllowedHosts...),
		DefaultAction: string(req.Egress.DefaultAction),
		HostBinary:    req.HostBinary,
		Workdir:       req.Workdir,
	})
	if err != nil {
		return nil, fmt.Errorf("start hosted app: %w", p.enrichStartAppError(req.SessionID, err))
	}
	p.mu.Lock()
	if session, ok := p.sessions[req.SessionID]; ok && session != nil {
		session.State = SessionStateRunning
	}
	p.mu.Unlock()
	return &HostedApp{
		ID:         resp.GetId(),
		SessionID:  resp.GetSessionId(),
		AppName:    resp.GetAppName(),
		DialTarget: resp.GetDialTarget(),
	}, nil
}

func (p *executableProvider) enrichStartAppError(sessionID string, err error) error {
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

func supportFromProto(src *proto.RuntimeSupport) Support {
	if src == nil {
		return Support{}
	}
	return Support{
		CanHostApps:              src.GetCanHostApps(),
		EgressMode:               egressModeFromProto(src.GetEgressMode()),
		SupportsPrepareWorkspace: src.GetSupportsPrepareWorkspace(),
	}
}

func workspaceToProto(workspace *Workspace) *proto.AgentWorkspace {
	if workspace == nil {
		return nil
	}
	out := &proto.AgentWorkspace{
		Checkouts: make([]*proto.AgentWorkspaceGitCheckout, 0, len(workspace.Checkouts)),
		Cwd:       workspace.CWD,
	}
	for i := range workspace.Checkouts {
		checkout := workspace.Checkouts[i]
		out.Checkouts = append(out.Checkouts, &proto.AgentWorkspaceGitCheckout{
			Url:  checkout.URL,
			Ref:  checkout.Ref,
			Path: checkout.Path,
		})
	}
	return out
}

func preparedWorkspaceFromProto(workspace *proto.PreparedAgentWorkspace) *PreparedWorkspace {
	if workspace == nil {
		return nil
	}
	return &PreparedWorkspace{
		Root: workspace.GetRoot(),
		CWD:  workspace.GetCwd(),
	}
}

func egressModeFromProto(src proto.RuntimeEgressMode) EgressMode {
	switch src {
	case proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME:
		return EgressModeHostname
	case proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_CIDR:
		return EgressModeCIDR
	default:
		return EgressModeNone
	}
}

func sessionFromProto(src *proto.RuntimeSession) *Session {
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

func sessionLifecycleFromProto(src *proto.RuntimeSessionLifecycle) *SessionLifecycle {
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

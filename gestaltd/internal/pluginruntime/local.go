package pluginruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

type LocalProvider struct {
	nextID uint64

	telemetry metricutil.TelemetryProviders
	mu        sync.Mutex
	sessions  map[string]*localSession
	closed    bool
}

type localSession struct {
	id       string
	rootDir  string
	state    SessionState
	metadata map[string]string
	bindings []localBinding
	plugin   *localPlugin
}

type localBinding struct {
	id         string
	envVar     string
	envTarget  string
	socketPath string
	relay      HostServiceRelay
}

type localPlugin struct {
	id      string
	name    string
	process *providerhost.PluginProcess
}

type LocalOption func(*LocalProvider)

func WithLocalTelemetry(telemetry metricutil.TelemetryProviders) LocalOption {
	return func(p *LocalProvider) {
		p.telemetry = telemetry
	}
}

func NewLocalProvider(opts ...LocalOption) *LocalProvider {
	p := &LocalProvider{
		sessions: make(map[string]*localSession),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p
}

func (p *LocalProvider) Support(context.Context) (Support, error) {
	return Support{
		CanHostPlugins:    true,
		HostServiceAccess: HostServiceAccessDirect,
		EgressMode:        EgressModeHostname,
		LaunchMode:        LaunchModeHostPath,
		ExecutionTarget: ExecutionTarget{
			GOOS:   runtime.GOOS,
			GOARCH: runtime.GOARCH,
		},
	}, nil
}

func (p *LocalProvider) StartSession(_ context.Context, req StartSessionRequest) (*Session, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin runtime is not configured")
	}

	rootDir, err := providerhost.NewPluginTempDir("gestalt-plugin-runtime-*")
	if err != nil {
		return nil, fmt.Errorf("create runtime session dir: %w", err)
	}
	sessionID := p.newID("session")

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = os.RemoveAll(rootDir)
		return nil, fmt.Errorf("plugin runtime is closed")
	}
	session := &localSession{
		id:       sessionID,
		rootDir:  rootDir,
		state:    SessionStateReady,
		metadata: cloneStringMap(req.Metadata),
	}
	if session.metadata == nil {
		session.metadata = map[string]string{}
	}
	p.sessions[sessionID] = session
	return cloneSession(session), nil
}

func (p *LocalProvider) ListSessions(_ context.Context) ([]Session, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin runtime is not configured")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, fmt.Errorf("plugin runtime is closed")
	}

	sessionIDs := make([]string, 0, len(p.sessions))
	for sessionID := range p.sessions {
		sessionIDs = append(sessionIDs, sessionID)
	}
	slices.Sort(sessionIDs)

	out := make([]Session, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		session := cloneSession(p.sessions[sessionID])
		if session == nil {
			continue
		}
		out = append(out, *session)
	}
	return out, nil
}

func (p *LocalProvider) GetSession(_ context.Context, req GetSessionRequest) (*Session, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin runtime is not configured")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.sessionLocked(req.SessionID)
	if err != nil {
		return nil, err
	}
	return cloneSession(session), nil
}

func (p *LocalProvider) StopSession(_ context.Context, req StopSessionRequest) error {
	if p == nil {
		return nil
	}

	var plugin *providerhost.PluginProcess
	var rootDir string

	p.mu.Lock()
	session, ok := p.sessions[req.SessionID]
	if ok {
		delete(p.sessions, req.SessionID)
		if session.plugin != nil {
			plugin = session.plugin.process
		}
		rootDir = session.rootDir
	}
	p.mu.Unlock()

	var errs []error
	if plugin != nil {
		if err := plugin.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if rootDir != "" {
		if err := os.RemoveAll(rootDir); err != nil {
			errs = append(errs, fmt.Errorf("remove runtime session dir: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (p *LocalProvider) BindHostService(_ context.Context, req BindHostServiceRequest) (*HostServiceBinding, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin runtime is not configured")
	}
	if req.EnvVar == "" {
		return nil, fmt.Errorf("host service env var is required")
	}

	relay, envTarget, socketPath, err := normalizeHostServiceBinding(req)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.sessionLocked(req.SessionID)
	if err != nil {
		return nil, err
	}
	binding := localBinding{
		id:         p.newID("binding"),
		envVar:     req.EnvVar,
		envTarget:  envTarget,
		socketPath: socketPath,
		relay:      relay,
	}
	session.bindings = append(session.bindings, binding)
	return &HostServiceBinding{
		ID:         binding.id,
		SessionID:  session.id,
		EnvVar:     binding.envVar,
		SocketPath: binding.socketPath,
		Relay:      binding.relay,
	}, nil
}

func (p *LocalProvider) StartPlugin(ctx context.Context, req StartPluginRequest) (*HostedPlugin, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin runtime is not configured")
	}
	if req.Command == "" {
		return nil, fmt.Errorf("plugin command is required")
	}

	p.mu.Lock()
	session, err := p.sessionLocked(req.SessionID)
	if err != nil {
		p.mu.Unlock()
		return nil, err
	}
	if session.plugin != nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("plugin runtime session %q already has a running plugin", req.SessionID)
	}
	boundEnv := make(map[string]string, len(session.bindings))
	for _, binding := range session.bindings {
		boundEnv[binding.envVar] = binding.envTarget
	}
	session.state = SessionStateRunning
	rootDir := session.rootDir
	p.mu.Unlock()

	env := cloneStringMap(req.Env)
	if env == nil {
		env = map[string]string{}
	}
	for key, value := range boundEnv {
		env[key] = value
	}

	process, err := providerhost.StartPluginProcess(ctx, providerhost.ProcessConfig{
		Command:       req.Command,
		Args:          req.Args,
		Env:           env,
		AllowedHosts:  req.AllowedHosts,
		DefaultAction: egress.PolicyAction(req.DefaultAction),
		HostBinary:    req.HostBinary,
		SocketDir:     rootDir,
		ProviderName:  req.PluginName,
		Telemetry:     p.telemetry,
	})
	if err != nil {
		p.mu.Lock()
		if session, ok := p.sessions[req.SessionID]; ok {
			session.state = SessionStateFailed
		}
		p.mu.Unlock()
		return nil, err
	}

	plugin := &localPlugin{
		id:      p.newID("plugin"),
		name:    req.PluginName,
		process: process,
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err = p.sessionLocked(req.SessionID)
	if err != nil {
		_ = process.Close()
		return nil, err
	}
	session.plugin = plugin
	session.state = SessionStateRunning
	return &HostedPlugin{
		ID:         plugin.id,
		SessionID:  session.id,
		PluginName: plugin.name,
		DialTarget: "unix://" + filepath.Join(rootDir, "plugin.sock"),
	}, nil
}

func (p *LocalProvider) Close() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	sessionIDs := make([]string, 0, len(p.sessions))
	for sessionID := range p.sessions {
		sessionIDs = append(sessionIDs, sessionID)
	}
	p.mu.Unlock()

	var firstErr error
	for _, sessionID := range sessionIDs {
		if err := p.StopSession(context.Background(), StopSessionRequest{SessionID: sessionID}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *LocalProvider) newID(prefix string) string {
	value := atomic.AddUint64(&p.nextID, 1)
	return fmt.Sprintf("%s-%d", prefix, value)
}

func (p *LocalProvider) sessionLocked(sessionID string) (*localSession, error) {
	if p.closed {
		return nil, fmt.Errorf("plugin runtime is closed")
	}
	session, ok := p.sessions[sessionID]
	if !ok || session == nil {
		return nil, fmt.Errorf("plugin runtime session %q is not available", sessionID)
	}
	return session, nil
}

func cloneSession(session *localSession) *Session {
	if session == nil {
		return nil
	}
	return &Session{
		ID:       session.id,
		State:    session.state,
		Metadata: cloneStringMap(session.metadata),
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func normalizeHostServiceBinding(req BindHostServiceRequest) (HostServiceRelay, string, string, error) {
	if relay := req.Relay; relay.DialTarget != "" {
		network, address, err := dialTarget(relay.DialTarget)
		if err != nil {
			return HostServiceRelay{}, "", "", fmt.Errorf("host service relay: %w", err)
		}
		switch network {
		case "unix":
			return relay, address, address, nil
		case "tcp", "tls":
			return relay, relay.DialTarget, "", nil
		default:
			return HostServiceRelay{}, "", "", fmt.Errorf("host service relay network %q is not supported", network)
		}
	}
	if req.HostSocketPath == "" {
		return HostServiceRelay{}, "", "", fmt.Errorf("host service relay is required")
	}
	return HostServiceRelay{DialTarget: "unix://" + req.HostSocketPath}, req.HostSocketPath, req.HostSocketPath, nil
}

package pluginruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/runtimelogs"
)

type LocalProvider struct {
	nextID uint64

	runtimeProviderName string
	telemetry           metricutil.TelemetryProviders
	sessionLogs         runtimelogs.Store
	mu                  sync.Mutex
	sessions            map[string]*localSession
	closed              bool
}

type localSession struct {
	id       string
	rootDir  string
	state    SessionState
	metadata map[string]string
	bindings []localBinding
	plugin   *localPlugin
	logSeq   uint64
}

type localBinding struct {
	id        string
	envVar    string
	envTarget string
	relay     HostServiceRelay
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

func WithLocalRuntimeSessionLogs(runtimeProviderName string, store runtimelogs.Store) LocalOption {
	return func(p *LocalProvider) {
		p.runtimeProviderName = runtimeProviderName
		p.sessionLogs = store
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
	if p.sessionLogs != nil {
		if err := p.sessionLogs.RegisterSession(context.Background(), runtimelogs.SessionRegistration{
			RuntimeProviderName: p.runtimeProviderName,
			SessionID:           sessionID,
			Metadata:            cloneStringMap(session.metadata),
		}); err != nil {
			slog.Warn("failed to register runtime session logs", "runtime_provider", p.runtimeProviderName, "session", sessionID, "error", err)
		}
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
	if p.sessionLogs != nil {
		_ = p.sessionLogs.MarkSessionStopped(context.Background(), p.runtimeProviderName, req.SessionID, time.Now().UTC())
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

	relay, envTarget, err := normalizeHostServiceBinding(req)
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
		id:        p.newID("binding"),
		envVar:    req.EnvVar,
		envTarget: envTarget,
		relay:     relay,
	}
	session.bindings = append(session.bindings, binding)
	return &HostServiceBinding{
		ID:        binding.id,
		SessionID: session.id,
		EnvVar:    binding.envVar,
		Relay:     binding.relay,
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

	stdout := io.Writer(nil)
	stderr := io.Writer(nil)
	if p.sessionLogs != nil {
		stdout = newSessionLogWriter(p.sessionLogs, p.runtimeProviderName, req.SessionID, runtimelogs.StreamStdout, &session.logSeq)
		stderr = newSessionLogWriter(p.sessionLogs, p.runtimeProviderName, req.SessionID, runtimelogs.StreamStderr, &session.logSeq)
		_, _ = p.sessionLogs.AppendSessionLogs(context.Background(), p.runtimeProviderName, req.SessionID, []runtimelogs.AppendEntry{{
			SourceSeq:  int64(atomic.AddUint64(&session.logSeq, 1)),
			Stream:     runtimelogs.StreamRuntime,
			Message:    fmt.Sprintf("starting plugin %q", req.PluginName),
			ObservedAt: time.Now().UTC(),
		}})
	}

	process, err := providerhost.StartPluginProcess(ctx, providerhost.ProcessConfig{
		Command: req.Command,
		Args:    req.Args,
		Env:     env,
		Egress: egress.Policy{
			AllowedHosts:  append([]string(nil), req.Egress.AllowedHosts...),
			DefaultAction: egress.PolicyAction(req.Egress.DefaultAction),
		},
		HostBinary:   req.HostBinary,
		SocketDir:    rootDir,
		ProviderName: req.PluginName,
		Telemetry:    p.telemetry,
		Stdout:       stdout,
		Stderr:       stderr,
	})
	if err != nil {
		p.mu.Lock()
		if session, ok := p.sessions[req.SessionID]; ok {
			session.state = SessionStateFailed
		}
		p.mu.Unlock()
		if p.sessionLogs != nil {
			_, _ = p.sessionLogs.AppendSessionLogs(context.Background(), p.runtimeProviderName, req.SessionID, []runtimelogs.AppendEntry{{
				SourceSeq:  int64(atomic.AddUint64(&session.logSeq, 1)),
				Stream:     runtimelogs.StreamRuntime,
				Message:    err.Error(),
				ObservedAt: time.Now().UTC(),
			}})
		}
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

func normalizeHostServiceBinding(req BindHostServiceRequest) (HostServiceRelay, string, error) {
	if relay := req.Relay; relay.DialTarget != "" {
		network, address, err := dialTarget(relay.DialTarget)
		if err != nil {
			return HostServiceRelay{}, "", fmt.Errorf("host service relay: %w", err)
		}
		switch network {
		case "unix":
			return relay, address, nil
		case "tcp", "tls":
			return relay, relay.DialTarget, nil
		default:
			return HostServiceRelay{}, "", fmt.Errorf("host service relay network %q is not supported", network)
		}
	}
	return HostServiceRelay{}, "", fmt.Errorf("host service relay is required")
}

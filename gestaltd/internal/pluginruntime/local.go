package pluginruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

type LocalProvider struct {
	nextID uint64

	mu       sync.Mutex
	sessions map[string]*localSession
	closed   bool
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
	id             string
	envVar         string
	hostSocketPath string
}

type localPlugin struct {
	id      string
	name    string
	process *providerhost.PluginProcess
}

func NewLocalProvider() *LocalProvider {
	return &LocalProvider{
		sessions: make(map[string]*localSession),
	}
}

func (p *LocalProvider) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{
		HostedPluginRuntime: true,
		HostServiceTunnels:  true,
		ProviderGRPCTunnel:  true,
		HostnameProxyEgress: true,
		HostPathExecution:   true,
		ExecutionGOOS:       runtime.GOOS,
		ExecutionGOARCH:     runtime.GOARCH,
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
	if req.HostSocketPath == "" {
		return nil, fmt.Errorf("host service socket path is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.sessionLocked(req.SessionID)
	if err != nil {
		return nil, err
	}
	binding := localBinding{
		id:             p.newID("binding"),
		envVar:         req.EnvVar,
		hostSocketPath: req.HostSocketPath,
	}
	session.bindings = append(session.bindings, binding)
	return &HostServiceBinding{
		ID:         binding.id,
		SessionID:  session.id,
		EnvVar:     binding.envVar,
		SocketPath: binding.hostSocketPath,
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
		boundEnv[binding.envVar] = binding.hostSocketPath
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

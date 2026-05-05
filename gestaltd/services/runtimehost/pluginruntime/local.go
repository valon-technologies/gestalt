package pluginruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
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
	plugin   *localPlugin
	logSeq   uint64
}

type localPlugin struct {
	id      string
	name    string
	process *runtimehost.PluginProcess
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
		CanHostPlugins:           true,
		EgressMode:               EgressModeHostname,
		SupportsPrepareWorkspace: true,
	}, nil
}

func (p *LocalProvider) StartSession(_ context.Context, req StartSessionRequest) (*Session, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin runtime is not configured")
	}

	rootDir, err := runtimehost.NewPluginTempDir("gestalt-plugin-runtime-*")
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

	var plugin *runtimehost.PluginProcess
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

func (p *LocalProvider) PrepareWorkspace(ctx context.Context, req PrepareWorkspaceRequest) (*PreparedWorkspace, error) {
	if p == nil {
		return nil, fmt.Errorf("plugin runtime is not configured")
	}
	if err := validateWorkspaceID(req.AgentSessionID); err != nil {
		return nil, fmt.Errorf("agent session id: %w", err)
	}
	workspace, err := normalizeRuntimeWorkspace(req.Workspace)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.sessionLocked(req.SessionID)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(session.rootDir, "workspaces", req.AgentSessionID)
	spec, err := json.Marshal(workspace)
	if err != nil {
		return nil, fmt.Errorf("marshal workspace spec: %w", err)
	}
	marker := filepath.Join(root, ".gestalt-workspace.json")
	if existing, err := os.ReadFile(marker); err == nil {
		if !bytes.Equal(existing, spec) {
			return nil, fmt.Errorf("workspace for agent session %q was already prepared with a different spec", req.AgentSessionID)
		}
		return preparedLocalWorkspace(root, workspace.CWD)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read workspace marker: %w", err)
	}
	if err := os.RemoveAll(root); err != nil {
		return nil, fmt.Errorf("remove partial workspace: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace root: %w", err)
	}
	for _, checkout := range workspace.Checkouts {
		if err := prepareLocalGitCheckout(ctx, root, checkout); err != nil {
			_ = os.RemoveAll(root)
			return nil, err
		}
	}
	prepared, err := preparedLocalWorkspace(root, workspace.CWD)
	if err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	if err := os.WriteFile(marker, spec, 0o644); err != nil {
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("write workspace marker: %w", err)
	}
	return prepared, nil
}

func (p *LocalProvider) RemoveWorkspace(_ context.Context, req RemoveWorkspaceRequest) error {
	if p == nil {
		return nil
	}
	if err := validateWorkspaceID(req.AgentSessionID); err != nil {
		return fmt.Errorf("agent session id: %w", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.sessionLocked(req.SessionID)
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(session.rootDir, "workspaces", req.AgentSessionID))
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
	session.state = SessionStateRunning
	rootDir := session.rootDir
	p.mu.Unlock()

	env := cloneStringMap(req.Env)
	if env == nil {
		env = map[string]string{}
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

	process, err := runtimehost.StartPluginProcess(ctx, runtimehost.ProcessConfig{
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

func normalizeRuntimeWorkspace(src *Workspace) (*coreagent.Workspace, error) {
	if src == nil {
		return nil, fmt.Errorf("workspace is required")
	}
	workspace := &coreagent.Workspace{
		Checkouts: make([]coreagent.WorkspaceGitCheckout, 0, len(src.Checkouts)),
		CWD:       src.CWD,
	}
	for _, checkout := range src.Checkouts {
		workspace.Checkouts = append(workspace.Checkouts, coreagent.WorkspaceGitCheckout{
			URL:  checkout.URL,
			Ref:  checkout.Ref,
			Path: checkout.Path,
		})
	}
	normalized, err := coreagent.NormalizeWorkspace(workspace)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func validateWorkspaceID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("is required")
	}
	if value == "." || value == ".." || filepath.Clean(value) != value {
		return fmt.Errorf("must be a single path segment")
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("contains unsupported character %q", r)
	}
	return nil
}

func prepareLocalGitCheckout(ctx context.Context, root string, checkout coreagent.WorkspaceGitCheckout) error {
	target, err := localWorkspaceChild(root, checkout.Path)
	if err != nil {
		return fmt.Errorf("workspace checkout %q: %w", checkout.Path, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create checkout parent: %w", err)
	}
	if err := runLocalGit(ctx, "", "clone", checkout.URL, target); err != nil {
		return fmt.Errorf("clone %q: %w", checkout.URL, err)
	}
	if checkout.Ref == "" {
		return nil
	}
	if err := runLocalGit(ctx, target, "fetch", "origin", checkout.Ref); err != nil {
		return fmt.Errorf("fetch %q: %w", checkout.Ref, err)
	}
	if err := runLocalGit(ctx, target, "checkout", "--detach", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("checkout %q: %w", checkout.Ref, err)
	}
	return nil
}

func runLocalGit(ctx context.Context, dir string, args ...string) error {
	gitArgs := append([]string{"-c", "protocol.file.allow=always"}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func preparedLocalWorkspace(root string, cwd string) (*PreparedWorkspace, error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	cwdPath, err := localWorkspaceChild(root, cwd)
	if err != nil {
		return nil, fmt.Errorf("workspace cwd: %w", err)
	}
	cwdReal, err := filepath.EvalSymlinks(cwdPath)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace cwd: %w", err)
	}
	if !localPathWithin(rootReal, cwdReal) {
		return nil, fmt.Errorf("workspace cwd escapes workspace root")
	}
	return &PreparedWorkspace{Root: rootReal, CWD: cwdReal}, nil
}

func localWorkspaceChild(root string, rel string) (string, error) {
	clean := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	if !localPathWithin(root, clean) {
		return "", fmt.Errorf("path escapes workspace root")
	}
	return clean, nil
}

func localPathWithin(parent string, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

package runtimeprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
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
	state    string
	metadata map[string]string
	app      *localApp
}

type localApp struct {
	id      string
	name    string
	process *runtimehost.AppProcess
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

func (p *LocalProvider) Support(context.Context) (*proto.RuntimeSupport, error) {
	return &proto.RuntimeSupport{
		CanHostApps:              true,
		EgressMode:               proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME,
		SupportsPrepareWorkspace: true,
	}, nil
}

func (p *LocalProvider) StartSession(_ context.Context, req *proto.StartRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	if p == nil {
		return nil, fmt.Errorf("runtime provider is not configured")
	}

	rootDir, err := runtimehost.NewPluginTempDir("gestalt-app-runtime-*")
	if err != nil {
		return nil, fmt.Errorf("create runtime session dir: %w", err)
	}
	sessionID := p.newID("session")

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = os.RemoveAll(rootDir)
		return nil, fmt.Errorf("runtime provider is closed")
	}
	session := &localSession{
		id:       sessionID,
		rootDir:  rootDir,
		state:    SessionStateReady,
		metadata: cloneStringMap(req.GetMetadata()),
	}
	if session.metadata == nil {
		session.metadata = map[string]string{}
	}
	p.sessions[sessionID] = session
	return cloneSession(session), nil
}

func (p *LocalProvider) ListSessions(_ context.Context, req *proto.ListRuntimeSessionsRequest) (*proto.ListRuntimeSessionsResponse, error) {
	if p == nil {
		return nil, fmt.Errorf("runtime provider is not configured")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, fmt.Errorf("runtime provider is closed")
	}

	sessionIDs := make([]string, 0, len(p.sessions))
	for sessionID := range p.sessions {
		sessionIDs = append(sessionIDs, sessionID)
	}
	slices.Sort(sessionIDs)
	pageIDs, nextPageToken, err := paginateSortedSessionIDs(sessionIDs, req)
	if err != nil {
		return nil, err
	}

	out := make([]*proto.RuntimeSession, 0, len(pageIDs))
	for _, sessionID := range pageIDs {
		session := cloneSession(p.sessions[sessionID])
		if session == nil {
			continue
		}
		out = append(out, session)
	}
	return &proto.ListRuntimeSessionsResponse{
		Sessions:      out,
		NextPageToken: nextPageToken,
	}, nil
}

func (p *LocalProvider) GetSession(_ context.Context, req *proto.GetRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	if p == nil {
		return nil, fmt.Errorf("runtime provider is not configured")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.sessionLocked(req.GetSessionId())
	if err != nil {
		return nil, err
	}
	return cloneSession(session), nil
}

func (p *LocalProvider) StopSession(_ context.Context, req *proto.StopRuntimeSessionRequest) error {
	if p == nil {
		return nil
	}

	var app *runtimehost.AppProcess
	var rootDir string

	p.mu.Lock()
	session, ok := p.sessions[req.GetSessionId()]
	if ok {
		delete(p.sessions, req.GetSessionId())
		if session.app != nil {
			app = session.app.process
		}
		rootDir = session.rootDir
	}
	p.mu.Unlock()

	var errs []error
	if app != nil {
		if err := app.Close(); err != nil {
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

func (p *LocalProvider) PrepareWorkspace(ctx context.Context, req *proto.PrepareRuntimeWorkspaceRequest) (*proto.PrepareRuntimeWorkspaceResponse, error) {
	if p == nil {
		return nil, fmt.Errorf("runtime provider is not configured")
	}
	if err := validateWorkspaceID(req.GetAgentSessionId()); err != nil {
		return nil, fmt.Errorf("agent session id: %w", err)
	}
	workspace, err := normalizeRuntimeWorkspace(req.GetWorkspace())
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.sessionLocked(req.GetSessionId())
	if err != nil {
		return nil, err
	}
	root := filepath.Join(session.rootDir, "workspaces", req.GetAgentSessionId())
	spec, err := json.Marshal(workspace)
	if err != nil {
		return nil, fmt.Errorf("marshal workspace spec: %w", err)
	}
	marker := filepath.Join(root, ".gestalt-workspace.json")
	if existing, err := os.ReadFile(marker); err == nil {
		if !bytes.Equal(existing, spec) {
			return nil, fmt.Errorf("workspace for agent session %q was already prepared with a different spec", req.GetAgentSessionId())
		}
		prepared, err := preparedLocalWorkspace(root, workspace.CWD)
		if err != nil {
			return nil, err
		}
		return &proto.PrepareRuntimeWorkspaceResponse{Workspace: prepared}, nil
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
	return &proto.PrepareRuntimeWorkspaceResponse{Workspace: prepared}, nil
}

func (p *LocalProvider) RemoveWorkspace(_ context.Context, req *proto.RemoveRuntimeWorkspaceRequest) error {
	if p == nil {
		return nil
	}
	if err := validateWorkspaceID(req.GetAgentSessionId()); err != nil {
		return fmt.Errorf("agent session id: %w", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	session, err := p.sessionLocked(req.GetSessionId())
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(session.rootDir, "workspaces", req.GetAgentSessionId()))
}

func (p *LocalProvider) StartApp(ctx context.Context, req *proto.StartHostedAppRequest) (*proto.HostedApp, error) {
	if p == nil {
		return nil, fmt.Errorf("runtime provider is not configured")
	}
	if req.GetCommand() == "" {
		return nil, fmt.Errorf("app command is required")
	}

	p.mu.Lock()
	session, err := p.sessionLocked(req.GetSessionId())
	if err != nil {
		p.mu.Unlock()
		return nil, err
	}
	if session.app != nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("runtime session %q already has a running app", req.GetSessionId())
	}
	session.state = SessionStateRunning
	rootDir := session.rootDir
	p.mu.Unlock()

	env := cloneStringMap(req.GetEnv())
	if env == nil {
		env = map[string]string{}
	}

	process, err := runtimehost.StartAppProcess(ctx, runtimehost.ProcessConfig{
		Command: req.GetCommand(),
		Args:    req.GetArgs(),
		Workdir: req.GetWorkdir(),
		Env:     env,
		Egress: egress.Policy{
			AllowedHosts:  append([]string(nil), req.GetAllowedHosts()...),
			DefaultAction: egress.PolicyAction(req.GetDefaultAction()),
		},
		HostBinary:   req.GetHostBinary(),
		SocketDir:    rootDir,
		ProviderName: req.GetAppName(),
		Telemetry:    p.telemetry,
	})
	if err != nil {
		p.mu.Lock()
		if session, ok := p.sessions[req.GetSessionId()]; ok {
			session.state = SessionStateFailed
		}
		p.mu.Unlock()
		return nil, err
	}

	hostedApp := &localApp{
		id:      p.newID("app"),
		name:    req.GetAppName(),
		process: process,
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	session, err = p.sessionLocked(req.GetSessionId())
	if err != nil {
		_ = process.Close()
		return nil, err
	}
	session.app = hostedApp
	session.state = SessionStateRunning
	return &proto.HostedApp{
		Id:         hostedApp.id,
		SessionId:  session.id,
		AppName:    hostedApp.name,
		DialTarget: "unix://" + filepath.Join(rootDir, "app.sock"),
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
		if err := p.StopSession(context.Background(), &proto.StopRuntimeSessionRequest{SessionId: sessionID}); err != nil && firstErr == nil {
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
		return nil, fmt.Errorf("runtime provider is closed")
	}
	session, ok := p.sessions[sessionID]
	if !ok || session == nil {
		return nil, fmt.Errorf("runtime session %q is not available", sessionID)
	}
	return session, nil
}

func normalizeRuntimeWorkspace(src *proto.AgentWorkspace) (*coreagent.Workspace, error) {
	if src == nil {
		return nil, fmt.Errorf("workspace is required")
	}
	workspace := &coreagent.Workspace{
		Checkouts: make([]coreagent.WorkspaceGitCheckout, 0, len(src.GetCheckouts())),
		CWD:       src.GetCwd(),
	}
	for _, checkout := range src.GetCheckouts() {
		workspace.Checkouts = append(workspace.Checkouts, coreagent.WorkspaceGitCheckout{
			URL:  checkout.GetUrl(),
			Ref:  checkout.GetRef(),
			Path: checkout.GetPath(),
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

func preparedLocalWorkspace(root string, cwd string) (*proto.PreparedAgentWorkspace, error) {
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
	return &proto.PreparedAgentWorkspace{Root: rootReal, Cwd: cwdReal}, nil
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

func cloneSession(session *localSession) *proto.RuntimeSession {
	if session == nil {
		return nil
	}
	return &proto.RuntimeSession{
		Id:       session.id,
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

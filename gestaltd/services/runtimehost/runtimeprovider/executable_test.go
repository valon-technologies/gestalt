package runtimeprovider

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestExecutableProviderReadsRuntimeSupport(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runtimeBin := buildMinimalRuntimeProviderBinary(t)
	runtimeProvider, err := NewExecutableProvider(ctx, ExecutableConfig{
		Name:    "modal",
		Command: runtimeBin,
	})
	if err != nil {
		t.Fatalf("NewExecutableProvider: %v", err)
	}
	t.Cleanup(func() {
		_ = runtimeProvider.Close()
	})

	support, err := runtimeProvider.Support(ctx)
	if err != nil {
		t.Fatalf("Support: %v", err)
	}
	if !support.GetCanHostApps() || support.GetEgressMode() != proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_UNSPECIFIED {
		t.Fatalf("Support = %#v, want can_host_apps with unspecified egress", support)
	}
}

func TestExecutableProviderForwardsStartAppWorkdir(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runtimeBin := buildMinimalRuntimeProviderBinary(t)
	runtimeProvider, err := NewExecutableProvider(ctx, ExecutableConfig{
		Name:    "modal",
		Command: runtimeBin,
	})
	if err != nil {
		t.Fatalf("NewExecutableProvider: %v", err)
	}
	t.Cleanup(func() {
		_ = runtimeProvider.Close()
	})

	session, err := runtimeProvider.StartSession(ctx, &proto.StartRuntimeSessionRequest{
		AppName: "agent",
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	hosted, err := runtimeProvider.StartApp(ctx, &proto.StartHostedAppRequest{
		SessionId: session.GetId(),
		AppName:   "agent",
		Command:   "/bin/plugin",
		Workdir:   "/tmp/provider-root",
	})
	if err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	if hosted.DialTarget != "workdir:///tmp/provider-root" {
		t.Fatalf("DialTarget = %q, want forwarded workdir", hosted.DialTarget)
	}
}

func buildMinimalRuntimeProviderBinary(t *testing.T) string {
	t.Helper()

	repoRoot := repoRootForRuntimeTests(t)
	moduleDir := t.TempDir()
	goMod := "module minimalruntime\n\n" +
		"go 1.26.5\n\n" +
		"require github.com/valon-technologies/gestalt/sdk/go v0.0.0\n\n" +
		"replace github.com/valon-technologies/gestalt/sdk/go => " + filepath.ToSlash(filepath.Join(repoRoot, "sdk", "go")) + "\n" +
		"replace github.com/valon-technologies/gestalt/server/rpc => " + filepath.ToSlash(filepath.Join(repoRoot, "gestaltd", "rpc")) + "\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "main.go"), []byte(minimalRuntimeProviderSource), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	bin := filepath.Join(moduleDir, "minimalruntime")
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = moduleDir
	if output, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy minimal runtime provider: %v\n%s", err, output)
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = moduleDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build minimal runtime provider: %v\n%s", err, output)
	}
	return bin
}

func repoRootForRuntimeTests(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "sdk", "go", "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repository root from %s", file)
		}
	}
}

const minimalRuntimeProviderSource = `package main

import (
	"context"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type runtimeProvider struct {
	mu       sync.Mutex
	sessions map[string]gestalt.RuntimeSession
}

func newRuntimeProvider() *runtimeProvider {
	return &runtimeProvider{sessions: make(map[string]gestalt.RuntimeSession)}
}

func (p *runtimeProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (p *runtimeProvider) GetSupport(context.Context) (gestalt.RuntimeSupport, error) {
	return gestalt.RuntimeSupport{CanHostApps: true}, nil
}

func (p *runtimeProvider) StartSession(_ context.Context, req gestalt.StartRuntimeSessionRequest) (gestalt.RuntimeSession, error) {
	sessionID := strings.TrimSpace(req.AppName) + "-session"
	if sessionID == "-session" {
		sessionID = "runtime-session"
	}
	session := gestalt.RuntimeSession{ID: sessionID, State: "ready", Metadata: req.Metadata}
	p.mu.Lock()
	p.sessions[sessionID] = session
	p.mu.Unlock()
	return session, nil
}

func (p *runtimeProvider) GetSession(_ context.Context, sessionID string) (gestalt.RuntimeSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[strings.TrimSpace(sessionID)]
	if !ok {
		return gestalt.RuntimeSession{}, status.Error(codes.NotFound, "session not found")
	}
	return session, nil
}

func (p *runtimeProvider) ListSessions(_ context.Context, req gestalt.ListRuntimeSessionsRequest) (gestalt.ListRuntimeSessionsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sessions := make([]gestalt.RuntimeSession, 0, len(p.sessions))
	for _, session := range p.sessions {
		sessions = append(sessions, session)
	}
	return gestalt.ListRuntimeSessionsResponse{Sessions: sessions, NextPageToken: req.PageToken}, nil
}

func (p *runtimeProvider) StopSession(_ context.Context, sessionID string) error {
	p.mu.Lock()
	delete(p.sessions, strings.TrimSpace(sessionID))
	p.mu.Unlock()
	return nil
}

func (p *runtimeProvider) StartApp(_ context.Context, req gestalt.StartHostedAppRequest) (gestalt.HostedApp, error) {
	if req.Workdir != "" {
		return gestalt.HostedApp{
			ID:         "hosted-" + req.AppName,
			SessionID:  req.SessionID,
			AppName:    req.AppName,
			DialTarget: "workdir://" + req.Workdir,
		}, nil
	}
	return gestalt.HostedApp{}, status.Error(codes.Internal, "runtime start failed")
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeRuntimeProvider(ctx, newRuntimeProvider()); err != nil {
		panic(err)
	}
}
`

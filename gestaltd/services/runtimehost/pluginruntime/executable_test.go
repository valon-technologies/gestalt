package pluginruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
)

func TestExecutableProviderIgnoresLegacyDirectHostServiceAccess(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runtimeBin := buildRuntimeLogProviderBinary(t)
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
	want := Support{CanHostPlugins: true, EgressMode: EgressModeNone}
	if !reflect.DeepEqual(support, want) {
		t.Fatalf("Support = %#v, want %#v", support, want)
	}
}

func TestExecutableProviderIncludesPushedRuntimeLogsInStartupFailures(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runtimeBin := buildRuntimeLogProviderBinary(t)
	runtimeProvider, err := NewExecutableProvider(ctx, ExecutableConfig{
		Name:    "modal",
		Command: runtimeBin,
		HostServices: []runtimehost.HostService{{
			Name:   "runtime_logs",
			EnvVar: runtimehost.DefaultRuntimeLogHostSocketEnv,
			Register: func(srv *grpc.Server) {
				runtimehost.RegisterRuntimeLogHostServer(srv, "modal", services.RuntimeSessionLogs.AppendSessionLogs)
			},
		}},
		SessionLogs: services.RuntimeSessionLogs,
	})
	if err != nil {
		t.Fatalf("NewExecutableProvider: %v", err)
	}
	t.Cleanup(func() {
		_ = runtimeProvider.Close()
	})

	session, err := runtimeProvider.StartSession(ctx, StartSessionRequest{
		PluginName: "agent",
		Metadata: map[string]string{
			"provider_name": "agent",
			"provider_kind": "agent",
			"owner_kind":    "test",
			"owner_id":      "runtime-log-ingest",
		},
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	_, err = runtimeProvider.StartPlugin(ctx, StartPluginRequest{
		SessionID:  session.ID,
		PluginName: "agent",
		Command:    "/bin/false",
	})
	if err == nil {
		t.Fatal("StartPlugin succeeded, want startup failure")
	}
	if !strings.Contains(err.Error(), "runtime start failed") {
		t.Fatalf("StartPlugin error = %q, want runtime failure", err)
	}
	if !strings.Contains(err.Error(), "recent runtime logs:") {
		t.Fatalf("StartPlugin error = %q, want recent runtime logs", err)
	}
	if !strings.Contains(err.Error(), "[runtime] runtime boot") {
		t.Fatalf("StartPlugin error = %q, want runtime log entry", err)
	}
	if !strings.Contains(err.Error(), "[runtime] runtime boot\n[stdout] stdout line\n[stderr] stderr line\n") {
		t.Fatalf("StartPlugin error = %q, want newline-delimited runtime logs", err)
	}
	if !strings.Contains(err.Error(), "[stderr] stderr line") {
		t.Fatalf("StartPlugin error = %q, want stderr log entry", err)
	}

	logs, err := services.RuntimeSessionLogs.ListSessionLogs(ctx, "modal", session.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListSessionLogs: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("runtime session logs len = %d, want 3", len(logs))
	}
	if logs[0].Stream != "runtime" || logs[0].Message != "runtime boot" {
		t.Fatalf("logs[0] = %#v, want runtime boot", logs[0])
	}
	if logs[1].Stream != "stdout" || logs[1].Message != "stdout line\n" {
		t.Fatalf("logs[1] = %#v, want stdout line", logs[1])
	}
	if logs[2].Stream != "stderr" || logs[2].Message != "stderr line\n" {
		t.Fatalf("logs[2] = %#v, want stderr line", logs[2])
	}
}

func buildRuntimeLogProviderBinary(t *testing.T) string {
	t.Helper()

	repoRoot := repoRootForPluginRuntimeTests(t)
	moduleDir := t.TempDir()
	goMod := "module runtimehostlogs\n\ngo 1.26\n\nrequire github.com/valon-technologies/gestalt/sdk/go v0.0.0\n\nreplace github.com/valon-technologies/gestalt/sdk/go => " + filepath.ToSlash(filepath.Join(repoRoot, "sdk/go")) + "\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "main.go"), []byte(runtimeLogProviderSource), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	bin := filepath.Join(moduleDir, "runtimehostlogs")
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = moduleDir
	if output, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy runtime log provider: %v\n%s", err, output)
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = moduleDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build runtime log provider: %v\n%s", err, output)
	}
	return bin
}

func repoRootForPluginRuntimeTests(t *testing.T) string {
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

const runtimeLogProviderSource = `package main

import (
	"context"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type runtimeProvider struct {
	proto.UnimplementedPluginRuntimeProviderServer

	mu       sync.Mutex
	sessions map[string]*proto.PluginRuntimeSession
}

func newRuntimeProvider() *runtimeProvider {
	return &runtimeProvider{sessions: make(map[string]*proto.PluginRuntimeSession)}
}

func (p *runtimeProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (p *runtimeProvider) GetSupport(context.Context, *emptypb.Empty) (*proto.PluginRuntimeSupport, error) {
	return &proto.PluginRuntimeSupport{
		CanHostPlugins: true,
		HostServiceAccess: proto.PluginRuntimeHostServiceAccess_PLUGIN_RUNTIME_HOST_SERVICE_ACCESS_DIRECT,
	}, nil
}

func (p *runtimeProvider) StartSession(_ context.Context, req *proto.StartPluginRuntimeSessionRequest) (*proto.PluginRuntimeSession, error) {
	sessionID := strings.TrimSpace(req.GetPluginName()) + "-session"
	if sessionID == "-session" {
		sessionID = "runtime-session"
	}
	session := &proto.PluginRuntimeSession{
		Id:       sessionID,
		State:    "ready",
		Metadata: req.GetMetadata(),
	}
	p.mu.Lock()
	p.sessions[sessionID] = session
	p.mu.Unlock()
	return session, nil
}

func (p *runtimeProvider) GetSession(_ context.Context, req *proto.GetPluginRuntimeSessionRequest) (*proto.PluginRuntimeSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[strings.TrimSpace(req.GetSessionId())]
	if !ok {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	return session, nil
}

func (p *runtimeProvider) StopSession(_ context.Context, req *proto.StopPluginRuntimeSessionRequest) (*emptypb.Empty, error) {
	p.mu.Lock()
	delete(p.sessions, strings.TrimSpace(req.GetSessionId()))
	p.mu.Unlock()
	return &emptypb.Empty{}, nil
}

func (p *runtimeProvider) StartPlugin(ctx context.Context, req *proto.StartHostedPluginRequest) (*proto.HostedPlugin, error) {
	host, err := gestalt.RuntimeLogHost()
	if err == nil {
		defer func() { _ = host.Close() }()
		now := time.Now().UTC()
		_, _ = host.AppendLogs(ctx, &proto.AppendPluginRuntimeLogsRequest{
			SessionId: req.GetSessionId(),
			Logs: []*proto.PluginRuntimeLogEntry{
				{
					Stream:     proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_RUNTIME,
					Message:    "runtime boot",
					ObservedAt: timestamppb.New(now),
					SourceSeq:  1,
				},
				{
					Stream:     proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_STDOUT,
					Message:    "stdout line\n",
					ObservedAt: timestamppb.New(now.Add(time.Second)),
					SourceSeq:  2,
				},
				{
					Stream:     proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_STDERR,
					Message:    "stderr line\n",
					ObservedAt: timestamppb.New(now.Add(2 * time.Second)),
					SourceSeq:  3,
				},
			},
		})
	}
	return nil, status.Error(codes.Internal, "runtime start failed")
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServePluginRuntimeProvider(ctx, newRuntimeProvider()); err != nil {
		panic(err)
	}
}
`

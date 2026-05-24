package gestalt_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type pluginRuntimeTransportProvider struct {
	prepareReq gestalt.PrepareRuntimeWorkspaceRequest
	removeReq  gestalt.RemoveRuntimeWorkspaceRequest
	pluginReq  gestalt.StartHostedAppRequest
	listReq    gestalt.ListRuntimeSessionsRequest
}

func (*pluginRuntimeTransportProvider) GetMetadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{Name: "runtime"}
}

func (*pluginRuntimeTransportProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (*pluginRuntimeTransportProvider) Close() error {
	return nil
}

func (*pluginRuntimeTransportProvider) GetSupport(context.Context) (gestalt.RuntimeSupport, error) {
	return gestalt.RuntimeSupport{
		CanHostApps:              true,
		SupportsPrepareWorkspace: true,
	}, nil
}

func (*pluginRuntimeTransportProvider) StartSession(context.Context, gestalt.StartRuntimeSessionRequest) (gestalt.RuntimeSession, error) {
	return gestalt.RuntimeSession{ID: "runtime-session-1", State: "ready"}, nil
}

func (*pluginRuntimeTransportProvider) GetSession(context.Context, string) (gestalt.RuntimeSession, error) {
	return gestalt.RuntimeSession{ID: "runtime-session-1", State: "ready"}, nil
}

func (p *pluginRuntimeTransportProvider) ListSessions(_ context.Context, req gestalt.ListRuntimeSessionsRequest) (gestalt.ListRuntimeSessionsResponse, error) {
	p.listReq = req
	return gestalt.ListRuntimeSessionsResponse{
		Sessions: []gestalt.RuntimeSession{{ID: "runtime-session-1", State: "ready"}},
	}, nil
}

func (*pluginRuntimeTransportProvider) StopSession(context.Context, string) error {
	return nil
}

func (p *pluginRuntimeTransportProvider) StartApp(_ context.Context, req gestalt.StartHostedAppRequest) (gestalt.HostedApp, error) {
	p.pluginReq = req
	return gestalt.HostedApp{ID: "plugin-1", SessionID: req.SessionID, AppName: req.AppName}, nil
}

func (p *pluginRuntimeTransportProvider) PrepareWorkspace(_ context.Context, req gestalt.PrepareRuntimeWorkspaceRequest) (gestalt.PrepareRuntimeWorkspaceResponse, error) {
	p.prepareReq = req
	return gestalt.PrepareRuntimeWorkspaceResponse{
		Workspace: &gestalt.PreparedAgentWorkspace{
			Root: "/tmp/runtime-session-1/workspaces/agent-session-1",
			CWD:  "/tmp/runtime-session-1/workspaces/agent-session-1/app",
		},
	}, nil
}

func (p *pluginRuntimeTransportProvider) RemoveWorkspace(_ context.Context, req gestalt.RemoveRuntimeWorkspaceRequest) error {
	p.removeReq = req
	return nil
}

type pluginRuntimeTransportBasicProvider struct{}

func (*pluginRuntimeTransportBasicProvider) GetMetadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{Name: "runtime"}
}

func (*pluginRuntimeTransportBasicProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (*pluginRuntimeTransportBasicProvider) Close() error {
	return nil
}

func (*pluginRuntimeTransportBasicProvider) GetSupport(context.Context) (gestalt.RuntimeSupport, error) {
	return gestalt.RuntimeSupport{CanHostApps: true}, nil
}

func (*pluginRuntimeTransportBasicProvider) StartSession(context.Context, gestalt.StartRuntimeSessionRequest) (gestalt.RuntimeSession, error) {
	return gestalt.RuntimeSession{ID: "runtime-session-1", State: "ready"}, nil
}

func (*pluginRuntimeTransportBasicProvider) GetSession(context.Context, string) (gestalt.RuntimeSession, error) {
	return gestalt.RuntimeSession{ID: "runtime-session-1", State: "ready"}, nil
}

func (*pluginRuntimeTransportBasicProvider) ListSessions(context.Context, gestalt.ListRuntimeSessionsRequest) (gestalt.ListRuntimeSessionsResponse, error) {
	return gestalt.ListRuntimeSessionsResponse{
		Sessions: []gestalt.RuntimeSession{{ID: "runtime-session-1", State: "ready"}},
	}, nil
}

func (*pluginRuntimeTransportBasicProvider) StopSession(context.Context, string) error {
	return nil
}

func (*pluginRuntimeTransportBasicProvider) StartApp(context.Context, gestalt.StartHostedAppRequest) (gestalt.HostedApp, error) {
	return gestalt.HostedApp{ID: "plugin-1", SessionID: "runtime-session-1"}, nil
}

func TestRuntimeProviderWorkspaceTransport(t *testing.T) {
	socket := pluginRuntimeTransportSocket(t)
	t.Setenv(proto.EnvProviderSocket, socket)
	provider := &pluginRuntimeTransportProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeRuntimeProvider(ctx, provider)
	}()
	conn := newUnixConn(t, socket)
	client := proto.NewRuntimeProviderClient(conn)

	support, err := client.GetSupport(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetSupport: %v", err)
	}
	if !support.GetSupportsPrepareWorkspace() {
		t.Fatalf("supports_prepare_workspace = false, want true")
	}

	prepared, err := client.PrepareWorkspace(context.Background(), &proto.PrepareRuntimeWorkspaceRequest{
		SessionId:      "runtime-session-1",
		AgentSessionId: "agent-session-1",
		Workspace: &proto.AgentWorkspace{
			Cwd: "app",
			Checkouts: []*proto.AgentWorkspaceGitCheckout{{
				Url:  "git@github.com:valon-technologies/app.git",
				Ref:  "refs/heads/main",
				Path: "app",
			}},
		},
	})
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	if prepared.GetWorkspace().GetCwd() != "/tmp/runtime-session-1/workspaces/agent-session-1/app" {
		t.Fatalf("prepared cwd = %q", prepared.GetWorkspace().GetCwd())
	}
	if provider.prepareReq.SessionID != "runtime-session-1" || provider.prepareReq.AgentSessionID != "agent-session-1" {
		t.Fatalf("prepare request = %#v", provider.prepareReq)
	}
	if provider.prepareReq.Workspace == nil || provider.prepareReq.Workspace.CWD != "app" || len(provider.prepareReq.Workspace.Checkouts) != 1 {
		t.Fatalf("prepare workspace = %#v", provider.prepareReq.Workspace)
	}

	if _, err := client.RemoveWorkspace(context.Background(), &proto.RemoveRuntimeWorkspaceRequest{
		SessionId:      "runtime-session-1",
		AgentSessionId: "agent-session-1",
	}); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}
	if provider.removeReq.SessionID != "runtime-session-1" || provider.removeReq.AgentSessionID != "agent-session-1" {
		t.Fatalf("remove request = %#v", provider.removeReq)
	}

	hosted, err := client.StartApp(context.Background(), &proto.StartHostedAppRequest{
		SessionId: "runtime-session-1",
		AppName:   "github",
		Command:   "/bin/plugin",
		Workdir:   "/tmp/runtime-session-1/providers/github",
	})
	if err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	if hosted.GetSessionId() != "runtime-session-1" || hosted.GetAppName() != "github" {
		t.Fatalf("hosted app = %#v", hosted)
	}
	if provider.pluginReq.Workdir != "/tmp/runtime-session-1/providers/github" {
		t.Fatalf("start app workdir = %q", provider.pluginReq.Workdir)
	}

	if _, err := client.ListSessions(context.Background(), &proto.ListRuntimeSessionsRequest{
		PageToken: "next-page",
	}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if provider.listReq.PageSize != 0 || provider.listReq.PageToken != "next-page" {
		t.Fatalf("list sessions request = %#v, want token-only request forwarded without default page size", provider.listReq)
	}

	cancel()
	waitServeResult(t, errCh)
}

func TestRuntimeProviderWorkspaceTransportUnimplemented(t *testing.T) {
	socket := pluginRuntimeTransportSocket(t)
	t.Setenv(proto.EnvProviderSocket, socket)
	provider := &pluginRuntimeTransportBasicProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeRuntimeProvider(ctx, provider)
	}()
	conn := newUnixConn(t, socket)
	client := proto.NewRuntimeProviderClient(conn)

	_, err := client.PrepareWorkspace(context.Background(), &proto.PrepareRuntimeWorkspaceRequest{
		SessionId:      "runtime-session-1",
		AgentSessionId: "agent-session-1",
		Workspace:      &proto.AgentWorkspace{Cwd: "app"},
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("PrepareWorkspace error = %v, want Unimplemented", err)
	}

	cancel()
	waitServeResult(t, errCh)
}

func pluginRuntimeTransportSocket(t *testing.T) string {
	t.Helper()
	socket := filepath.Join("/tmp", "gestalt-runtime-"+strconv.Itoa(os.Getpid())+"-"+t.Name()+".sock")
	_ = os.Remove(socket)
	t.Cleanup(func() { _ = os.Remove(socket) })
	return socket
}

package gestalt_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type pluginRuntimeTransportProvider struct {
	prepareReq gestalt.PreparePluginRuntimeWorkspaceRequest
	removeReq  gestalt.RemovePluginRuntimeWorkspaceRequest
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

func (*pluginRuntimeTransportProvider) GetSupport(context.Context) (gestalt.PluginRuntimeSupport, error) {
	return gestalt.PluginRuntimeSupport{
		CanHostPlugins:           true,
		SupportsPrepareWorkspace: true,
	}, nil
}

func (*pluginRuntimeTransportProvider) StartSession(context.Context, gestalt.StartPluginRuntimeSessionRequest) (gestalt.PluginRuntimeSession, error) {
	return gestalt.PluginRuntimeSession{ID: "runtime-session-1", State: "ready"}, nil
}

func (*pluginRuntimeTransportProvider) GetSession(context.Context, string) (gestalt.PluginRuntimeSession, error) {
	return gestalt.PluginRuntimeSession{ID: "runtime-session-1", State: "ready"}, nil
}

func (*pluginRuntimeTransportProvider) ListSessions(context.Context) ([]gestalt.PluginRuntimeSession, error) {
	return []gestalt.PluginRuntimeSession{{ID: "runtime-session-1", State: "ready"}}, nil
}

func (*pluginRuntimeTransportProvider) StopSession(context.Context, string) error {
	return nil
}

func (*pluginRuntimeTransportProvider) StartPlugin(context.Context, gestalt.StartHostedPluginRequest) (gestalt.HostedPlugin, error) {
	return gestalt.HostedPlugin{ID: "plugin-1", SessionID: "runtime-session-1"}, nil
}

func (p *pluginRuntimeTransportProvider) PrepareWorkspace(_ context.Context, req gestalt.PreparePluginRuntimeWorkspaceRequest) (gestalt.PreparePluginRuntimeWorkspaceResponse, error) {
	p.prepareReq = req
	return gestalt.PreparePluginRuntimeWorkspaceResponse{
		Workspace: &gestalt.PreparedAgentWorkspace{
			Root: "/tmp/runtime-session-1/workspaces/agent-session-1",
			CWD:  "/tmp/runtime-session-1/workspaces/agent-session-1/app",
		},
	}, nil
}

func (p *pluginRuntimeTransportProvider) RemoveWorkspace(_ context.Context, req gestalt.RemovePluginRuntimeWorkspaceRequest) error {
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

func (*pluginRuntimeTransportBasicProvider) GetSupport(context.Context) (gestalt.PluginRuntimeSupport, error) {
	return gestalt.PluginRuntimeSupport{CanHostPlugins: true}, nil
}

func (*pluginRuntimeTransportBasicProvider) StartSession(context.Context, gestalt.StartPluginRuntimeSessionRequest) (gestalt.PluginRuntimeSession, error) {
	return gestalt.PluginRuntimeSession{ID: "runtime-session-1", State: "ready"}, nil
}

func (*pluginRuntimeTransportBasicProvider) GetSession(context.Context, string) (gestalt.PluginRuntimeSession, error) {
	return gestalt.PluginRuntimeSession{ID: "runtime-session-1", State: "ready"}, nil
}

func (*pluginRuntimeTransportBasicProvider) ListSessions(context.Context) ([]gestalt.PluginRuntimeSession, error) {
	return []gestalt.PluginRuntimeSession{{ID: "runtime-session-1", State: "ready"}}, nil
}

func (*pluginRuntimeTransportBasicProvider) StopSession(context.Context, string) error {
	return nil
}

func (*pluginRuntimeTransportBasicProvider) StartPlugin(context.Context, gestalt.StartHostedPluginRequest) (gestalt.HostedPlugin, error) {
	return gestalt.HostedPlugin{ID: "plugin-1", SessionID: "runtime-session-1"}, nil
}

func TestPluginRuntimeProviderWorkspaceTransport(t *testing.T) {
	socket := pluginRuntimeTransportSocket(t)
	t.Setenv(proto.EnvProviderSocket, socket)
	provider := &pluginRuntimeTransportProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServePluginRuntimeProvider(ctx, provider)
	}()
	conn := newUnixConn(t, socket)
	client := proto.NewPluginRuntimeProviderClient(conn)

	support, err := client.GetSupport(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetSupport: %v", err)
	}
	if !support.GetSupportsPrepareWorkspace() {
		t.Fatalf("supports_prepare_workspace = false, want true")
	}

	prepared, err := client.PrepareWorkspace(context.Background(), &proto.PreparePluginRuntimeWorkspaceRequest{
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

	if _, err := client.RemoveWorkspace(context.Background(), &proto.RemovePluginRuntimeWorkspaceRequest{
		SessionId:      "runtime-session-1",
		AgentSessionId: "agent-session-1",
	}); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}
	if provider.removeReq.SessionID != "runtime-session-1" || provider.removeReq.AgentSessionID != "agent-session-1" {
		t.Fatalf("remove request = %#v", provider.removeReq)
	}

	cancel()
	waitServeResult(t, errCh)
}

func TestPluginRuntimeProviderWorkspaceTransportUnimplemented(t *testing.T) {
	socket := pluginRuntimeTransportSocket(t)
	t.Setenv(proto.EnvProviderSocket, socket)
	provider := &pluginRuntimeTransportBasicProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServePluginRuntimeProvider(ctx, provider)
	}()
	conn := newUnixConn(t, socket)
	client := proto.NewPluginRuntimeProviderClient(conn)

	_, err := client.PrepareWorkspace(context.Background(), &proto.PreparePluginRuntimeWorkspaceRequest{
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

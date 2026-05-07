package gestalt_test

import (
	"context"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fullAgentProvider struct {
	gestalt.UnimplementedAgentProvider
	closeTracker
	configuredName string
}

func (p *fullAgentProvider) Configure(_ context.Context, name string, _ map[string]any) error {
	p.configuredName = name
	return nil
}

func (p *fullAgentProvider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindAgent,
		Name:        "stub-agent",
		DisplayName: "Stub Agent",
		Version:     "1.0",
	}
}

func (p *fullAgentProvider) CreateSession(_ context.Context, req *gestalt.CreateAgentProviderSessionRequest) (*gestalt.AgentSession, error) {
	return &gestalt.AgentSession{
		Id:           req.GetSessionId(),
		ProviderName: p.configuredName,
		Model:        req.GetModel(),
		ClientRef:    req.GetClientRef(),
		State:        gestalt.AgentSessionStateActive,
	}, nil
}

func (p *fullAgentProvider) GetCapabilities(context.Context, *gestalt.GetAgentProviderCapabilitiesRequest) (*gestalt.AgentProviderCapabilities, error) {
	return &gestalt.AgentProviderCapabilities{StreamingText: true, ToolCalls: true}, nil
}

func TestAgentProviderTypedTransportRoundTrip(t *testing.T) {
	socket := newSocketPath(t, "agent.sock")
	t.Setenv(proto.EnvProviderSocket, socket)
	t.Setenv(proto.EnvProviderName, "agent-test")

	ctx, cancel := context.WithCancel(context.Background())
	provider := &fullAgentProvider{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeAgentProvider(ctx, provider)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("agent provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	runtimeClient := proto.NewProviderLifecycleClient(conn)
	agentClient := proto.NewAgentProviderClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()

	meta, err := runtimeClient.GetProviderIdentity(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetProviderIdentity: %v", err)
	}
	if meta.GetKind() != proto.ProviderKind_PROVIDER_KIND_AGENT {
		t.Fatalf("kind = %v, want AGENT", meta.GetKind())
	}
	if _, err := runtimeClient.ConfigureProvider(rpcCtx, &proto.ConfigureProviderRequest{
		Name:            "agent-test",
		ProtocolVersion: proto.CurrentProtocolVersion,
	}); err != nil {
		t.Fatalf("ConfigureProvider: %v", err)
	}

	session, err := agentClient.CreateSession(rpcCtx, &proto.CreateAgentProviderSessionRequest{
		SessionId: "session-1",
		Model:     "gpt-5.1",
		ClientRef: "client-session-1",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.GetId() != "session-1" || session.GetProviderName() != "agent-test" {
		t.Fatalf("CreateSession = %+v, want session id and configured provider name", session)
	}

	capabilities, err := agentClient.GetCapabilities(rpcCtx, &proto.GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if !capabilities.GetStreamingText() || !capabilities.GetToolCalls() {
		t.Fatalf("GetCapabilities = %+v, want streaming text and tool calls", capabilities)
	}
}

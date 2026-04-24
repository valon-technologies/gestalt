package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type pluginInvokerTransportHarness struct {
	proto.UnimplementedPluginInvokerServer

	mu       sync.Mutex
	requests []*proto.PluginInvokeRequest
	tokens   []string
}

func (h *pluginInvokerTransportHarness) Invoke(ctx context.Context, req *proto.PluginInvokeRequest) (*proto.OperationResult, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.requests = append(h.requests, &proto.PluginInvokeRequest{
		InvocationToken: req.GetInvocationToken(),
		Plugin:          req.GetPlugin(),
		Operation:       req.GetOperation(),
		Params:          cloneStruct(req.GetParams()),
		Connection:      req.GetConnection(),
		Instance:        req.GetInstance(),
	})
	h.mu.Unlock()

	return &proto.OperationResult{Status: 207, Body: "relay-ok"}, nil
}

func cloneStruct(src *structpb.Struct) *structpb.Struct {
	if src == nil {
		return nil
	}
	return gproto.Clone(src).(*structpb.Struct)
}

func TestTransport_PluginInvokerTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &pluginInvokerTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterPluginInvokerServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvPluginInvokerSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvPluginInvokerSocketToken, "relay-token-go")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	t.Setenv("https_proxy", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")

	client, err := gestalt.Invoker("parent-token")
	if err != nil {
		t.Fatalf("Invoker: %v", err)
	}
	defer func() { _ = client.Close() }()

	result, err := client.Invoke(context.Background(), "github", "get_issue", map[string]any{
		"issue_number": 42,
	}, nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != 207 || result.Body != "relay-ok" {
		t.Fatalf("Invoke result = %+v, want status=207 body=relay-ok", result)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 1 || harness.tokens[0] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go]", harness.tokens)
	}
	if len(harness.requests) != 1 {
		t.Fatalf("invoke requests len = %d, want 1", len(harness.requests))
	}
	if harness.requests[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("invocation token = %q, want %q", harness.requests[0].GetInvocationToken(), "parent-token")
	}
	if harness.requests[0].GetPlugin() != "github" || harness.requests[0].GetOperation() != "get_issue" {
		t.Fatalf("invoke target = %s.%s, want github.get_issue", harness.requests[0].GetPlugin(), harness.requests[0].GetOperation())
	}
}

package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type pluginInvokerTransportHarness struct {
	proto.UnimplementedPluginInvokerServer

	mu       sync.Mutex
	requests []*proto.PluginInvokeRequest
	graphQL  []*proto.PluginInvokeGraphQLRequest
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
		IdempotencyKey:  req.GetIdempotencyKey(),
	})
	h.mu.Unlock()

	return &proto.OperationResult{Status: 207, Body: "relay-ok"}, nil
}

func (h *pluginInvokerTransportHarness) InvokeGraphQL(ctx context.Context, req *proto.PluginInvokeGraphQLRequest) (*proto.OperationResult, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.graphQL = append(h.graphQL, &proto.PluginInvokeGraphQLRequest{
		InvocationToken: req.GetInvocationToken(),
		Plugin:          req.GetPlugin(),
		Document:        req.GetDocument(),
		Variables:       cloneStruct(req.GetVariables()),
		Connection:      req.GetConnection(),
		Instance:        req.GetInstance(),
		IdempotencyKey:  req.GetIdempotencyKey(),
	})
	h.mu.Unlock()

	return &proto.OperationResult{Status: 208, Body: "graphql-ok"}, nil
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
	}, &gestalt.InvokeOptions{
		IdempotencyKey: " issue-42-create ",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != 207 || result.Body != "relay-ok" {
		t.Fatalf("Invoke result = %+v, want status=207 body=relay-ok", result)
	}
	graphQLResult, err := client.InvokeGraphQL(context.Background(), "linear", " query { viewer { id } } ", map[string]any{
		"team": "eng",
	}, &gestalt.InvokeOptions{
		IdempotencyKey: " graphql-call-42 ",
	})
	if err != nil {
		t.Fatalf("InvokeGraphQL: %v", err)
	}
	if graphQLResult.Status != 208 || graphQLResult.Body != "graphql-ok" {
		t.Fatalf("InvokeGraphQL result = %+v, want status=208 body=graphql-ok", graphQLResult)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 2 || harness.tokens[0] != "relay-token-go" || harness.tokens[1] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want two relay-token-go entries", harness.tokens)
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
	if harness.requests[0].GetIdempotencyKey() != "issue-42-create" {
		t.Fatalf("idempotency key = %q, want issue-42-create", harness.requests[0].GetIdempotencyKey())
	}
	if len(harness.graphQL) != 1 {
		t.Fatalf("graphql requests len = %d, want 1", len(harness.graphQL))
	}
	if harness.graphQL[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("graphql invocation token = %q, want parent-token", harness.graphQL[0].GetInvocationToken())
	}
	if harness.graphQL[0].GetPlugin() != "linear" || harness.graphQL[0].GetDocument() != "query { viewer { id } }" {
		t.Fatalf("graphql request = %s %q, want linear trimmed document", harness.graphQL[0].GetPlugin(), harness.graphQL[0].GetDocument())
	}
	if harness.graphQL[0].GetIdempotencyKey() != "graphql-call-42" {
		t.Fatalf("graphql idempotency key = %q, want graphql-call-42", harness.graphQL[0].GetIdempotencyKey())
	}
}

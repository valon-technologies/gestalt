package gestalt_test

import (
	"context"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type invokeIssueParams struct {
	IssueNumber int `json:"issue_number"`
}

type invokeOmitEmptyParams struct {
	IssueNumber int               `json:"issue_number"`
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type pointerMarshaler struct {
	Hidden string
}

func (*pointerMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`{"redacted":true}`), nil
}

type EmbeddedIssueParams struct {
	IssueNumber int `json:"issue_number"`
}

type embeddedIssueParams struct {
	EmbeddedIssueParams
}

type pluginAppTransportHarness struct {
	proto.UnimplementedAppServer

	mu       sync.Mutex
	requests []*proto.AppInvokeRequest
	graphQL  []*proto.AppInvokeGraphQLRequest
	tokens   []string
}

func (h *pluginAppTransportHarness) Invoke(ctx context.Context, req *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.requests = append(h.requests, &proto.AppInvokeRequest{
		InvocationToken: req.GetInvocationToken(),
		App:             req.GetApp(),
		Operation:       req.GetOperation(),
		Params:          cloneStruct(req.GetParams()),
		Connection:      req.GetConnection(),
		Instance:        req.GetInstance(),
		IdempotencyKey:  req.GetIdempotencyKey(),
	})
	h.mu.Unlock()

	return &proto.OperationResult{Status: 207, Body: "relay-ok"}, nil
}

func (h *pluginAppTransportHarness) InvokeGraphQL(ctx context.Context, req *proto.AppInvokeGraphQLRequest) (*proto.OperationResult, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.graphQL = append(h.graphQL, &proto.AppInvokeGraphQLRequest{
		InvocationToken: req.GetInvocationToken(),
		App:             req.GetApp(),
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

func TestTransport_AppTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &pluginAppTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterAppServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	t.Setenv("https_proxy", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")

	client, err := gestalt.NewApp("parent-token")
	if err != nil {
		t.Fatalf("App: %v", err)
	}

	result, err := client.Invoke(context.Background(), "github", "get_issue", invokeIssueParams{
		IssueNumber: 42,
	}, &gestalt.InvokeOptions{
		IdempotencyKey: " issue-42-create ",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != 207 || result.Body != "relay-ok" {
		t.Fatalf("Invoke result = %+v, want status=207 body=relay-ok", result)
	}
	result, err = client.Invoke(context.Background(), "github", "get_issue", invokeOmitEmptyParams{
		IssueNumber: 43,
		Tags:        []string{},
		Metadata:    map[string]string{},
	}, nil)
	if err != nil {
		t.Fatalf("Invoke omitempty params: %v", err)
	}
	if result.Status != 207 || result.Body != "relay-ok" {
		t.Fatalf("Invoke omitempty result = %+v, want status=207 body=relay-ok", result)
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
	if _, err := client.Invoke(context.Background(), "github", "bad", time.Now(), nil); err == nil {
		t.Fatal("Invoke(time.Time) error = nil, want error")
	}
	if _, err := client.Invoke(context.Background(), "github", "bad", map[int]any{1: "bad"}, nil); err == nil {
		t.Fatal("Invoke(non-string map key) error = nil, want error")
	}
	if _, err := client.Invoke(context.Background(), "github", "bad", map[string]any{"score": math.NaN()}, nil); err == nil {
		t.Fatal("Invoke(NaN) error = nil, want error")
	}
	if _, err := client.Invoke(context.Background(), "github", "bad", pointerMarshaler{Hidden: "secret"}, nil); err == nil {
		t.Fatal("Invoke(pointer json.Marshaler) error = nil, want error")
	}
	if _, err := client.Invoke(context.Background(), "github", "bad", map[string]any{"bad": pointerMarshaler{Hidden: "secret"}}, nil); err == nil {
		t.Fatal("Invoke(map pointer json.Marshaler) error = nil, want error")
	}
	if _, err := client.Invoke(context.Background(), "github", "bad", embeddedIssueParams{EmbeddedIssueParams: EmbeddedIssueParams{IssueNumber: 42}}, nil); err == nil {
		t.Fatal("Invoke(anonymous embedded field) error = nil, want error")
	}
	if _, err := client.Invoke(context.Background(), "github", "bad", "not-object", nil); err == nil {
		t.Fatal("Invoke(non-object) error = nil, want error")
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 3 || harness.tokens[0] != "relay-token-go" || harness.tokens[1] != "relay-token-go" || harness.tokens[2] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want three relay-token-go entries", harness.tokens)
	}
	if len(harness.requests) != 2 {
		t.Fatalf("invoke requests len = %d, want 2", len(harness.requests))
	}
	if harness.requests[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("invocation token = %q, want %q", harness.requests[0].GetInvocationToken(), "parent-token")
	}
	if harness.requests[0].GetApp() != "github" || harness.requests[0].GetOperation() != "get_issue" {
		t.Fatalf("invoke target = %s.%s, want github.get_issue", harness.requests[0].GetApp(), harness.requests[0].GetOperation())
	}
	if harness.requests[0].GetIdempotencyKey() != "issue-42-create" {
		t.Fatalf("idempotency key = %q, want issue-42-create", harness.requests[0].GetIdempotencyKey())
	}
	if got := harness.requests[0].GetParams().AsMap(); got["issue_number"] != float64(42) {
		t.Fatalf("invoke params = %#v, want issue_number=42", got)
	}
	if got := harness.requests[1].GetParams().AsMap(); len(got) != 1 || got["issue_number"] != float64(43) {
		t.Fatalf("omitempty invoke params = %#v, want only issue_number=43", got)
	}
	if len(harness.graphQL) != 1 {
		t.Fatalf("graphql requests len = %d, want 1", len(harness.graphQL))
	}
	if harness.graphQL[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("graphql invocation token = %q, want parent-token", harness.graphQL[0].GetInvocationToken())
	}
	if harness.graphQL[0].GetApp() != "linear" || harness.graphQL[0].GetDocument() != "query { viewer { id } }" {
		t.Fatalf("graphql request = %s %q, want linear trimmed document", harness.graphQL[0].GetApp(), harness.graphQL[0].GetDocument())
	}
	if harness.graphQL[0].GetIdempotencyKey() != "graphql-call-42" {
		t.Fatalf("graphql idempotency key = %q, want graphql-call-42", harness.graphQL[0].GetIdempotencyKey())
	}
}

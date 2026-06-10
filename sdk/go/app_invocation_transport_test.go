package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
)

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
	h.requests = append(h.requests, gproto.Clone(req).(*proto.AppInvokeRequest))
	h.mu.Unlock()

	return &proto.OperationResult{
		Status: 207,
		Headers: map[string]*proto.StringList{
			"Location": &proto.StringList{Values: []string{"https://example.test/created"}},
		},
		Body: []byte(`{"status":"success","data":"relay-ok"}`),
	}, nil
}

func (h *pluginAppTransportHarness) InvokeGraphQL(ctx context.Context, req *proto.AppInvokeGraphQLRequest) (*proto.OperationResult, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.graphQL = append(h.graphQL, gproto.Clone(req).(*proto.AppInvokeGraphQLRequest))
	h.mu.Unlock()

	return &proto.OperationResult{
		Status: 208,
		Body:   []byte("graphql-ok"),
	}, nil
}

func appTransportRequestContext() *client.RequestContext {
	return &client.RequestContext{
		Subject: &client.SubjectContext{
			Id:                  "user:transport",
			CredentialSubjectId: "user:transport",
			Email:               "transport@example.test",
		},
	}
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

	app, err := client.ConnectApp(context.Background(), "", client.WithRequestContext(appTransportRequestContext()))
	if err != nil {
		t.Fatalf("App: %v", err)
	}

	result, err := app.InvokeRaw(context.Background(), &client.AppInvokeRequest{
		App:            "github",
		Operation:      "get_issue",
		Params:         map[string]any{"issue_number": 42},
		IdempotencyKey: "issue-42-create",
		Context: &client.RequestContext{
			Subject: appTransportRequestContext().Subject,
			Workflow: map[string]any{
				"runId": "run-42",
				"step":  map[string]any{"id": "notify"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != 207 || string(result.Body) != `{"status":"success","data":"relay-ok"}` {
		t.Fatalf("Invoke result = %+v, want status=207 success envelope", result)
	}
	location := result.Headers["Location"]
	if location == nil || len(location.Values) != 1 || location.Values[0] != "https://example.test/created" {
		t.Fatalf("Invoke Location header = %#v, want https://example.test/created", location)
	}

	decoded, err := app.Invoke(context.Background(), "github", "get_issue", "", "", "", "", map[string]any{
		"issue_number": 43,
	})
	if err != nil {
		t.Fatalf("Invoke flattened params: %v", err)
	}
	if decoded != "relay-ok" {
		t.Fatalf("Invoke flattened result = %#v, want decoded envelope data relay-ok", decoded)
	}

	graphQLResult, err := app.InvokeGraphQL(context.Background(), "linear", "query { viewer { id } }", "", "", "graphql-call-42", map[string]any{
		"team": "eng",
	})
	if err != nil {
		t.Fatalf("InvokeGraphQL: %v", err)
	}
	if graphQLResult.Status != 208 || string(graphQLResult.Body) != "graphql-ok" {
		t.Fatalf("InvokeGraphQL result = %+v, want status=208 body=graphql-ok", graphQLResult)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 3 || harness.tokens[0] != "relay-token-go" || harness.tokens[1] != "relay-token-go" || harness.tokens[2] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want three relay-token-go entries", harness.tokens)
	}
	if len(harness.requests) != 2 {
		t.Fatalf("invoke requests len = %d, want 2", len(harness.requests))
	}
	if got := harness.requests[0].GetContext().GetSubject().GetId(); got != "user:transport" {
		t.Fatalf("subject = %q, want user:transport", got)
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
	if got := harness.requests[0].GetContext().GetWorkflow().AsMap(); got["runId"] != "run-42" {
		t.Fatalf("invoke workflow context = %#v, want runId=run-42", got)
	}
	if got := harness.requests[1].GetContext().GetSubject().GetId(); got != "user:transport" {
		t.Fatalf("flattened invoke subject = %q, want injected ambient context", got)
	}
	if got := harness.requests[1].GetParams().AsMap(); len(got) != 1 || got["issue_number"] != float64(43) {
		t.Fatalf("flattened invoke params = %#v, want only issue_number=43", got)
	}
	if len(harness.graphQL) != 1 {
		t.Fatalf("graphql requests len = %d, want 1", len(harness.graphQL))
	}
	if got := harness.graphQL[0].GetContext().GetSubject().GetId(); got != "user:transport" {
		t.Fatalf("graphql subject = %q, want user:transport", got)
	}
	if harness.graphQL[0].GetApp() != "linear" || harness.graphQL[0].GetDocument() != "query { viewer { id } }" {
		t.Fatalf("graphql request = %s %q, want linear document", harness.graphQL[0].GetApp(), harness.graphQL[0].GetDocument())
	}
	if harness.graphQL[0].GetIdempotencyKey() != "graphql-call-42" {
		t.Fatalf("graphql idempotency key = %q, want graphql-call-42", harness.graphQL[0].GetIdempotencyKey())
	}
}

func TestTransport_AppCallerAndWorkflowContext(t *testing.T) {
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

	app, err := client.ConnectApp(context.Background(), "", client.WithRequestContext(&client.RequestContext{
		Subject: &client.SubjectContext{
			Id:                  "service_account:workflow-runner",
			CredentialSubjectId: "service_account:workflow-runner",
		},
		Caller: &client.ProviderContext{Kind: "workflow", Name: "temporal"},
		Workflow: map[string]any{
			"providerName":  "temporal",
			"runId":         "run-1",
			"currentStepId": "react",
		},
	}))
	if err != nil {
		t.Fatalf("App: %v", err)
	}
	if _, err := app.InvokeRaw(context.Background(), &client.AppInvokeRequest{
		App:       "slack",
		Operation: "events.addReaction",
		Params:    map[string]any{},
	}); err != nil {
		t.Fatalf("InvokeRaw: %v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.requests) != 1 {
		t.Fatalf("invoke requests len = %d, want 1", len(harness.requests))
	}
	got := harness.requests[0].GetContext()
	if got.GetCaller().GetKind() != "workflow" || got.GetCaller().GetName() != "temporal" {
		t.Fatalf("caller = %#v, want workflow temporal", got.GetCaller())
	}
	if got.GetSubject().GetId() != "service_account:workflow-runner" {
		t.Fatalf("subject = %q, want workflow runner", got.GetSubject().GetId())
	}
	if got.GetWorkflow().AsMap()["currentStepId"] != "react" {
		t.Fatalf("workflow context = %#v, want currentStepId=react", got.GetWorkflow().AsMap())
	}
}

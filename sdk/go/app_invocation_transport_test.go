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
	h.requests = append(h.requests, gproto.Clone(req).(*proto.AppInvokeRequest))
	h.mu.Unlock()

	return &proto.OperationResult{
		Status: 207,
		Headers: map[string]*proto.StringList{
			"Location": &proto.StringList{Values: []string{"https://example.test/created"}},
		},
		Body: "relay-ok",
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
		Body:   "graphql-ok",
	}, nil
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

	client, err := gestalt.NewAppFromRequest(appTransportRequest())
	if err != nil {
		t.Fatalf("App: %v", err)
	}

	result, err := client.Invoke(context.Background(), "github", "get_issue", invokeIssueParams{
		IssueNumber: 42,
	}, &gestalt.InvokeOptions{
		IdempotencyKey: " issue-42-create ",
		WorkflowContext: map[string]any{
			"runId": "run-42",
			"step":  map[string]any{"id": "notify"},
		},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != 207 || result.Body != "relay-ok" {
		t.Fatalf("Invoke result = %+v, want status=207 body=relay-ok", result)
	}
	if got := result.Headers.Get("Location"); got != "https://example.test/created" {
		t.Fatalf("Invoke Location header = %q, want https://example.test/created", got)
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
	}, &gestalt.InvokeGraphQLOptions{
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
	if got := harness.requests[1].GetParams().AsMap(); len(got) != 1 || got["issue_number"] != float64(43) {
		t.Fatalf("omitempty invoke params = %#v, want only issue_number=43", got)
	}
	if len(harness.graphQL) != 1 {
		t.Fatalf("graphql requests len = %d, want 1", len(harness.graphQL))
	}
	if got := harness.graphQL[0].GetContext().GetSubject().GetId(); got != "user:transport" {
		t.Fatalf("graphql subject = %q, want user:transport", got)
	}
	if harness.graphQL[0].GetApp() != "linear" || harness.graphQL[0].GetDocument() != "query { viewer { id } }" {
		t.Fatalf("graphql request = %s %q, want linear trimmed document", harness.graphQL[0].GetApp(), harness.graphQL[0].GetDocument())
	}
	if harness.graphQL[0].GetIdempotencyKey() != "graphql-call-42" {
		t.Fatalf("graphql idempotency key = %q, want graphql-call-42", harness.graphQL[0].GetIdempotencyKey())
	}
}

func TestTransport_AppRequestBuilderCallerAndWorkflowContext(t *testing.T) {
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

	req, err := gestalt.NewRequest(gestalt.RequestInput{
		Subject: gestalt.Subject{ID: "service_account:workflow-runner", CredentialSubjectID: "service_account:workflow-runner"},
		Caller:  gestalt.RequestCaller{Kind: gestalt.RequestCallerKindWorkflow, Name: "temporal"},
		WorkflowContext: map[string]any{
			"providerName":  "temporal",
			"runId":         "run-1",
			"currentStepId": "react",
		},
	})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	client, err := gestalt.NewAppFromRequest(req)
	if err != nil {
		t.Fatalf("App: %v", err)
	}
	if _, err := client.Invoke(context.Background(), "slack", "events.addReaction", map[string]any{}, nil); err != nil {
		t.Fatalf("Invoke: %v", err)
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

func appTransportRequest() gestalt.Request {
	return gestalt.Request{
		Subject: gestalt.Subject{
			ID:                  "user:transport",
			CredentialSubjectID: "user:transport",
			Email:               "transport@example.test",
		},
	}
}

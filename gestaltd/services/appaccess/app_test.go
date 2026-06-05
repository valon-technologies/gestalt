package appaccess

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type recordingAppInvocation struct {
	idempotencyKey      string
	internalConnection  bool
	connection          string
	workflowContext     map[string]any
	requestID           string
	depth               int
	callChain           []string
	clientIP            string
	subjectID           string
	credentialSubjectID string
	agentSubjectID      string
	runAsSubjectID      string
	providerName        string
	instance            string
	operation           string
	credentialMode      core.ConnectionMode
	params              map[string]any
	graphQLProviderName string
	graphQLDocument     string
	graphQLVariables    map[string]any
}

func (i *recordingAppInvocation) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	runAsAudit := invocation.RunAsAuditFromContext(ctx)
	i.idempotencyKey = invocation.IdempotencyKeyFromContext(ctx)
	i.internalConnection = invocation.InternalConnectionAccessFromContext(ctx)
	i.connection = invocation.ConnectionFromContext(ctx)
	i.workflowContext = invocation.WorkflowContextFromContext(ctx)
	if meta := invocation.MetaFromContext(ctx); meta != nil {
		i.requestID = meta.RequestID
		i.depth = meta.Depth
		i.callChain = append([]string(nil), meta.CallChain...)
	}
	i.clientIP = invocation.RequestMetaFromContext(ctx).ClientIP
	if p != nil {
		i.subjectID = p.SubjectID
		i.credentialSubjectID = p.CredentialSubjectID
	}
	if runAsAudit.AgentSubject != nil {
		i.agentSubjectID = runAsAudit.AgentSubject.SubjectID
	}
	if runAsAudit.RunAsSubject != nil {
		i.runAsSubjectID = runAsAudit.RunAsSubject.SubjectID
	}
	i.providerName = providerName
	i.instance = instance
	i.operation = operation
	i.credentialMode = invocation.CredentialModeOverrideFromContext(ctx)
	i.params = params
	return &core.OperationResult{Status: 202, Body: "accepted"}, nil
}

func (i *recordingAppInvocation) InvokeGraphQL(ctx context.Context, _ *principal.Principal, providerName, _ string, request invocation.GraphQLRequest) (*core.OperationResult, error) {
	i.credentialMode = invocation.CredentialModeOverrideFromContext(ctx)
	i.graphQLProviderName = providerName
	i.graphQLDocument = request.Document
	i.graphQLVariables = request.Variables
	return &core.OperationResult{Status: 208, Body: "graphql-accepted"}, nil
}

type recordingAuthorizationProvider struct {
	allowed  bool
	requests []*proto.CheckAccessRequest
}

func (p *recordingAuthorizationProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.requests = append(p.requests, req)
	return &proto.CheckAccessResponse{Allowed: p.allowed}, nil
}

func (p *recordingAuthorizationProvider) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return nil, fmt.Errorf("unexpected CheckAccessMany")
}
func (p *recordingAuthorizationProvider) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return nil, fmt.Errorf("unexpected ListRelationships")
}
func (p *recordingAuthorizationProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return nil, fmt.Errorf("unexpected AddRelationship")
}
func (p *recordingAuthorizationProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return nil, fmt.Errorf("unexpected DeleteRelationship")
}
func (p *recordingAuthorizationProvider) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return nil, fmt.Errorf("unexpected SetAuthorizationState")
}
func (p *recordingAuthorizationProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return nil, fmt.Errorf("unexpected GetActiveModelRef")
}
func (p *recordingAuthorizationProvider) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return nil, fmt.Errorf("unexpected SetActiveModel")
}
func (p *recordingAuthorizationProvider) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return nil, fmt.Errorf("unexpected ListActiveModelResourceTypes")
}
func (p *recordingAuthorizationProvider) Ping(context.Context) error { return nil }
func (p *recordingAuthorizationProvider) Close() error               { return nil }

func TestAppServerInvokeUsesRequestContextAndAuthorization(t *testing.T) {
	t.Parallel()

	authz := &recordingAuthorizationProvider{allowed: true}
	invoker := &recordingAppInvocation{}
	server := NewAppServer(
		invoker,
		WithAuthorizationProvider(authz),
		WithCallerApp("caller", AppAccessProfiles{
			"target": {
				Operations: map[string]core.ConnectionMode{"do.thing": core.ConnectionModeSubject},
				OperationDelegations: map[string]AppAccessDelegation{
					"do.thing": {RunAs: &core.RunAsSubject{
						SubjectID:           "service_account:runner",
						CredentialSubjectID: "service_account:runner",
					}},
				},
			},
		}),
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	params, err := structpb.NewStruct(map[string]any{"title": "hello"})
	if err != nil {
		t.Fatalf("NewStruct params: %v", err)
	}

	resp, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:            " target ",
		Operation:      " do.thing ",
		Params:         params,
		IdempotencyKey: " call-123 ",
		Context: requestContext(t, "caller", map[string]any{
			"providerName": "workflow-provider",
			"runId":        "run-123",
		}),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetStatus() != 202 || resp.GetBody() != "accepted" {
		t.Fatalf("Invoke response = %+v, want accepted", resp)
	}

	if len(authz.requests) != 1 {
		t.Fatalf("authorization requests = %d, want 1", len(authz.requests))
	}
	if got := authz.requests[0].GetSubject().GetType() + ":" + authz.requests[0].GetSubject().GetId(); got != "app:caller" {
		t.Fatalf("authorization subject = %q, want app:caller", got)
	}
	if got := authz.requests[0].GetAction().GetName(); got != "invoke" {
		t.Fatalf("authorization action = %q, want invoke", got)
	}
	if got := authz.requests[0].GetResource().GetType() + ":" + authz.requests[0].GetResource().GetId(); got != "gestalt.app.operation:target/operations/do.thing" {
		t.Fatalf("authorization resource = %q", got)
	}
	if invoker.subjectID != "service_account:runner" || invoker.credentialSubjectID != "service_account:runner" {
		t.Fatalf("principal = %s/%s, want delegated runner", invoker.subjectID, invoker.credentialSubjectID)
	}
	if invoker.agentSubjectID != "user:test-user" || invoker.runAsSubjectID != "service_account:runner" {
		t.Fatalf("run-as audit = %s/%s, want user:test-user/service_account:runner", invoker.agentSubjectID, invoker.runAsSubjectID)
	}
	if invoker.credentialMode != core.ConnectionModeSubject {
		t.Fatalf("credential mode = %q, want subject", invoker.credentialMode)
	}
	if got := invocation.WorkflowContextString(invoker.workflowContext, "runId"); got != "run-123" {
		t.Fatalf("workflow runId = %q, want run-123", got)
	}
	if invoker.requestID != "req-1" || invoker.depth != 2 || !slices.Equal(invoker.callChain, []string{"caller/default/start"}) {
		t.Fatalf("invocation meta = %s/%d/%v", invoker.requestID, invoker.depth, invoker.callChain)
	}
	if !invoker.internalConnection || invoker.connection != "ctx-connection" || invoker.clientIP != "203.0.113.10" {
		t.Fatalf("context propagation internal=%v connection=%q clientIP=%q", invoker.internalConnection, invoker.connection, invoker.clientIP)
	}
	if invoker.idempotencyKey != "call-123" || invoker.params["title"] != "hello" {
		t.Fatalf("request propagation idempotency=%q params=%v", invoker.idempotencyKey, invoker.params)
	}
}

func TestAppServerInvokeGraphQLAuthorizesSurface(t *testing.T) {
	t.Parallel()

	authz := &recordingAuthorizationProvider{allowed: true}
	invoker := &recordingAppInvocation{}
	server := NewAppServer(invoker, WithAuthorizationProvider(authz), WithCallerApp("caller", AppAccessProfiles{
		"graph": {
			Surfaces: map[string]core.ConnectionMode{"graphql": core.ConnectionModeSubject},
		},
	}))
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	variables, err := structpb.NewStruct(map[string]any{"team": "eng"})
	if err != nil {
		t.Fatalf("NewStruct variables: %v", err)
	}

	resp, err := client.InvokeGraphQL(context.Background(), &proto.AppInvokeGraphQLRequest{
		App:       " graph ",
		Document:  " query Viewer { viewer { id } } ",
		Variables: variables,
		Context:   requestContext(t, "caller", nil),
	})
	if err != nil {
		t.Fatalf("InvokeGraphQL: %v", err)
	}
	if resp.GetStatus() != 208 || resp.GetBody() != "graphql-accepted" {
		t.Fatalf("InvokeGraphQL response = %+v, want graphql accepted", resp)
	}
	if got := authz.requests[0].GetResource().GetType() + ":" + authz.requests[0].GetResource().GetId(); got != "gestalt.app.surface:graph/surfaces/graphql" {
		t.Fatalf("authorization resource = %q", got)
	}
	if invoker.credentialMode != core.ConnectionModeSubject {
		t.Fatalf("credential mode = %q, want subject", invoker.credentialMode)
	}
	if invoker.graphQLProviderName != "graph" || invoker.graphQLDocument != "query Viewer { viewer { id } }" || invoker.graphQLVariables["team"] != "eng" {
		t.Fatalf("graphql request = %s %q %v", invoker.graphQLProviderName, invoker.graphQLDocument, invoker.graphQLVariables)
	}
}

func TestAppServerInvokeFailsClosedWithoutAuthorizedCaller(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		authz   *recordingAuthorizationProvider
		context *proto.RequestContext
		want    codes.Code
	}{
		{name: "missing authorization provider", context: requestContext(t, "caller", nil), want: codes.FailedPrecondition},
		{name: "wrong caller", authz: &recordingAuthorizationProvider{allowed: true}, context: requestContext(t, "forged", nil), want: codes.PermissionDenied},
		{name: "denied", authz: &recordingAuthorizationProvider{allowed: false}, context: requestContext(t, "caller", nil), want: codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := []AppServerOption{WithCallerApp("caller", nil)}
			if tc.authz != nil {
				opts = append(opts, WithAuthorizationProvider(tc.authz))
			}
			server := NewAppServer(&recordingAppInvocation{}, opts...)
			client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
				proto.RegisterAppServer(srv, server)
			}))
			_, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
				App:       "target",
				Operation: "do.thing",
				Context:   tc.context,
			})
			if got := status.Code(err); got != tc.want {
				t.Fatalf("Invoke status = %s, want %s (err=%v)", got, tc.want, err)
			}
		})
	}
}

func requestContext(t *testing.T, caller string, workflow map[string]any) *proto.RequestContext {
	t.Helper()

	var workflowStruct *structpb.Struct
	if workflow != nil {
		var err error
		workflowStruct, err = structpb.NewStruct(workflow)
		if err != nil {
			t.Fatalf("NewStruct workflow: %v", err)
		}
	}
	return &proto.RequestContext{
		Subject: &proto.SubjectContext{
			Id:                  "user:test-user",
			CredentialSubjectId: "user:test-user",
			Email:               "test@example.com",
		},
		Credential: &proto.CredentialContext{
			Mode:       "subject",
			SubjectId:  "user:test-user",
			Connection: "ctx-connection",
			Instance:   "ctx-instance",
		},
		Workflow: workflowStruct,
		Caller: &proto.ProviderContext{
			Kind: "app",
			Name: caller,
		},
		Invocation: &proto.InvocationContext{
			RequestId:                "req-1",
			Depth:                    2,
			CallChain:                []string{"caller/default/start"},
			InternalConnectionAccess: true,
			Connection:               "ctx-connection",
		},
		RequestMeta: &proto.RequestMetaContext{
			ClientIp: "203.0.113.10",
		},
	}
}

package appaccess

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
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
	toolRefs            []coreagent.ToolRef
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
		i.credentialSubjectID = p.SubjectID
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
	if refs := invocation.ToolRefsContextFromContext(ctx); refs.Set {
		i.toolRefs = append([]coreagent.ToolRef(nil), refs.Refs...)
	}
	i.params = params
	return &core.OperationResult{Status: 202, Body: []byte("accepted")}, nil
}

func (i *recordingAppInvocation) InvokeGraphQL(ctx context.Context, _ *principal.Principal, providerName, _ string, request invocation.GraphQLRequest) (*core.OperationResult, error) {
	i.credentialMode = invocation.CredentialModeOverrideFromContext(ctx)
	i.graphQLProviderName = providerName
	i.graphQLDocument = request.Document
	i.graphQLVariables = request.Variables
	return &core.OperationResult{Status: 208, Body: []byte("graphql-accepted")}, nil
}

type recordingAgentAppAuthorizer struct {
	requests []invocation.AgentAppAuthorizationRequest
	response invocation.AgentAppAuthorization
	err      error
}

func (a *recordingAgentAppAuthorizer) AuthorizeAppInvocation(_ context.Context, req invocation.AgentAppAuthorizationRequest) (invocation.AgentAppAuthorization, error) {
	a.requests = append(a.requests, req)
	return a.response, a.err
}

func TestAppServerInvokeOpenInvocationPropagatesRequestContext(t *testing.T) {
	t.Parallel()

	invoker := &recordingAppInvocation{}
	server := NewAppServer(invoker)
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
		CredentialMode: "subject",
		Context: requestContext(t, "caller", map[string]any{
			"providerName": "workflow-provider",
			"runId":        "run-123",
		}),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetStatus() != 202 || string(resp.GetBody()) != "accepted" {
		t.Fatalf("Invoke response = %+v, want accepted", resp)
	}
	if invoker.subjectID != "user:test-user" || invoker.credentialSubjectID != "user:test-user" {
		t.Fatalf("principal = %s/%s, want caller subject", invoker.subjectID, invoker.credentialSubjectID)
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

func TestAppServerInvokeAppliesRequestRunAs(t *testing.T) {
	t.Parallel()

	invoker := &recordingAppInvocation{}
	server := NewAppServer(invoker)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))

	resp, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "target",
		Operation: "do.thing",
		RunAs: &proto.SubjectContext{
			Id: "service_account:runner",
		},
		Context: requestContext(t, "caller", nil),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetStatus() != 202 {
		t.Fatalf("Invoke response = %+v, want accepted", resp)
	}
	if invoker.subjectID != "service_account:runner" || invoker.credentialSubjectID != "service_account:runner" {
		t.Fatalf("principal = %s/%s, want delegated runner", invoker.subjectID, invoker.credentialSubjectID)
	}
	if invoker.agentSubjectID != "user:test-user" || invoker.runAsSubjectID != "service_account:runner" {
		t.Fatalf("run-as audit = %s/%s, want user:test-user/service_account:runner", invoker.agentSubjectID, invoker.runAsSubjectID)
	}
}

func TestAppServerInvokeIgnoresEmptyRequestRunAs(t *testing.T) {
	t.Parallel()

	invoker := &recordingAppInvocation{}
	server := NewAppServer(invoker)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))

	resp, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "target",
		Operation: "do.thing",
		RunAs:     &proto.SubjectContext{},
		Context:   requestContext(t, "caller", nil),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetStatus() != 202 {
		t.Fatalf("Invoke response = %+v, want accepted", resp)
	}
	if invoker.subjectID != "user:test-user" {
		t.Fatalf("principal = %s, want original caller subject", invoker.subjectID)
	}
	if invoker.runAsSubjectID != "" {
		t.Fatalf("run-as audit subject = %q, want no delegation", invoker.runAsSubjectID)
	}
}

func TestAppServerInvokeAuthorizesAgentTurnAppOperation(t *testing.T) {
	t.Parallel()

	invoker := &recordingAppInvocation{}
	agentAuth := &recordingAgentAppAuthorizer{
		response: invocation.AgentAppAuthorization{
			Principal: &principal.Principal{
				SubjectID: "user:runner",
			},
			CredentialMode: core.ConnectionModeSubject,
			Connection:     "team-primary",
			Instance:       "workspace-1",
			RunAs: &core.RunAsSubject{
				SubjectID: "service_account:automation",
			},
			ToolRefs: []coreagent.ToolRef{{
				App:            "slack",
				Operation:      "chat.postMessage",
				Connection:     "team-primary",
				CredentialMode: core.ConnectionModeSubject,
			}},
			ToolRefsSet: true,
		},
	}
	server := NewAppServer(
		invoker,
		WithAgentAppInvocationAuthorizer(agentAuth),
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	reqCtx := requestContextWithCallerKind(t, "workflow", "temporal", nil)
	reqCtx.Agent = &proto.AgentInvocationContext{
		ProviderName: "alpha",
		SessionId:    "session-1",
		TurnId:       "turn-1",
	}
	reqCtx.ToolRefsSet = true
	reqCtx.ToolRefs = []*proto.AgentToolRef{{
		App:       "github",
		Operation: "issues.create",
	}}

	resp, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:            "slack",
		Operation:      "chat.postMessage",
		Connection:     "forged-request-connection",
		Instance:       "forged-request-instance",
		CredentialMode: "subject",
		Context:        reqCtx,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetStatus() != 202 || string(resp.GetBody()) != "accepted" {
		t.Fatalf("Invoke response = %+v, want accepted", resp)
	}
	if len(agentAuth.requests) != 1 {
		t.Fatalf("agent authorization requests = %d, want 1", len(agentAuth.requests))
	}
	gotReq := agentAuth.requests[0]
	if gotReq.AgentProviderName != "alpha" {
		t.Fatalf("agent authorization agent provider = %q, want alpha", gotReq.AgentProviderName)
	}
	if gotReq.CallerKind != invocation.ProviderKindWorkflow || gotReq.CallerName != "temporal" {
		t.Fatalf("agent authorization caller = %s/%s, want workflow/temporal", gotReq.CallerKind, gotReq.CallerName)
	}
	if gotReq.Agent.ProviderName != "alpha" || gotReq.Agent.SessionID != "session-1" || gotReq.Agent.TurnID != "turn-1" {
		t.Fatalf("agent authorization context = %#v, want alpha/session-1/turn-1", gotReq.Agent)
	}
	if gotReq.App != "slack" || gotReq.Operation != "chat.postMessage" || gotReq.Connection != "forged-request-connection" || gotReq.CredentialMode != core.ConnectionModeSubject {
		t.Fatalf("agent authorization target = %s.%s/%s/%s", gotReq.App, gotReq.Operation, gotReq.Connection, gotReq.CredentialMode)
	}
	if invoker.subjectID != "service_account:automation" || invoker.credentialSubjectID != "service_account:automation" {
		t.Fatalf("invocation principal = %s/%s, want delegated automation subject", invoker.subjectID, invoker.credentialSubjectID)
	}
	if invoker.agentSubjectID != "user:runner" || invoker.runAsSubjectID != "service_account:automation" {
		t.Fatalf("run-as audit = %s/%s, want user:runner/service_account:automation", invoker.agentSubjectID, invoker.runAsSubjectID)
	}
	if invoker.providerName != "slack" || invoker.operation != "chat.postMessage" || invoker.connection != "team-primary" || invoker.instance != "workspace-1" {
		t.Fatalf("invocation target = %s.%s/%s/%s", invoker.providerName, invoker.operation, invoker.connection, invoker.instance)
	}
	if invoker.internalConnection {
		t.Fatal("internal connection access = true, want false for agent-authorized invocation")
	}
	if invoker.credentialMode != core.ConnectionModeSubject {
		t.Fatalf("credential mode = %q, want subject", invoker.credentialMode)
	}
	if len(invoker.toolRefs) != 1 || invoker.toolRefs[0].App != "slack" || invoker.toolRefs[0].Operation != "chat.postMessage" {
		t.Fatalf("tool refs = %#v, want authorized slack chat.postMessage refs", invoker.toolRefs)
	}
}

func TestAppServerInvokeRejectsRequestRunAsForAgentCaller(t *testing.T) {
	t.Parallel()

	server := NewAppServer(
		&recordingAppInvocation{},
		WithAgentAppInvocationAuthorizer(&recordingAgentAppAuthorizer{
			response: invocation.AgentAppAuthorization{
				Principal: &principal.Principal{SubjectID: "user:runner"},
			},
		}),
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	reqCtx := requestContextWithCallerKind(t, "workflow", "temporal", nil)
	reqCtx.Agent = &proto.AgentInvocationContext{
		ProviderName: "alpha",
		SessionId:    "session-1",
		TurnId:       "turn-1",
	}

	_, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "slack",
		Operation: "chat.postMessage",
		RunAs: &proto.SubjectContext{
			Id: "service_account:forged",
		},
		Context: reqCtx,
	})
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("Invoke status = %s, want %s (err=%v)", got, codes.PermissionDenied, err)
	}
}

func TestAppServerInvokeRejectsRequestRunAsForWorkflowCaller(t *testing.T) {
	t.Parallel()

	server := NewAppServer(&recordingAppInvocation{})
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	workflow := map[string]any{
		"providerName":         "temporal",
		"runId":                "run-123",
		"definitionId":         "slack_reactions",
		"definitionGeneration": 3,
		"workflowKey":          "thread:123",
		"currentStepId":        "react",
		"currentStep": map[string]any{
			"id":    "react",
			"index": 0,
		},
		"target": map[string]any{
			"kind": "steps",
			"steps": []any{
				map[string]any{
					"id":             "react",
					"kind":           "app",
					"app":            "slack",
					"operation":      "events.addReaction",
					"connection":     "team-primary",
					"credentialMode": "subject",
				},
			},
		},
	}

	_, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:            "slack",
		Operation:      "events.addReaction",
		Connection:     "team-primary",
		CredentialMode: "subject",
		RunAs: &proto.SubjectContext{
			Id: "service_account:forged",
		},
		Context: requestContextWithCallerKind(t, "workflow", "temporal", workflow),
	})
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("Invoke status = %s, want %s (err=%v)", got, codes.PermissionDenied, err)
	}
}

func TestAppServerInvokeResolvesAgentProviderFromCaller(t *testing.T) {
	t.Parallel()

	// The app-invocation host service is registered once, globally, so the
	// agent provider is resolved from the caller context instead of a
	// registration-baked serving provider.
	agentAuth := &recordingAgentAppAuthorizer{
		response: invocation.AgentAppAuthorization{
			Principal: &principal.Principal{SubjectID: "user:runner"},
		},
	}
	server := NewAppServer(
		&recordingAppInvocation{},
		WithAgentAppInvocationAuthorizer(agentAuth),
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	reqCtx := requestContextWithCallerKind(t, "workflow", "temporal", nil)
	reqCtx.Agent = &proto.AgentInvocationContext{
		ProviderName: "alpha",
		SessionId:    "session-1",
		TurnId:       "turn-1",
	}

	_, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "slack",
		Operation: "chat.postMessage",
		Context:   reqCtx,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(agentAuth.requests) != 1 {
		t.Fatalf("agent authorization requests = %d, want 1", len(agentAuth.requests))
	}
	if got := agentAuth.requests[0].AgentProviderName; got != "alpha" {
		t.Fatalf("agent authorization agent provider = %q, want alpha from the agent invocation context", got)
	}
}

func TestAppServerInvokeGraphQLAllowsOpenSurfaceInvocation(t *testing.T) {
	t.Parallel()

	invoker := &recordingAppInvocation{}
	server := NewAppServer(invoker)
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
	if resp.GetStatus() != 208 || string(resp.GetBody()) != "graphql-accepted" {
		t.Fatalf("InvokeGraphQL response = %+v, want graphql accepted", resp)
	}
	if invoker.graphQLProviderName != "graph" || invoker.graphQLDocument != "query Viewer { viewer { id } }" || invoker.graphQLVariables["team"] != "eng" {
		t.Fatalf("graphql request = %s %q %v", invoker.graphQLProviderName, invoker.graphQLDocument, invoker.graphQLVariables)
	}
}

func TestAppServerInvokeGraphQLRejectsWorkflowCaller(t *testing.T) {
	t.Parallel()

	invoker := &recordingAppInvocation{}
	server := NewAppServer(invoker)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	workflow := map[string]any{
		"providerName":         "temporal",
		"runId":                "run-123",
		"definitionId":         "slack_reactions",
		"definitionGeneration": 3,
		"workflowKey":          "thread:123",
		"currentStepId":        "react",
		"currentStep": map[string]any{
			"id":    "react",
			"index": 0,
		},
		"target": map[string]any{
			"kind": "steps",
			"steps": []any{
				map[string]any{
					"id":        "react",
					"kind":      "app",
					"app":       "graph",
					"operation": "events.addReaction",
				},
			},
		},
	}

	_, err := client.InvokeGraphQL(context.Background(), &proto.AppInvokeGraphQLRequest{
		App:      "graph",
		Document: "query Viewer { viewer { id } }",
		Context:  requestContextWithCallerKind(t, "workflow", "temporal", workflow),
	})
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("InvokeGraphQL status = %s, want %s (err=%v)", got, codes.PermissionDenied, err)
	}
	if invoker.graphQLProviderName != "" {
		t.Fatalf("graphql provider was invoked, want no invocation")
	}
}

func TestAppServerInvokeAuthorizesWorkflowCurrentAppStep(t *testing.T) {
	t.Parallel()

	invoker := &recordingAppInvocation{}
	server := NewAppServer(invoker)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	workflow := map[string]any{
		"providerName":         "temporal",
		"runId":                "run-123",
		"definitionId":         "slack_reactions",
		"definitionGeneration": 3,
		"workflowKey":          "thread:123",
		"currentStepId":        "react",
		"currentStep": map[string]any{
			"id":    "react",
			"index": 0,
		},
		"target": map[string]any{
			"kind": "steps",
			"steps": []any{
				map[string]any{
					"id":             "react",
					"kind":           "app",
					"app":            "slack",
					"operation":      "events.addReaction",
					"connection":     "team-primary",
					"credentialMode": "subject",
				},
			},
		},
	}

	resp, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:            "slack",
		Operation:      "events.addReaction",
		Connection:     "team-primary",
		CredentialMode: "subject",
		Context:        requestContextWithCallerKind(t, "workflow", "temporal", workflow),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetStatus() != 202 || string(resp.GetBody()) != "accepted" {
		t.Fatalf("Invoke response = %+v, want accepted", resp)
	}
	if invoker.subjectID != "user:test-user" || invoker.credentialMode != core.ConnectionModeSubject {
		t.Fatalf("invocation principal/mode = %s/%q, want workflow subject/subject", invoker.subjectID, invoker.credentialMode)
	}
	if got := invocation.WorkflowContextString(invoker.workflowContext, "currentStepId"); got != "react" {
		t.Fatalf("workflow currentStepId = %q, want react", got)
	}

	_, err = client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:            "slack",
		Operation:      "chat.postMessage",
		Connection:     "team-primary",
		CredentialMode: "subject",
		Context:        requestContextWithCallerKind(t, "workflow", "temporal", workflow),
	})
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("Invoke mismatched operation status = %s, want %s (err=%v)", got, codes.PermissionDenied, err)
	}
}

func TestAppServerInvokeRejectsForgedCallerMismatchingVerifiedCaller(t *testing.T) {
	t.Parallel()

	// On the relay path the verified token stamps the caller provider in the
	// request context; a forged proto caller must not override it.
	server := NewAppServer(&recordingAppInvocation{})
	// Simulate the relay restoring the verified caller-provider from the
	// token before the request reaches the app host service.
	restoreVerifiedCaller := grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(invocation.WithCallerProvider(ctx, invocation.ProviderKindApp, "caller"), req)
	})
	client := proto.NewAppClient(newBufconnConnWithOptions(t, []grpc.ServerOption{restoreVerifiedCaller}, nil, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	_, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "target",
		Operation: "do.thing",
		Context:   requestContext(t, "forged", nil),
	})
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("Invoke status = %s, want %s (err=%v)", got, codes.PermissionDenied, err)
	}
}

func requestContext(t *testing.T, caller string, workflow map[string]any) *proto.RequestContext {
	return requestContextWithCallerKind(t, "app", caller, workflow)
}

func requestContextWithCallerKind(t *testing.T, callerKind, caller string, workflow map[string]any) *proto.RequestContext {
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
			Id:    "user:test-user",
			Email: "test@example.com",
		},
		Credential: &proto.CredentialContext{
			Mode:       "subject",
			SubjectId:  "user:test-user",
			Connection: "ctx-connection",
			Instance:   "ctx-instance",
		},
		Workflow: workflowStruct,
		Caller: &proto.ProviderContext{
			Kind: callerKind,
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

func (i *recordingAppInvocation) InvokeStream(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (core.StreamReader, error) {
	i.providerName = providerName
	i.instance = instance
	i.operation = operation
	i.params = params
	if p != nil {
		i.subjectID = p.SubjectID
	}
	return &sliceStreamReader{
		frames: []*core.InvokeFrame{
			{Metadata: &core.InvokeMetadata{Status: 200, MediaType: "application/x-ndjson"}},
			{Data: []byte(`{"event":"start"}` + "\n")},
			{Data: []byte(`{"event":"end"}` + "\n")},
		},
	}, nil
}

type sliceStreamReader struct {
	frames []*core.InvokeFrame
	idx    int
}

func (r *sliceStreamReader) Recv() (*core.InvokeFrame, error) {
	if r.idx >= len(r.frames) {
		return nil, io.EOF
	}
	f := r.frames[r.idx]
	r.idx++
	return f, nil
}

func TestAppServerInvokeStream(t *testing.T) {
	t.Parallel()
	rec := &recordingAppInvocation{}
	server := NewAppServer(rec)
	stream := &invokeStreamServerStub{ctx: context.Background()}
	err := server.InvokeStream(&proto.AppInvokeRequest{
		App:       "example",
		Operation: "events.watch",
		Params:    structpbStruct(t, map[string]any{"since": "now"}),
		Context:   requestContext(t, "caller", nil),
	}, stream)
	if err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}
	if rec.providerName != "example" || rec.operation != "events.watch" {
		t.Fatalf("recorded provider=%q operation=%q", rec.providerName, rec.operation)
	}
	if len(stream.frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(stream.frames))
	}
	if m := stream.frames[0].GetMetadata(); m == nil || m.GetStatus() != 200 || m.GetMediaType() != "application/x-ndjson" {
		t.Fatalf("metadata frame = %+v", m)
	}
	if string(stream.frames[1].GetData()) != `{"event":"start"}`+"\n" {
		t.Fatalf("data frame 1 = %q", string(stream.frames[1].GetData()))
	}
}

type invokeStreamServerStub struct {
	proto.App_InvokeStreamServer
	ctx    context.Context
	frames []*proto.InvokeFrame
}

func (s *invokeStreamServerStub) Context() context.Context { return s.ctx }
func (s *invokeStreamServerStub) Send(frame *proto.InvokeFrame) error {
	s.frames = append(s.frames, frame)
	return nil
}

func structpbStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	return s
}

func TestInvokeFrameToProtoEmitsBothMetadataAndData(t *testing.T) {
	t.Parallel()
	frame := &core.InvokeFrame{
		Metadata: &core.InvokeMetadata{
			Status:    500,
			MediaType: "application/json",
		},
		Data: []byte(`{"error":"boom"}`),
	}
	frames := invokeFrameToProto(frame)
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if frames[0].GetMetadata() == nil || frames[0].GetMetadata().GetStatus() != 500 {
		t.Fatalf("first frame metadata = %+v", frames[0].GetMetadata())
	}
	if string(frames[1].GetData()) != `{"error":"boom"}` {
		t.Fatalf("second frame data = %q", string(frames[1].GetData()))
	}
}

func TestInvokeFrameToProtoMetadataOnlyYieldsOneFrame(t *testing.T) {
	t.Parallel()
	frame := &core.InvokeFrame{
		Metadata: &core.InvokeMetadata{Status: 200, MediaType: "application/x-ndjson"},
	}
	frames := invokeFrameToProto(frame)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].GetMetadata() == nil {
		t.Fatal("expected metadata frame")
	}
}

func TestInvokeFrameToProtoDataOnlyYieldsOneFrame(t *testing.T) {
	t.Parallel()
	frame := &core.InvokeFrame{Data: []byte("chunk")}
	frames := invokeFrameToProto(frame)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if string(frames[0].GetData()) != "chunk" {
		t.Fatalf("data = %q", string(frames[0].GetData()))
	}
}

func TestAppStreamErrorMapsAllInvocationErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{"provider not found", invocation.ErrProviderNotFound, codes.NotFound},
		{"operation not found", invocation.ErrOperationNotFound, codes.NotFound},
		{"not authenticated", invocation.ErrNotAuthenticated, codes.Unauthenticated},
		{"authorization denied", invocation.ErrAuthorizationDenied, codes.PermissionDenied},
		{"scope denied", invocation.ErrScopeDenied, codes.PermissionDenied},
		{"no credential", invocation.ErrNoCredential, codes.FailedPrecondition},
		{"reconnect required", invocation.ErrReconnectRequired, codes.FailedPrecondition},
		{"invalid invocation", invocation.ErrInvalidInvocation, codes.InvalidArgument},
		{"streaming unsupported", invocation.ErrStreamingUnsupported, codes.FailedPrecondition},
		{"ambiguous instance", invocation.ErrAmbiguousInstance, codes.Aborted},
		{"max depth", &invocation.MaxDepthError{Depth: 5, Max: 4}, codes.ResourceExhausted},
		{"rate limit", &invocation.RateLimitError{Provider: "foo"}, codes.ResourceExhausted},
		{"recursion", &invocation.RecursionError{Provider: "foo", Operation: "bar"}, codes.FailedPrecondition},
		{"unknown", assertErr("unknown"), codes.Internal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st, ok := status.FromError(appStreamError(tc.err))
			if !ok {
				t.Fatalf("appStreamError did not return a gRPC status: %v", appStreamError(tc.err))
			}
			if st.Code() != tc.wantCode {
				t.Fatalf("got code %s, want %s", st.Code(), tc.wantCode)
			}
		})
	}
}

type assertErrType string

func (e assertErrType) Error() string { return string(e) }
func assertErr(s string) error        { return assertErrType(s) }

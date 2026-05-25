package appaccess

import (
	"context"
	"fmt"
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
	idempotencyKey        string
	internalConnection    bool
	workflowContext       map[string]any
	providerName          string
	instance              string
	operation             string
	credentialMode        core.ConnectionMode
	params                map[string]any
	graphQLIdempotencyKey string
	graphQLProviderName   string
	graphQLInstance       string
	graphQLDocument       string
	graphQLVariables      map[string]any
}

func (i *recordingAppInvocation) Invoke(ctx context.Context, _ *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	i.idempotencyKey = invocation.IdempotencyKeyFromContext(ctx)
	i.internalConnection = invocation.InternalConnectionAccessFromContext(ctx)
	i.workflowContext = invocation.WorkflowContextFromContext(ctx)
	i.providerName = providerName
	i.instance = instance
	i.operation = operation
	i.credentialMode = invocation.CredentialModeOverrideFromContext(ctx)
	i.params = params
	return &core.OperationResult{Status: 202, Body: "accepted"}, nil
}

func TestAppServerInvokeRestoresWorkflowContext(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-workflow-context-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "service_account:workflow",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	})
	ctx = invocation.WithWorkflowContext(ctx, map[string]any{
		"providerName": "temporal",
		"runId":        "run-123",
		"step": map[string]any{
			"id": "notify",
		},
	})
	rootToken, err := tokens.MintRootToken(ctx, "workflow-provider", InvocationGrants{
		"slack": {Operations: map[string]core.ConnectionMode{"chat.postMessage": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	invoker := &recordingAppInvocation{}
	server := NewAppServer(
		"workflow-provider",
		[]invocation.AppInvocationDependency{{App: "slack", Operation: "chat.postMessage"}},
		invoker,
		tokens,
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	if _, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: rootToken,
		App:             "slack",
		Operation:       "chat.postMessage",
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if got := invocation.WorkflowContextString(invoker.workflowContext, "providerName"); got != "temporal" {
		t.Fatalf("providerName = %q, want temporal", got)
	}
	if got := invocation.WorkflowContextString(invoker.workflowContext, "runId"); got != "run-123" {
		t.Fatalf("runId = %q, want run-123", got)
	}
	step := invocation.WorkflowContextMap(invoker.workflowContext, "step")
	if got := invocation.WorkflowContextString(step, "id"); got != "notify" {
		t.Fatalf("step.id = %q, want notify", got)
	}
}

func TestAppServerInvokeCredentialModeForForwardedToken(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		grantMode   core.ConnectionMode
		requestMode string
		deps        []invocation.AppInvocationDependency
		want        core.ConnectionMode
		wantErr     codes.Code
	}{
		{
			name:        "explicit token mode",
			grantMode:   core.ConnectionModeNone,
			requestMode: "none",
			want:        core.ConnectionModeNone,
		},
		{
			name:        "explicit unqualified mode",
			requestMode: "none",
			wantErr:     codes.PermissionDenied,
		},
		{
			name: "host declared mode ignored for forwarded token",
			deps: []invocation.AppInvocationDependency{{
				App:            "slack",
				Operation:      "chat.postMessage",
				CredentialMode: core.ConnectionModeUser,
			}},
		},
		{
			name:        "explicit host declared mode ignored for forwarded token",
			requestMode: "user",
			deps: []invocation.AppInvocationDependency{{
				App:            "slack",
				Operation:      "chat.postMessage",
				CredentialMode: core.ConnectionModeUser,
			}},
			wantErr: codes.PermissionDenied,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := NewInvocationTokenManager([]byte("forwarded-token-" + tc.name))
			if err != nil {
				t.Fatalf("NewInvocationTokenManager: %v", err)
			}
			ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
				SubjectID: "service_account:caller",
				Kind:      principal.Kind("service_account"),
				Source:    principal.SourceAPIToken,
			})
			rootToken, err := tokens.MintRootToken(ctx, "caller", InvocationGrants{
				"slack": {Operations: map[string]core.ConnectionMode{"chat.postMessage": tc.grantMode}},
			})
			if err != nil {
				t.Fatalf("MintRootToken: %v", err)
			}

			invoker := &recordingAppInvocation{}
			server := NewAppServer("workflow-provider", tc.deps, invoker, tokens)
			client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
				proto.RegisterAppServer(srv, server)
			}))
			_, err = client.Invoke(context.Background(), &proto.AppInvokeRequest{
				InvocationToken: rootToken,
				App:             "slack",
				Operation:       "chat.postMessage",
				CredentialMode:  tc.requestMode,
			})
			if tc.wantErr != codes.OK {
				if got := status.Code(err); got != tc.wantErr {
					t.Fatalf("Invoke status = %s, want %s (err=%v)", got, tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if invoker.credentialMode != tc.want {
				t.Fatalf("credential mode override = %q, want %q", invoker.credentialMode, tc.want)
			}
		})
	}
}

func TestAppServerInvokePropagatesInternalConnectionAccess(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "service_account:workflow-config",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	})
	ctx = invocation.WithInternalConnectionAccess(ctx)
	rootToken, err := tokens.MintRootToken(ctx, "brain", InvocationGrants{
		"slack": {Operations: map[string]core.ConnectionMode{"conversations.history": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	invoker := &recordingAppInvocation{}
	server := NewAppServer(
		"brain",
		[]invocation.AppInvocationDependency{
			{App: "slack", Operation: "conversations.history"},
		},
		invoker,
		tokens,
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	if _, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: rootToken,
		App:             "slack",
		Operation:       "conversations.history",
		Connection:      "bot",
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !invoker.internalConnection {
		t.Fatal("internal connection access was not restored from the invocation token")
	}
}

func TestAppServerInvokeMapsInvalidInvocationToInvalidArgument(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-invalid-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "service_account:workflow-config",
		Kind:      principal.Kind("service_account"),
		Source:    principal.SourceAPIToken,
	})
	rootToken, err := tokens.MintRootToken(ctx, "brain", InvocationGrants{
		"gmail": {Operations: map[string]core.ConnectionMode{"gmail.users.messages.modify": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	server := NewAppServer(
		"brain",
		[]invocation.AppInvocationDependency{{App: "gmail", Operation: "gmail.users.messages.modify"}},
		erroringAppInvocation{err: fmt.Errorf("%w: bad connection override", invocation.ErrInvalidInvocation)},
		tokens,
	)
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	_, err = client.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: rootToken,
		App:             "gmail",
		Operation:       "gmail.users.messages.modify",
		Connection:      "override",
	})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("Invoke status = %s, want %s (err=%v)", got, codes.InvalidArgument, err)
	}
}

func (i *recordingAppInvocation) InvokeGraphQL(ctx context.Context, _ *principal.Principal, providerName, instance string, request invocation.GraphQLRequest) (*core.OperationResult, error) {
	i.graphQLIdempotencyKey = invocation.IdempotencyKeyFromContext(ctx)
	i.graphQLProviderName = providerName
	i.graphQLInstance = instance
	i.graphQLDocument = request.Document
	i.graphQLVariables = request.Variables
	return &core.OperationResult{Status: 208, Body: "graphql-accepted"}, nil
}

type erroringAppInvocation struct {
	err error
}

func (i erroringAppInvocation) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return nil, i.err
}

func TestAppServerInvokePropagatesIdempotencyKey(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "user:test-user",
		UserID:    "test-user",
		Kind:      principal.KindUser,
		Source:    principal.SourceSession,
	})
	rootToken, err := tokens.MintRootToken(ctx, "caller", InvocationGrants{
		"github": {Operations: map[string]core.ConnectionMode{"issues.create": ""}},
		"linear": {Surfaces: map[string]struct{}{"graphql": {}}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	invoker := &recordingAppInvocation{}
	server := NewAppServer(
		"caller",
		[]invocation.AppInvocationDependency{
			{App: "github", Operation: "issues.create"},
			{App: "linear", Surface: "graphql"},
		},
		invoker,
		tokens,
	)
	params, err := structpb.NewStruct(map[string]any{"title": "bug"})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	client := proto.NewAppClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAppServer(srv, server)
	}))
	resp, err := client.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: rootToken,
		App:             " github ",
		Operation:       " issues.create ",
		Instance:        " prod ",
		IdempotencyKey:  " tool-call-123 ",
		Params:          params,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetStatus() != 202 || resp.GetBody() != "accepted" {
		t.Fatalf("Invoke response = %+v, want status=202 body=accepted", resp)
	}
	if invoker.idempotencyKey != "tool-call-123" {
		t.Fatalf("idempotency key = %q, want tool-call-123", invoker.idempotencyKey)
	}
	if invoker.providerName != "github" || invoker.operation != "issues.create" || invoker.instance != "prod" {
		t.Fatalf("target = %s.%s/%s, want github.issues.create/prod", invoker.providerName, invoker.operation, invoker.instance)
	}
	if invoker.params["title"] != "bug" {
		t.Fatalf("params = %#v, want title=bug", invoker.params)
	}

	variables, err := structpb.NewStruct(map[string]any{"team": "eng"})
	if err != nil {
		t.Fatalf("NewStruct variables: %v", err)
	}
	graphQLResp, err := client.InvokeGraphQL(context.Background(), &proto.AppInvokeGraphQLRequest{
		InvocationToken: rootToken,
		App:             " linear ",
		Document:        " query Viewer { viewer { id } } ",
		Instance:        " prod ",
		IdempotencyKey:  " graphql-call-123 ",
		Variables:       variables,
	})
	if err != nil {
		t.Fatalf("InvokeGraphQL: %v", err)
	}
	if graphQLResp.GetStatus() != 208 || graphQLResp.GetBody() != "graphql-accepted" {
		t.Fatalf("InvokeGraphQL response = %+v, want status=208 body=graphql-accepted", graphQLResp)
	}
	if invoker.graphQLIdempotencyKey != "graphql-call-123" {
		t.Fatalf("graphql idempotency key = %q, want graphql-call-123", invoker.graphQLIdempotencyKey)
	}
	if invoker.graphQLProviderName != "linear" || invoker.graphQLInstance != "prod" {
		t.Fatalf("graphql target = %s/%s, want linear/prod", invoker.graphQLProviderName, invoker.graphQLInstance)
	}
	if invoker.graphQLDocument != "query Viewer { viewer { id } }" {
		t.Fatalf("graphql document = %q, want trimmed document", invoker.graphQLDocument)
	}
	if invoker.graphQLVariables["team"] != "eng" {
		t.Fatalf("graphql variables = %#v, want team=eng", invoker.graphQLVariables)
	}
}

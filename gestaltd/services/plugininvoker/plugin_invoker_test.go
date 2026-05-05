package plugininvoker

import (
	"context"
	"fmt"
	"testing"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type recordingPluginInvoker struct {
	idempotencyKey        string
	internalConnection    bool
	providerName          string
	instance              string
	operation             string
	params                map[string]any
	graphQLIdempotencyKey string
	graphQLProviderName   string
	graphQLInstance       string
	graphQLDocument       string
	graphQLVariables      map[string]any
}

func (i *recordingPluginInvoker) Invoke(ctx context.Context, _ *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	i.idempotencyKey = invocation.IdempotencyKeyFromContext(ctx)
	i.internalConnection = invocation.InternalConnectionAccessFromContext(ctx)
	i.providerName = providerName
	i.instance = instance
	i.operation = operation
	i.params = params
	return &core.OperationResult{Status: 202, Body: "accepted"}, nil
}

func TestPluginInvokerServerInvokePropagatesInternalConnectionAccess(t *testing.T) {
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

	invoker := &recordingPluginInvoker{}
	server := NewPluginInvokerServer(
		"brain",
		[]invocation.PluginInvocationDependency{
			{Plugin: "slack", Operation: "conversations.history"},
		},
		invoker,
		tokens,
	)
	client := proto.NewPluginInvokerClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterPluginInvokerServer(srv, server)
	}))
	if _, err := client.Invoke(context.Background(), &proto.PluginInvokeRequest{
		InvocationToken: rootToken,
		Plugin:          "slack",
		Operation:       "conversations.history",
		Connection:      "bot",
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !invoker.internalConnection {
		t.Fatal("internal connection access was not restored from the invocation token")
	}
}

func TestPluginInvokerServerInvokeUsesRequestedPlatformConnectionWithoutCredentialModeOverride(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("plugin-invoker-platform-test-secret"))
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
		"gmail": {Operations: map[string]core.ConnectionMode{"messages.list": ""}},
	})
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	externalCreds := coretesting.NewStubExternalCredentialProvider()
	externalCreds.ResolveCredentialFunc = func(_ context.Context, req *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
		if req.Mode != core.ConnectionModePlatform || req.Connection != "platform" {
			t.Fatalf("ResolveCredential target = mode:%q connection:%q, want platform/platform", req.Mode, req.Connection)
		}
		if req.Auth.GrantType != "refresh_token" || req.Auth.RefreshToken != "refresh-token" {
			t.Fatalf("ResolveCredential auth = %+v, want refresh_token with refresh token", req.Auth)
		}
		return &core.ResolveExternalCredentialResponse{Token: "platform-gmail-token"}, nil
	}
	gmail := &coretesting.StubIntegration{
		N:        "gmail",
		ConnMode: core.ConnectionModeUser,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID:     "messages.list",
			Method: "GET",
		}}},
		ExecuteFn: func(ctx context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
			if cred := invocation.CredentialContextFromContext(ctx); cred.Mode != core.ConnectionModePlatform || cred.Connection != "platform" {
				t.Fatalf("credential context = %#v, want platform/platform", cred)
			}
			return &core.OperationResult{Status: 200, Body: token}, nil
		},
	}
	broker := invocation.NewBroker(
		testutil.NewProviderRegistry(t, gmail),
		nil,
		externalCreds,
		invocation.WithConnectionRuntime(invocation.ConnectionRuntimeMap{
			"gmail": {
				"platform": {
					Mode:     core.ConnectionModePlatform,
					Exposure: core.ConnectionExposureInternal,
					AuthConfig: core.ExternalCredentialAuthConfig{
						Type:         "oauth2",
						GrantType:    "refresh_token",
						TokenURL:     "https://oauth2.googleapis.com/token",
						ClientID:     "client-id",
						ClientSecret: "client-secret",
						RefreshToken: "refresh-token",
					},
				},
			},
		}.Resolve),
	)
	server := NewPluginInvokerServer(
		"brain",
		[]invocation.PluginInvocationDependency{{Plugin: "gmail", Operation: "messages.list"}},
		broker,
		tokens,
	)
	client := proto.NewPluginInvokerClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterPluginInvokerServer(srv, server)
	}))
	resp, err := client.Invoke(context.Background(), &proto.PluginInvokeRequest{
		InvocationToken: rootToken,
		Plugin:          "gmail",
		Operation:       "messages.list",
		Connection:      "platform",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetBody() != "platform-gmail-token" {
		t.Fatalf("body = %q, want platform token", resp.GetBody())
	}
}

func TestPluginInvokerServerInvokeMapsInvalidInvocationToInvalidArgument(t *testing.T) {
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

	server := NewPluginInvokerServer(
		"brain",
		[]invocation.PluginInvocationDependency{{Plugin: "gmail", Operation: "gmail.users.messages.modify"}},
		erroringPluginInvoker{err: fmt.Errorf("%w: bad connection override", invocation.ErrInvalidInvocation)},
		tokens,
	)
	client := proto.NewPluginInvokerClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterPluginInvokerServer(srv, server)
	}))
	_, err = client.Invoke(context.Background(), &proto.PluginInvokeRequest{
		InvocationToken: rootToken,
		Plugin:          "gmail",
		Operation:       "gmail.users.messages.modify",
		Connection:      "platform",
	})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("Invoke status = %s, want %s (err=%v)", got, codes.InvalidArgument, err)
	}
}

func (i *recordingPluginInvoker) InvokeGraphQL(ctx context.Context, _ *principal.Principal, providerName, instance string, request invocation.GraphQLRequest) (*core.OperationResult, error) {
	i.graphQLIdempotencyKey = invocation.IdempotencyKeyFromContext(ctx)
	i.graphQLProviderName = providerName
	i.graphQLInstance = instance
	i.graphQLDocument = request.Document
	i.graphQLVariables = request.Variables
	return &core.OperationResult{Status: 208, Body: "graphql-accepted"}, nil
}

type erroringPluginInvoker struct {
	err error
}

func (i erroringPluginInvoker) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return nil, i.err
}

func TestPluginInvokerServerInvokePropagatesIdempotencyKey(t *testing.T) {
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

	invoker := &recordingPluginInvoker{}
	server := NewPluginInvokerServer(
		"caller",
		[]invocation.PluginInvocationDependency{
			{Plugin: "github", Operation: "issues.create"},
			{Plugin: "linear", Surface: "graphql"},
		},
		invoker,
		tokens,
	)
	params, err := structpb.NewStruct(map[string]any{"title": "bug"})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	client := proto.NewPluginInvokerClient(newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterPluginInvokerServer(srv, server)
	}))
	resp, err := client.Invoke(context.Background(), &proto.PluginInvokeRequest{
		InvocationToken: rootToken,
		Plugin:          " github ",
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
	graphQLResp, err := client.InvokeGraphQL(context.Background(), &proto.PluginInvokeGraphQLRequest{
		InvocationToken: rootToken,
		Plugin:          " linear ",
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

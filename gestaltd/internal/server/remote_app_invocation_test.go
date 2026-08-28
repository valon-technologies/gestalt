package server

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type publicRemoteInvocationProvider struct {
	mu           sync.Mutex
	invokeCalls  int
	graphQLCalls int
	lastSubject  string
	lastCaller   invocation.CallerProvider
}

func (p *publicRemoteInvocationProvider) Name() string        { return "data-schema-explorer" }
func (p *publicRemoteInvocationProvider) DisplayName() string { return p.Name() }
func (p *publicRemoteInvocationProvider) Description() string { return "test schema app" }
func (p *publicRemoteInvocationProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeNone
}
func (p *publicRemoteInvocationProvider) AuthTypes() []string { return nil }
func (p *publicRemoteInvocationProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *publicRemoteInvocationProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *publicRemoteInvocationProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *publicRemoteInvocationProvider) ConnectionForOperation(string) string        { return "" }
func (p *publicRemoteInvocationProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name:       "data-schema-explorer",
		Operations: []catalog.CatalogOperation{{ID: "get_schema", Transport: catalog.TransportApp}},
	}
}

func (p *publicRemoteInvocationProvider) Execute(ctx context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	p.record(ctx, false)
	return &core.OperationResult{Status: 200, Body: []byte(`{"schema":"current"}`)}, nil
}

func (p *publicRemoteInvocationProvider) InvokeGraphQL(ctx context.Context, _ core.GraphQLRequest, _ string) (*core.OperationResult, error) {
	p.record(ctx, true)
	return &core.OperationResult{Status: 200, Body: []byte(`{"schema":"graphql"}`)}, nil
}

func (p *publicRemoteInvocationProvider) record(ctx context.Context, graphQL bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if graphQL {
		p.graphQLCalls++
	} else {
		p.invokeCalls++
	}
	if caller := invocation.CallerProviderFromContext(ctx); caller.Name != "" {
		p.lastCaller = caller
	}
	if current := principal.FromContext(ctx); current != nil {
		p.lastSubject = current.SubjectID
	}
}

func (p *publicRemoteInvocationProvider) snapshot() (int, int, string, invocation.CallerProvider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.invokeCalls, p.graphQLCalls, p.lastSubject, p.lastCaller
}

func TestDevRemoteAppInvocationUsesPublicGatewayContextAndAllowlist(t *testing.T) {
	t.Parallel()

	remoteProvider := &publicRemoteInvocationProvider{}
	remoteRegistry := registry.New()
	if err := remoteRegistry.Providers.Register(remoteProvider.Name(), remoteProvider); err != nil {
		t.Fatalf("register remote provider: %v", err)
	}
	remoteServices := testutil.NewStubServices(t)
	remoteBroker := invocation.NewBroker(&remoteRegistry.Providers, remoteServices.Users, remoteServices.ExternalCredentials)

	identity := &coretesting.StubIdentityProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{Active: true, Subject: "user:alice"}, nil
		},
	}
	publicMethods, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}
	remoteTransport := providergateway.NewProviderGatewayTransport()
	remoteTransport.SetIdentityProvider(identity)
	remoteTransport.SetPublicMethods(publicMethods)
	remoteTransport.SetPublicBaseURL("https://remote.test")
	var ordinaryRequestWithoutContext atomic.Int32
	var graphQLRequestWithoutContext atomic.Int32
	publicServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			switch request := req.(type) {
			case *proto.AppInvokeRequest:
				if request.GetContext() == nil {
					ordinaryRequestWithoutContext.Add(1)
				}
			case *proto.AppInvokeGraphQLRequest:
				if request.GetContext() == nil {
					graphQLRequestWithoutContext.Add(1)
				}
			}
			return handler(ctx, req)
		},
		publicPrepareUnaryInterceptor(remoteTransport, nil),
	))
	publicrpc.RegisterPublicServers(publicServer, publicrpc.Servers{App: appaccessservice.NewServer(remoteBroker)})
	remoteConn := newBufconnClientConn(t, publicServer, func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer test-token"), method, req, reply, cc, opts...)
	})
	remoteClient := proto.NewAppClient(remoteConn)

	localRegistry := registry.New()
	remoteProviderProxy := appservice.NewGestaltRemote(remoteClient, appservice.StaticProviderSpec{
		Name: "data-schema-explorer",
		Catalog: &catalog.Catalog{
			Name: "data-schema-explorer",
			Operations: []catalog.CatalogOperation{
				{ID: "get_schema", Transport: catalog.TransportApp},
				{ID: "graphql", Transport: catalog.TransportApp},
			},
		},
		ConnectionMode: core.ConnectionModeNone,
	})
	if remoteProviderProxy == nil {
		t.Fatal("NewGestaltRemote returned nil")
	}
	if err := localRegistry.Providers.Register("data-schema-explorer", remoteProviderProxy); err != nil {
		t.Fatalf("register remote proxy: %v", err)
	}
	localServices := testutil.NewStubServices(t)
	localBroker := invocation.NewBroker(&localRegistry.Providers, localServices.Users, localServices.ExternalCredentials)
	localServer := grpc.NewServer()
	proto.RegisterAppServer(localServer, appaccessservice.NewServer(localBroker))
	localConn := newBufconnClientConn(t, localServer, nil)
	localClient := proto.NewAppClient(localConn)

	callerContext := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "user:alice", Kind: principal.KindUser})
	callerContext = invocation.WithCallerProvider(callerContext, invocation.ProviderKindApp, "vds-forge")
	requestContext, err := appaccessservice.RequestContextProto(callerContext, "", invocation.CallerProvider{Kind: invocation.ProviderKindApp, Name: "vds-forge"})
	if err != nil {
		t.Fatalf("RequestContextProto: %v", err)
	}

	response, err := localClient.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "data-schema-explorer",
		Operation: "get_schema",
		Context:   requestContext,
	})
	if err != nil {
		t.Fatalf("local Invoke: %v", err)
	}
	if response.GetStatus() != 200 || string(response.GetBody()) != `{"schema":"current"}` {
		t.Fatalf("local response = %+v, want current schema", response)
	}

	graphqlResponse, err := localClient.InvokeGraphQL(context.Background(), &proto.AppInvokeGraphQLRequest{
		App:      "data-schema-explorer",
		Document: "query Schema { schema }",
		Context:  requestContext,
	})
	if err != nil {
		t.Fatalf("local InvokeGraphQL: %v", err)
	}
	if graphqlResponse.GetStatus() != 200 || string(graphqlResponse.GetBody()) != `{"schema":"graphql"}` {
		t.Fatalf("local GraphQL response = %+v, want graphql schema", graphqlResponse)
	}

	_, err = localClient.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "data-schema-explorer",
		Operation: "not-allowlisted",
		Context:   requestContext,
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("non-allowlisted error = %v, want NotFound", err)
	}
	emptyRemote := appservice.NewGestaltRemote(remoteClient, appservice.StaticProviderSpec{
		Name:    "empty-remote-app",
		Catalog: &catalog.Catalog{Name: "empty-remote-app"},
	})
	if emptyRemote == nil {
		t.Fatal("NewGestaltRemote returned nil for empty catalog")
	}
	if _, err := emptyRemote.(core.GraphQLSurfaceInvoker).InvokeGraphQL(context.Background(), core.GraphQLRequest{Document: "query Schema { schema }"}, ""); !errors.Is(err, invocation.ErrOperationNotFound) {
		t.Fatalf("empty-catalog GraphQL error = %v, want ErrOperationNotFound", err)
	}
	if ordinaryRequestWithoutContext.Load() != 1 || graphQLRequestWithoutContext.Load() != 1 {
		t.Fatalf("outbound request context counts = ordinary %d graphql %d, want 1 each", ordinaryRequestWithoutContext.Load(), graphQLRequestWithoutContext.Load())
	}
	invokeCalls, graphQLCalls, subject, caller := remoteProvider.snapshot()
	if invokeCalls != 1 || graphQLCalls != 1 {
		t.Fatalf("remote provider calls = invoke %d graphql %d, want 1 each", invokeCalls, graphQLCalls)
	}
	if subject != "user:alice" || caller.Kind != invocation.ProviderKindApp || caller.Name != "gestaltd" {
		t.Fatalf("remote identity = subject %q caller %#v, want user:alice app/gestaltd", subject, caller)
	}

	_, err = remoteClient.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "data-schema-explorer",
		Operation: "get_schema",
		Context:   &proto.RequestContext{Subject: &proto.SubjectContext{Id: "user:forged"}},
	})
	if status.Code(err) != codes.InvalidArgument || status.Convert(err).Message() != "context is server-filled" {
		t.Fatalf("explicit context error = %v, want InvalidArgument context is server-filled", err)
	}
}

func newBufconnClientConn(t *testing.T, server *grpc.Server, intercept grpc.UnaryClientInterceptor) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	dialOptions := []grpc.DialOption{
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if intercept != nil {
		dialOptions = append(dialOptions, grpc.WithUnaryInterceptor(intercept))
	}
	conn, err := grpc.NewClient("passthrough:///bufnet", dialOptions...)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

package invocation

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

func testRemoteRequestContext(ctx context.Context, _ string) (*proto.RequestContext, error) {
	p := principal.FromContext(ctx)
	if p == nil {
		return nil, nil
	}
	return &proto.RequestContext{
		Subject: &proto.SubjectContext{Id: p.SubjectID},
	}, nil
}

type recordingAppServer struct {
	proto.UnimplementedAppServer
	lastReq *proto.AppInvokeRequest
}

func (s *recordingAppServer) Invoke(_ context.Context, req *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	s.lastReq = req
	return &proto.OperationResult{Status: 200, Body: []byte(`{"ok":true}`)}, nil
}

func TestBrokerInvokeDelegatesDeclaredRemoteApp(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	appSrv := &recordingAppServer{}
	proto.RegisterAppServer(srv, appSrv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t),
		svc.Users,
		svc.ExternalCredentials,
		WithRemoteAppRouting(func(name string) bool {
			return name == "linear"
		}, proto.NewAppClient(conn), "https://local.example", testRemoteRequestContext),
	)

	p := &principal.Principal{
		SubjectID: "user:test",
		Kind:      principal.KindUser,
		Scopes:    []string{"linear"},
	}
	result, err := broker.Invoke(context.Background(), p, "linear", "", "issues.list", map[string]any{"first": 10})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result == nil || result.Status != 200 {
		t.Fatalf("result = %#v, want status 200", result)
	}
	if appSrv.lastReq == nil {
		t.Fatal("remote App.Invoke was not called")
	}
	if appSrv.lastReq.GetApp() != "linear" || appSrv.lastReq.GetOperation() != "issues.list" {
		t.Fatalf("remote request = app %q operation %q", appSrv.lastReq.GetApp(), appSrv.lastReq.GetOperation())
	}
	if appSrv.lastReq.GetContext() == nil || appSrv.lastReq.GetContext().GetSubject().GetId() != "user:test" {
		t.Fatalf("remote request context = %#v, want subject user:test", appSrv.lastReq.GetContext())
	}
}

func TestBrokerInvokeLocalProviderWinsOverRemote(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	proto.RegisterAppServer(srv, &recordingAppServer{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:          "linear",
			ConnMode:   core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{ID: "issues.list"}}},
			ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: 201, Body: []byte("local")}, nil
			},
		}),
		svc.Users,
		svc.ExternalCredentials,
		WithRemoteAppRouting(func(name string) bool { return name == "linear" }, proto.NewAppClient(conn), "", testRemoteRequestContext),
	)

	p := &principal.Principal{SubjectID: "user:test", Kind: principal.KindUser, Scopes: []string{"linear"}}
	result, err := broker.Invoke(context.Background(), p, "linear", "", "issues.list", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result == nil || result.Status != 201 || string(result.Body) != "local" {
		t.Fatalf("result = %#v, want local 201", result)
	}
}

func TestBrokerInvokeUnknownAppRemainsNotFound(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t),
		svc.Users,
		svc.ExternalCredentials,
		WithRemoteAppRouting(func(string) bool { return false }, nil, "", testRemoteRequestContext),
	)

	_, err := broker.Invoke(context.Background(), &principal.Principal{
		SubjectID: "user:test",
		Kind:      principal.KindUser,
		Scopes:    []string{"missing"},
	}, "missing", "", "op", nil)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("err = %v, want ErrProviderNotFound", err)
	}
}

func TestBrokerInvokeRemoteAuthErrorPropagates(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	proto.RegisterAppServer(srv, &remoteStatusAppServer{code: codes.PermissionDenied, msg: "remote denied"})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t),
		svc.Users,
		svc.ExternalCredentials,
		WithRemoteAppRouting(func(name string) bool { return name == "linear" }, proto.NewAppClient(conn), "", testRemoteRequestContext),
	)

	_, err = broker.Invoke(context.Background(), &principal.Principal{
		SubjectID: "user:test",
		Kind:      principal.KindUser,
		Scopes:    []string{"linear"},
	}, "linear", "", "issues.list", map[string]any{})
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("err = %v, want ErrAuthorizationDenied", err)
	}
}

type remoteStatusAppServer struct {
	proto.UnimplementedAppServer
	code codes.Code
	msg  string
}

func (s *remoteStatusAppServer) Invoke(context.Context, *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	return nil, status.Error(s.code, s.msg)
}

func TestBrokerInvokeGraphQLDelegatesRemote(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	graphqlSrv := &recordingGraphQLAppServer{}
	proto.RegisterAppServer(srv, graphqlSrv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	svc := testutil.NewStubServices(t)
	broker := NewBroker(
		testutil.NewProviderRegistry(t),
		svc.Users,
		svc.ExternalCredentials,
		WithRemoteAppRouting(func(name string) bool { return name == "linear" }, proto.NewAppClient(conn), "", testRemoteRequestContext),
	)

	variables, err := structpb.NewStruct(map[string]any{"team": "eng"})
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	_, err = broker.InvokeGraphQL(context.Background(), &principal.Principal{
		SubjectID: "user:test",
		Kind:      principal.KindUser,
		Scopes:    []string{"linear"},
	}, "linear", "", GraphQLRequest{Document: "query { viewer { id } }", Variables: variables.AsMap()})
	if err != nil {
		t.Fatalf("InvokeGraphQL: %v", err)
	}
	if graphqlSrv.lastReq == nil || graphqlSrv.lastReq.GetApp() != "linear" {
		t.Fatalf("remote graphql request = %#v", graphqlSrv.lastReq)
	}
}

type recordingGraphQLAppServer struct {
	proto.UnimplementedAppServer
	lastReq *proto.AppInvokeGraphQLRequest
}

func (s *recordingGraphQLAppServer) InvokeGraphQL(_ context.Context, req *proto.AppInvokeGraphQLRequest) (*proto.OperationResult, error) {
	s.lastReq = req
	return &proto.OperationResult{Status: 200}, nil
}

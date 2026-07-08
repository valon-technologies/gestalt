package remote_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAppProviderInvokesRemotePublicApp(t *testing.T) {
	t.Parallel()

	baseURL, appServer, closeServer := startRemoteAppTestServer(t)
	defer closeServer()

	clientSet, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:   baseURL,
		Token: "gst_api_test",
	})
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	defer func() { _ = clientSet.Close() }()

	prov := remote.NewAppProvider(clientSet.App, appservice.StaticProviderSpec{
		Name: "linear",
		Catalog: &catalog.Catalog{
			Name: "linear",
			Operations: []catalog.CatalogOperation{
				{ID: "issues.list", Transport: catalog.TransportApp},
			},
		},
	})
	result, err := prov.Execute(context.Background(), "issues.list", map[string]any{"team": "ENG"}, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != 200 || string(result.Body) != `{"ok":true}` {
		t.Fatalf("result = %+v, want 200/{\"ok\":true}", result)
	}
	if appServer.lastApp != "linear" || appServer.lastOperation != "issues.list" {
		t.Fatalf("remote request = %q/%q, want linear/issues.list", appServer.lastApp, appServer.lastOperation)
	}
}

func TestAppProviderPropagatesRemoteNotFound(t *testing.T) {
	t.Parallel()

	baseURL, appServer, closeServer := startRemoteAppTestServer(t)
	defer closeServer()
	appServer.invokeErr = status.Error(codes.NotFound, "app not found")

	clientSet, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:   baseURL,
		Token: "gst_api_test",
	})
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	defer func() { _ = clientSet.Close() }()

	prov := remote.NewAppProvider(clientSet.App, appservice.StaticProviderSpec{
		Name:    "missing-app",
		Catalog: &catalog.Catalog{Name: "missing-app", Operations: []catalog.CatalogOperation{{ID: "issues.list", Transport: catalog.TransportApp}}},
	})
	_, err = prov.Execute(context.Background(), "issues.list", nil, "")
	if err == nil {
		t.Fatal("Execute = nil, want not found")
	}
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("Execute error = %v, want provider not found", err)
	}
}

type recordingRemoteAppServer struct {
	proto.UnimplementedAppServer
	lastApp       string
	lastOperation string
	invokeErr     error
}

func (s *recordingRemoteAppServer) Invoke(_ context.Context, req *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	s.lastApp = req.GetApp()
	s.lastOperation = req.GetOperation()
	if req.GetContext() != nil {
		return nil, status.Error(codes.InvalidArgument, "public invoke must not include request context")
	}
	if s.invokeErr != nil {
		return nil, s.invokeErr
	}
	return &proto.OperationResult{Status: 200, Body: []byte(`{"ok":true}`)}, nil
}

func startRemoteAppTestServer(t *testing.T) (baseURL string, appServer *recordingRemoteAppServer, closeFn func()) {
	t.Helper()

	appServer = &recordingRemoteAppServer{}
	server := grpc.NewServer()
	publicrpc.RegisterPublicAppServer(server, appServer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	return "http://" + listener.Addr().String(), appServer, func() {
		server.GracefulStop()
		<-done
		_ = listener.Close()
	}
}

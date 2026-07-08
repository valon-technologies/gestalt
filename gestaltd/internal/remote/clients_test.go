package remote_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/remote"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestNewClientSetRequiresURL(t *testing.T) {
	t.Parallel()

	_, err := remote.NewClientSet(context.Background(), remote.Config{Token: "token"})
	if err == nil || !strings.Contains(err.Error(), "remote URL is required") {
		t.Fatalf("NewClientSet error = %v, want remote URL is required", err)
	}
}

func TestNewClientSetRequiresToken(t *testing.T) {
	t.Parallel()

	_, err := remote.NewClientSet(context.Background(), remote.Config{URL: "https://gestalt.test"})
	if err == nil || !strings.Contains(err.Error(), "remote token is required") {
		t.Fatalf("NewClientSet error = %v, want remote token is required", err)
	}
}

func TestWithBearerAttachesAuthorizationMetadata(t *testing.T) {
	t.Parallel()

	ctx := remote.WithBearer(context.Background(), "gst_api_test")
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected outgoing metadata")
	}
	values := md.Get("authorization")
	if len(values) != 1 || values[0] != "Bearer gst_api_test" {
		t.Fatalf("authorization metadata = %#v, want Bearer gst_api_test", values)
	}
}

func TestClientSetPreservesGRPCStatusCodes(t *testing.T) {
	t.Parallel()

	ts := startStatusTestServer(t, codes.PermissionDenied, "forbidden-app")
	defer ts.Close()

	clientSet, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:         ts.URL,
		Token:       "gst_api_test",
		DialOptions: testDialOptions(t, ts),
	})
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	defer func() { _ = clientSet.Close() }()

	_, err = clientSet.App.Invoke(remote.WithBearer(context.Background(), "gst_api_test"), &proto.AppInvokeRequest{
		App:       "linear",
		Operation: "issues.list",
	})
	if err == nil {
		t.Fatal("App.Invoke: expected error, got nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError: %v", err)
	}
	if st.Code() != codes.PermissionDenied || st.Message() != "forbidden-app" {
		t.Fatalf("status = (%v, %q), want (%v, %q)", st.Code(), st.Message(), codes.PermissionDenied, "forbidden-app")
	}
}

func TestClientSetCloseReleasesConnection(t *testing.T) {
	t.Parallel()

	ts := startStatusTestServer(t, codes.OK, "")
	defer ts.Close()

	clientSet, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:         ts.URL,
		Token:       "gst_api_test",
		DialOptions: testDialOptions(t, ts),
	})
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	if err := clientSet.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func testDialOptions(t *testing.T, ts *httptest.Server) []grpc.DialOption {
	t.Helper()
	targetURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})),
		grpc.WithAuthority(targetURL.Host),
	}
}

func TestDialOptionsRejectURLWithPath(t *testing.T) {
	t.Parallel()

	_, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:   "https://gestalt.test/api",
		Token: "gst_api_test",
	})
	if err == nil || !strings.Contains(err.Error(), "must not include a path") {
		t.Fatalf("NewClientSet error = %v, want path rejection", err)
	}
}

func TestClientSetUsesBearerOnInvoke(t *testing.T) {
	t.Parallel()

	srv := grpc.NewServer()
	proto.RegisterAppServer(srv, metadataAppServer{t: t})
	ts := httptest.NewUnstartedServer(http.HandlerFunc(srv.ServeHTTP))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)

	clientSet, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:         ts.URL,
		Token:       "gst_api_test",
		DialOptions: testDialOptions(t, ts),
	})
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	defer func() { _ = clientSet.Close() }()

	_, err = clientSet.App.Invoke(remote.WithBearer(context.Background(), "gst_api_test"), &proto.AppInvokeRequest{
		App:       "linear",
		Operation: "issues.list",
	})
	if err != nil {
		t.Fatalf("App.Invoke: %v", err)
	}
}

func startStatusTestServer(t *testing.T, code codes.Code, message string) *httptest.Server {
	t.Helper()

	srv := grpc.NewServer()
	proto.RegisterAppServer(srv, statusAppServer{code: code, message: message})
	ts := httptest.NewUnstartedServer(http.HandlerFunc(srv.ServeHTTP))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return ts
}

type statusAppServer struct {
	proto.UnimplementedAppServer
	code    codes.Code
	message string
}

func (s statusAppServer) Invoke(context.Context, *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	if s.code == codes.OK {
		return &proto.OperationResult{}, nil
	}
	return nil, status.Error(s.code, s.message)
}

type metadataAppServer struct {
	proto.UnimplementedAppServer
	t *testing.T
}

func (s metadataAppServer) Invoke(ctx context.Context, _ *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		s.t.Fatal("expected incoming metadata")
	}
	values := md.Get("authorization")
	if len(values) != 1 || values[0] != "Bearer gst_api_test" {
		s.t.Fatalf("authorization metadata = %#v, want Bearer gst_api_test", values)
	}
	return &proto.OperationResult{}, nil
}

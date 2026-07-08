package remote_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/remote"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestNewClientSetRequiresURL(t *testing.T) {
	t.Parallel()

	_, err := remote.NewClientSet(context.Background(), remote.Config{Token: "token"})
	if err == nil {
		t.Fatal("NewClientSet: expected error for empty URL, got nil")
	}
	if !strings.Contains(err.Error(), "URL is required") {
		t.Fatalf("error = %q, want URL required", err.Error())
	}
}

func TestNewClientSetRequiresToken(t *testing.T) {
	t.Parallel()

	_, err := remote.NewClientSet(context.Background(), remote.Config{URL: "https://valon.tools"})
	if err == nil {
		t.Fatal("NewClientSet: expected error for empty token, got nil")
	}
	if !strings.Contains(err.Error(), "token is required") {
		t.Fatalf("error = %q, want token required", err.Error())
	}
}

func TestWithBearerMetadata(t *testing.T) {
	t.Parallel()

	ctx := remote.WithBearer(context.Background(), "gst_api_test")
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("metadata.FromOutgoingContext: missing outgoing metadata")
	}
	values := md.Get("authorization")
	if len(values) != 1 || values[0] != "Bearer gst_api_test" {
		t.Fatalf("authorization metadata = %#v, want Bearer gst_api_test", values)
	}
}

func TestRemoteGRPCTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url        string
		wantTarget string
		wantErr    string
	}{
		{"https://valon.tools", "valon.tools:443", ""},
		{"https://valon.tools:8443", "valon.tools:8443", ""},
		{"http://localhost:8080", "localhost:8080", ""},
		{"ftp://valon.tools", "", "unsupported URL scheme"},
		{"not-a-url", "", "must include scheme and host"},
	}
	for _, tc := range tests {
		target, creds, err := remote.RemoteGRPCTarget(tc.url)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("RemoteGRPCTarget(%q) err = %v, want %q", tc.url, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("RemoteGRPCTarget(%q): %v", tc.url, err)
		}
		if target != tc.wantTarget {
			t.Fatalf("target = %q, want %q", target, tc.wantTarget)
		}
		if creds == nil {
			t.Fatalf("RemoteGRPCTarget(%q): missing transport credentials", tc.url)
		}
	}
}

func TestRemoteStatusCodesPreserved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code codes.Code
		msg  string
	}{
		{"Unauthenticated", codes.Unauthenticated, "remote token invalid"},
		{"PermissionDenied", codes.PermissionDenied, "remote access denied"},
		{"NotFound", codes.NotFound, "remote app missing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := newTestClientSet(t, tc.code, tc.msg)
			defer func() { _ = client.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := client.App.Invoke(ctx, &proto.AppInvokeRequest{
				App:       "slack",
				Operation: "events.reply",
			})
			if err == nil {
				t.Fatal("App.Invoke: expected error, got nil")
			}
			if status.Code(err) != tc.code {
				t.Fatalf("status code = %v, want %v (%v)", status.Code(err), tc.code, err)
			}
			if status.Convert(err).Message() != tc.msg {
				t.Fatalf("status message = %q, want %q", status.Convert(err).Message(), tc.msg)
			}
		})
	}
}

func TestClientSetCloseReleasesConnection(t *testing.T) {
	t.Parallel()

	client := newTestClientSet(t, codes.OK, "")
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestOutgoingMetadataIncludesBearerOnRPC(t *testing.T) {
	t.Parallel()

	var gotAuth string
	client := newTestClientSet(t, codes.OK, "", func(ctx context.Context) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			t.Fatal("missing incoming metadata")
		}
		values := md.Get("authorization")
		if len(values) != 1 {
			t.Fatalf("authorization metadata = %#v", values)
		}
		gotAuth = values[0]
	})
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.App.Invoke(ctx, &proto.AppInvokeRequest{
		App:       "slack",
		Operation: "events.reply",
	}); err != nil {
		t.Fatalf("App.Invoke: %v", err)
	}
	if gotAuth != "Bearer remote-test-token" {
		t.Fatalf("authorization = %q, want Bearer remote-test-token", gotAuth)
	}
}

func newTestClientSet(
	t *testing.T,
	code codes.Code,
	msg string,
	hooks ...func(context.Context),
) *remote.ClientSet {
	t.Helper()

	const token = "remote-test-token"
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	proto.RegisterAppServer(srv, &testAppServer{
		code:  code,
		msg:   msg,
		hooks: hooks,
	})
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(func(
			ctx context.Context,
			method string,
			req, reply any,
			cc *grpc.ClientConn,
			invoker grpc.UnaryInvoker,
			opts ...grpc.CallOption,
		) error {
			return invoker(remote.WithBearer(ctx, token), method, req, reply, cc, opts...)
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}

	return remote.NewTestClientSet(conn)
}

type testAppServer struct {
	proto.UnimplementedAppServer
	code  codes.Code
	msg   string
	hooks []func(context.Context)
}

func (s *testAppServer) Invoke(ctx context.Context, _ *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	for _, hook := range s.hooks {
		hook(ctx)
	}
	if s.code != codes.OK {
		return nil, status.Error(s.code, s.msg)
	}
	return &proto.OperationResult{}, nil
}

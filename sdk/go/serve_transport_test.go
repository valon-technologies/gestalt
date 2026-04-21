package gestalt_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestServeProviderRoundTrip(t *testing.T) {
	socket := newSocketPath(t, "plugin.sock")
	t.Setenv(proto.EnvProviderSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	provider := &closeableStubProvider{}
	router := gestalt.MustRouter(
		gestalt.Register(
			gestalt.Operation[stubInput, stubOutput]{
				ID:     "test_op",
				Method: "POST",
			},
			(*closeableStubProvider).testOp,
		),
	)
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeProvider(ctx, provider, router)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	client := proto.NewIntegrationProviderClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), time.Second)
	defer rpcCancel()

	meta, err := client.GetMetadata(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetSupportsSessionCatalog() {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestServeAuthenticationProviderClosesProviderOnShutdown(t *testing.T) {
	socket := newSocketPath(t, "a.sock")
	t.Setenv(proto.EnvProviderSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	auth := &closeableStubAuthenticationProvider{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeAuthenticationProvider(ctx, auth)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !auth.closed.Load() {
			t.Fatal("authentication provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	client := proto.NewAuthenticationProviderClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), time.Second)
	defer rpcCancel()

	resp, err := client.BeginLogin(rpcCtx, &proto.BeginLoginRequest{CallbackUrl: "https://gestalt.example.test/callback"}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if resp.GetAuthorizationUrl() == "" {
		t.Fatal("BeginLogin returned empty authorization URL")
	}
}

type closeTracker struct {
	closed atomic.Bool
}

func (c *closeTracker) Close() error {
	c.closed.Store(true)
	return nil
}

type closeableStubProvider struct {
	stubProvider
	closeTracker
}

type closeableStubAuthenticationProvider struct {
	closeTracker
}

func (p *closeableStubAuthenticationProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (p *closeableStubAuthenticationProvider) BeginLogin(_ context.Context, _ *gestalt.BeginLoginRequest) (*gestalt.BeginLoginResponse, error) {
	return &gestalt.BeginLoginResponse{AuthorizationUrl: "https://auth.example.test/login"}, nil
}

func (p *closeableStubAuthenticationProvider) CompleteLogin(_ context.Context, _ *gestalt.CompleteLoginRequest) (*gestalt.AuthenticatedUser, error) {
	return &gestalt.AuthenticatedUser{Email: "user@example.com"}, nil
}

func newSocketPath(t *testing.T, name string) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "gst-sdk-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

package gestalt_test

import (
	"context"
	"net"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/authorization"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type authorizationTransportHarness struct {
	proto.UnimplementedAuthorizationProviderServer
	tokens chan string
}

func (h *authorizationTransportHarness) CheckAccess(ctx context.Context, _ *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	h.tokens <- firstMetadataValue(ctx, "x-gestalt-host-service-relay-token")
	return &proto.CheckAccessResponse{Allowed: true}, nil
}

func TestTransportAuthorizationExplicitTargetUsesRelayTokenEnv(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &authorizationTransportHarness{tokens: make(chan string, 1)}
	srv := grpc.NewServer()
	proto.RegisterAuthorizationProviderServer(srv, harness)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")
	client, err := authorization.New(
		context.Background(),
		authorization.WithTarget("tcp://"+lis.Addr().String()),
	)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	response, err := client.CheckAccess(context.Background(), authorization.CheckAccessRequest{})
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if response == nil || !response.Allowed {
		t.Fatalf("response = %#v, want allowed", response)
	}
	if got := <-harness.tokens; got != "relay-token-go" {
		t.Fatalf("relay token = %q, want relay-token-go", got)
	}
}

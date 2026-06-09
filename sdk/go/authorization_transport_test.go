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
	tokens  chan string
	modelID string
}

func (h *authorizationTransportHarness) CheckAccess(ctx context.Context, _ *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	h.tokens <- firstMetadataValue(ctx, "x-gestalt-host-service-relay-token")
	return &proto.CheckAccessResponse{Allowed: true, ModelId: h.modelID}, nil
}

func TestTransportAuthorizationClientsKeepOwnConnections(t *testing.T) {
	firstHarness, firstTarget := startAuthorizationTransportHarness(t, "first")
	secondHarness, secondTarget := startAuthorizationTransportHarness(t, "second")

	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-first")
	first, err := authorization.New(
		context.Background(),
		authorization.WithTarget(firstTarget),
	)
	if err != nil {
		t.Fatalf("first authorization.New: %v", err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Fatalf("first Close: %v", err)
		}
		if err := first.Close(); err != nil {
			t.Fatalf("first second Close: %v", err)
		}
	})

	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-second")
	second, err := authorization.New(
		context.Background(),
		authorization.WithTarget(secondTarget),
	)
	if err != nil {
		t.Fatalf("second authorization.New: %v", err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Fatalf("second Close: %v", err)
		}
	})

	firstResponse, err := first.CheckAccess(context.Background(), authorization.CheckAccessRequest{})
	if err != nil {
		t.Fatalf("first CheckAccess after second client: %v", err)
	}
	if firstResponse == nil || !firstResponse.Allowed || firstResponse.ModelID != "first" {
		t.Fatalf("first response = %#v, want allowed first", firstResponse)
	}
	if got := <-firstHarness.tokens; got != "relay-token-first" {
		t.Fatalf("first relay token = %q, want relay-token-first", got)
	}

	secondResponse, err := second.CheckAccess(context.Background(), authorization.CheckAccessRequest{})
	if err != nil {
		t.Fatalf("second CheckAccess: %v", err)
	}
	if secondResponse == nil || !secondResponse.Allowed || secondResponse.ModelID != "second" {
		t.Fatalf("second response = %#v, want allowed second", secondResponse)
	}
	if got := <-secondHarness.tokens; got != "relay-token-second" {
		t.Fatalf("second relay token = %q, want relay-token-second", got)
	}
}

func startAuthorizationTransportHarness(t *testing.T, modelID string) (*authorizationTransportHarness, string) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &authorizationTransportHarness{tokens: make(chan string, 1), modelID: modelID}
	srv := grpc.NewServer()
	proto.RegisterAuthorizationProviderServer(srv, harness)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return harness, "tcp://" + lis.Addr().String()
}

package secrets

import (
	"context"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type secretsStub struct {
	proto.SecretsProviderClient

	getCtx context.Context
}

func (s *secretsStub) GetSecret(ctx context.Context, req *proto.GetSecretRequest, _ ...grpc.CallOption) (*proto.GetSecretResponse, error) {
	s.getCtx = ctx
	return &proto.GetSecretResponse{Value: "secret:" + req.GetName()}, nil
}

func TestClientGetSecret(t *testing.T) {
	t.Parallel()

	client := NewClient(&secretsStub{}, Options{})
	value, err := client.GetSecret(context.Background(), "db-password")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if value != "secret:db-password" {
		t.Fatalf("GetSecret = %q, want %q", value, "secret:db-password")
	}
}

func TestClientUsesUnaryTimeout(t *testing.T) {
	t.Parallel()

	const timeout = 30 * time.Second
	stub := &secretsStub{}
	client := NewClient(stub, Options{UnaryTimeout: timeout})
	if _, err := client.GetSecret(context.Background(), "db-password"); err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	deadline, ok := stub.getCtx.Deadline()
	if !ok {
		t.Fatal("GetSecret context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= timeout-2*time.Second || remaining > timeout {
		t.Fatalf("deadline remaining = %s, want within 2s of %s", remaining, timeout)
	}
}

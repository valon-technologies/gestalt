package gestalt_test

import (
	"context"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubSecretsProvider struct {
	closeTracker
	secrets map[string]string
}

func (p *stubSecretsProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (p *stubSecretsProvider) HealthCheck(context.Context) error {
	return nil
}

func (p *stubSecretsProvider) GetSecret(_ context.Context, name string) (string, error) {
	v, ok := p.secrets[name]
	if !ok {
		return "", gestalt.ErrSecretNotFound
	}
	return v, nil
}

func TestSecretsProviderRoundTrip(t *testing.T) {
	socket := newSocketPath(t, "secrets.sock")
	t.Setenv(proto.EnvPluginSocket, socket)

	ctx, cancel := context.WithCancel(context.Background())
	provider := &stubSecretsProvider{
		secrets: map[string]string{"db-password": "hunter2"},
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeSecretsProvider(ctx, provider)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	client := proto.NewSecretsProviderClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()

	resp, err := client.GetSecret(rpcCtx, &proto.GetSecretRequest{Name: "db-password"}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if resp.GetValue() != "hunter2" {
		t.Fatalf("value = %q, want %q", resp.GetValue(), "hunter2")
	}

	_, err = client.GetSecret(rpcCtx, &proto.GetSecretRequest{Name: "nonexistent"})
	if err == nil {
		t.Fatal("GetSecret(nonexistent) should return error")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
		t.Fatalf("GetSecret(nonexistent) code = %v, want NOT_FOUND", err)
	}
}

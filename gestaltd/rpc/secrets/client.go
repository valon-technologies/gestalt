package secrets

import (
	"context"
	"fmt"

	secretsapi "github.com/valon-technologies/gestalt/sdk/go/secrets"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

var _ secretsapi.Client = (*rpcClient)(nil)

type rpcClient struct {
	grpc proto.SecretsProviderClient
	opts Options
}

// Close is a no-op because this client uses shared transport.
func (c *rpcClient) Close() error { return nil }

func (c *rpcClient) GetSecret(ctx context.Context, name string) (string, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.GetSecret(ctx, &proto.GetSecretRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("get secret: %w", err)
	}
	return resp.GetValue(), nil
}

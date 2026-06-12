package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/client"
)

// secretsHandler bridges the ergonomic [SecretsProvider] facade onto the
// generated transport handler; wire conversion lives in the generated
// adapter.
type secretsHandler struct {
	client.UnimplementedSecretsProvider
	secrets SecretsProvider
}

func (s secretsHandler) GetSecret(ctx context.Context, request *client.GetSecretRequest) (*client.GetSecretResponse, error) {
	value, err := s.secrets.GetSecret(ctx, request.GetName())
	if err != nil {
		return nil, providerRPCError("get secret", err)
	}
	return &client.GetSecretResponse{Value: value}, nil
}

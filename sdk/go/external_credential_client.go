package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

const EnvExternalCredentialSocket = "GESTALT_EXTERNAL_CREDENTIAL_SOCKET"
const EnvExternalCredentialSocketToken = EnvExternalCredentialSocket + "_TOKEN"

type ExternalCredentialClient struct {
	client proto.ExternalCredentialProviderClient
}

var sharedExternalCredentialTransport sharedManagerTransport[proto.ExternalCredentialProviderClient]

// ExternalCredentials connects to the ExternalCredentialProvider exposed by
// gestaltd.
func ExternalCredentials() (*ExternalCredentialClient, error) {
	target := os.Getenv(EnvExternalCredentialSocket)
	if target == "" {
		return nil, fmt.Errorf("external credentials: %s is not set", EnvExternalCredentialSocket)
	}
	token := os.Getenv(EnvExternalCredentialSocketToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "external credentials", target, token, &sharedExternalCredentialTransport, proto.NewExternalCredentialProviderClient)
	if err != nil {
		return nil, err
	}
	return &ExternalCredentialClient{client: client}, nil
}

func (c *ExternalCredentialClient) Close() error { return nil }

func (c *ExternalCredentialClient) UpsertCredential(ctx context.Context, req *UpsertExternalCredentialRequest) (*ExternalCredential, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	return c.client.UpsertCredential(ctx, req)
}

func (c *ExternalCredentialClient) GetCredential(ctx context.Context, req *GetExternalCredentialRequest) (*ExternalCredential, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	return c.client.GetCredential(ctx, req)
}

func (c *ExternalCredentialClient) ListCredentials(ctx context.Context, req *ListExternalCredentialsRequest) (*ListExternalCredentialsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	return c.client.ListCredentials(ctx, req)
}

func (c *ExternalCredentialClient) DeleteCredential(ctx context.Context, req *DeleteExternalCredentialRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return fmt.Errorf("external credentials: request is required")
	}
	_, err := c.client.DeleteCredential(ctx, req)
	return err
}

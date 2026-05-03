package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
)

// EnvExternalCredentialSocket names the environment variable containing the
// external-credential service target.
const EnvExternalCredentialSocket = "GESTALT_EXTERNAL_CREDENTIAL_SOCKET"

// EnvExternalCredentialSocketToken names the optional external-credential
// relay-token variable.
const EnvExternalCredentialSocketToken = EnvExternalCredentialSocket + "_TOKEN"

// ExternalCredentialClient calls the host-managed external credential provider.
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

// Close is a no-op compatibility method because this client uses shared transport.
func (c *ExternalCredentialClient) Close() error { return nil }

// UpsertCredential creates or updates a host-managed external credential.
func (c *ExternalCredentialClient) UpsertCredential(ctx context.Context, req *UpsertExternalCredentialRequest) (*ExternalCredential, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	return c.client.UpsertCredential(ctx, req)
}

// GetCredential fetches one host-managed external credential.
func (c *ExternalCredentialClient) GetCredential(ctx context.Context, req *GetExternalCredentialRequest) (*ExternalCredential, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	return c.client.GetCredential(ctx, req)
}

// ListCredentials lists host-managed external credentials.
func (c *ExternalCredentialClient) ListCredentials(ctx context.Context, req *ListExternalCredentialsRequest) (*ListExternalCredentialsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	return c.client.ListCredentials(ctx, req)
}

// DeleteCredential deletes one host-managed external credential.
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

func (c *ExternalCredentialClient) ValidateCredentialConfig(ctx context.Context, req *ValidateExternalCredentialConfigRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return fmt.Errorf("external credentials: request is required")
	}
	_, err := c.client.ValidateCredentialConfig(ctx, req)
	return err
}

func (c *ExternalCredentialClient) ResolveCredential(ctx context.Context, req *ResolveExternalCredentialRequest) (*ResolveExternalCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	return c.client.ResolveCredential(ctx, req)
}

func (c *ExternalCredentialClient) ExchangeCredential(ctx context.Context, req *ExchangeExternalCredentialRequest) (*ExchangeExternalCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	return c.client.ExchangeCredential(ctx, req)
}

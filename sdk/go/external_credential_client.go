package gestalt

import (
	"context"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ExternalCredentialClient calls the host-managed external credential provider.
type ExternalCredentialClient struct {
	client proto.ExternalCredentialProviderClient
}

var sharedExternalCredentialTransport sharedManagerTransport[proto.ExternalCredentialProviderClient]

// ExternalCredentials connects to the ExternalCredentialProvider exposed by
// gestaltd.
func ExternalCredentials() (*ExternalCredentialClient, error) {
	target, token, err := hostServiceTarget("external credentials")
	if err != nil {
		return nil, err
	}

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
	resp, err := c.client.UpsertCredential(ctx, upsertExternalCredentialRequestToProto(req))
	if err != nil {
		return nil, err
	}
	return externalCredentialFromProto(resp)
}

// GetCredential fetches one host-managed external credential.
func (c *ExternalCredentialClient) GetCredential(ctx context.Context, req *GetExternalCredentialRequest) (*ExternalCredential, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	resp, err := c.client.GetCredential(ctx, getExternalCredentialRequestToProto(req))
	if externalCredentialHostServiceMissing(err) {
		return nil, ErrExternalCredentialNotFound
	}
	if err != nil {
		return nil, err
	}
	return externalCredentialFromProto(resp)
}

// ListCredentials lists host-managed external credentials.
func (c *ExternalCredentialClient) ListCredentials(ctx context.Context, req *ListExternalCredentialsRequest) (*ListExternalCredentialsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	resp, err := c.client.ListCredentials(ctx, listExternalCredentialsRequestToProto(req))
	if externalCredentialHostServiceMissing(err) {
		return &ListExternalCredentialsResponse{}, nil
	}
	if err != nil {
		return nil, err
	}
	return listExternalCredentialsResponseFromProto(resp)
}

// DeleteCredential deletes one host-managed external credential.
func (c *ExternalCredentialClient) DeleteCredential(ctx context.Context, req *DeleteExternalCredentialRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return fmt.Errorf("external credentials: request is required")
	}
	_, err := c.client.DeleteCredential(ctx, deleteExternalCredentialRequestToProto(req))
	return err
}

func (c *ExternalCredentialClient) ValidateCredentialConfig(ctx context.Context, req *ValidateExternalCredentialConfigRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return fmt.Errorf("external credentials: request is required")
	}
	_, err := c.client.ValidateCredentialConfig(ctx, validateExternalCredentialConfigRequestToProto(req))
	return err
}

func externalCredentialHostServiceMissing(err error) bool {
	if status.Code(err) != codes.Unimplemented {
		return false
	}
	return strings.Contains(status.Convert(err).Message(), "unknown service gestalt.provider.v1.ExternalCredentialProvider")
}

func (c *ExternalCredentialClient) ResolveCredential(ctx context.Context, req *ResolveExternalCredentialRequest) (*ResolveExternalCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	resp, err := c.client.ResolveCredential(ctx, resolveExternalCredentialRequestToProto(req))
	if externalCredentialHostServiceMissing(err) {
		return nil, ErrExternalCredentialNotFound
	}
	if err != nil {
		return nil, err
	}
	return resolveExternalCredentialResponseFromProto(resp)
}

func (c *ExternalCredentialClient) ExchangeCredential(ctx context.Context, req *ExchangeExternalCredentialRequest) (*ExchangeExternalCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("external credentials: client is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	resp, err := c.client.ExchangeCredential(ctx, exchangeExternalCredentialRequestToProto(req))
	if err != nil {
		return nil, err
	}
	return exchangeExternalCredentialResponseFromProto(resp), nil
}

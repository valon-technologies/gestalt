package externalcredentials

import (
	"context"
	"fmt"

	sdkexternalcredentials "github.com/valon-technologies/gestalt/sdk/go/externalcredentials"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

var _ sdkexternalcredentials.ExternalCredentials = (*rpcClient)(nil)

type rpcClient struct {
	grpc proto.ExternalCredentialProviderClient
	opts Options
}

// Close is a no-op because this capability uses shared transport.
func (c *rpcClient) Close() error { return nil }

func (c *rpcClient) UpsertCredential(ctx context.Context, req *UpsertExternalCredentialRequest) (*ExternalCredential, error) {
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.UpsertCredential(ctx, upsertExternalCredentialRequestToProto(req))
	if err != nil {
		return nil, err
	}
	return externalCredentialFromProto(resp)
}

func (c *rpcClient) GetCredential(ctx context.Context, req *GetExternalCredentialRequest) (*ExternalCredential, error) {
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.GetCredential(ctx, getExternalCredentialRequestToProto(req))
	if err != nil {
		return nil, err
	}
	return externalCredentialFromProto(resp)
}

func (c *rpcClient) ListCredentials(ctx context.Context, req *ListExternalCredentialsRequest) (*ListExternalCredentialsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.ListCredentials(ctx, listExternalCredentialsRequestToProto(req))
	if err != nil {
		return nil, err
	}
	return listExternalCredentialsResponseFromProto(resp)
}

func (c *rpcClient) DeleteCredential(ctx context.Context, req *DeleteExternalCredentialRequest) error {
	if req == nil {
		return fmt.Errorf("external credentials: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err := c.grpc.DeleteCredential(ctx, deleteExternalCredentialRequestToProto(req))
	return err
}

func (c *rpcClient) ValidateCredentialConfig(ctx context.Context, req *ValidateExternalCredentialConfigRequest) error {
	if req == nil {
		return fmt.Errorf("external credentials: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err := c.grpc.ValidateCredentialConfig(ctx, validateExternalCredentialConfigRequestToProto(req))
	return err
}

func (c *rpcClient) ResolveCredential(ctx context.Context, req *ResolveExternalCredentialRequest) (*ResolveExternalCredentialResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.ResolveCredential(ctx, resolveExternalCredentialRequestToProto(req))
	if err != nil {
		return nil, err
	}
	return resolveExternalCredentialResponseFromProto(resp)
}

func (c *rpcClient) ExchangeCredential(ctx context.Context, req *ExchangeExternalCredentialRequest) (*ExchangeExternalCredentialResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("external credentials: request is required")
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.ExchangeCredential(ctx, exchangeExternalCredentialRequestToProto(req))
	if err != nil {
		return nil, err
	}
	return exchangeExternalCredentialResponseFromProto(resp), nil
}

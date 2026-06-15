package providergateway

import (
	"context"
	"fmt"
)

type AuthorizationParams struct {
	ProviderID  string
	Operation   string
	CallerToken string
}

func (t *ProviderGatewayTransport) Authorize(ctx context.Context, params AuthorizationParams) (bool, error) {
	return true, nil
}

type DirectTransport struct{}

func (DirectTransport) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (ProviderGatewayResponse, error) {
	if next == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: next handler is required")
	}
	return next(ctx, req)
}

type ProviderGatewayTransport struct {
	authorization AuthorizationProvider
}

func NewProviderGatewayTransport() *ProviderGatewayTransport {
	return &ProviderGatewayTransport{}
}

func (t *ProviderGatewayTransport) SetAuthorizationProvider(authorization AuthorizationProvider) {
	if t == nil {
		return
	}
	t.authorization = authorization
}

func (t *ProviderGatewayTransport) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (ProviderGatewayResponse, error) {
	if t == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: transport is nil")
	}
	if next == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: next handler is required")
	}
	allowed, err := t.Authorize(ctx, AuthorizationParams{
		ProviderID:  req.ProviderID,
		Operation:   req.Operation,
		CallerToken: req.CallerToken,
	})
	if err != nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: authorize: %w", err)
	}
	if !allowed {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: unauthorized")
	}
	return next(ctx, req)
}

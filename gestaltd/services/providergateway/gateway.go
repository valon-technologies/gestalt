package providergateway

import (
	"context"
	"fmt"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type AuthorizationParams struct {
	ProviderID  string
	Operation   string
	CallerToken string
}

func (t *ProviderGatewayTransport) Authorize(ctx context.Context, params AuthorizationParams) (bool, error) {
	if t == nil || t.authorization == nil {
		return true, nil
	}
	req := &proto.CheckAccessRequest{
		Action: &proto.Action{
			Name: params.Operation,
		},
	}
	if params.ProviderID != "" {
		req.Resource = &proto.Resource{
			Type: "provider",
			Id:   params.ProviderID,
		}
	}
	ctx = WithSource(ctx, GatewaySourceInternal)
	ctx = WithCallerToken(ctx, params.CallerToken)
	resp, err := t.authorization.CheckAccess(ctx, req)
	if err != nil {
		return false, err
	}
	if resp == nil {
		return false, fmt.Errorf("provider gateway: check access returned nil response")
	}
	return resp.GetAllowed(), nil
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

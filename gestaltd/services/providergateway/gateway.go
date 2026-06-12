package providergateway

import (
	"context"
	"fmt"
	"time"
)

type Gateway struct {
	callerTokenIssuer *CallerTokenIssuer
	transport         Transport
}

type AuthorizationParams struct {
	ProviderID  string
	Operation   string
	CallerToken string
}

type GatewayOption func(*Gateway)

func WithCallerTokenIssuer(issuer *CallerTokenIssuer) GatewayOption {
	return func(g *Gateway) {
		g.callerTokenIssuer = issuer
	}
}

func WithTransport(transport Transport) GatewayOption {
	return func(g *Gateway) {
		g.transport = transport
	}
}

func New(opts ...GatewayOption) *Gateway {
	g := &Gateway{transport: DirectTransport{}}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	return g
}

func (g *Gateway) IssueCallerToken(subjectID string, now time.Time) (string, bool, error) {
	if g == nil || g.callerTokenIssuer == nil {
		return "", false, nil
	}
	claims, err := GenerateCallerTokenClaims(subjectID, now)
	if err != nil {
		return "", true, err
	}
	token, err := g.callerTokenIssuer.Issue(claims)
	if err != nil {
		return "", true, err
	}
	return token, true, nil
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
	next Transport
}

func NewProviderGatewayTransport(next Transport) *ProviderGatewayTransport {
	if next == nil {
		next = DirectTransport{}
	}
	return &ProviderGatewayTransport{next: next}
}

func (t *ProviderGatewayTransport) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (ProviderGatewayResponse, error) {
	if t == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: transport is nil")
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
	return t.next.Invoke(ctx, req, next)
}

func (g *Gateway) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (ProviderGatewayResponse, error) {
	transport := Transport(DirectTransport{})
	if g != nil && g.transport != nil {
		transport = g.transport
	}
	return transport.Invoke(ctx, req, next)
}

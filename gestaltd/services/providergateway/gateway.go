package providergateway

import (
	"context"
	"fmt"
	"time"
)

type Gateway struct {
	callerTokenIssuer *CallerTokenIssuer
}

type GatewayOption func(*Gateway)

func WithCallerTokenIssuer(issuer *CallerTokenIssuer) GatewayOption {
	return func(g *Gateway) {
		g.callerTokenIssuer = issuer
	}
}

func New(opts ...GatewayOption) *Gateway {
	g := &Gateway{}
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

func (g *Gateway) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (ProviderGatewayResponse, error) {
	if next == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: next handler is required")
	}
	return next(ctx, req)
}

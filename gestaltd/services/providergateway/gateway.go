package providergateway

import (
	"context"
	"fmt"
	"strings"
)

type Gateway struct {
	callerTokenIssuer *CallerTokenIssuer
}

type CallerTokenIssuer struct {
	privateKey string
}

func NewCallerTokenIssuer(privateKey string) *CallerTokenIssuer {
	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		return nil
	}
	return &CallerTokenIssuer{privateKey: privateKey}
}

func (i *CallerTokenIssuer) privateKeyForTesting() string {
	if i == nil {
		return ""
	}
	return i.privateKey
}

type GatewayOption func(*Gateway)

func WithCallerTokenPrivateKey(privateKey string) GatewayOption {
	return WithCallerTokenIssuer(NewCallerTokenIssuer(privateKey))
}

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

func (g *Gateway) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (ProviderGatewayResponse, error) {
	if next == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: next handler is required")
	}
	return next(ctx, req)
}

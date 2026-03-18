package oauth

import (
	"context"
	"fmt"
	"net/url"

	"github.com/valon-technologies/toolshed/core"
)

// ManualAuthHandler implements the non-OAuth parts of core.Integration
// for integrations that use static API keys or tokens.
type ManualAuthHandler struct{}

func (h ManualAuthHandler) AuthorizationURL(state string, _ []string) string {
	return "manual://configure?state=" + url.QueryEscape(state)
}

func (h ManualAuthHandler) ExchangeCode(_ context.Context, code string) (*core.TokenResponse, error) {
	if code == "" {
		return nil, fmt.Errorf("token is required")
	}
	return &core.TokenResponse{AccessToken: code, TokenType: "Bearer"}, nil
}

func (h ManualAuthHandler) RefreshToken(_ context.Context, refreshToken string) (*core.TokenResponse, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("token is required")
	}
	return &core.TokenResponse{AccessToken: refreshToken, TokenType: "Bearer"}, nil
}

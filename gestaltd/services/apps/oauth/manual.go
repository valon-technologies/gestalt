package oauth

import (
	"context"
	"fmt"
	"net/url"

	"github.com/valon-technologies/gestalt/server/core"
)

type ManualAuthHandler struct{}

func (h ManualAuthHandler) IsManual() bool { return true }

func (h ManualAuthHandler) AuthorizationURL(state string, _ []string) string {
	return "manual://configure?state=" + url.QueryEscape(state)
}

func (h ManualAuthHandler) ExchangeCode(_ context.Context, code string) (*core.OAuthTokenResponse, error) {
	if code == "" {
		return nil, fmt.Errorf("token is required")
	}
	return &core.OAuthTokenResponse{AccessToken: code, TokenType: "Bearer"}, nil
}

func (h ManualAuthHandler) RefreshToken(_ context.Context, refreshToken string) (*core.OAuthTokenResponse, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("token is required")
	}
	return &core.OAuthTokenResponse{AccessToken: refreshToken, TokenType: "Bearer"}, nil
}

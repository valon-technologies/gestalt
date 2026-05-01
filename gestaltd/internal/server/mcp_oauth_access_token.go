package server

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const (
	mcpOAuthAccessTokenIssuer   = "gestaltd"
	mcpOAuthAccessTokenAudience = "gestalt-mcp"
)

type mcpOAuthAccessTokenClaims struct {
	jwt.RegisteredClaims
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Scope       string `json:"scope,omitempty"`
}

func (s *Server) issueMCPOAuthAccessToken(identity *core.UserIdentity, scope string, ttl time.Duration) (string, error) {
	if len(s.sessionIssuer) == 0 {
		return "", fmt.Errorf("mcp oauth access token secret is not configured")
	}
	claims := mcpOAuthAccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    mcpOAuthAccessTokenIssuer,
			Audience:  jwt.ClaimStrings{mcpOAuthAccessTokenAudience},
			ExpiresAt: jwt.NewNumericDate(s.now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(s.now()),
		},
		Email:       identity.Email,
		DisplayName: identity.DisplayName,
		AvatarURL:   identity.AvatarURL,
		Scope:       scope,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.sessionIssuer)
}

func (s *Server) validateMCPOAuthAccessToken(tokenStr string) (*principal.Principal, error) {
	if len(s.sessionIssuer) == 0 {
		return nil, principal.ErrInvalidToken
	}
	token, err := jwt.ParseWithClaims(
		tokenStr,
		&mcpOAuthAccessTokenClaims{},
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return s.sessionIssuer, nil
		},
		jwt.WithIssuer(mcpOAuthAccessTokenIssuer),
		jwt.WithAudience(mcpOAuthAccessTokenAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, principal.ErrInvalidToken
	}
	claims, ok := token.Claims.(*mcpOAuthAccessTokenClaims)
	if !ok || claims.Email == "" {
		return nil, principal.ErrInvalidToken
	}
	return &principal.Principal{
		Identity: &core.UserIdentity{
			Email:       claims.Email,
			DisplayName: claims.DisplayName,
			AvatarURL:   claims.AvatarURL,
		},
		Kind:   principal.KindUser,
		Source: principal.SourceSession,
	}, nil
}

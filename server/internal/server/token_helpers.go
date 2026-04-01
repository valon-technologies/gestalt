package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const defaultIssuedTokenExpiry = 30 * 24 * time.Hour
const neverExpiresHint = "never"

type issuedToken struct {
	Plaintext string
	Hashed    string
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt *time.Time
}

func (s *Server) nowUTCSecond() time.Time {
	return s.now().UTC().Truncate(time.Second)
}

func (s *Server) issueToken() (*issuedToken, error) {
	return s.issueTokenWithTypeAndExpiryHint(principal.TokenTypeAPI, "")
}

func (s *Server) issueTokenWithTypeAndExpiryHint(typ principal.TokenType, expiryHint string) (*issuedToken, error) {
	plaintext, hashed, err := principal.GenerateToken(typ)
	if err != nil {
		return nil, err
	}

	now := s.nowUTCSecond()
	expiresAt, err := s.tokenExpiryForHint(now, expiryHint)
	if err != nil {
		return nil, err
	}
	return &issuedToken{
		Plaintext: plaintext,
		Hashed:    hashed,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Server) tokenExpiryForHint(now time.Time, expiryHint string) (*time.Time, error) {
	hint := strings.TrimSpace(expiryHint)
	switch {
	case hint == "":
		ttl := s.apiTokenTTL
		if ttl == 0 {
			ttl = defaultIssuedTokenExpiry
		}
		expiry := now.Add(ttl)
		return &expiry, nil
	case strings.EqualFold(hint, neverExpiresHint):
		return nil, nil
	default:
		ttl, err := config.ParseDuration(hint)
		if err != nil {
			return nil, fmt.Errorf("invalid expires_in: %w", err)
		}
		expiry := now.Add(ttl)
		return &expiry, nil
	}
}

func apiTokenInfoFromCore(token *core.APIToken) apiTokenInfo {
	return apiTokenInfo{
		ID:        token.ID,
		Name:      token.Name,
		Scopes:    token.Scopes,
		CreatedAt: token.CreatedAt,
		ExpiresAt: token.ExpiresAt,
	}
}

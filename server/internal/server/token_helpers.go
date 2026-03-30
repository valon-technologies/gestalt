package server

import (
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const defaultIssuedTokenExpiry = 30 * 24 * time.Hour

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
	return s.issueTokenWithType(principal.TokenTypeAPI)
}

func (s *Server) issueTokenWithType(typ principal.TokenType) (*issuedToken, error) {
	plaintext, hashed, err := principal.GenerateToken(typ)
	if err != nil {
		return nil, err
	}

	now := s.nowUTCSecond()
	ttl := s.apiTokenTTL
	if ttl == 0 {
		ttl = defaultIssuedTokenExpiry
	}
	expiry := now.Add(ttl)
	return &issuedToken{
		Plaintext: plaintext,
		Hashed:    hashed,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: &expiry,
	}, nil
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

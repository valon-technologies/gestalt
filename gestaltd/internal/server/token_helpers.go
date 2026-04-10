package server

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const defaultIssuedTokenExpiry = 30 * 24 * time.Hour
const cliLoginTokenName = "cli-token"

func (s *Server) nowUTCSecond() time.Time {
	return s.now().UTC().Truncate(time.Second)
}

func (s *Server) issueAPIToken(ctx context.Context, userID, name, scopes string, nonExpiring bool) (*core.APIToken, string, error) {
	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		return nil, "", err
	}

	now := s.nowUTCSecond()
	apiToken := &core.APIToken{
		ID:          uuid.NewString(),
		UserID:      userID,
		Name:        name,
		HashedToken: hashed,
		Scopes:      scopes,
		ExpiresAt:   s.apiTokenExpiry(now, nonExpiring),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.apiTokens.StoreAPIToken(ctx, apiToken); err != nil {
		return nil, "", err
	}
	return apiToken, plaintext, nil
}

func (s *Server) apiTokenExpiry(now time.Time, nonExpiring bool) *time.Time {
	if nonExpiring {
		return nil
	}
	ttl := s.apiTokenTTL
	if ttl == 0 {
		ttl = defaultIssuedTokenExpiry
	}
	expiry := now.Add(ttl)
	return &expiry
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

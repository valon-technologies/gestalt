package server

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const defaultIssuedTokenExpiry = 30 * 24 * time.Hour
const defaultCLIRefreshTokenExpiry = 90 * 24 * time.Hour
const internalAPITokenNamePrefix = "__gestalt_internal__:"
const cliRefreshTokenName = internalAPITokenNamePrefix + "cli-refresh"

func (s *Server) nowUTCSecond() time.Time {
	return s.now().UTC().Truncate(time.Second)
}

func (s *Server) issueStoredToken(ctx context.Context, userID, name, scopes string, typ principal.TokenType, expiresAt *time.Time) (*core.APIToken, string, error) {
	plaintext, hashed, err := principal.GenerateToken(typ)
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
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.datastore.StoreAPIToken(ctx, apiToken); err != nil {
		return nil, "", err
	}
	return apiToken, plaintext, nil
}

func (s *Server) issueAPIToken(ctx context.Context, userID, name, scopes string, nonExpiring bool) (*core.APIToken, string, error) {
	return s.issueStoredToken(
		ctx,
		userID,
		name,
		scopes,
		principal.TokenTypeAPI,
		s.apiTokenExpiry(s.nowUTCSecond(), nonExpiring),
	)
}

func (s *Server) issueCLIRefreshToken(ctx context.Context, userID string) (*core.APIToken, string, error) {
	expiry := s.nowUTCSecond().Add(defaultCLIRefreshTokenExpiry)
	return s.issueStoredToken(ctx, userID, cliRefreshTokenName, "", principal.TokenTypeAPI, &expiry)
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

func isInternalAPITokenName(name string) bool {
	return strings.HasPrefix(name, internalAPITokenNamePrefix)
}

func isCLIRefreshToken(token *core.APIToken) bool {
	return token != nil && token.Name == cliRefreshTokenName
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

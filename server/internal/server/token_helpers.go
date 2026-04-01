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
	return s.issueTokenWithType(principal.TokenTypeAPI, s.defaultTokenExpiry(s.nowUTCSecond()))
}

func (s *Server) issueNonExpiringToken() (*issuedToken, error) {
	return s.issueTokenWithType(principal.TokenTypeAPI, nil)
}

func (s *Server) issueTokenWithType(typ principal.TokenType, expiresAt *time.Time) (*issuedToken, error) {
	plaintext, hashed, err := principal.GenerateToken(typ)
	if err != nil {
		return nil, err
	}

	now := s.nowUTCSecond()
	return &issuedToken{
		Plaintext: plaintext,
		Hashed:    hashed,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Server) defaultTokenExpiry(now time.Time) *time.Time {
	ttl := s.apiTokenTTL
	if ttl == 0 {
		ttl = defaultIssuedTokenExpiry
	}
	expiry := now.Add(ttl)
	return &expiry
}

func (s *Server) storeAPIToken(ctx context.Context, userID, name, scopes string, issued *issuedToken) (*core.APIToken, error) {
	apiToken := &core.APIToken{
		ID:          uuid.NewString(),
		UserID:      userID,
		Name:        name,
		HashedToken: issued.Hashed,
		Scopes:      scopes,
		ExpiresAt:   issued.ExpiresAt,
		CreatedAt:   issued.CreatedAt,
		UpdatedAt:   issued.UpdatedAt,
	}
	if err := s.datastore.StoreAPIToken(ctx, apiToken); err != nil {
		return nil, err
	}
	return apiToken, nil
}

func (s *Server) issueCLILoginToken(ctx context.Context, userID string) (*core.APIToken, *issuedToken, error) {
	issued, err := s.issueNonExpiringToken()
	if err != nil {
		return nil, nil, err
	}
	apiToken, err := s.storeAPIToken(ctx, userID, cliLoginTokenName, "", issued)
	if err != nil {
		return nil, nil, err
	}
	return apiToken, issued, nil
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

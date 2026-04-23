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
	return s.issueOwnedAPIToken(ctx, &core.APIToken{
		OwnerKind:           core.APITokenOwnerKindUser,
		OwnerID:             userID,
		CredentialSubjectID: principal.UserSubjectID(userID),
		Name:                name,
		Scopes:              scopes,
	}, nonExpiring)
}

func (s *Server) issueOwnedAPIToken(ctx context.Context, apiToken *core.APIToken, nonExpiring bool) (*core.APIToken, string, error) {
	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		return nil, "", err
	}

	now := s.nowUTCSecond()
	apiToken.ID = uuid.NewString()
	apiToken.HashedToken = hashed
	apiToken.ExpiresAt = s.apiTokenExpiry(now, nonExpiring)
	apiToken.CreatedAt = now
	apiToken.UpdatedAt = now
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
		ID:          token.ID,
		Name:        token.Name,
		Scopes:      token.Scopes,
		Permissions: append([]core.AccessPermission(nil), token.Permissions...),
		CreatedAt:   token.CreatedAt,
		ExpiresAt:   token.ExpiresAt,
	}
}

package server

import (
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/principal"
)

const defaultIssuedTokenExpiry = 90 * 24 * time.Hour

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
	plaintext, err := generateRandomHex(32)
	if err != nil {
		return nil, err
	}

	now := s.nowUTCSecond()
	expiry := now.Add(defaultIssuedTokenExpiry)
	return &issuedToken{
		Plaintext: plaintext,
		Hashed:    principal.HashToken(plaintext),
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

func egressClientResponseFromCore(client *core.EgressClient) egressClientResponse {
	return egressClientResponse{
		ID:          client.ID,
		Name:        client.Name,
		Description: client.Description,
		CreatedByID: client.CreatedByID,
		CreatedAt:   client.CreatedAt,
		UpdatedAt:   client.UpdatedAt,
	}
}

func egressClientTokenInfoFromCore(token *core.EgressClientToken) egressClientTokenInfo {
	return egressClientTokenInfo{
		ID:        token.ID,
		Name:      token.Name,
		CreatedAt: token.CreatedAt,
		ExpiresAt: token.ExpiresAt,
	}
}

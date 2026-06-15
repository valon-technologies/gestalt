package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/authentication"
)

const cliLoginTokenName = "cli-token"

type createGrantResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name,omitempty"`
	Token     string     `json:"token,omitempty"`
	Scopes    []string   `json:"scopes,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type grantInfo struct {
	ID        string   `json:"id"`
	Scopes    []string `json:"scopes,omitempty"`
	CreatedAt int64    `json:"createdAt,omitempty"`
	ExpiresAt int64    `json:"expiresAt,omitempty"`
}

func (s *Server) callerAuthContext(ctx context.Context, r *http.Request) context.Context {
	token, err := requestBearerToken(r)
	if err != nil || strings.TrimSpace(token) == "" {
		if c, cookieErr := r.Cookie(sessionCookieName); cookieErr == nil {
			token = strings.TrimSpace(c.Value)
		}
	}
	if strings.TrimSpace(token) == "" {
		return ctx
	}
	return authentication.WithCallerBearerToken(ctx, token)
}

func (s *Server) callerBearerToken(r *http.Request) (string, error) {
	token, err := requestBearerToken(r)
	if err == nil && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token), nil
	}
	if c, cookieErr := r.Cookie(sessionCookieName); cookieErr == nil {
		if v := strings.TrimSpace(c.Value); v != "" {
			return v, nil
		}
	}
	return "", errors.New("caller bearer token required")
}

func grantInfoFromResponse(grantID string, resp *core.GetGrantResponse) grantInfo {
	info := grantInfo{ID: grantID}
	if resp == nil {
		return info
	}
	info.CreatedAt = resp.CreatedAt
	info.ExpiresAt = resp.ExpiresAt
	for _, scope := range resp.Scopes {
		if scope.Scope != "" {
			info.Scopes = append(info.Scopes, scope.Scope)
		}
	}
	return info
}

func tokenExpiresAt(now func() time.Time, expiresIn int) *time.Time {
	if expiresIn <= 0 {
		return nil
	}
	t := now().UTC().Add(time.Duration(expiresIn) * time.Second)
	return &t
}

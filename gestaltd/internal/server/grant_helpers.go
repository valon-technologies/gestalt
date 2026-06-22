package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity"
)

type createGrantResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name,omitempty"`
	Token     string     `json:"token,omitempty"`
	Scopes    []string   `json:"scopes,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type grantInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	CreatedAt string   `json:"createdAt,omitempty"`
	ExpiresAt string   `json:"expiresAt,omitempty"`
}

func (s *Server) callerAuthContext(ctx context.Context, r *http.Request) context.Context {
	token, err := requestSessionOrBearerToken(r)
	if err != nil || strings.TrimSpace(token) == "" {
		return ctx
	}
	return identity.WithCallerBearerToken(ctx, token)
}

func (s *Server) callerBearerToken(r *http.Request) (string, error) {
	token, err := requestSessionOrBearerToken(r)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", errors.New("caller bearer token required")
	}
	return strings.TrimSpace(token), nil
}

func grantUnixSecondsToRFC3339(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

func grantInfoFromResponse(grantID string, resp *core.GetGrantResponse) grantInfo {
	info := grantInfo{ID: grantID, Name: grantID}
	if resp == nil {
		return info
	}
	info.CreatedAt = grantUnixSecondsToRFC3339(resp.CreatedAt)
	info.ExpiresAt = grantUnixSecondsToRFC3339(resp.ExpiresAt)
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

// tokenExpiresIn resolves a caller-supplied API-token lifetime hint to the
// seconds value passed to the identity provider. A nil or zero hint yields 0,
// meaning the provider applies its default. Negative values and values above
// MaxTokenExpiresInSeconds are rejected.
func tokenExpiresIn(expiresIn *int64) (int64, error) {
	if expiresIn == nil {
		return 0, nil
	}
	if *expiresIn < 0 || *expiresIn > core.MaxTokenExpiresInSeconds {
		return 0, errors.New("expiresIn out of range")
	}
	if *expiresIn == 0 {
		return 0, nil
	}
	return *expiresIn, nil
}

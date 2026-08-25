package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type createGrantResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name,omitempty"`
	Token     string     `json:"token,omitempty"`
	Scopes    []string   `json:"scopes,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (s *Server) callerAuthContext(ctx context.Context, r *http.Request) context.Context {
	token, err := requestSessionOrBearerToken(r)
	if err != nil || strings.TrimSpace(token) == "" {
		return ctx
	}
	token = strings.TrimSpace(token)
	ctx = identity.WithCallerBearerToken(ctx, token)
	p := PrincipalFromContext(ctx)
	if p == nil {
		return ctx
	}
	subject := strings.TrimSpace(principal.Canonicalized(p).SubjectID)
	if subject == "" {
		return ctx
	}
	return withVerifiedCallerSubject(ctx, subject)
}

func withVerifiedCallerSubject(ctx context.Context, subject string) context.Context {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return ctx
	}
	call := gestalt.IdentityCallContextFromContext(ctx)
	call.CallerSubjectID = subject
	ctx = gestalt.WithIdentityCallContext(ctx, call)
	return gestalt.WithTrustedCallerSubject(ctx, subject)
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

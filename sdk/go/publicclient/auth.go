package publicclient

import (
	"context"
	"fmt"
	"strings"
)

// Auth supplies credentials for public gestaltd requests.
type Auth interface {
	Apply(ctx context.Context, req *Request) error
}

// Request is outgoing HTTP metadata that Auth implementations may mutate.
type Request struct {
	Headers map[string]string
}

// TokenProvider returns a bearer token for one outbound call.
type TokenProvider func(ctx context.Context) (string, error)

type bearerAuth struct {
	provider TokenProvider
}

// Bearer sends Authorization: Bearer <token> using provider on every call.
func Bearer(provider TokenProvider) Auth {
	return bearerAuth{provider: provider}
}

func (a bearerAuth) Apply(ctx context.Context, req *Request) error {
	if a.provider == nil || req == nil {
		return nil
	}
	token, err := a.provider(ctx)
	if err != nil {
		return fmt.Errorf("publicclient: bearer token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	req.Headers["Authorization"] = "Bearer " + token
	return nil
}

// Unauthenticated sends no credentials.
func Unauthenticated() Auth { return nil }

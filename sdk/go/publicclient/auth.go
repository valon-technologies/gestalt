package publicclient

import "context"

// Auth supplies credentials for public gestaltd requests.
type Auth interface {
	Apply(ctx context.Context, req *Request) error
}

// Request is the outgoing HTTP request metadata Auth implementations mutate.
type Request struct {
	Headers map[string]string
}

// BearerAuth sends Authorization: Bearer <token> on every request.
type BearerAuth struct {
	Token string
}

// Apply implements Auth.
func (a BearerAuth) Apply(_ context.Context, req *Request) error {
	if req == nil {
		return nil
	}
	if a.Token != "" {
		if req.Headers == nil {
			req.Headers = map[string]string{}
		}
		req.Headers["Authorization"] = "Bearer " + a.Token
	}
	return nil
}

// NoAuth sends no credentials. gRPC calls may still use per-RPC metadata.
type NoAuth struct{}

// Apply implements Auth.
func (NoAuth) Apply(context.Context, *Request) error { return nil }

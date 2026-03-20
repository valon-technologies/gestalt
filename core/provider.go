package core

import "context"

type ConnectionMode string

const (
	ConnectionModeNone     ConnectionMode = "none"
	ConnectionModeUser     ConnectionMode = "user"
	ConnectionModeIdentity ConnectionMode = "identity"
	ConnectionModeEither   ConnectionMode = "either"
)

type Provider interface {
	Name() string
	DisplayName() string
	Description() string
	ConnectionMode() ConnectionMode
	ListOperations() []Operation
	Execute(ctx context.Context, operation string, params map[string]any, token string) (*OperationResult, error)
}

type OAuthProvider interface {
	Provider
	AuthorizationURL(state string, scopes []string) string
	ExchangeCode(ctx context.Context, code string) (*TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error)
}

type ManualProvider interface {
	Provider
	SupportsManualAuth() bool
}

// CatalogProvider is an optional interface that providers can implement to
// expose rich MCP-facing metadata beyond the flat Operation list.
// Returns *integration.Catalog; declared as any to avoid an import cycle.
type CatalogProvider interface {
	Catalog() any
}

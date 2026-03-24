package core

import (
	"context"
	"net/http"

	"github.com/valon-technologies/gestalt/core/catalog"
)

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
type CatalogProvider interface {
	Catalog() *catalog.Catalog
}

type ConnectionParamDef struct {
	Required    bool
	Description string
	Default     string
	From        string // "" = user-provided, "token_response" = extracted from OAuth response
	Field       string // JSON field name for token_response extraction
}

type ConnectionParamProvider interface {
	ConnectionParamDefs() map[string]ConnectionParamDef
}

type PostConnectHook func(ctx context.Context, token *IntegrationToken, client *http.Client) (map[string]string, error)

type PostConnectProvider interface {
	PostConnectHook() PostConnectHook
}

type AuthTypeLister interface {
	AuthTypes() []string
}

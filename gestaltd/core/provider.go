package core

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
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
	Catalog() *catalog.Catalog
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

// SessionCatalogProvider is an optional interface for providers whose MCP tool
// surface depends on request-scoped authentication and must be resolved after a
// user is connected.
type SessionCatalogProvider interface {
	CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error)
}

// OperationConnectionProvider is an optional interface for providers whose
// operations should default to different stored connections. This is used by
// composite providers that route different operations to different backends.
type OperationConnectionProvider interface {
	ConnectionForOperation(operation string) string
}

type ConnectionParamDef struct {
	Required    bool
	Description string
	Default     string
	From        string // "" = user-provided, "token_response" = from OAuth response, "discovery" = from post-connect discovery
	Field       string // JSON field name for token_response extraction
}

type ConnectionParamProvider interface {
	ConnectionParamDefs() map[string]ConnectionParamDef
}

type CredentialFieldDef struct {
	Name        string
	Label       string
	Description string
	HelpURL     string
}

type CredentialFieldsProvider interface {
	CredentialFields() []CredentialFieldDef
}

type AuthTypeLister interface {
	AuthTypes() []string
}

type DiscoveryCandidate struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata"`
}

type DiscoveryConfig struct {
	URL             string
	ItemsPath       string
	IDPath          string
	NamePath        string
	Metadata map[string]string
}

type DiscoveryConfigProvider interface {
	DiscoveryConfig() *DiscoveryConfig
}

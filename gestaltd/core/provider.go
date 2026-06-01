package core

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type ConnectionMode string

const (
	ConnectionModeNone    ConnectionMode = "none"
	ConnectionModeSubject ConnectionMode = "subject"
)

func NormalizeConnectionMode(mode ConnectionMode) ConnectionMode {
	normalized := normalizeConnectionModeValue(mode)
	switch normalized {
	case "", ConnectionModeSubject:
		return ConnectionModeSubject
	case ConnectionModeNone:
		return ConnectionModeNone
	default:
		return normalized
	}
}

func NormalizeOptionalConnectionMode(mode ConnectionMode) ConnectionMode {
	normalized := normalizeConnectionModeValue(mode)
	if normalized == "" {
		return ""
	}
	return NormalizeConnectionMode(normalized)
}

func normalizeConnectionModeValue(mode ConnectionMode) ConnectionMode {
	return ConnectionMode(strings.ToLower(strings.TrimSpace(string(mode))))
}

type Provider interface {
	Name() string
	DisplayName() string
	Description() string
	ConnectionMode() ConnectionMode
	AuthTypes() []string
	ConnectionParamDefs() map[string]ConnectionParamDef
	CredentialFields() []CredentialFieldDef
	DiscoveryConfig() *DiscoveryConfig
	ConnectionForOperation(operation string) string
	Catalog() *catalog.Catalog
	Execute(ctx context.Context, operation string, params map[string]any, token string) (*OperationResult, error)
}

// OperationConnectionSelector maps a caller-supplied parameter value to the
// named connection Gestalt should use for an operation invocation.
type OperationConnectionSelector struct {
	Parameter string
	Default   string
	Values    map[string]string
}

// OperationConnectionResolver is implemented by providers that choose an
// operation connection from invocation parameters before credential lookup.
type OperationConnectionResolver interface {
	ResolveConnectionForOperation(operation string, params map[string]any) (string, error)
}

// OperationConnectionOverridePolicy is implemented by providers that can tell
// whether a caller-supplied connection may override the operation connection.
type OperationConnectionOverridePolicy interface {
	OperationConnectionOverrideAllowed(operation string, params map[string]any) bool
}

// GraphQLSurfaceInvoker is an optional interface for providers that expose a
// raw GraphQL surface in addition to cataloged operations.
type GraphQLSurfaceInvoker interface {
	InvokeGraphQL(ctx context.Context, request GraphQLRequest, token string) (*OperationResult, error)
}

type GraphQLRequest struct {
	Document      string
	OperationName string
	Variables     map[string]any
}

type OAuthProvider interface {
	Provider
	AuthorizationURL(state string, scopes []string) string
	ExchangeCode(ctx context.Context, code string) (*TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error)
}

// SessionCatalogProvider is an optional interface for providers whose MCP tool
// surface depends on request-scoped authentication and must be resolved after a
// user is connected.
type SessionCatalogProvider interface {
	CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error)
}

type ConnectionParamDef struct {
	Required    bool
	Description string
	Default     string
	From        string // "" = user-provided, "token_response" = from OAuth response, "discovery" = from provider discovery
	Field       string // JSON field name for token_response extraction
}

type CredentialFieldDef struct {
	Name        string
	Label       string
	Description string
}

type DiscoveryCandidate struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata"`
}

type DiscoveryConfig struct {
	URL       string
	ItemsPath string
	IDPath    string
	NamePath  string
	Metadata  map[string]string
}

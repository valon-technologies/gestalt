package core

import (
	"context"
	"slices"

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

type ConnectionSpec struct {
	AuthTypes        []string
	ConnectionParams map[string]ConnectionParamDef
	Discovery        *DiscoveryConfig
}

func (s ConnectionSpec) Clone() ConnectionSpec {
	return ConnectionSpec{
		AuthTypes:        slices.Clone(s.AuthTypes),
		ConnectionParams: cloneConnectionParamDefs(s.ConnectionParams),
		Discovery:        cloneDiscoveryConfig(s.Discovery),
	}
}

type ConnectionSpecProvider interface {
	ConnectionSpec() ConnectionSpec
}

// ManualProvider is a legacy compatibility interface for manual-auth metadata.
// New host-side code should prefer ConnectionSpecProvider.
type ManualProvider interface {
	Provider
	SupportsManualAuth() bool
}

// CatalogProvider is an optional interface that providers can implement to
// expose rich MCP-facing metadata beyond the flat Operation list.
type CatalogProvider interface {
	Catalog() *catalog.Catalog
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
	From        string // "" = user-provided, "token_response" = from OAuth response, "discovery" = from post-connect discovery
	Field       string // JSON field name for token_response extraction
}

// ConnectionParamProvider is a legacy compatibility interface for connection
// parameter metadata. New host-side code should prefer ConnectionSpecProvider.
type ConnectionParamProvider interface {
	ConnectionParamDefs() map[string]ConnectionParamDef
}

// AuthTypeLister is a legacy compatibility interface for auth-type metadata.
// New host-side code should prefer ConnectionSpecProvider.
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
	MetadataMapping map[string]string
}

// DiscoveryConfigProvider is a legacy compatibility interface for post-connect
// discovery metadata. New host-side code should prefer ConnectionSpecProvider.
type DiscoveryConfigProvider interface {
	DiscoveryConfig() *DiscoveryConfig
}

func cloneConnectionParamDefs(src map[string]ConnectionParamDef) map[string]ConnectionParamDef {
	if src == nil {
		return nil
	}
	dst := make(map[string]ConnectionParamDef, len(src))
	for name, def := range src {
		dst[name] = def
	}
	return dst
}

func cloneDiscoveryConfig(src *DiscoveryConfig) *DiscoveryConfig {
	if src == nil {
		return nil
	}
	dst := *src
	if src.MetadataMapping != nil {
		dst.MetadataMapping = make(map[string]string, len(src.MetadataMapping))
		for key, value := range src.MetadataMapping {
			dst.MetadataMapping[key] = value
		}
	}
	return &dst
}

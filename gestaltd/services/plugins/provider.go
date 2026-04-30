package plugins

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type ConnectionMode = core.ConnectionMode

const (
	ConnectionModeNone     = core.ConnectionModeNone
	ConnectionModeUser     = core.ConnectionModeUser
	ConnectionModePlatform = core.ConnectionModePlatform
)

func NormalizeConnectionMode(mode ConnectionMode) ConnectionMode {
	return core.NormalizeConnectionMode(mode)
}

type Provider = core.Provider
type OAuthProvider = core.OAuthProvider
type GraphQLSurfaceInvoker = core.GraphQLSurfaceInvoker
type SessionCatalogProvider = core.SessionCatalogProvider
type PostConnectCapable = core.PostConnectCapable

type OperationResult = core.OperationResult
type GraphQLRequest = core.GraphQLRequest
type TokenResponse = core.TokenResponse
type ConnectionParamDef = core.ConnectionParamDef
type CredentialFieldDef = core.CredentialFieldDef
type DiscoveryCandidate = core.DiscoveryCandidate
type DiscoveryConfig = core.DiscoveryConfig

type OperationConnectionSelector = core.OperationConnectionSelector
type OperationConnectionResolver = core.OperationConnectionResolver
type OperationConnectionOverridePolicy = core.OperationConnectionOverridePolicy

type Registry interface {
	Register(name string, provider Provider) error
	Replace(name string, provider Provider) error
	Remove(name string)
	Get(name string) (Provider, error)
	List() []string
}

type CatalogProvider interface {
	Catalog() *catalog.Catalog
}

type Executor interface {
	Execute(ctx context.Context, operation string, params map[string]any, token string) (*OperationResult, error)
}

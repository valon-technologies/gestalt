package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

// ProviderKind identifies the protocol surface a provider implements.
type ProviderKind string

const (
	ProviderKindIntegration ProviderKind = "integration"
	ProviderKindAuth       ProviderKind = "auth"
	ProviderKindDatastore  ProviderKind = "datastore"
	ProviderKindSecrets    ProviderKind = "secrets"
)

// ProviderMetadata describes a provider instance independent of its concrete
// kind.
type ProviderMetadata struct {
	Kind        ProviderKind
	Name        string
	DisplayName string
	Description string
	Version     string
}

// Provider is the common startup contract shared by every provider kind.
type Provider interface {
	Configure(ctx context.Context, name string, config map[string]any) error
}

// PluginProvider is a deprecated alias for Provider.
type PluginProvider = Provider

// MetadataProvider is implemented by providers that can describe themselves
// without consulting the manifest or host-side configuration.
type MetadataProvider interface {
	Metadata() ProviderMetadata
}

// HealthChecker is implemented by providers that can report their runtime health
// to the host.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// Closer is implemented by providers that need explicit shutdown handling.
type Closer interface {
	Close() error
}

// WarningsProvider is implemented by providers that expose non-fatal startup or
// environment warnings the host should surface.
type WarningsProvider interface {
	Warnings() []string
}

type SessionCatalogProvider interface {
	CatalogForRequest(ctx context.Context, token string) (*proto.Catalog, error)
}

type OperationResult struct {
	Status int
	Body   string
}

type connectionParamsKey struct{}

// WithConnectionParams returns a child context carrying the given connection
// parameters. The host calls this before invoking an executable operation so
// providers can retrieve resolved credentials via [ConnectionParams].
func WithConnectionParams(ctx context.Context, params map[string]string) context.Context {
	return context.WithValue(ctx, connectionParamsKey{}, params)
}

// ConnectionParams extracts the connection parameters stored by
// [WithConnectionParams]. Returns nil if none are present.
func ConnectionParams(ctx context.Context) map[string]string {
	params, _ := ctx.Value(connectionParamsKey{}).(map[string]string)
	return params
}

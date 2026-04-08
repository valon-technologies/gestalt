package gestalt

import "context"

// ProviderKind identifies the protocol surface a provider implements.
type ProviderKind string

const (
	ProviderKindIntegration ProviderKind = "integration"
	ProviderKindAuth        ProviderKind = "auth"
	ProviderKindDatastore   ProviderKind = "datastore"
	ProviderKindSecrets     ProviderKind = "secrets"
	ProviderKindTelemetry   ProviderKind = "telemetry"
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

// RuntimeProvider is the common startup contract shared by every provider kind.
type RuntimeProvider interface {
	Configure(ctx context.Context, name string, config map[string]any) error
}

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

package gestalt

import (
	"context"
	"encoding/json"
)

// ProviderKind identifies the protocol surface a provider implements.
type ProviderKind string

const (
	ProviderKindIntegration ProviderKind = "integration"
	ProviderKindAuth        ProviderKind = "auth"
	ProviderKindDatastore ProviderKind = "datastore"
	ProviderKindSecrets  ProviderKind = "secrets"
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

// PluginProvider is the common startup contract shared by every provider kind.
type PluginProvider interface {
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

type SessionCatalogProvider interface {
	CatalogForRequest(ctx context.Context, token string) (*Catalog, error)
}

type Catalog struct {
	Name        string             `json:"name" yaml:"name"`
	DisplayName string             `json:"displayName" yaml:"display_name,omitempty"`
	Description string             `json:"description" yaml:"description,omitempty"`
	IconSVG     string             `json:"iconSvg,omitempty" yaml:"icon_svg,omitempty"`
	Operations  []CatalogOperation `json:"operations" yaml:"operations"`
}

// CatalogOperation describes a single executable operation exposed by an
// integration provider. Operations are invoked by ID; executable providers do
// not declare HTTP routes.
type CatalogOperation struct {
	ID             string               `json:"id" yaml:"id"`
	Method         string               `json:"method" yaml:"method"`
	Title          string               `json:"title,omitempty" yaml:"title,omitempty"`
	Description    string               `json:"description,omitempty" yaml:"description,omitempty"`
	InputSchema    json.RawMessage      `json:"inputSchema,omitempty" yaml:"-"`
	OutputSchema   json.RawMessage      `json:"outputSchema,omitempty" yaml:"-"`
	Annotations    OperationAnnotations `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	Parameters     []CatalogParameter   `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	RequiredScopes []string             `json:"requiredScopes,omitempty" yaml:"required_scopes,omitempty"`
	Tags           []string             `json:"tags,omitempty" yaml:"tags,omitempty"`
	ReadOnly       bool                 `json:"readOnly,omitempty" yaml:"read_only,omitempty"`
	Visible        *bool                `json:"visible,omitempty" yaml:"visible,omitempty"`
}

type OperationAnnotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty" yaml:"read_only_hint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty" yaml:"idempotent_hint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty" yaml:"destructive_hint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty" yaml:"open_world_hint,omitempty"`
}

type CatalogParameter struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Default     any    `json:"default,omitempty" yaml:"default,omitempty"`
}

// OperationResult holds the response from an executable operation.
// Status is an HTTP-like status code; Body is the response payload, typically JSON.
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

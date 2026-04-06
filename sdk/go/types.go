package gestalt

import (
	"context"
	"encoding/json"
)

// Provider is the core interface that every executable provider plugin must
// implement.
//
// Static metadata still comes from the manifest. Executable helper operations
// are declared separately through the typed router passed to [ServeProvider].
type Provider interface {
	Configure(ctx context.Context, name string, config map[string]any) error
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

// CatalogOperation describes a single executable operation exposed by a provider
// plugin. Operations are invoked by ID; executable plugins do not declare
// HTTP routes.
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

package gestalt

import (
	"context"
	"encoding/json"
)

// ConnectionMode describes how a provider authenticates with its upstream
// service. Static connection behavior belongs in the plugin manifest rather
// than the Go provider interface, but the type remains available for
// compatibility and for session-catalog related code.
type ConnectionMode string

const (
	// ConnectionModeNone means the provider needs no credentials.
	ConnectionModeNone ConnectionMode = "none"
	// ConnectionModeUser requires per-user OAuth tokens supplied by the host.
	ConnectionModeUser ConnectionMode = "user"
	// ConnectionModeIdentity uses a shared service identity configured on the host.
	ConnectionModeIdentity ConnectionMode = "identity"
	// ConnectionModeEither accepts either a user token or a service identity.
	ConnectionModeEither ConnectionMode = "either"
)

// Provider is the core interface that every executable provider plugin must
// implement. Static metadata and the static operation catalog are supplied by
// the plugin manifest; the provider only receives startup configuration and
// executes operations when called by the host.
//
// The token argument to Execute is a user OAuth token supplied by the host
// when the manifest declares a user or either connection mode.
type Provider interface {
	Configure(ctx context.Context, name string, config map[string]any) error
	Execute(ctx context.Context, operation string, params map[string]any, token string) (*OperationResult, error)
}

type SessionCatalogProvider interface {
	CatalogForRequest(ctx context.Context, token string) (*Catalog, error)
}

type Catalog struct {
	Name        string             `json:"name"`
	DisplayName string             `json:"displayName"`
	Description string             `json:"description"`
	IconSVG     string             `json:"iconSvg,omitempty"`
	Operations  []CatalogOperation `json:"operations"`
}

// CatalogOperation describes a single executable operation exposed by a provider
// plugin. Operations are invoked by ID; executable plugins do not declare
// HTTP routes.
type CatalogOperation struct {
	ID             string               `json:"id"`
	Method         string               `json:"method"`
	Title          string               `json:"title,omitempty"`
	Description    string               `json:"description,omitempty"`
	InputSchema    json.RawMessage      `json:"inputSchema,omitempty"`
	OutputSchema   json.RawMessage      `json:"outputSchema,omitempty"`
	Annotations    OperationAnnotations `json:"annotations,omitempty"`
	Parameters     []CatalogParameter   `json:"parameters,omitempty"`
	RequiredScopes []string             `json:"requiredScopes,omitempty"`
	Tags           []string             `json:"tags,omitempty"`
	ReadOnly       bool                 `json:"readOnly,omitempty"`
	Visible        *bool                `json:"visible,omitempty"`
}

type OperationAnnotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

type CatalogParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     any    `json:"default,omitempty"`
}

// OperationResult holds the response from a [Provider.Execute] call.
// Status is an HTTP-like status code; Body is the response payload, typically JSON.
type OperationResult struct {
	Status int
	Body   string
}

// ConnectionParamDef describes a single credential or configuration value that
// a provider needs from the host's connection store. From and Field control
// where the value is sourced (e.g. from an OAuth token field).
type ConnectionParamDef struct {
	Required    bool
	Description string
	Default     string
	From        string
	Field       string
}

// ConnectionParamProvider previously declared static connection parameters from
// code. Static connection metadata now belongs in manifests, but the interface
// remains for compatibility and is ignored by the executable plugin host.
type ConnectionParamProvider interface {
	ConnectionParamDefs() map[string]ConnectionParamDef
}

// ManualAuthProvider is retained for compatibility and is ignored by the
// executable plugin host, which reads auth behavior from manifests.
type ManualAuthProvider interface {
	SupportsManualAuth() bool
}

// AuthTypeLister is retained for compatibility and is ignored by the
// executable plugin host, which reads auth behavior from manifests.
type AuthTypeLister interface {
	AuthTypes() []string
}

type connectionParamsKey struct{}

// WithConnectionParams returns a child context carrying the given connection
// parameters. The host calls this before invoking [Provider.Execute] so that
// providers implementing [ConnectionParamProvider] can retrieve their
// resolved credentials via [ConnectionParams].
func WithConnectionParams(ctx context.Context, params map[string]string) context.Context {
	return context.WithValue(ctx, connectionParamsKey{}, params)
}

// ConnectionParams extracts the connection parameters stored by
// [WithConnectionParams]. Returns nil if none are present.
func ConnectionParams(ctx context.Context) map[string]string {
	params, _ := ctx.Value(connectionParamsKey{}).(map[string]string)
	return params
}

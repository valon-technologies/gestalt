package pluginsdk

import (
	"context"
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

type ProviderStarter interface {
	Start(ctx context.Context, name string, config map[string]any) error
}

type Operation struct {
	Name        string
	Description string
	Method      string
	Parameters  []Parameter
}

type Parameter struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Default     any
}

type OperationResult struct {
	Status int
	Body   string
}

type ConnectionParamDef struct {
	Required    bool
	Description string
	Default     string
	From        string
	Field       string
}

type ConnectionParamProvider interface {
	ConnectionParamDefs() map[string]ConnectionParamDef
}



type ManualAuthProvider interface {
	SupportsManualAuth() bool
}

type AuthTypeLister interface {
	AuthTypes() []string
}

type connectionParamsKey struct{}

func WithConnectionParams(ctx context.Context, params map[string]string) context.Context {
	return context.WithValue(ctx, connectionParamsKey{}, params)
}

func ConnectionParams(ctx context.Context) map[string]string {
	params, _ := ctx.Value(connectionParamsKey{}).(map[string]string)
	return params
}

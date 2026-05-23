package core

import (
	"context"
	"regexp"
)

type connectionParamsKey struct{}

// AppConnectionName is the implicit connection name used when storing
// tokens for plugin-only integrations that do not declare YAML connections.
const AppConnectionName = "_app"

// AppConnectionAlias is the user-facing alias that maps to
// AppConnectionName. In hybrid integrations, mcp.connection can be set
// to "app" to reuse the plugin's OAuth token.
const AppConnectionAlias = "app"

var (
	safeConnectionValue = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	safeInstanceValue   = regexp.MustCompile(`^[a-zA-Z0-9._ -]+$`)
)

// ResolveConnectionAlias maps the user-facing "app" alias to the internal
// AppConnectionName. All other names pass through unchanged.
func ResolveConnectionAlias(name string) string {
	if name == AppConnectionAlias {
		return AppConnectionName
	}
	return name
}

func SafeConnectionValue(value string) bool {
	return safeConnectionValue.MatchString(value)
}

func SafeInstanceValue(value string) bool {
	return safeInstanceValue.MatchString(value)
}

func WithConnectionParams(ctx context.Context, params map[string]string) context.Context {
	return context.WithValue(ctx, connectionParamsKey{}, params)
}

func ConnectionParams(ctx context.Context) map[string]string {
	params, _ := ctx.Value(connectionParamsKey{}).(map[string]string)
	return params
}

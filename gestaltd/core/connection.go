package core

import (
	"context"
	"regexp"
)

type connectionParamsKey struct{}

// PluginConnectionName is the implicit connection name used when storing
// tokens for plugin-only integrations that do not declare YAML connections.
const PluginConnectionName = "_plugin"

// PluginConnectionAlias is the user-facing alias that maps to
// PluginConnectionName. In hybrid integrations, mcp.connection can be set
// to "plugin" to reuse the plugin's OAuth token.
const PluginConnectionAlias = "plugin"

var (
	safeConnectionValue = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	safeInstanceValue   = regexp.MustCompile(`^[a-zA-Z0-9._ -]+$`)
)

// ResolveConnectionAlias maps the user-facing "plugin" alias to the internal
// PluginConnectionName. All other names pass through unchanged.
func ResolveConnectionAlias(name string) string {
	if name == PluginConnectionAlias {
		return PluginConnectionName
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

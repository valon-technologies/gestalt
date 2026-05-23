package gestalt

import (
	"context"
	"strings"
)

const (
	EnvHostServiceSocket        = "GESTALT_HOST_SERVICE_SOCKET"
	EnvHostServiceToken         = "GESTALT_HOST_SERVICE_TOKEN"
	HostServiceBindingMetadata  = "x-gestalt-host-binding"
	hostServiceRelayTokenHeader = "x-gestalt-host-service-relay-token"
)

type hostServiceBindingKey struct{}

func withHostServiceBinding(ctx context.Context, binding string) context.Context {
	binding = strings.TrimSpace(binding)
	if binding == "" {
		return ctx
	}
	return context.WithValue(ctx, hostServiceBindingKey{}, binding)
}

func hostServiceBindingFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(hostServiceBindingKey{}).(string)
	return strings.TrimSpace(value)
}

package runtimehost

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func BindingFromIncomingContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get(HostServiceBindingHeader) {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func ResolveBinding[T any](ctx context.Context, serviceName string, defaultBinding string, bindings map[string]T) (T, error) {
	var zero T
	binding := BindingFromIncomingContext(ctx)
	if binding == "" {
		binding = strings.TrimSpace(defaultBinding)
	}
	if binding == "" {
		return zero, status.Errorf(codes.InvalidArgument, "%s binding is required", serviceName)
	}
	resolved, ok := bindings[binding]
	if !ok {
		return zero, status.Errorf(codes.NotFound, "%s binding %q is not available", serviceName, binding)
	}
	return resolved, nil
}

func (s HostService) AllowsMethod(methodPath string) bool {
	methodPath = strings.TrimSpace(methodPath)
	if methodPath == "" {
		return false
	}
	for _, prefix := range s.MethodPrefixes {
		if strings.HasPrefix(methodPath, strings.TrimSpace(prefix)) {
			return true
		}
	}
	return false
}

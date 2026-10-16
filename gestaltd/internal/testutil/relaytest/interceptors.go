// Package relaytest provides relay-token validation for SDK transport fixtures.
package relaytest

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// RequireRelayTokenUnary checks relay metadata before handling unary requests.
func RequireRelayTokenUnary(expected string) grpc.UnaryServerInterceptor {
	expected = strings.TrimSpace(expected)
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := validateRelayToken(ctx, expected); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// RequireRelayTokenStream checks relay metadata before handling streaming requests.
func RequireRelayTokenStream(expected string) grpc.StreamServerInterceptor {
	expected = strings.TrimSpace(expected)
	return func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := validateRelayToken(stream.Context(), expected); err != nil {
			return err
		}
		return handler(srv, stream)
	}
}

func validateRelayToken(ctx context.Context, expected string) error {
	if expected == "" {
		return nil
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing relay token metadata")
	}
	values := md.Get(runtimehost.HostServiceRelayTokenHeader)
	if len(values) == 0 || strings.TrimSpace(values[0]) != expected {
		return status.Error(codes.Unauthenticated, "invalid relay token metadata")
	}
	return nil
}

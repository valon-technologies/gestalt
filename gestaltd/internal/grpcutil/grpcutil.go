// Package grpcutil provides shared helpers for gRPC transports and request detection.
package grpcutil

import (
	"net/http"
	"strings"

	"google.golang.org/grpc"
)

// InternalMaxReceiveMessageBytes is the maximum unary response size for
// internal app-provider clients. Public gRPC clients keep the default limit.
const InternalMaxReceiveMessageBytes = 16 * 1024 * 1024

// InternalClientDialOption allows internal provider clients to receive large
// operation results without changing public gRPC server or client limits.
func InternalClientDialOption() grpc.DialOption {
	return grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(InternalMaxReceiveMessageBytes))
}

// IsGRPCRequest reports whether r is a gRPC request, identified by the
// Content-Type header prefix "application/grpc". Used by HTTP handlers that
// multiplex gRPC and HTTP/1.1 on the same listener (e.g. the main server's
// publicGRPCMiddleware and the reverse tunnel's dispatch handler).
func IsGRPCRequest(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	return strings.HasPrefix(contentType, "application/grpc")
}

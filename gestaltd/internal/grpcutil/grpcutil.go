// Package grpcutil provides shared helpers for internal gRPC transports.
package grpcutil

import (
	"net/http"
	"strings"

	"google.golang.org/grpc"
)

// InternalMaxReceiveMessageBytes is the short-term maximum response size for
// internal gRPC clients. It gives headroom above the observed Bradley Gmail
// attachment response without allowing full Gmail-sized unary payloads.
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

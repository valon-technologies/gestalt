// Package grpcutil provides shared helpers for gRPC-over-HTTP dispatch.
package grpcutil

import (
	"net/http"
	"strings"
)

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

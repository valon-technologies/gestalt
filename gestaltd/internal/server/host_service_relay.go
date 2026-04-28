package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/grpc/codes"
)

func (s *Server) hostServiceRelayMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.hostServiceRelayToken(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !isGRPCRequest(r) || s.routeProfile == RouteProfileManagement || s.hostServiceRelayTokens == nil {
			next.ServeHTTP(w, r)
			return
		}

		target, err := s.hostServiceRelayTokens.ResolveToken(token)
		if err != nil {
			writeGRPCTrailersOnly(w, codes.Unauthenticated, "invalid-host-service-relay-token")
			return
		}
		if !hostServiceRelayMethodAllowed(r.URL.Path, target.MethodPrefix) {
			writeGRPCTrailersOnly(w, codes.PermissionDenied, "host-service-relay-method-not-allowed")
			return
		}

		handler, err := s.hostServiceHandler(r.Context(), target)
		if err != nil {
			writeGRPCTrailersOnly(w, codes.Unauthenticated, "invalid-host-service-relay-session")
			return
		}
		if handler == nil {
			writeGRPCTrailersOnly(w, codes.Unavailable, "host-service-relay-unavailable")
			return
		}
		relayReq := r.Clone(r.Context())
		relayReq.Header = r.Header.Clone()
		relayReq.Header.Del(providerhost.HostServiceRelayTokenHeader)
		handler.ServeHTTP(w, relayReq)
	})
}

func (s *Server) hostServiceRelayToken(r *http.Request) string {
	if s == nil || r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get(providerhost.HostServiceRelayTokenHeader))
}

func isGRPCRequest(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	return strings.HasPrefix(contentType, "application/grpc")
}

func hostServiceRelayMethodAllowed(path, methodPrefix string) bool {
	methodPrefix = strings.TrimSpace(methodPrefix)
	if methodPrefix == "" {
		return true
	}
	if path == methodPrefix {
		return true
	}
	if strings.HasSuffix(methodPrefix, "/") {
		return strings.HasPrefix(path, methodPrefix)
	}
	return strings.HasPrefix(path, methodPrefix+"/")
}

func writeGRPCTrailersOnly(w http.ResponseWriter, code codes.Code, message string) {
	headers := w.Header()
	headers.Set("Content-Type", "application/grpc")
	headers.Set("Trailer", "Grpc-Status, Grpc-Message")
	headers.Set("Grpc-Status", strconv.Itoa(int(code)))
	if message != "" {
		headers.Set("Grpc-Message", message)
	}
	w.WriteHeader(http.StatusOK)
}

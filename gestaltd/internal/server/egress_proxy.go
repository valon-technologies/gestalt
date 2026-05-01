package server

import (
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/services/egress"
)

const (
	proxyAuthorizationHeader = "Proxy-Authorization"
	egressProxyDialTimeout   = 10 * time.Second
)

func (s *Server) egressProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.egressProxyToken(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		if s.routeProfile == RouteProfileManagement || s.egressProxyTokens == nil || !isEgressProxyRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		target, err := s.egressProxyTokens.ResolveToken(token)
		if err != nil {
			http.Error(w, "invalid egress proxy token", http.StatusProxyAuthRequired)
			return
		}
		host := proxyTargetHost(r)
		if host == "" {
			http.Error(w, "proxy target host is required", http.StatusBadRequest)
			return
		}
		if err := egress.CheckHost(target.AllowedHosts, host, target.DefaultAction); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		newEgressProxyHandler().ServeHTTP(w, r)
	})
}

func (s *Server) egressProxyToken(r *http.Request) string {
	if s == nil || r == nil {
		return ""
	}
	return extractProxyAuthorizationToken(r.Header.Get(proxyAuthorizationHeader))
}

func isEgressProxyRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method == http.MethodConnect {
		return true
	}
	return r.URL != nil && r.URL.IsAbs()
}

func proxyTargetHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	var host string
	switch {
	case r.Method == http.MethodConnect:
		host = strings.TrimSpace(r.Host)
	case r.URL != nil && r.URL.Host != "":
		host = strings.TrimSpace(r.URL.Hostname())
	default:
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func extractProxyAuthorizationToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	if token, ok := strings.CutPrefix(header, "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	if token, ok := strings.CutPrefix(header, "Basic "); ok {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(token))
		if err != nil {
			return ""
		}
		user, pass, found := strings.Cut(string(decoded), ":")
		if found && strings.TrimSpace(pass) != "" {
			return strings.TrimSpace(pass)
		}
		return strings.TrimSpace(user)
	}
	return ""
}

func newEgressProxyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			handleEgressProxyConnect(w, r)
			return
		}
		handleEgressProxyHTTP(w, r)
	})
}

func handleEgressProxyHTTP(w http.ResponseWriter, r *http.Request) {
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header = out.Header.Clone()
	out.Header.Del(proxyAuthorizationHeader)
	if out.URL == nil || !out.URL.IsAbs() {
		http.Error(w, "proxy target URL is required", http.StatusBadRequest)
		return
	}
	out.Host = out.URL.Host

	resp, err := transport.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func handleEgressProxyConnect(w http.ResponseWriter, r *http.Request) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	targetAddr := strings.TrimSpace(r.Host)
	if targetAddr == "" {
		http.Error(w, "proxy target address is required", http.StatusBadRequest)
		return
	}
	if _, _, err := net.SplitHostPort(targetAddr); err != nil {
		targetAddr = net.JoinHostPort(targetAddr, "443")
	}

	var dialer net.Dialer
	targetConn, err := dialer.DialContext(r.Context(), "tcp", targetAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	clientConn, clientRW, err := hijacker.Hijack()
	if err != nil {
		_ = targetConn.Close()
		return
	}
	if _, err := clientRW.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = clientConn.Close()
		_ = targetConn.Close()
		return
	}
	if err := clientRW.Flush(); err != nil {
		_ = clientConn.Close()
		_ = targetConn.Close()
		return
	}

	deadline := time.Now().Add(10 * time.Minute)
	_ = clientConn.SetDeadline(deadline)
	_ = targetConn.SetDeadline(deadline)

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(targetConn, clientRW)
		closeWrite(targetConn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(clientConn, targetConn)
		closeWrite(clientConn)
		done <- struct{}{}
	}()
	<-done
	<-done
	_ = clientConn.Close()
	_ = targetConn.Close()
}

func closeWrite(c net.Conn) {
	if closeWriter, ok := c.(interface{ CloseWrite() error }); ok {
		_ = closeWriter.CloseWrite()
	}
}
